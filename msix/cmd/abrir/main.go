// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// LumniaZap.exe — o item "Lumnia Zap" do Menu Iniciar. Equivale ao atalho da
// Mesa: se a ponte nao responde, sobe o servico.exe e espera ate 30 s; depois
// abre o painel no navegador. Abrir o app uma vez depois de instalar tambem e
// o que ativa a startup task do MSIX para os proximos logins.

//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"lumnia-zap/msix/comum"
)

func caixa(titulo, msg string) {
	u := syscall.NewLazyDLL("user32.dll")
	t, _ := syscall.UTF16PtrFromString(titulo)
	m, _ := syscall.UTF16PtrFromString(msg)
	u.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40)
}

func main() {
	if !comum.Vivo() {
		svc := exec.Command(filepath.Join(comum.PastaInstalacao(), "servico.exe"))
		svc.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | NEW_PROCESS_GROUP
		if err := svc.Start(); err == nil {
			_ = svc.Process.Release()
		}
		for i := 0; i < 60 && !comum.Vivo(); i++ {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !comum.Vivo() {
		caixa("Lumnia Zap", "A ponte não respondeu em 30 segundos.\n\nO registro está em:\n"+
			filepath.Join(comum.PastaLogs(), "bridge.log"))
		return
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", comum.Painel).Start()
}
