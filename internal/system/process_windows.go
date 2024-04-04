// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package system

import (
	"os"
	"os/exec"
	"syscall"
)

// SetProcessGroup sets the process group flag for the command
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func KillGroup(process *os.Process) error {
	return process.Kill()
}
