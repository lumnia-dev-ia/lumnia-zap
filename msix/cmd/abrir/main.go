// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// LumniaZap.exe — o item "Lumnia Zap" do Menu Iniciar. Equivale ao atalho da
// Mesa: se a ponte nao responde, sobe o servico.exe e espera ate 30 s; depois
// abre o painel no navegador. Abrir o app uma vez depois de instalar tambem e
// o que ativa a startup task do MSIX para os proximos logins.
//
// 1.1.1: antes de tudo, se houver a instalacao classica (.exe) neste PC,
// oferece passar o historico e a conexao para a Store (pacote migrar). Sem
// isso o painel aberto era o da ponte ANTIGA (caso do Douglas, 05/11).

//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"lumnia-zap/msix/comum"
	"lumnia-zap/msix/migrar"
)

func caixa(titulo, msg string) {
	u := syscall.NewLazyDLL("user32.dll")
	t, _ := syscall.UTF16PtrFromString(titulo)
	m, _ := syscall.UTF16PtrFromString(msg)
	u.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40)
}

// pergunta: Sim/Não; true = Sim.
func pergunta(titulo, msg string) bool {
	u := syscall.NewLazyDLL("user32.dll")
	t, _ := syscall.UTF16PtrFromString(titulo)
	m, _ := syscall.UTF16PtrFromString(msg)
	r, _, _ := u.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x4|0x20) // YESNO | QUESTION
	return r == 6                                                                                                 // IDYES
}

func oferecerMigracao() {
	sis := comum.SistemaWindows{}
	p := comum.PastasMigracao()
	alheia := comum.Vivo() && comum.StatusTipo() != "windows-store"
	if !migrar.Pendente(sis, p) && !alheia {
		return
	}
	if !pergunta("Lumnia Zap", "Encontrei a instalação antiga do Lumnia Zap neste PC.\n\n"+
		"Quer passar o histórico e a conexão do WhatsApp para a versão da Microsoft Store?\n\n"+
		"O Claude será fechado durante a passagem (abra de novo no fim). A instalação antiga "+
		"fica guardada, só deixa de iniciar sozinha.") {
		if alheia {
			caixa("Lumnia Zap", "Enquanto a instalação antiga estiver ligada, a versão da Store não sobe e o painel que abre é o antigo.\n\n"+
				"Quando quiser passar, abra o Lumnia Zap pelo Menu Iniciar de novo.")
		}
		return
	}
	r, err := migrar.Migrar(sis, p, time.Now())
	if err != nil {
		caixa("Lumnia Zap", "A passagem não foi concluída: "+err.Error())
		return
	}
	msg := fmt.Sprintf("Pronto. O histórico (%.1f MB) passou para a versão da Microsoft Store.", float64(r.BytesCopiados)/(1<<20))
	if len(r.ConfigsFalhas) > 0 {
		msg += "\n\nNão consegui ajustar a configuração do Claude em:\n" + strings.Join(r.ConfigsFalhas, "\n") +
			"\n\nInstale a extensão do Lumnia Zap no Claude (lumnia.dev/zap)."
	}
	msg += "\n\nSe o painel pedir o QR code, escaneie de novo: o histórico fica. Depois abra o Claude."
	caixa("Lumnia Zap", msg)
}

func main() {
	oferecerMigracao()
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
