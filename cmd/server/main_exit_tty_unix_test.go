//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func detachControllingTTY(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
