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
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func zipDir(dir string, archive string) error {
	f, err := os.Create(archive)

	if err != nil {
		return err
	}

	defer f.Close()

	w := zip.NewWriter(f)

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		var entry string
		if entry, err = filepath.Rel(dir, path); err != nil {
			return err
		}

		if entry == "." {
			return nil
		}

		if d.IsDir() {
			_, err := w.Create(entry + "\\")

			return err
		}

		var out io.Writer
		var in io.ReadCloser
		if out, err = w.Create(entry); err != nil {
			return err
		}

		if in, err = os.Open(path); err != nil {
			return err
		}
		defer in.Close()

		_, err = io.Copy(out, in)

		return err
	})

	if err != nil {
		return err
	}

	return w.Close()
}

func unzipDir(archive string, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if err = func() error {
			var in io.ReadCloser
			var out io.WriteCloser

			if in, err = f.Open(); err != nil {
				return err
			}

			defer in.Close()

			name := strings.ReplaceAll(f.Name, "\\", string(filepath.Separator))
			path := filepath.Join(dir, name)

			if strings.HasSuffix(f.Name, "\\") {
				err = os.Mkdir(path, 0755)

				if err != nil && !os.IsExist(err) {
					return err
				}
				return nil
			}

			if out, err = os.Create(path); err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, in)

			return err
		}(); err != nil {
			return err
		}
	}

	return nil
}
