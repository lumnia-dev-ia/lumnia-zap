// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

func semJanela(*exec.Cmd) {}

// emGrupoProprio poe o processo num grupo so dele e, ao cancelar o contexto,
// mata o grupo inteiro (o `claude -p` e os filhos que ele abriu no terminal).
func emGrupoProprio(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
}
