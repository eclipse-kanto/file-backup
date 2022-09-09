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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/eclipse-kanto/file-upload/logger"
)

type command struct {
	name string
	arg  string
}

const shell = "/bin/sh"

// GetName get name of command
func (c *command) GetName() string {
	return c.name
}

// GetArgs get args of command
func (c *command) GetArg() string {
	return c.arg
}

//MarshalJSON marshals command type to JSON
func (c *command) MarshalJSON() ([]byte, error) {
	s := c.String()

	return json.Marshal(s)
}

//UnmarshalJSON unmarshalls command type from JSON
func (c *command) UnmarshalJSON(b []byte) error {
	var value string
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}

	cmd := command{}
	err := cmd.Set(value)

	if err != nil {
		log.Fatalf("Failed to parse command value '%s': %v\n", value, err)
		return err
	}

	*c = cmd
	return nil
}

func (c *command) String() string {
	if len(c.arg) == 0 {
		return c.name
	}

	if runtime.GOOS != "windows" && c.name == shell {
		return c.arg
	}

	return fmt.Sprint(c.name, " ", c.arg)
}

func (c *command) Set(value string) error {
	if value == "" {
		c.name = ""
		c.arg = ""
	} else {
		path, err := filepath.Abs(value)
		if err != nil {
			return err
		}

		_, err = os.Stat(path)
		if err != nil {
			return err
		}

		if runtime.GOOS != "windows" && strings.HasSuffix(value, ".sh") {
			c.name = shell
			c.arg = path
		} else {
			c.name = path
		}
	}

	return nil
}

func (c *command) defined() bool {
	return len(c.name) > 0
}

func (c *command) execute(dir string) error {
	if !c.defined() {
		return nil
	}

	cmd := exec.Command(c.name, c.arg)
	cmd.Dir = dir

	logger.Infof("executing command %v in dir %s", cmd, dir)
	err := cmd.Run()

	if err != nil {
		logger.Errorf("error while executing command %v: %v", cmd, err)
	}

	return err
}
