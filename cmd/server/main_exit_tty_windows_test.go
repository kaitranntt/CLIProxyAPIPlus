//go:build windows

package main

import "os/exec"

func detachControllingTTY(*exec.Cmd) {}
