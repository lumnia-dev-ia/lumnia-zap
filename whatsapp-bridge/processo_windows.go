// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// semJanela: a ponte roda escondida; um tasklist filho não pode piscar
// uma janela de console na tela da pessoa.
func semJanela(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
