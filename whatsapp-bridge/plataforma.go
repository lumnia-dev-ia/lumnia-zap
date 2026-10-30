// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// plataforma.go — o pouco que muda entre o Mac da pessoa e o computador do
// Beatitude na nuvem (Linux). O resto da ponte é o mesmo binário.

import (
	"runtime"
)

// beatitudeDisponivel diz se o módulo Beatitude existe nesta plataforma. Ele
// é só para Mac e Linux: no Windows a ponte não inicia o módulo, não registra
// as rotas e o painel não mostra o cartão nem o link (decisão de 30/10/2026 —
// quem instala no Windows quer só o WhatsApp no Claude).
func beatitudeDisponivel() bool {
	return runtime.GOOS != "windows"
}

// maquina é como as mensagens chamam o computador onde a ponte roda:
// "Mac" no macOS, "computador" no Linux da nuvem ("neste computador").
func maquina() string {
	if runtime.GOOS == "darwin" {
		return "Mac"
	}
	return "computador"
}

// shellDeLogin é o shell usado no último recurso para achar o claude no PATH
// da pessoa: zsh no macOS, bash no Ubuntu.
func shellDeLogin() string {
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// sistemaDoNavegador é o nome do sistema como o Claude in Chrome o mostra na
// lista de navegadores conectados ("Browser 1 (Linux)", "Browser 2 (macOS)").
func sistemaDoNavegador() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	}
	return "Linux"
}
