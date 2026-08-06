//go:build windows

// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"os/exec"
	"syscall"
)

// subirDireto sobe o launcher sem janela e com console proprio
// (CREATE_NO_WINDOW): nao aparece nada na tela e o processo sobrevive ao
// fechamento da janela do instalador — sem console proprio, fechar o
// instalador derrubaria a ponte junto.
func subirDireto(launcher string) {
	c := exec.Command("cmd", "/c", launcher)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = c.Start()
}
