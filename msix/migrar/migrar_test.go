// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package migrar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type falso struct {
	tarefa, portaLivre, claude bool
	apagou, encerrou, fechou   bool
	pausaDurante               bool
	localState                 string
}

func (f *falso) TarefaClassicaExiste() bool { return f.tarefa }
func (f *falso) ApagarTarefaClassica() error {
	f.apagou = true
	f.tarefa = false
	return nil
}
func (f *falso) EncerrarProcessosEm(string) error { f.encerrou = true; return nil }
func (f *falso) EncerrarPonteDaStore() error {
	_, err := os.Stat(ArquivoPausa(f.localState))
	f.pausaDurante = err == nil // a pausa precisa existir ANTES de derrubar a ponte da Store
	return nil
}
func (f *falso) PortaLivre(time.Duration) bool { return f.portaLivre }
func (f *falso) ClaudeAberto() bool            { return f.claude }
func (f *falso) FecharClaude() error           { f.fechou = true; return nil }

func montar(t *testing.T) Pastas {
	raiz := t.TempDir()
	p := Pastas{
		Classica:   filepath.Join(raiz, "LumniaZap"),
		LocalState: filepath.Join(raiz, "Packages", "Lumnia.LumniaZap_27md6avg2vwam", "LocalState"),
		Startup:    filepath.Join(raiz, "Startup"),
		AliasMCP:   `C:\Users\x\AppData\Local\Microsoft\WindowsApps\lumnia-zap-mcp.exe`,
	}
	_ = os.MkdirAll(p.storeClassico(), 0o755)
	_ = os.WriteFile(filepath.Join(p.storeClassico(), "messages.db"), []byte("historico-classico"), 0o644)
	_ = os.WriteFile(filepath.Join(p.storeClassico(), "whatsapp.db"), []byte("sessao"), 0o644)
	_ = os.MkdirAll(p.storePacote(), 0o755)
	_ = os.WriteFile(filepath.Join(p.storePacote(), "messages.db"), []byte("vazio-da-store"), 0o644)
	_ = os.MkdirAll(p.Startup, 0o755)
	_ = os.WriteFile(filepath.Join(p.Startup, "Lumnia Zap.lnk"), []byte("x"), 0o644)
	cfg := filepath.Join(raiz, "Claude")
	_ = os.MkdirAll(cfg, 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "claude_desktop_config.json"),
		[]byte("\xef\xbb\xbf"+`{"preferences":{"a":1},"mcpServers":{"lumnia-zap":{"command":"uv.exe","args":["--directory","C:\\LumniaZap\\whatsapp-mcp-server"]}}}`), 0o600)
	semEntrada := filepath.Join(raiz, "Claude-Data", "claude_x")
	_ = os.MkdirAll(semEntrada, 0o755)
	_ = os.WriteFile(filepath.Join(semEntrada, "claude_desktop_config.json"), []byte(`{"preferences":{}}`), 0o600)
	p.Configs = []string{cfg, semEntrada, filepath.Join(raiz, "naoexiste")}
	return p
}

func TestMigrarCaminhoFeliz(t *testing.T) {
	p := montar(t)
	s := &falso{tarefa: true, portaLivre: true, claude: true, localState: p.LocalState}
	if !Pendente(s, p) {
		t.Fatal("deveria haver migração pendente")
	}
	r, err := Migrar(s, p, time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !s.apagou || !s.encerrou || !s.fechou || !s.pausaDurante {
		t.Fatalf("passos do sistema: %+v", s)
	}
	b, _ := os.ReadFile(filepath.Join(p.storePacote(), "messages.db"))
	if string(b) != "historico-classico" {
		t.Fatalf("histórico não chegou: %q", b)
	}
	if bak, _ := os.ReadFile(filepath.Join(r.StoreAnterior, "messages.db")); string(bak) != "vazio-da-store" {
		t.Fatal("store da Store deveria virar backup, não sumir")
	}
	if _, err := os.Stat(filepath.Join(p.storeClassico(), "messages.db")); err != nil {
		t.Fatal("a pasta clássica tem de ficar intacta")
	}
	if _, err := os.Stat(filepath.Join(p.Startup, "Lumnia Zap.lnk")); !os.IsNotExist(err) {
		t.Fatal("atalho de inicialização da clássica deveria sair")
	}
	cfg, _ := os.ReadFile(filepath.Join(p.Configs[0], "claude_desktop_config.json"))
	if strings.HasPrefix(string(cfg), "\xef\xbb\xbf") || !strings.Contains(string(cfg), "lumnia-zap-mcp.exe") || !strings.Contains(string(cfg), `"preferences"`) {
		t.Fatalf("config repontada errada: %s", cfg)
	}
	if len(r.Configs) != 1 || len(r.ConfigsFalhas) != 0 {
		t.Fatalf("configs: %+v", r)
	}
	if _, err := os.Stat(ArquivoPausa(p.LocalState)); !os.IsNotExist(err) {
		t.Fatal("pausa tem de sair no fim")
	}
	var m Resultado
	raw, _ := os.ReadFile(p.marcador())
	if json.Unmarshal(raw, &m) != nil || m.BytesCopiados == 0 {
		t.Fatalf("marcador: %s", raw)
	}
	if Pendente(s, p) {
		t.Fatal("depois de migrar não pergunta de novo")
	}
}

func TestMigrarPortaOcupadaNaoCopia(t *testing.T) {
	p := montar(t)
	s := &falso{tarefa: true, portaLivre: false, localState: p.LocalState}
	if _, err := Migrar(s, p, time.Now()); err == nil {
		t.Fatal("deveria abortar")
	}
	b, _ := os.ReadFile(filepath.Join(p.storePacote(), "messages.db"))
	if string(b) != "vazio-da-store" {
		t.Fatal("nada pode ser copiado com a porta ocupada")
	}
	if _, err := os.Stat(p.marcador()); !os.IsNotExist(err) {
		t.Fatal("sem marcador quando aborta")
	}
}

func TestPendenteSoComClassica(t *testing.T) {
	raiz := t.TempDir()
	p := Pastas{Classica: filepath.Join(raiz, "nada"), LocalState: filepath.Join(raiz, "ls")}
	if Pendente(&falso{}, p) {
		t.Fatal("sem clássica, nada a migrar")
	}
	if !Pendente(&falso{tarefa: true}, p) {
		t.Fatal("só a tarefa já indica clássica")
	}
}

func TestPausaVencidaNaoTravaAPonte(t *testing.T) {
	ls := t.TempDir()
	if EmPausa(ls, time.Now()) {
		t.Fatal("sem arquivo, sem pausa")
	}
	_ = os.WriteFile(ArquivoPausa(ls), []byte("x"), 0o644)
	if !EmPausa(ls, time.Now()) {
		t.Fatal("pausa recente vale")
	}
	if EmPausa(ls, time.Now().Add(PausaMaxima+time.Minute)) {
		t.Fatal("pausa de migração interrompida não pode travar a ponte para sempre")
	}
	if _, err := os.Stat(ArquivoPausa(ls)); !os.IsNotExist(err) {
		t.Fatal("pausa vencida deveria ser removida")
	}
}
