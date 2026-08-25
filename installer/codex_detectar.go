// Detecção do OpenAI Codex no macOS. A parte comum (registro no config.toml)
// está em codex.go, idêntico nos dois instaladores.
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// detectarCodex diz se o OpenAI Codex parece instalado neste Mac.
//
// A checagem é conservadora e nunca bloqueia a instalação: errar para "não
// achei" só faz o instalador pular o registro no Codex (o Claude segue
// normal), e rodar o instalador de novo depois resolve. O sinal mais forte é
// a pasta ~/.codex (o Codex a cria no primeiro uso, para config e login);
// os caminhos fixos cobrem instalações via Homebrew e npm, porque um app
// aberto pelo Finder roda com PATH mínimo e o LookPath sozinho não os veria.
func detectarCodex(home string) bool {
	if st, err := os.Stat(codexHome(home)); err == nil && st.IsDir() {
		return true
	}
	for _, c := range []string{
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
		filepath.Join(home, ".local", "bin", "codex"),
		filepath.Join(home, ".npm-global", "bin", "codex"),
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return true
		}
	}
	_, err := exec.LookPath("codex")
	return err == nil
}
