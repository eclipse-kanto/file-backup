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

//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caarlos0/env/v6"
	"github.com/eclipse-kanto/file-backup/client"
	fut "github.com/eclipse-kanto/file-upload/integration"
	"github.com/eclipse-kanto/file-upload/uploaders"
	"github.com/eclipse-kanto/kanto/integration/util"
	"github.com/eclipse/ditto-clients-golang/protocol"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/net/websocket"
)

type backupAndRestoreParams struct {
	Options map[string]string `json:"Options"`
}

func newBackupAndRestoreParams(dir string, downloadURL string) (*backupAndRestoreParams, error) {
	absBackupDirPath, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &backupAndRestoreParams{
		Options: map[string]string{
			client.BackupDirProperty: absBackupDirPath,
			uploaders.URLProp:        downloadURL,
		},
	}, nil
}

func (suite *fileBackupSuite) SetupSuite() {
	suite.Setup(suite.T())

	opts := env.Options{RequiredIfNoDef: false}
	suite.backupAndRestoreCfg = backupAndRestoreConfig{}
	require.NoError(suite.T(), env.Parse(&suite.backupAndRestoreCfg, opts), "failed to process backup and restore environment variables")
	suite.T().Logf("backup and restore test configuration - %v", suite.backupAndRestoreCfg)
	suite.AssertEmptyDir(suite.backupAndRestoreCfg.BackupDir)
	suite.AssertEmptyDir(suite.backupAndRestoreCfg.RestoreDir)

	suite.ThingURL = util.GetThingURL(suite.Cfg.DigitalTwinAPIAddress, suite.ThingCfg.DeviceID)
	suite.FeatureURL = util.GetFeatureURL(suite.ThingURL, client.BackupAndRestoreFeatureID)
}

func (suite *fileBackupSuite) TearDownSuite() {
	suite.TearDown()
}

func TestHTTPFileBackup(t *testing.T) {
	suite.Run(t, new(httpFileBackupSuite))
}

func TestAzureFileBackup(t *testing.T) {
	suite.Run(t, new(azureFileBackupSuite))
}

func TestAWSFileBackup(t *testing.T) {
	suite.Run(t, new(awsFileBackupSuite))
}

func (suite *httpFileBackupSuite) TestFileBackup() {
	suite.SetupStorageProvider(fut.GenericStorageProvider)
	defer suite.TearDownStorageProvider()
	suite.testBackupAndRestore()
}

func (suite *azureFileBackupSuite) TestFileBackup() {
	suite.SetupStorageProvider(fut.AzureStorageProvider)
	defer suite.TearDownStorageProvider()
	suite.testBackupAndRestore()
}

func (suite *awsFileBackupSuite) TestFileBackup() {
	suite.SetupStorageProvider(fut.AWSStorageProvider)
	defer suite.TearDownStorageProvider()
	suite.testBackupAndRestore()
}

func (suite *fileBackupSuite) testBackupAndRestore() {
	backupDir := suite.backupAndRestoreCfg.BackupDir
	restoreDir := suite.backupAndRestoreCfg.RestoreDir
	defer suite.RemoveFilesSilently(backupDir)
	defer suite.RemoveFilesSilently(restoreDir)

	_, err := fut.CreateTestFiles(backupDir, backupFilesCount)
	require.NoError(suite.T(), err, "creating test files failed")

	params, err := newBackupAndRestoreParams(backupDir, "")
	require.NoError(suite.T(), err, msgFailedInitializeBackupAndRestoreParams)
	backupOpFunc := func() string {
		requestedFiles := suite.UploadRequests(client.BackupAndRestoreFeatureID, client.OperationBackup, params, 1)
		require.Equal(suite.T(), 1, len(requestedFiles), "there must be only 1 uploaded backup file")
		suite.StartUploads(client.BackupAndRestoreFeatureID, requestedFiles)

		var restoreURL string
		for correlationID := range requestedFiles { // 1 element
			restoreURL, err = suite.DownloadURL(correlationID)
			require.NoErrorf(suite.T(), err, "cannot get download URL for upload %s", correlationID)
		}
		return restoreURL
	}
	backupCheckFunc := func(statuses []interface{}) {
		suite.assertStatuses(statuses, client.BackupStarted, client.BackupFinished)
	}
	restoreURL := suite.runOperation(backupOpFunc, backupCheckFunc, client.BackupFinished, client.BackupFailed)
	require.NotEmptyf(suite.T(), restoreURL, "empty restore URL returned from backup operation")

	params, err = newBackupAndRestoreParams(restoreDir, restoreURL)
	require.NoError(suite.T(), err, msgFailedInitializeBackupAndRestoreParams)
	restoreOpFunc := func() string {
		_, err = util.ExecuteOperation(suite.Cfg, suite.FeatureURL, client.OperationRestore, params)
		require.NoErrorf(suite.T(), err, msgFailedExecuteOperation, client.OperationRestore)
		return ""
	}
	restoreCheckFunc := func(statuses []interface{}) {
		suite.assertStatuses(statuses, client.RestoreStarted, client.RestoreFinished)
	}
	suite.runOperation(restoreOpFunc, restoreCheckFunc, client.RestoreFinished, client.RestoreFailed)
	suite.assertDirectoryContent(backupDir, restoreDir)
}

func (suite *fileBackupSuite) runOperation(operation func() string, statusCheck func([]interface{}), terminalStates ...string) string {
	conn, err := util.NewDigitalTwinWSConnection(suite.Cfg)
	require.NoError(suite.T(), err, msgFailedCreateWebsocketConnection)
	defer conn.Close()

	util.SubscribeForWSMessages(suite.Cfg, conn, util.StartSendEvents, fmt.Sprintf(eventFilterTemplate, client.BackupAndRestoreFeatureID))
	defer util.UnsubscribeFromWSMessages(suite.Cfg, conn, util.StopSendEvents)

	result := operation()
	statuses := suite.getLastOperationStatuses(conn, terminalStates...)
	statusCheck(statuses)
	return result
}

func (suite *fileBackupSuite) getLastOperationStatuses(conn *websocket.Conn, terminalStates ...string) []interface{} {
	pathLastOperation := util.GetFeaturePropertyPath(client.BackupAndRestoreFeatureID, client.LastOperationProperty)
	topicCreated := util.GetTwinEventTopic(suite.ThingCfg.DeviceID, protocol.ActionCreated)
	topicModified := util.GetTwinEventTopic(suite.ThingCfg.DeviceID, protocol.ActionModified)

	statuses := []interface{}{}
	err := util.ProcessWSMessages(suite.Cfg, conn,
		func(msg *protocol.Envelope) (bool, error) {
			if (msg.Topic.String() == topicCreated || msg.Topic.String() == topicModified) && msg.Path == pathLastOperation {
				statuses = append(statuses, msg.Value)
				return fut.ContainsState(msg.Value, terminalStates...), nil
			}
			return true, fmt.Errorf(msgUnexpectedValue, msg.Value)
		})
	require.NoError(suite.T(), err, "error processing last operation status events")
	return statuses
}

func (suite *fileBackupSuite) assertDirectoryContent(backupDir string, restoreDir string) {
	backupFiles, err := os.ReadDir(backupDir)
	require.NoErrorf(suite.T(), err, msgFailedReadDirectory, backupDir)
	restoreFiles, err := os.ReadDir(restoreDir)
	require.NoErrorf(suite.T(), err, msgFailedReadDirectory, restoreDir)
	require.Equal(suite.T(), len(backupFiles), len(restoreFiles), "backup and restore file count differ")
	for _, backupFile := range backupFiles {
		expectedFile := filepath.Join(backupDir, backupFile.Name())
		restoreFile := filepath.Join(restoreDir, backupFile.Name())
		require.FileExistsf(suite.T(), restoreFile, "restore file %s does not exist", restoreFile)
		suite.T().Logf("comparing backup file %s with restore file %s", expectedFile, restoreFile)
		suite.assertFiles(expectedFile, restoreFile)
	}
}

func (suite *fileBackupSuite) assertFiles(expectedFile string, restoredFile string) {
	restoredBytes, err := os.ReadFile(restoredFile)
	require.NoErrorf(suite.T(), err, "cannot read file %s", restoredFile)
	suite.AssertContent(expectedFile, restoredBytes)
}

func (suite *fileBackupSuite) assertStatuses(statuses []interface{}, expectedStates ...string) {
	type backupAndRestoreStatus struct {
		State    string `json:"state"`
		Progress int    `json:"progress"`
	}
	require.Equal(suite.T(), len(expectedStates), len(statuses))
	for i, status := range statuses {
		backupAndRestoreStatus := backupAndRestoreStatus{}
		err := util.Convert(statuses[i], &backupAndRestoreStatus)
		require.NoErrorf(suite.T(), err, "cannot convert %v to backup and restore status", status)
		require.Equal(suite.T(), expectedStates[i], backupAndRestoreStatus.State, "wrong backup and restore state")
		if i == 0 {
			require.Equal(suite.T(), 0, backupAndRestoreStatus.Progress, msgWrongProgress)
		} else { // final one
			require.Equal(suite.T(), 100, backupAndRestoreStatus.Progress, msgWrongProgress)
		}
	}
}
