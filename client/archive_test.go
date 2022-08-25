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

package client

import (
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestArchive(t *testing.T) {
	basedir, err := os.MkdirTemp(".", "testdir")
	assertNoErr(t, err)
	defer removeTestDir(basedir)

	backupDir, _, err := createBackupDir(basedir)

	assertNoErr(t, err)

	archive, err := pickFilePath(basedir, "test", "bck")
	assertNoErr(t, err)

	err = zipDir(backupDir, archive)
	assertNoErr(t, err)

	restoreDir := filepath.Join(basedir, "restore")
	err = os.Mkdir(restoreDir, 0700)
	assertNoErr(t, err)

	err = unzipDir(archive, restoreDir)
	assertNoErr(t, err)

	compareDirs(t, backupDir, restoreDir)
}

func compareDirs(t *testing.T, sourceDir string, targetDir string) {
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == sourceDir {
			return nil
		}

		sourcePath, err := filepath.Rel(sourceDir, path)
		assertNoErr(t, err)

		destPath := filepath.Join(targetDir, sourcePath)
		if !pathExisting(destPath) {
			t.Errorf("file/dir '%s' not found in target dir '%s'", sourcePath, targetDir)
		}

		if !d.IsDir() {
			sourceData, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}

			destData, err := ioutil.ReadFile(destPath)
			if err != nil {
				return err
			}

			sourceStr := string(sourceData)
			destStr := string(destData)
			if sourceStr != destStr {
				t.Errorf("file '%s' contents not matching - expected '%s', but was '%s'",
					sourcePath, sourceStr, destStr)
			}
		}

		return nil
	})

	assertNoErr(t, err)
}

func assertNoErr(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Error(err)
	}
}

func createBackupDir(parent string) (string, string, error) {
	name, err := os.MkdirTemp(parent, "backup")
	if err != nil {
		return "", "", err
	}

	toClean := name //clean on error
	defer func() {
		removeTestDir(toClean)
	}()

	subPath := filepath.Join(name, "sub")
	err = os.Mkdir(subPath, 0700)
	if err != nil {
		return "", "", err
	}

	if err := ioutil.WriteFile(filepath.Join(name, "a.txt"), ([]byte)("Just a test file"), 0666); err != nil {
		return "", "", err
	}

	if err := ioutil.WriteFile(filepath.Join(subPath, "b.txt"), ([]byte)("Another test file"), 0666); err != nil {
		return "", "", err
	}

	toClean = "" //all went fine - nothing to clean

	return name, subPath, nil
}

func removeTestDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		log.Println(err)
	}
}
