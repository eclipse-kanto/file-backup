// Copyright (c) 2022 Contributors to the Eclipse Foundation
//
// See the NOTICE file(s) distributed with this work for additional
// information regarding copyright ownership.
//
// This program and the accompanying materials are made available under the
// terms of the Eclipse Public License 2.0 which is available at
// https://www.eclipse.org/legal/epl-2.0, or the Apache License, Version 2.0
// which is available at https://www.apache.org/licenses/LICENSE-2.0.
//
// SPDX-License-Identifier: EPL-2.0 OR Apache-2.0

package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	upload "github.com/eclipse-kanto/file-upload/client"
	"github.com/eclipse-kanto/file-upload/logger"
	"github.com/eclipse-kanto/file-upload/uploaders"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// BackupAndRestoreConfig contains configuration for BackupAndRestore feature
type BackupAndRestoreConfig struct {
	upload.UploadableConfig

	Dir     string `json:"dir,omitempty" descr:"Directory to be backed up"`
	Storage string `json:"storage,omitempty" def:"./storage" descr:"Directory where backups and downloads will be stored"`

	backupsDir   string
	downloadsDir string

	BackupCmd  command `json:"backupCmd,omitempty" descr:"Command to be executed before backup is done.\nThe command is executed in temporary directory in which it must collect all files needed for the backup."`
	RestoreCmd command `json:"restoreCmd,omitempty" descr:"Command to be executed after restore.\nThe command is executed in temporary directory, where the backup archive is extracted."`

	KeepUploaded bool `json:"keepUploaded,omitempty" def:"false" descr:"Keep locally successfully uploaded backups"`

	Mode upload.AccessMode `json:"mode,omitempty" def:"strict" descr:"{mode}"`
}

// BackupAndRestore feature implementation
type BackupAndRestore struct {
	cfg *BackupAndRestoreConfig

	uploadable *upload.AutoUploadable

	status       *backupAndRestoreStatus
	statusEvents *upload.StatusEventsConsumer

	uidCounter int64
	mutex      sync.Mutex

	httpClient *http.Client
}

type backupAndRestoreStatus upload.UploadStatus

const (
	// BackupDirProperty represents a constant for backup directory flag
	BackupDirProperty = "backup.dir"

	backupStarted  = "BACKUP_STARTED"
	backupFinished = "BACKUP_FINISHED"
	backupFailed   = "BACKUP_FAILED"

	restoreStarted  = "RESTORE_STARTED"
	restoreFinished = "RESTORE_FINISHED"
	restoreFailed   = "RESTORE_FAILED"

	lastOperationProperty = "lastOperation"
)

// Error messages constants
const (
	ErrNoDownloadURL     = "Download URL not specified!"
	ErrNoBackupDir       = "backup directory not specified"
	ErrBackupDirNotFound = "backup directory '%s' not found"
	ErrAccessForbidden   = "accessing directory '%s' is not permitted under mode '%v'"
)

// Validate checks if backup and restore feature configuration is valid
func (cfg *BackupAndRestoreConfig) Validate() {
	cfg.UploadableConfig.Validate()

	if cfg.Dir == "" && cfg.Mode != upload.ModeLax && !cfg.BackupCmd.defined() && !cfg.RestoreCmd.defined() {
		log.Fatalln("Neither backup directory nor backup/restore commands are specified!" +
			"\nTo permit backup/restore of any directory set 'mode' property to 'lax'.")
	}

	if cfg.Dir != "" {
		if !pathExisting(cfg.Dir) {
			log.Printf("Directory '%s' not found!\n", cfg.Dir)
		}
	}

	cfg.UploadableConfig.Delete = !cfg.KeepUploaded

	cfg.backupsDir = path.Join(cfg.Storage, "/backups")
	cfg.downloadsDir = path.Join(cfg.Storage, "/downloads")

	if err := os.MkdirAll(cfg.backupsDir, 0755); err != nil && !os.IsExist(err) {
		log.Fatalf("Failed to create backups directory '%s': %v\n", cfg.backupsDir, err)
	}

	if err := os.MkdirAll(cfg.downloadsDir, 0755); err != nil && !os.IsExist(err) {
		log.Fatalf("Failed to create downloads directory '%s': %v\n", cfg.downloadsDir, err)
	}
}

// NewBackupAndRestore constructs backup and restore feature from the provided configuration
func NewBackupAndRestore(backupCfg *BackupAndRestoreConfig) (*BackupAndRestore, error) {
	result := &BackupAndRestore{}

	result.cfg = backupCfg

	definitions := []string{"com.bosch.iot.suite.manager.backup:BackupAndRestore:1.0.0",
		"com.bosch.iot.suite.manager.upload:Uploadable:1.0.0"}

	uploadable, err := upload.NewAutoUploadable(&backupCfg.UploadableConfig, result, definitions...)

	if err != nil {
		return nil, err
	}

	result.uploadable = uploadable
	result.statusEvents = upload.NewStatusEventsConsumer(100)

	result.httpClient, err = newHTTPClient(backupCfg.ServerCert)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// Connect connects backup and restore to the ditto endpoint
func (br *BackupAndRestore) Connect(mqttClient MQTT.Client, edgeCfg *upload.EdgeConfiguration) {
	br.uploadable.Connect(mqttClient, edgeCfg)

	br.statusEvents.Start(func(e interface{}) {
		br.uploadable.UpdateProperty(lastOperationProperty, e)
	})
}

// Disconnect disconnects backup and restore from the ditto endpoint
func (br *BackupAndRestore) Disconnect() {
	br.statusEvents.Stop()

	br.uploadable.Disconnect()
}

// ******* UploadCustomizer methods *******//

// DoTrigger triggers a backup operation, sending a file upload request
func (br *BackupAndRestore) DoTrigger(correlationID string, options map[string]string) error {
	entries, err := os.ReadDir(br.cfg.backupsDir)

	if err != nil {
		logger.Errorf("failed to trigger upload %s: %v", correlationID, err)

		return err
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			path := path.Join(br.cfg.backupsDir, e.Name())
			files = append(files, path)
		}
	}

	br.uploadable.UploadFiles(correlationID, files, options)

	return nil
}

// HandleOperation invokes an operation, using the provided payload data
func (br *BackupAndRestore) HandleOperation(operation string, payload []byte) *upload.ErrorResponse {
	switch operation {
	case "backup":
		return br.backup(payload)
	case "restore":
		return br.restore(payload)
	default:
		msg := fmt.Sprintf("Unsupported operation '%s'!", operation)
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: msg,
			ErrorCode: upload.ErrorCodeParameterInvalid}
	}
}

// OnTick periodically invokes an operation on backup and restore feature
func (br *BackupAndRestore) OnTick() {
	status, resp := br.startOperation(br.nextUID(), backupStarted)

	if resp != nil {
		logger.Errorf("periodic backup failed: %v", resp.Message)
	} else {
		go br.doBackup(status, nil, nil)
	}
}

// ******* END UploadCustomizer methods *******//

type backupRestoreParams struct {
	CorrelationID string            `json:"correlationId"`
	Providers     []string          `json:"providers"`
	Options       map[string]string `json:"options"`
}

func (br *BackupAndRestore) backup(payload []byte) *upload.ErrorResponse {
	params := &backupRestoreParams{}

	err := json.Unmarshal(payload, params)
	if err != nil {
		msg := fmt.Sprintf("invalid 'backup' operation parameters: %v", string(payload))
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: msg,
			ErrorCode: upload.ErrorCodeParameterInvalid}
	}

	logger.Infof("backup called: %+v", params)

	if err := br.checkBackupDir(params.Options); err != nil {
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: err.Error(),
			ErrorCode: upload.ErrorCodeExecutionFailed}
	}

	status, resp := br.startOperation(params.CorrelationID, backupStarted)

	if resp == nil {
		go br.doBackup(status, params.Providers, params.Options)
	}

	return resp
}

func (br *BackupAndRestore) restore(payload []byte) *upload.ErrorResponse {
	params := &backupRestoreParams{}

	err := json.Unmarshal(payload, params)
	if err != nil {
		msg := fmt.Sprintf("invalid 'restore' operation parameters: %v", string(payload))
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: msg,
			ErrorCode: upload.ErrorCodeParameterInvalid}
	}

	logger.Infof("restore called: %+v", params)

	if params.Options[uploaders.URLProp] == "" {
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: ErrNoDownloadURL,
			ErrorCode: upload.ErrorCodeParameterInvalid}
	}

	if err := br.checkBackupDir(params.Options); err != nil {
		return &upload.ErrorResponse{Status: http.StatusBadRequest, Message: err.Error(),
			ErrorCode: upload.ErrorCodeExecutionFailed}
	}

	status, resp := br.startOperation(params.CorrelationID, restoreStarted)

	if resp == nil {
		go br.doRestore(status, params.Providers, params.Options)
	}

	return nil
}

func (br *BackupAndRestore) checkBackupDir(options map[string]string) error {
	dir := options[BackupDirProperty]
	if dir == "" && br.cfg.Dir == "" && !br.cfg.BackupCmd.defined() && !br.cfg.RestoreCmd.defined() {
		return errors.New(ErrNoBackupDir)
	}

	if dir != "" {
		if !pathExisting(dir) {
			return fmt.Errorf(ErrBackupDirNotFound, dir)
		}

		if permitted, err := br.isAccessPermitted(dir); err != nil {
			return err
		} else if !permitted {
			return fmt.Errorf(ErrAccessForbidden, dir, br.cfg.Mode)
		}
	}

	return nil
}

func (br *BackupAndRestore) isAccessPermitted(dir string) (bool, error) {
	if br.cfg.Mode == upload.ModeLax {
		return true, nil
	}

	var dynamic, static string
	var err error

	if dynamic, err = filepath.Abs(dir); err != nil {
		return false, err
	}

	if static, err = filepath.Abs(br.cfg.Dir); err != nil {
		return false, err
	}

	if br.cfg.Mode == upload.ModeStrict {
		return dynamic == static, nil
	}

	if br.cfg.Mode == upload.ModeScoped {
		return isSubDir(dynamic, static), nil
	}

	logger.Errorf("unexpected access mode value: %v", br.cfg.Mode)

	return false, err
}

// dir and sub must be absolute paths
func isSubDir(sub string, dir string) bool {
	last := ""
	for p := sub; p != last; p = filepath.Dir(last) {
		if dir == p {
			return true
		}

		last = p
	}

	return false
}

func (br *BackupAndRestore) doBackup(status *backupAndRestoreStatus, providers []string, options map[string]string) {
	cmd := br.cfg.BackupCmd
	dir := br.cfg.Dir

	newDir, ok := options[BackupDirProperty]
	if ok {
		dir = newDir
	}

	temp := false
	var err error
	if cmd.defined() {
		dir, err = os.MkdirTemp(br.cfg.Storage, "backup")

		if err == nil {
			temp = true

			err = cmd.execute(dir)
		}
	}

	path := ""
	if err == nil {
		path, err = pickFilePath(br.cfg.backupsDir, "backup", "zip")
	}

	if err == nil {
		err = zipDir(dir, path)
	}

	if temp {
		err := os.RemoveAll(dir)

		if err != nil {
			logger.Warnf("failed to delete temp directory %s: %v", dir, err)
		}
	}

	br.mutex.Lock()
	status.EndTime = time.Now()
	if err != nil {
		status.State = backupFailed
		status.Message = err.Error()
	} else {
		status.Progress = 100
		status.State = backupFinished
	}

	br.updateStatus(status)
	br.mutex.Unlock()

	if err == nil {
		files := []string{path}
		br.uploadable.UploadFiles(status.CorrelationID, files, options)
	}
}

func (br *BackupAndRestore) doRestore(status *backupAndRestoreStatus, providers []string, options map[string]string) {
	backupFile, err := pickFilePath(br.cfg.downloadsDir, "backup", "zip")

	if err == nil {
		downloadURL := options[uploaders.URLProp]
		headers := uploaders.ExtractDictionary(options, uploaders.HeadersPrefix)
		err = br.downloadFile(downloadURL, headers, backupFile)
	}

	if err == nil {
		dir := br.cfg.Dir
		if newDir, ok := options[BackupDirProperty]; ok {
			dir = newDir
		}

		err = br.restoreFromFile(backupFile, dir)
	}

	if err == nil {
		err := os.Remove(backupFile)

		if err != nil {
			logger.Warnf("failed to delete downloaded file %s: %v", backupFile, err)
		}
	}

	br.mutex.Lock()
	status.EndTime = time.Now()
	if err != nil {
		status.State = restoreFailed
		status.Message = err.Error()
	} else {
		status.Progress = 100
		status.State = restoreFinished
	}

	br.updateStatus(status)
	br.mutex.Unlock()
}

func (br *BackupAndRestore) restoreFromFile(backupFile string, dir string) error {
	cmd := br.cfg.RestoreCmd
	temp := false

	if cmd.defined() {
		var err error
		dir, err = os.MkdirTemp(br.cfg.Storage, "restore")

		if err != nil {
			return err
		}

		temp = true
	}

	if err := unzipDir(backupFile, dir); err != nil {
		return err
	}

	err := cmd.execute(dir)

	if temp {
		err := os.RemoveAll(dir)

		if err != nil {
			logger.Warnf("failed to delete temp directory %s: %v", dir, err)
		}
	}

	return err
}

func (br *BackupAndRestore) startOperation(correlationID string, newState string) (*backupAndRestoreStatus, *upload.ErrorResponse) {
	br.mutex.Lock()
	defer br.mutex.Unlock()

	if br.status != nil {
		if br.status.State == backupStarted {
			return nil, &upload.ErrorResponse{Status: http.StatusConflict,
				Message: "Backup operation is currently in progress!", ErrorCode: upload.ErrorCodeExecutionFailed}
		} else if br.status.State == restoreStarted {
			return nil, &upload.ErrorResponse{Status: http.StatusConflict,
				Message: "Restore operation is currently in progress!", ErrorCode: upload.ErrorCodeExecutionFailed}
		}
	}

	if correlationID == "" {
		correlationID = br.nextUID()
	}

	br.status = &backupAndRestoreStatus{}
	br.status.CorrelationID = correlationID
	br.status.StartTime = time.Now()
	br.status.State = newState

	br.updateStatus(br.status)

	return br.status, nil
}

func (br *BackupAndRestore) updateStatus(status *backupAndRestoreStatus) {
	s := *status

	br.statusEvents.Add(s)
}

func (br *BackupAndRestore) nextUID() string {
	c := atomic.AddInt64(&br.uidCounter, 1)

	return fmt.Sprintf("backup_restore-id-%d", c)
}

func (br *BackupAndRestore) downloadFile(url string, headers map[string]string, filepath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := br.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download failed - code: %d, status: %s", resp.StatusCode, resp.Status)
	}

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func pickFilePath(dir string, name string, ext string) (string, error) {
	const timestampFormat = "2006-01-02-15.04.05.000"
	timestamp := time.Now().UTC().Format(timestampFormat)

	fullName := fmt.Sprintf("%s-%s.%s", name, timestamp, ext)

	for i := 0; i < 10; i++ {
		path := path.Join(dir, fullName)
		if !pathExisting(path) {
			return path, nil
		}

		fullName = fmt.Sprintf("%s-%s_%02d.%s", name, timestamp, i+1, ext)
	}

	return "", fmt.Errorf("Failed to find free name for %s/%s.%s", dir, name, ext)
}

func newHTTPClient(serverCert string) (*http.Client, error) {
	var caCertPool *x509.CertPool
	if serverCert != "" {
		caCert, err := ioutil.ReadFile(serverCert)
		if err != nil {
			return nil, fmt.Errorf("failed to read server certificate file '%s' - %w", serverCert, err)
		}
		caCertPool = x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
	}

	tlsConfing := &tls.Config{
		InsecureSkipVerify: false,
		RootCAs:            caCertPool,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
		CipherSuites:       supportedCipherSuites(),
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfing}}

	return client, nil
}

func supportedCipherSuites() []uint16 {
	cs := tls.CipherSuites()
	cid := make([]uint16, len(cs))
	for i := range cs {
		cid[i] = cs[i].ID
	}
	return cid
}

func pathExisting(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	return true
}
