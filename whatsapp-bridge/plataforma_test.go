// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"runtime"
	"testing"
)

func TestPlataformaNomes(t *testing.T) {
	m, s, n := maquina(), shellDeLogin(), sistemaDoNavegador()
	if runtime.GOOS == "darwin" {
		if m != "Mac" || s != "/bin/zsh" || n != "macOS" {
			t.Fatalf("darwin: %q %q %q", m, s, n)
		}
		return
	}
	if runtime.GOOS == "linux" && (m != "computador" || s != "/bin/bash" || n != "Linux") {
		t.Fatalf("linux: %q %q %q", m, s, n)
	}
}

func TestMordomoMicrosoftSoLeituraERascunho(t *testing.T) {
	liberada := map[string]bool{}
	for _, f := range beatitudeFerramentasLiberadas {
		liberada[f] = true
	}
	for _, f := range []string{
		"mcp__claude_ai_Microsoft_365__outlook_email_search",
		"mcp__claude_ai_Microsoft_365__outlook_calendar_search",
		"mcp__claude_ai_Microsoft_365__outlook_create_draft",
	} {
		if !liberada[f] {
			t.Errorf("deveria estar liberada: %s", f)
		}
	}
	for _, f := range beatitudeFerramentasNegadas {
		if liberada[f] {
			t.Errorf("negada e liberada ao mesmo tempo: %s", f)
		}
	}
	for _, f := range []string{
		"mcp__claude_ai_Microsoft_365__outlook_send_mail",
		"mcp__claude_ai_Microsoft_365__outlook_send_draft",
		"mcp__claude_ai_Microsoft_365__outlook_forward_mail",
	} {
		if liberada[f] {
			t.Errorf("envio não pode estar liberado: %s", f)
		}
	}
}
