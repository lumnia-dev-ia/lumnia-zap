// Atalho Lumnia Zap — o "Lumnia Zap.app" que fica na Mesa.
//
// Antes era um script de shell montado pelo instalador na maquina da pessoa,
// sem assinatura. No macOS 15+ o Gatekeeper avalia esse app pela procedencia
// (com.apple.provenance, que o xattr -c nao remove) e o bloqueia com "a Apple
// nao pode verificar...". Agora o atalho e um binario de verdade, compilado e
// assinado no release.sh, notarizado junto com o instalador e copiado por ele
// para a Mesa com a assinatura intacta.
//
// Faz o mesmo que o script fazia: se a ponte nao responde, reacorda o servico
// do launchd e espera ate 20 s; depois abre o painel no navegador.
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	label    = "dev.lumnia.zap"
	painel   = "http://localhost:8080"
	statusEP = painel + "/api/panel/status"
)

func vivo() bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(statusEP)
	if err != nil {
		return false
	}
	r.Body.Close()
	return true
}

func acordar() {
	uid := fmt.Sprint(os.Getuid())
	alvo := "gui/" + uid + "/" + label
	exec.Command("/bin/launchctl", "enable", alvo).Run()
	if err := exec.Command("/bin/launchctl", "kickstart", "-k", alvo).Run(); err != nil {
		home, _ := os.UserHomeDir()
		plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		exec.Command("/bin/launchctl", "bootstrap", "gui/"+uid, plist).Run()
	}
}

func main() {
	if !vivo() {
		acordar()
		for i := 0; i < 40 && !vivo(); i++ {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if vivo() {
		exec.Command("/usr/bin/open", painel).Run()
		return
	}
	exec.Command("/usr/bin/osascript", "-e",
		`display alert "Lumnia Zap indisponível" message "A ponte não respondeu. Rode o instalador de novo." as critical`).Run()
	os.Exit(1)
}
