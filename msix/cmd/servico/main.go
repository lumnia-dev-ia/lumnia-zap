// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// servico.exe — substitui a Tarefa Agendada + iniciar.cmd da instalacao
// classica. E o executavel da startup task do MSIX (windows.startupTask):
// o Windows o sobe no login do usuario. Ele mantem a ponte de pe:
//
//   - instancia unica por mutex nomeado (Local\LumniaZapServico);
//   - roda a ponte com a pasta de trabalho em LocalState\whatsapp-bridge,
//     onde ela cria store/;
//   - toda a saida da ponte vai para LocalState\logs\bridge.log (gira a 5 MB);
//   - se a ponte cair, espera 10 s e sobe de novo — mesmo laco do iniciar.cmd.
//
// Compilado com -H windowsgui: nenhuma janela de console. A ponte (console)
// sobe com CREATE_NO_WINDOW pelo mesmo motivo.

//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"lumnia-zap/msix/comum"
)

const createNoWindow = 0x08000000

func mutexUnico(nome string) bool {
	k := syscall.NewLazyDLL("kernel32.dll")
	p, _ := syscall.UTF16PtrFromString(nome)
	h, _, err := k.NewProc("CreateMutexW").Call(0, 1, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return true // sem mutex, segue (melhor duas tentativas que nenhuma)
	}
	return err != syscall.Errno(183) // ERROR_ALREADY_EXISTS
}

func main() {
	if !mutexUnico(`Local\LumniaZapServico`) {
		return
	}
	ponteExe := filepath.Join(comum.PastaInstalacao(), "ponte", "whatsapp-bridge.exe")
	trabalho := comum.PastaPonte()
	_ = os.MkdirAll(trabalho, 0o755)

	for {
		log, _ := comum.AbrirLog("bridge.log")
		comum.Linha(log, "===== inicio (pacote %q) =====", comum.FamiliaDoPacote())
		comum.Linha(log, "ponte: %s", ponteExe)
		comum.Linha(log, "pasta de trabalho: %s", trabalho)

		if comum.Vivo() {
			// Outra ponte ja ocupa a 8080 (ex.: instalacao classica ligada).
			comum.Linha(log, "porta 8080 ja responde — outra ponte rodando? aguardando 60 s")
			log.Close()
			time.Sleep(60 * time.Second)
			continue
		}

		cmd := exec.Command(ponteExe)
		cmd.Dir = trabalho
		if log != nil { // sem log, a saida e descartada
			cmd.Stdout = log
			cmd.Stderr = log
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		err := cmd.Run()
		codigo := -1
		if cmd.ProcessState != nil {
			codigo = cmd.ProcessState.ExitCode()
		}
		comum.Linha(log, "===== ponte saiu (codigo %d, erro %v); nova tentativa em 10 s =====", codigo, err)
		log.Close()
		time.Sleep(10 * time.Second)
	}
}
