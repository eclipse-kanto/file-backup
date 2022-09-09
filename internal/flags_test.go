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
	"os"
	"testing"

	flags "github.com/eclipse-kanto/file-upload/flagparse"
	. "github.com/eclipse-kanto/file-upload/flagparsetest"
)

const (
	testConfigFile = "testdata/testConfig.json"
)

var (
	testConfig BackupFileConfig
)

func TestMain(m *testing.M) {
	originalArgs := os.Args

	flags.LoadJSON(testConfigFile, &testConfig)
	testConfig.Delete = !testConfig.KeepUploaded

	code := m.Run()

	os.Args = originalArgs
	os.Exit(code)
}

func TestNoArgs(t *testing.T) {
	ResetFlags()

	expected := getDefaultConfig(false)
	PassArgs()

	parseAndVerify(expected, t, false)
}

func TestCliArgs(t *testing.T) {
	ResetFlags()

	args := ConfigToArgs(t, &testConfig, configSkip, true)
	PassArgs(args...)

	parseAndVerify(&testConfig, t, false)
}

func TestConfig(t *testing.T) {
	ResetFlags()

	PassArgs(Arg{Name: flags.ConfigFile, Value: testConfigFile})
	parseAndVerify(&testConfig, t, false)
}

func TestConfigAndCliArgs(t *testing.T) {
	ResetFlags()

	config := BackupFileConfig{}
	config.Delete = true

	args := ConfigToArgs(t, &config, configSkip, true)
	args = append(args, Arg{Name: flags.ConfigFile, Value: testConfigFile})

	PassArgs(args...)
	parseAndVerify(&config, t, false)
}

func TestKeepUploaded(t *testing.T) {
	ResetFlags()

	config := getDefaultConfig(true)

	args := ConfigToArgs(t, config, configSkip, true)
	args = append(args, Arg{Name: keepUploaded, Value: "true"})
	PassArgs(args...)

	parseAndVerify(config, t, false)
}

func TestConfigFileNotExist(t *testing.T) {
	ResetFlags()

	expected := getDefaultConfig(false)

	PassArgs(Arg{Name: flags.ConfigFile, Value: "missingFile"})
	parseAndVerify(expected, t, true)
}

func getDefaultConfig(keepUploaded bool) *BackupFileConfig {
	cfg := BackupFileConfig{}
	flags.InitConfigDefaults(&cfg, configNames, configSkip)
	cfg.KeepUploaded = keepUploaded
	cfg.Delete = !cfg.KeepUploaded

	return &cfg
}

func parseAndVerify(expected *BackupFileConfig, t *testing.T, expectConfigFileNotFound bool) {
	parsedConfig, warn := ParseFlags("n/a")
	parsedConfig.Delete = !parsedConfig.KeepUploaded

	VerifyEquals(expected, parsedConfig, t, nil)
	VerifyNotFoundError(warn, expectConfigFileNotFound, t)
}
