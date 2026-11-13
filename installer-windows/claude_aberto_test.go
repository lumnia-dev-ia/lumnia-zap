// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWinGravarAtomicoEBackupUmaVez(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "claude_desktop_config.json")
	_ = os.WriteFile(cfg, []byte(`{"preferences":{"a":1}}`), 0o600)
	if err := gravarConfig(dir, `C:\LumniaZap`, `C:\uv.exe`); err != nil {
		t.Fatal(err)
	}
	if !configTemLumnia(dir) {
		t.Fatal("sem a entrada")
	}
	orig, _ := os.ReadFile(cfg + ".bak-lumnia-zap")
	_ = gravarConfig(dir, `C:\LumniaZap`, `C:\uv.exe`)
	depois, _ := os.ReadFile(cfg + ".bak-lumnia-zap")
	if string(orig) != string(depois) || string(orig) != `{"preferences":{"a":1}}` {
		t.Fatalf("backup deveria ser o original e não mudar: %q / %q", orig, depois)
	}
	if _, err := os.Stat(cfg + ".lumnia-novo"); !os.IsNotExist(err) {
		t.Fatal("temporário ficou para trás")
	}
}

func TestWinConferirComBOM(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "claude_desktop_config.json"),
		append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"mcpServers":{"lumnia-zap":{}}}`)...), 0o600)
	if len(conferirRegistros([]string{dir})) != 0 {
		t.Fatal("BOM não pode fazer a entrada parecer perdida")
	}
}

func TestWinPacotesDaStore(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "local")
	t.Setenv("LOCALAPPDATA", local)
	if len(pacotesDaStore(home)) != 0 {
		t.Fatal("sem pacote")
	}
	_ = os.MkdirAll(filepath.Join(local, "Packages", "Lumnia.LumniaZap_27md6avg2vwam"), 0o755)
	if len(pacotesDaStore(home)) != 1 {
		t.Fatal("pacote da Store não detectado")
	}
	if !temArgumento([]string{"--forcar"}, "--forcar") || !temArgumento([]string{"/FORCAR"}, "--forcar") || temArgumento(nil, "--forcar") {
		t.Fatal("argumento --forcar")
	}
}

func TestWinClaudeFechadoSemPergunta(t *testing.T) {
	antes := claudeAbertoFn
	defer func() { claudeAbertoFn = antes }()
	claudeAbertoFn = func() bool { return false }
	if err := garantirClaudeFechado(); err != nil {
		t.Fatal(err)
	}
}
