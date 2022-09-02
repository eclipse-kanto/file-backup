// Copyright (c) 2022 Contributors to the Eclipse Foundation
//
// See the NOTICE file(s) distributed with this work for additional
// information regarding copyright ownership.
//
// This program and the accompanying materials are made available under the
// terms of the Eclipse Public License 2.0 which is available at
// http://www.eclipse.org/legal/epl-2.0
//
// SPDX-License-Identifier: EPL-2.0

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eclipse-kanto/file-backup/client"
	flags "github.com/eclipse-kanto/file-backup/internal"
	upload "github.com/eclipse-kanto/file-upload/client"
	"github.com/eclipse-kanto/file-upload/logger"
)

var version = "dev"

func main() {
	config, warn := flags.ParseFlags(version)

	config.Validate()

	loggerOut, err := logger.SetupLogger(&config.LogConfig, "[FILE BACKUP]")
	if err != nil {
		log.Fatalln("Failed to initialize logger: ", err)
	}
	defer loggerOut.Close()

	if warn != nil {
		logger.Warn(warn)
	}

	logger.Infof("backup config: %+v", config.BackupAndRestoreConfig)
	logger.Infof("log config: %+v", config.LogConfig)

	backupRestore, err := client.NewBackupAndRestore(&config.BackupAndRestoreConfig)
	if err != nil {
		log.Println(err)
		return
	}

	p, err := upload.NewEdgeConnector(&config.BrokerConfig, backupRestore)
	if err != nil {
		log.Println(err)
		return
	}

	defer p.Close()

	chstop := make(chan os.Signal, 1)
	signal.Notify(chstop, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Press Ctrl+C to exit.")

	<-chstop
}
