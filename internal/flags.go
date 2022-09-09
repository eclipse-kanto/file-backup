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

package flags

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/eclipse-kanto/file-backup/client"
	upload "github.com/eclipse-kanto/file-upload/client"
	flags "github.com/eclipse-kanto/file-upload/flagparse"
	"github.com/eclipse-kanto/file-upload/logger"
)

const (
	// Uploadable Flags
	defaultName        = "BackupAndRestore"
	defaultType        = "file"
	defaultContext     = "edge"
	defaultPeriod      = time.Hour * 10
	defaultStopTimeout = time.Minute
	// Uploadable flags

	// Backup flags
	dir          = "dir"
	storage      = "storage"
	backupCmd    = "backupCmd"
	restoreCmd   = "restoreCmd"
	keepUploaded = "keepUploaded"

	defaultDir          = ""
	defaultStorage      = "./storage"
	backupsDir          = "/backups"
	downloadsDir        = "/downloads"
	defaultKeepUploaded = false
	// Backup flags
)

// BackupFileConfig holds broker, log file and backup and restore feature configurations
type BackupFileConfig struct {
	upload.BrokerConfig
	logger.LogConfig
	client.BackupAndRestoreConfig
}

var configNames = map[string]string{
	"featureID": "BackupAndRestore", "feature": "BackupAndRestore", "period": "Backup period",
	"action": "backup", "actions": "backups", "running_actions": "uploads/backups",
	"transfers": "uploads/downloads", "logFile": "log/file-backup.log",
	"mode": "Directory access mode. Restricts which directories can be requested dynamically for backup/restore" +
		" through '" + client.BackupDirProperty + "' operation property.\nAllowed values are:" +
		"\n  'strict' - dynamically specifying directory is forbidden, the 'dir' property must be used instead" +
		"\n  'scoped' - allows to backup/restore from/to any sub-directory of the one specified with the 'dir' property" +
		"\n  'lax' - allows backup of any directory and restore to any directory. Use with care!",
}

var configSkip = map[string]bool{"delete": true}

// ParseFlags Define & Parse all flags
func ParseFlags(version string) (*BackupFileConfig, flags.ConfigFileMissing) {
	printVersion := flag.Bool("version", false, "Prints current version and exits")

	configFile := flag.String(flags.ConfigFile, "", "Defines the configuration file")

	flagsConfig := &BackupFileConfig{}

	flags.InitFlagVars(flagsConfig, configNames, configSkip)
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	config := &BackupFileConfig{}
	warn := flags.LoadConfigFromFile(*configFile, config, configNames, configSkip)
	flags.ApplyFlags(config, *flagsConfig)

	return config, warn
}
