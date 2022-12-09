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

package integration

import (
	fut "github.com/eclipse-kanto/file-upload/integration"
)

const (
	featureID = "BackupAndRestore"

	backupFilesCount = 5

	operationBackup  = "backup"
	operationRestore = "restore"

	eventFilterTemplate = "like(resource:path,'/features/%s/*')"

	msgNoUploadCorrelationID                  = "no upload with correlation id: %s"
	msgFailedExecuteOperation                 = "failed to execute operation %s"
	msgFailedInitializeBackupAndRestoreParams = "failed to initialize backup and restore parameters"
	msgFailedReadDirectory                    = "failed to read directory %s"
	msgUnexpectedValue                        = "unexpected value: %v"
	msgFailedCreateWebsocketConnection        = "failed to create websocket connection"
	msgWrongProgress                          = "wrong backup and restore progress"
)

type backupAndRestoreConfig struct {
	HTTPServer string `env:"FBT_HTTP_SERVER"`
	BackupDir  string `env:"FBT_BACKUP_DIR"`
	RestoreDir string `env:"FBT_RESTORE_DIR"`
}

type fileBackupSuite struct {
	fut.FileUploadSuite
	backupAndRestoreCfg backupAndRestoreConfig
}

type httpFileBackupSuite struct {
	fileBackupSuite
}

type azureFileBackupSuite struct {
	fileBackupSuite
}

type awsFileBackupSuite struct {
	fileBackupSuite
}
