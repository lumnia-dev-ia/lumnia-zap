// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func contem(lista []string, x string) bool {
	for _, y := range lista {
		if y == x {
			return true
		}
	}
	return false
}

func TestConectoresNilSaoTodos(t *testing.T) {
	lib, neg := beatitudeMontarListas(nil)
	for _, f := range []string{"mcp__claude_ai_Gmail", "mcp__claude_ai_Microsoft_365__get_me", "mcp__claude_ai_Tim_o__list_boards", "mcp__lumnia-zap__list_chats"} {
		if !contem(lib, f) {
			t.Errorf("com nil deveria liberar %s", f)
		}
	}
	if !contem(neg, "mcp__claude_ai_Tim_o__send_message") || !contem(neg, "Read") {
		t.Error("negadas incompletas com nil")
	}
	if len(beatitudeRegrasConectores(nil)) < 5 {
		t.Error("regras dos conectores sumiram do prompt")
	}
}

func TestConectoresSoOsMarcados(t *testing.T) {
	lib, _ := beatitudeMontarListas([]string{"timao"})
	if !contem(lib, "mcp__claude_ai_Tim_o__create_task") {
		t.Error("Timão marcado e não liberado")
	}
	if contem(lib, "mcp__claude_ai_Gmail") || contem(lib, "mcp__claude_ai_Microsoft_365__get_me") {
		t.Error("conector desmarcado continuou liberado")
	}
	regras := strings.Join(beatitudeRegrasConectores([]string{"timao"}), "\n")
	if !strings.Contains(regras, "Timão") || strings.Contains(regras, "Gmail") {
		t.Errorf("prompt não segue os conectores marcados: %q", regras)
	}
	lib, _ = beatitudeMontarListas([]string{})
	if contem(lib, "mcp__claude_ai_Tim_o__create_task") || !contem(lib, "mcp__lumnia-zap__list_chats") {
		t.Error("lista vazia deveria deixar só a base")
	}
}

func TestConectoresIDsValidos(t *testing.T) {
	got := beatitudeIDsValidos([]string{"timao", "xyz", "google", "timao"})
	if strings.Join(got, ",") != "google,timao" {
		t.Errorf("ids: %v", got)
	}
	if beatitudeIDsValidos(nil) == nil {
		t.Error("nunca devolver nil (nil = todos)")
	}
}

func TestConectoresNegadosSoOQueNaoEraDeProposito(t *testing.T) {
	_, neg := beatitudeMontarListas([]string{"google"})
	got := beatitudeConectoresNegados([]string{
		"mcp__claude_ai_Gmail__send_message",   // de propósito: o prompt explica
		"mcp__claude_ai_Tim_o__list_boards",    // Timão desligado nesta máquina
		"mcp__claude_ai_Tim_o__create_task",    // mesmo conector, uma vez só
		"mcp__claude_ai_Notion__search",        // fora do catálogo
		"mcp__claude_ai_Beatitude__ler_perfil", // base, nunca entra
		"Read",                                 // embutida, não é conector
	}, neg)
	if strings.Join(got, "|") != "Notion|Timão" {
		t.Errorf("negados: %v", got)
	}
}

func TestServidorDaFerramenta(t *testing.T) {
	if s, ok := beatitudeServidorDaFerramenta("mcp__claude_ai_Microsoft_365__get_me"); !ok || s != "Microsoft_365" {
		t.Errorf("%q %v", s, ok)
	}
	if _, ok := beatitudeServidorDaFerramenta("mcp__lumnia-zap__list_chats"); ok {
		t.Error("Zap não é conector da conta")
	}
	if beatitudeNomeDoServidor("Google_Drive") != "Google (Agenda, Gmail, Drive)" {
		t.Error("Drive deveria cair no conector Google")
	}
}
