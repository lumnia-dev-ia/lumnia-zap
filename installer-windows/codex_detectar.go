// Detecção do OpenAI Codex no Windows. A parte comum (registro no
// config.toml) está em codex.go, idêntico nos dois instaladores.
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// detectarCodex diz se o OpenAI Codex parece instalado neste PC.
//
// A checagem é conservadora e nunca bloqueia a instalação: errar para "não
// achei" só faz o instalador pular o registro no Codex (o Claude segue
// normal), e rodar o instalador de novo depois resolve. O sinal mais forte é
// a pasta %USERPROFILE%\.codex (o Codex a cria no primeiro uso, para config e
// login). Os caminhos fixos cobrem a instalação via npm (%APPDATA%\npm), e o
// LookPath cobre quem tem o codex.exe no PATH — o instalador é um app de
// console, então herda o PATH do usuário, diferente do caso macOS/Finder.
func detectarCodex(home string) bool {
	if st, err := os.Stat(codexHome(home)); err == nil && st.IsDir() {
		return true
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		for _, c := range []string{
			filepath.Join(appdata, "npm", "codex.cmd"),
			filepath.Join(appdata, "npm", "codex.exe"),
		} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return true
			}
		}
	}
	for _, nome := range []string{"codex.exe", "codex.cmd", "codex"} {
		if _, err := exec.LookPath(nome); err == nil {
			return true
		}
	}
	return false
}
