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

//go:build unit

package client

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	upload "github.com/eclipse-kanto/file-upload/client"
	"github.com/eclipse-kanto/file-upload/uploaders"
	"github.com/eclipse/ditto-clients-golang/protocol"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	groupID        = "testGroupID"
	deviceID       = "testDeviceID"
	featureID      = "TestBackup"
	featureType    = "test_files"
	featureContext = "test"

	twin = "twin"
	live = "live"

	modify  = "modify"
	request = "request"

	properties = "properties"

	filePath = "file.path"

	nonexistingDir = "nonexisting-directory"

	serverCert = "testdata/cert.pem"
	serverKey  = "testdata/key.pem"
)

var (
	basedir string
	testCfg *BackupAndRestoreConfig
)

func setUp() {
	var err error

	basedir, err = os.MkdirTemp(".", "testdir")
	if err != nil {
		log.Fatalln(err)
	}

	basedir, err = filepath.Abs(basedir)
	if err != nil {
		log.Fatalln(err)
	}

	testCfg = &BackupAndRestoreConfig{}
	testCfg.UploadableConfig = upload.UploadableConfig{}

	testCfg.Mode = upload.ModeStrict
	testCfg.UploadableConfig.FeatureID = featureID
	testCfg.UploadableConfig.Type = featureType
	testCfg.UploadableConfig.Context = featureContext
	testCfg.ServerCert = serverCert

	testCfg.Storage = filepath.Join(basedir, "storage")

	testCfg.Period = upload.Duration(10 * time.Hour)
}

func tearDown() {
	removeTestDir(basedir)

	testCfg = nil
}

func TestPeriodicBackup(t *testing.T) {
	setUp()
	defer tearDown()

	testCfg.Period = upload.Duration(2 * time.Second)
	duration := 6 * time.Second
	till := time.Now().Add(duration)
	testCfg.ActiveTill = upload.Xtime{Time: &till}

	output := configureTestCommands(t)
	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	time.Sleep(time.Now().Sub(till))

	count := int(duration/time.Duration(testCfg.Period)) - 1
	for i := 0; i < count; i++ {
		checkBackupState(t, client, BackupStarted)
		checkBackupState(t, client, BackupFinished)

		client.liveMsg(t, request)
	}

	lines, err := readLines(output)
	assertNoErr(t, err)

	if len(lines) < count {
		t.Fatalf("expected at least %d lines in %s, but were %d", count, output, len(lines))
	}

	for i := 0; i < count; i++ {
		assertEqual(t, "output line", "backup", lines[i])
	}
}

func TestCommands(t *testing.T) {
	setUp()
	defer tearDown()

	output := configureTestCommands(t)
	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	file := runBackupOperation(t, b, client, nil)

	server := startDownloadServer(t, file, false)
	defer server.Close()

	runRestoreOperation(t, b, client, server.URL, nil, true)

	lines, err := readLines(output)
	assertNoErr(t, err)

	expected := []string{"backup", "restore"}
	assertEqual(t, "output file content", expected, lines)
}

func TestBackupAndRestore(t *testing.T) {
	setUp()
	defer tearDown()

	backupDir, _, err := createBackupDir(basedir)
	assertNoErr(t, err)
	testCfg.Dir = backupDir

	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	testBackupAndRestoreWithOptions(t, b, client, backupDir, nil)
}

func TestInsecureRestore(t *testing.T) {
	setUp()
	defer tearDown()

	backupDir, _, err := createBackupDir(basedir)
	assertNoErr(t, err)
	testCfg.Dir = backupDir

	archive, err := pickFilePath(basedir, "test", "bck")
	assertNoErr(t, err)

	err = zipDir(backupDir, archive)
	assertNoErr(t, err)

	server := startDownloadServer(t, archive, true)
	defer server.Close()

	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	runRestoreOperation(t, b, client, server.URL, nil, false)
}

func TestModeStrict(t *testing.T) {
	testAccessMode(t, upload.ModeStrict)
}

func TestModeScoped(t *testing.T) {
	testAccessMode(t, upload.ModeScoped)
}

func TestModeLax(t *testing.T) {
	testAccessMode(t, upload.ModeLax)
}

func testAccessMode(t *testing.T, mode upload.AccessMode) {
	setUp()
	defer tearDown()

	backupDir, subDir, err := createBackupDir(basedir)
	assertNoErr(t, err)

	testCfg.Dir = backupDir

	outsideDir, err := os.MkdirTemp(basedir, "outside")
	assertNoErr(t, err)

	testCfg.Mode = mode

	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	var allowed, forbidden []string
	switch mode {
	case upload.ModeStrict:
		allowed = []string{backupDir}
		forbidden = []string{subDir, outsideDir}
	case upload.ModeScoped:
		allowed = []string{backupDir, subDir}
		forbidden = []string{outsideDir}
	case upload.ModeLax:
		allowed = []string{backupDir, subDir, outsideDir}
		forbidden = []string{}
	default:
		t.Fatalf("unexpected access mode value: %v", mode)
	}

	for _, dir := range allowed {
		testBackupAndRestoreWithOptions(t, b, client, dir, nil)
	}

	for _, dir := range forbidden {
		assertDirAccessForbidden(t, b, dir)
	}
}

func assertDirAccessForbidden(t *testing.T, b *BackupAndRestore, dir string) {
	assertDirBackupError(t, b, dir, fmt.Sprintf(ErrAccessForbidden, dir, testCfg.Mode))
	assertDirRestoreError(t, b, dir, fmt.Sprintf(ErrAccessForbidden, dir, testCfg.Mode))
}

func TestBackupErrors(t *testing.T) {
	setUp()
	defer tearDown()

	testCfg.Mode = upload.ModeLax

	b, _ := newConnectedTestBackup(t)
	defer b.Disconnect()

	//no backup dir
	assertDirBackupError(t, b, "", ErrNoBackupDir)

	//backup dir not found
	assertDirBackupError(t, b, nonexistingDir, fmt.Sprintf(ErrBackupDirNotFound, nonexistingDir))
}

func TestRestoreErrors(t *testing.T) {
	setUp()
	defer tearDown()

	testCfg.Mode = upload.ModeLax

	b, _ := newConnectedTestBackup(t)
	defer b.Disconnect()

	//no download URL
	payload := getBackupRestorePayload(t, nil)
	resp := b.HandleOperation("restore", payload)
	assertResponse(t, resp, http.StatusBadRequest, ErrNoDownloadURL)

	//no backup dir
	assertDirRestoreError(t, b, "", ErrNoBackupDir)

	//backup dir not found
	assertDirRestoreError(t, b, nonexistingDir, fmt.Sprintf(ErrBackupDirNotFound, nonexistingDir))
}

func TestTrigger(t *testing.T) {
	setUp()
	defer tearDown()

	backupDir, _, err := createBackupDir(basedir)
	assertNoErr(t, err)
	testCfg.Dir = backupDir

	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	files := make([]string, 3)
	for i := range files {
		files[i] = runBackupOperation(t, b, client, nil)
	}

	err = b.DoTrigger("testCorrelationID", nil)
	assertNoErr(t, err)

	for i := 0; i < len(files); i++ {
		v := client.liveMsg(t, request)
		file := getFileFromMsg(t, v)

		assertStrIn(t, file, files)
	}
}

func TestDynamicDir(t *testing.T) {
	setUp()
	defer tearDown()

	testCfg.Mode = upload.ModeLax

	backupDir, _, err := createBackupDir(basedir)
	assertNoErr(t, err)

	b, client := newConnectedTestBackup(t)
	defer b.Disconnect()

	options := map[string]string{BackupDirProperty: backupDir}
	testBackupAndRestoreWithOptions(t, b, client, backupDir, options)
}

func assertDirBackupError(t *testing.T, b *BackupAndRestore, dir string, errorMsg string) {
	options := map[string]string{BackupDirProperty: dir}
	payload := getBackupRestorePayload(t, options)
	resp := b.HandleOperation("backup", payload)

	assertResponse(t, resp, http.StatusBadRequest, errorMsg)
}

func assertDirRestoreError(t *testing.T, b *BackupAndRestore, dir string, errorMsg string) {
	options := map[string]string{uploaders.URLProp: "http://localhost/fake", BackupDirProperty: dir}

	payload := getBackupRestorePayload(t, options)
	resp := b.HandleOperation(OperationRestore, payload)
	assertResponse(t, resp, http.StatusBadRequest, errorMsg)
}

func testBackupAndRestoreWithOptions(t *testing.T, b *BackupAndRestore, client *mockedClient,
	backupDir string, options map[string]string) {

	t.Helper()

	file := runBackupOperation(t, b, client, options)

	backupCopy := filepath.Join(basedir, "backupCopy")
	err := os.Rename(backupDir, backupCopy)
	assertNoErr(t, err)

	defer removeTestDir(backupCopy)

	err = os.Mkdir(backupDir, 0700)
	assertNoErr(t, err)

	server := startDownloadServer(t, file, false)
	defer server.Close()

	runRestoreOperation(t, b, client, server.URL, options, true)

	compareDirs(t, backupCopy, backupDir)
}

func runBackupOperation(t *testing.T, b *BackupAndRestore, client *mockedClient, options map[string]string) string {
	t.Helper()

	payload := getBackupRestorePayload(t, options)
	resp := b.HandleOperation(OperationBackup, payload)
	assertResponseOk(t, resp)

	checkBackupState(t, client, BackupStarted)
	checkBackupState(t, client, BackupFinished)

	v := client.liveMsg(t, request)
	file := getFileFromMsg(t, v)

	if file == "" {
		t.Fatalf("expected file path in options, but none found")
	}

	return file
}

func getFileFromMsg(t *testing.T, v map[string]interface{}) string {
	requestOptions, ok := v["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected 'options' type: %T", v["options"])
	}

	file := requestOptions[filePath].(string)

	return file
}

func runRestoreOperation(t *testing.T, b *BackupAndRestore, client *mockedClient, url string,
	options map[string]string, expectSuccess bool) {

	if options == nil {
		options = make(map[string]string)
	}
	options[uploaders.URLProp] = url

	payload := getBackupRestorePayload(t, options)

	resp := b.HandleOperation("restore", payload)
	assertResponseOk(t, resp)

	checkBackupState(t, client, RestoreStarted)

	if expectSuccess {
		checkBackupState(t, client, RestoreFinished)
	} else {
		checkBackupState(t, client, RestoreFailed)
	}

}

func getBackupRestorePayload(t *testing.T, options map[string]string) []byte {
	t.Helper()

	params := backupRestoreParams{CorrelationID: "testCorrelationID"}
	params.Options = options

	payload, err := json.Marshal(params)
	assertNoErr(t, err)

	return payload
}

func configureTestCommands(t *testing.T) string {
	output := filepath.Join(basedir, "out.txt")
	backupScript, err := createTestScript(basedir, "backup", fmt.Sprintf("echo %s>>%s", "backup", output))
	assertNoErr(t, err)
	testCfg.BackupCmd.Set(backupScript)

	restoreScript, err := createTestScript(basedir, "restore", fmt.Sprintf("echo %s>>%s", "restore", output))
	assertNoErr(t, err)
	testCfg.RestoreCmd.Set(restoreScript)

	return output
}

func newConnectedTestBackup(t *testing.T) (*BackupAndRestore, *mockedClient) {
	testCfg.Validate()

	client := newMockedClient()
	edgeCfg := &upload.EdgeConfiguration{DeviceID: groupID + ":" + deviceID, TenantID: "testTenantID", PolicyID: "testPolicyID"}

	b, err := NewBackupAndRestore(testCfg)
	assertNoErr(t, err)

	b.Connect(client, edgeCfg)

	v := client.twinMsg(t, modify)
	checkFeatureProperties(t, v)

	return b, client
}

func readLines(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines := make([]string, 0, 5)
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}

	return lines, nil
}

func startDownloadServer(t *testing.T, file string, insecure bool) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := ioutil.ReadFile(file)
		assertNoErr(t, err)

		_, err = w.Write(b)
		assertNoErr(t, err)
	}))

	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	assertNoErr(t, err)

	server.TLS = &tls.Config{}
	server.TLS.Certificates = []tls.Certificate{cert}

	if insecure {
		server.TLS.MaxVersion = tls.VersionTLS12
		server.TLS.CipherSuites = insecureCipherSuites()
	}

	server.StartTLS()

	server.URL = strings.ReplaceAll(server.URL, "127.0.0.1", "localhost")

	return server
}

func insecureCipherSuites() []uint16 {
	cs := tls.InsecureCipherSuites()
	cid := make([]uint16, len(cs))
	for i := range cs {
		cid[i] = cs[i].ID
	}

	return cid
}

func checkBackupState(t *testing.T, client *mockedClient, expected string) {
	t.Helper()

	value := client.twinMsg(t, modify)

	state := value["state"]
	if expected != state {
		t.Logf("payload %+v\n", value)
	}
	assertEqual(t, "backup state", expected, state)
}

func checkFeatureProperties(t *testing.T, value map[string]interface{}) {
	t.Helper()

	props := value[properties].(map[string]interface{})
	assertEqual(t, "feature type", featureType, props["type"])
	assertEqual(t, "feature context", featureContext, props["context"])
}

func assertResponseOk(t *testing.T, resp *upload.ErrorResponse) {
	t.Helper()

	if resp != nil {
		t.Fatalf("unexpected operation response - status: %d, message: %s", resp.Status, resp.Message)
	}
}

func assertResponse(t *testing.T, resp *upload.ErrorResponse, status int, msg string) {
	t.Helper()

	if resp == nil {
		t.Fatalf("expected error response, but there was none")
	} else if resp.Status != status || resp.Message != msg {
		t.Fatalf("expected error response [status: %d, message: %s], but was [status: %d, message: %s]",
			status, msg, resp.Status, resp.Message)
	}
}

func assertState(t *testing.T, expected string, status *backupAndRestoreStatus) {
	t.Helper()

	if status.State != expected {
		t.Errorf("expected state %s, but was %s", expected, status.State)
	}
}

func assertEqual(t *testing.T, name string, expected interface{}, actual interface{}) {
	t.Helper()

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("%s expected to be '%s', but was '%s'", name, expected, actual)
	}
}

func assertStrIn(t *testing.T, value string, list []string) {
	t.Helper()

	for _, v := range list {
		if value == v {
			return
		}
	}

	t.Errorf("value %v not found in list: %v", value, list)
}

// mockedToken represents mocked mqtt.Token interface used for testing.
type mockedClient struct {
	err  error
	twin chan *protocol.Envelope
	live chan *protocol.Envelope
	mu   sync.Mutex
}

func newMockedClient() *mockedClient {
	client := &mockedClient{}

	client.twin = make(chan *protocol.Envelope, 10)
	client.live = make(chan *protocol.Envelope, 10)

	return client
}

func (client *mockedClient) twinMsg(t *testing.T, action string) map[string]interface{} {
	t.Helper()

	return client.msg(t, twin, modify)
}

func (client *mockedClient) liveMsg(t *testing.T, action string) map[string]interface{} {
	t.Helper()

	return client.msg(t, live, action)
}

// value returns last payload value or waits 10sec for new payload.
func (client *mockedClient) msg(t *testing.T, channel string, action string) map[string]interface{} {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()

	ch := client.getChannel(channel)
	select {
	case env := <-ch:
		assertEqual(t, "group ID", groupID, env.Topic.Namespace)
		assertEqual(t, "device ID", deviceID, env.Topic.EntityName)
		assertEqual(t, "action", action, string(env.Topic.Action))

		// Valdidate its starting path.
		prefix := "/features/" + featureID
		if !strings.HasPrefix(env.Path, prefix) {
			t.Fatalf("message path do not starts with [%v]: %v", prefix, env.Path)
		}
		// Return its the value.

		m, ok := env.Value.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected payload type: %T", m)
		}
		return m
	case <-time.After(5 * time.Second):
		// Fail after the timeout.
		t.Fatal("failed to retrieve published data")
	}
	return nil
}

func (client *mockedClient) getChannel(channel string) chan *protocol.Envelope {
	if channel == twin {
		return client.twin
	} else if channel == live {
		return client.live
	}

	log.Fatalf("unknown channel: %s", channel)
	return nil
}

// IsConnected returns true.
func (client *mockedClient) IsConnected() bool {
	return true
}

// IsConnectionOpen returns true.
func (client *mockedClient) IsConnectionOpen() bool {
	return true
}

// Connect returns finished token.
func (client *mockedClient) Connect() mqtt.Token {
	return &mockedToken{err: client.err}
}

// Disconnect do nothing.
func (client *mockedClient) Disconnect(quiesce uint) {
	// Do nothing.
}

// Publish returns finished token and set client topic and payload.
func (client *mockedClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	env := &protocol.Envelope{}
	if err := json.Unmarshal(payload.([]byte), env); err != nil {
		log.Fatalf("unexpected error during data unmarshal: %v", err)
	}

	if env.Topic.Channel == live {
		client.live <- env
	} else if env.Topic.Channel == twin {
		client.twin <- env
	} else {
		log.Fatalf("unexpected message topic: %v", env.Topic)
	}

	return &mockedToken{err: client.err}
}

// Subscribe returns finished token.
func (client *mockedClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	return &mockedToken{err: client.err}
}

// SubscribeMultiple returns finished token.
func (client *mockedClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	return &mockedToken{err: client.err}
}

// Unsubscribe returns finished token.
func (client *mockedClient) Unsubscribe(topics ...string) mqtt.Token {
	return &mockedToken{err: client.err}
}

// AddRoute do nothing.
func (client *mockedClient) AddRoute(topic string, callback mqtt.MessageHandler) {
	// Do nothing.
}

// OptionsReader returns an empty struct.
func (client *mockedClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

// mockedToken represents mocked mqtt.Token interface used for testing.
type mockedToken struct {
	err error
}

// Wait returns immediately with true.
func (token *mockedToken) Wait() bool {
	return true
}

// WaitTimeout returns immediately with true.
func (token *mockedToken) WaitTimeout(time.Duration) bool {
	return true
}

// Done returns immediately with nil channel.
func (token *mockedToken) Done() <-chan struct{} {
	return nil
}

// Error returns the error if set.
func (token *mockedToken) Error() error {
	return token.err
}
