package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ambienteFalso monta um %LOCALAPPDATA% e um %APPDATA% vazios num diretório
// temporário e aponta as variáveis para eles.
func ambienteFalso(t *testing.T) (home, local, roaming string) {
	t.Helper()
	home = t.TempDir()
	local = filepath.Join(home, "AppData", "Local")
	roaming = filepath.Join(home, "AppData", "Roaming")
	for _, d := range []string{local, roaming} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	return
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func contem(lista []string, alvo string) bool {
	for _, x := range lista {
		if x == alvo {
			return true
		}
	}
	return false
}

// Caso do PC corporativo de 30/10/2026: Claude da Store recente, que lê a
// config em Claude-Data, com a pasta de Packages existindo e sem %APPDATA%\Claude.
func TestPastasConfigClaude_ClaudeData(t *testing.T) {
	home, local, roaming := ambienteFalso(t)
	dados := mkdir(t, filepath.Join(local, "Claude-Data", "claude_pzs8sxrjxfjjc"))
	pkg := mkdir(t, filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming"))

	dirs := pastasConfigClaude(home)
	if !contem(dirs, dados) {
		t.Fatalf("Claude-Data fora da lista: %v", dirs)
	}
	if !contem(dirs, filepath.Join(pkg, "Claude")) {
		t.Fatalf("caminho de Packages fora da lista: %v", dirs)
	}
	if contem(dirs, filepath.Join(roaming, "Claude")) {
		t.Fatalf("não deveria criar o clássico quando há Store: %v", dirs)
	}
	if !claudeInstalado(home) {
		t.Fatal("Claude-Data deveria contar como Claude instalado")
	}
}

func TestPastasConfigClaude_SoClaudeData(t *testing.T) {
	home, local, _ := ambienteFalso(t)
	dados := mkdir(t, filepath.Join(local, "Claude-Data", "claude_abc"))
	dirs := pastasConfigClaude(home)
	if len(dirs) != 1 || dirs[0] != dados {
		t.Fatalf("esperava só %s, veio %v", dados, dirs)
	}
}

func TestPastasConfigClaude_Classico(t *testing.T) {
	home, _, roaming := ambienteFalso(t)
	classico := mkdir(t, filepath.Join(roaming, "Claude"))
	dirs := pastasConfigClaude(home)
	if len(dirs) != 1 || dirs[0] != classico {
		t.Fatalf("esperava só o clássico, veio %v", dirs)
	}
}

func TestPastasConfigClaude_NadaPalpiteClassico(t *testing.T) {
	home, _, roaming := ambienteFalso(t)
	dirs := pastasConfigClaude(home)
	if len(dirs) != 1 || dirs[0] != filepath.Join(roaming, "Claude") {
		t.Fatalf("sem nenhum Claude, o palpite é o clássico; veio %v", dirs)
	}
	if claudeInstalado(home) {
		t.Fatal("nada instalado não pode contar como Claude")
	}
}

// Ignora arquivos soltos dentro de Claude-Data: só pastas valem.
func TestPastasClaudeData_IgnoraArquivo(t *testing.T) {
	home, local, _ := ambienteFalso(t)
	mkdir(t, filepath.Join(local, "Claude-Data"))
	if err := os.WriteFile(filepath.Join(local, "Claude-Data", "lixo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pastasClaudeData(home); len(got) != 0 {
		t.Fatalf("arquivo não é pasta de dados: %v", got)
	}
}

// O registro acrescenta o lumnia-zap preservando o que o Claude já tinha no
// arquivo de Claude-Data, e grava em todos os caminhos encontrados.
func TestRegistrarNoClaude_PreservaEGravaEmTodos(t *testing.T) {
	home, local, _ := ambienteFalso(t)
	dados := mkdir(t, filepath.Join(local, "Claude-Data", "claude_pzs8sxrjxfjjc"))
	pkg := mkdir(t, filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming"))
	existente := `{"preferences":{"sidebarMode":"chat"},"mcpServers":{"outro":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(dados, "claude_desktop_config.json"), []byte(existente), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := registrarNoClaude(home, `C:\LumniaZap`, `C:\uv.exe`); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{dados, filepath.Join(pkg, "Claude")} {
		raw, err := os.ReadFile(filepath.Join(dir, "claude_desktop_config.json"))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
			t.Fatalf("%s: gravado com BOM", dir)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("%s: JSON inválido: %v", dir, err)
		}
		srv, _ := cfg["mcpServers"].(map[string]any)
		if _, ok := srv["lumnia-zap"]; !ok {
			t.Fatalf("%s: sem lumnia-zap: %s", dir, raw)
		}
		if dir == dados {
			if _, ok := srv["outro"]; !ok {
				t.Fatalf("perdeu o servidor que já existia: %s", raw)
			}
			if _, ok := cfg["preferences"]; !ok {
				t.Fatalf("perdeu as preferências do Claude: %s", raw)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dados, "claude_desktop_config.json.bak-lumnia-zap")); err != nil {
		t.Fatal("sem backup do arquivo original:", err)
	}
}
