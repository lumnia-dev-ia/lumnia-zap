// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGarantirClaudeFechadoSemClaudeNaoPergunta(t *testing.T) {
	antes := claudeAbertoFn
	defer func() { claudeAbertoFn = antes }()
	claudeAbertoFn = func() bool { return false }
	if err := garantirClaudeFechado(); err != nil {
		t.Fatal(err)
	}
}

func TestGarantirClaudeFechadoDesisteSeNaoFecha(t *testing.T) {
	a, f, e := claudeAbertoFn, fecharClaudeFn, esperaFecharClaude
	defer func() { claudeAbertoFn, fecharClaudeFn, esperaFecharClaude = a, f, e }()
	claudeAbertoFn = func() bool { return true }
	fecharClaudeFn = func() error { return nil }
	esperaFecharClaude = 50 * time.Millisecond
	// Fora do macOS o osascript não existe: perguntarBotoes devolve false e a
	// instalação para com a orientação, sem gravar nada.
	err := garantirClaudeFechado()
	if err == nil || !strings.Contains(err.Error(), "Cmd+Q") {
		t.Fatalf("esperava parar pedindo Cmd+Q, veio %v", err)
	}
}

func TestGravarConfigAtomicaEBackupUmaVez(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, arquivoConfigClaude)
	semearConfig(t, dir, `{"preferences":{"x":1}}`)
	if err := gravarConfigClaude(dir, "/destino", "/uv"); err != nil {
		t.Fatal(err)
	}
	if !configTemLumnia(dir) {
		t.Fatal("entrada lumnia-zap não gravada")
	}
	if _, err := os.Stat(cfg + ".lumnia-novo"); !os.IsNotExist(err) {
		t.Fatal("temporário ficou para trás")
	}
	original, _ := os.ReadFile(cfg + ".bak-lumnia-zap")
	if strings.Contains(string(original), "lumnia-zap") {
		t.Fatal("backup deveria ser a config de antes")
	}
	// segunda instalação: o backup continua sendo o original
	if err := gravarConfigClaude(dir, "/destino", "/uv"); err != nil {
		t.Fatal(err)
	}
	depois, _ := os.ReadFile(cfg + ".bak-lumnia-zap")
	if string(depois) != string(original) {
		t.Fatal("backup foi sobrescrito na reinstalação")
	}
}

func TestConferirRegistrosAchaEntradaPerdida(t *testing.T) {
	ok, perdida := t.TempDir(), t.TempDir()
	semearConfig(t, ok, `{"mcpServers":{"lumnia-zap":{"command":"uv"}}}`)
	semearConfig(t, perdida, `{"preferences":{}}`) // o Claude regravou sem a entrada
	got := conferirRegistros([]string{ok, perdida})
	if len(got) != 1 || got[0] != perdida {
		t.Fatalf("perdidas: %v", got)
	}
}

func TestArrumarPastaDaPonte(t *testing.T) {
	destino := t.TempDir()
	// binário solto no lugar da pasta + histórico em .antigo
	if err := os.WriteFile(filepath.Join(destino, "whatsapp-bridge"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	antigo := filepath.Join(destino, "whatsapp-bridge.antigo", "store")
	if err := os.MkdirAll(antigo, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(antigo, "messages.db"), []byte("db"), 0o600)

	if err := arrumarPastaDaPonte(destino); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(destino, "whatsapp-bridge"))
	if err != nil || !st.IsDir() {
		t.Fatal("whatsapp-bridge deveria ser pasta agora")
	}
	if _, err := os.Stat(filepath.Join(destino, "whatsapp-bridge", "store", "messages.db")); err != nil {
		t.Fatal("histórico não voltou para o lugar")
	}
	soltos, _ := filepath.Glob(filepath.Join(destino, "whatsapp-bridge.binario-*"))
	if len(soltos) != 1 {
		t.Fatalf("binário solto deveria ter sido guardado: %v", soltos)
	}
	// store atual existente nunca é trocado pelo antigo
	_ = os.MkdirAll(filepath.Join(destino, "whatsapp-bridge.antigo", "store"), 0o755)
	if err := arrumarPastaDaPonte(destino); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destino, "whatsapp-bridge", "store", "messages.db")); err != nil {
		t.Fatal("store atual foi mexido")
	}
}
