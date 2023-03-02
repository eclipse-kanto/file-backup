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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testString = "Just a test"
const testFile = "test.txt"

func TestCommand(t *testing.T) {
	testDir, err := os.MkdirTemp(".", "testdir")
	assertNoErr(t, err)

	defer removeTestDir(testDir)

	script, err := createTestScript(testDir, "test", fmt.Sprintf("echo %s>%s", testString, testFile))
	assertNoErr(t, err)

	cmd := command{}
	err = cmd.Set(script)
	assertNoErr(t, err)

	workDir, err := os.MkdirTemp(testDir, "workdir")
	assertNoErr(t, err)

	err = cmd.execute(workDir)
	assertNoErr(t, err)

	path := filepath.Join(workDir, testFile)
	b, err := ioutil.ReadFile(path)
	assertNoErr(t, err)

	text := strings.TrimSpace(string(b))
	if testString != text {
		t.Errorf("incorrect test file contents - expected '%s', but was '%s'", testString, text)
	}
}

func createTestScript(dir string, name string, contents string) (string, error) {
	ext := "bat"
	if runtime.GOOS != "windows" {
		ext = "sh"

		contents = "#!/bin/sh\n" + contents
	}

	fileName := fmt.Sprintf("%s.%s", name, ext)
	path := filepath.Join(dir, fileName)
	err := ioutil.WriteFile(path, []byte(contents), 0666)

	return path, err
}
