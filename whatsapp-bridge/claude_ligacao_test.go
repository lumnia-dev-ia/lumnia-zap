// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPanelMetaDBIDEstavel(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo }()
	if err := panelMetaCriar(panel.store.db); err != nil {
		t.Fatal(err)
	}
	id := panelMetaGet("db_id")
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("db_id: %q", id)
	}
	_ = panelMetaCriar(panel.store.db) // segunda subida não troca o id
	if panelMetaGet("db_id") != id {
		t.Fatal("db_id mudou numa segunda subida")
	}
	panelMetaSet("x", "1")
	panelMetaSet("x", "2")
	if panelMetaGet("x") != "2" {
		t.Fatal("panelMetaSet não atualiza")
	}
}

func TestSinalDoClaudeEMesmoBanco(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo; ligacao.sinal = sinalClaude{}; ligacao.teste = nil }()
	_ = panelMetaCriar(panel.store.db)
	meu := panelMetaGet("db_id")
	ligacao.teste = &testeClaude{Codigo: "4821", Inicio: time.Now(), Enviada: true}

	post := func(corpo, tipo string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/panel/claude-sinal", strings.NewReader(corpo))
		r.Header.Set("Content-Type", tipo)
		w := httptest.NewRecorder()
		claudeSinalHandler(w, r)
		return w.Code
	}
	if post(`{"db_id":"x"}`, "text/plain") == http.StatusOK {
		t.Fatal("sem application/json não pode passar (CSRF de página aberta no navegador)")
	}
	if post(`{"db_id":"`+meu+`","ferramenta":"list_messages","teste_codigo":"4821"}`, "application/json") != http.StatusOK {
		t.Fatal("sinal recusado")
	}
	resp := map[string]interface{}{}
	ligacaoStatus(resp)
	c := resp["claude"].(map[string]interface{})
	if c["mesmo_banco"] != true || c["ferramenta"] != "list_messages" {
		t.Fatalf("claude: %v", c)
	}
	if tr := resp["teste"].(map[string]interface{}); tr["achou"] != true {
		t.Fatalf("teste: %v", tr)
	}
	// sinal de outro banco (instalação antiga): vermelho
	ligacao.sinal = sinalClaude{}
	post(`{"db_id":"outro","ferramenta":"list_chats","teste_codigo":"4821"}`, "application/json")
	resp = map[string]interface{}{}
	ligacaoStatus(resp)
	if resp["claude"].(map[string]interface{})["mesmo_banco"] != false {
		t.Fatal("banco diferente deveria aparecer")
	}
	if resp["teste"].(map[string]interface{})["outro_banco"] != true {
		t.Fatal("teste achado em outro banco deveria ficar vermelho")
	}
}

func TestEstadoConfigClaude(t *testing.T) {
	servidor := "/Users/x/Library/Application Support/LumniaZap/whatsapp-mcp-server"
	escrever := func(dir, conteudo string) {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "claude_desktop_config.json"), []byte(conteudo), 0o600)
	}
	ok, sem, outra, vazio := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	escrever(ok, `{"mcpServers":{"lumnia-zap":{"command":"uv","args":["--directory","`+servidor+`","run","main.py"]}}}`)
	escrever(sem, `{"preferences":{}}`)
	escrever(outra, `{"mcpServers":{"lumnia-zap":{"command":"uv","args":["--directory","/Users/x/Developer/lumnia-zap/whatsapp-mcp-server","run","main.py"]}}}`)
	casos := []struct {
		pastas []string
		quer   string
	}{
		{[]string{sem, ok}, "ok"},
		{[]string{sem}, "sem_entrada"},
		{[]string{outra, sem}, "outra_instalacao"},
		{[]string{vazio}, "sem_claude"},
		{nil, "nao_se_aplica"},
	}
	for _, c := range casos {
		if got, _ := estadoConfigClaude(c.pastas, servidor); got != c.quer {
			t.Errorf("%v: %s, queria %s", c.pastas, got, c.quer)
		}
	}
}

func TestRepararConfigPreservaEApontaParaEstaInstalacao(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "claude_desktop_config.json")
	_ = os.WriteFile(cfg, []byte("\xef\xbb\xbf"+`{"preferences":{"sidebarMode":"chat"},"mcpServers":{"outro":{"command":"x"}}}`), 0o600)
	feitas, err := repararConfigClaude([]string{dir}, "/srv/whatsapp-mcp-server", "/bin/uv")
	if err != nil || len(feitas) != 1 {
		t.Fatalf("%v %v", feitas, err)
	}
	if e, _ := estadoConfigClaude([]string{dir}, "/srv/whatsapp-mcp-server"); e != "ok" {
		t.Fatalf("depois de reparar: %s", e)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "sidebarMode") || !strings.Contains(string(b), `"outro"`) {
		t.Fatalf("reparar apagou o resto do arquivo: %s", b)
	}
	if _, err := os.Stat(cfg + ".bak-lumnia-zap"); err != nil {
		t.Fatal("sem backup")
	}
}

func TestRepararRecusaComClaudeAberto(t *testing.T) {
	antes := claudeAbertoPonte
	defer func() { claudeAbertoPonte = antes }()
	claudeAbertoPonte = func() bool { return true }
	r := httptest.NewRequest(http.MethodPost, "/api/panel/claude-reparar", nil)
	r.Header.Set("X-Lumnia-Painel", "1")
	w := httptest.NewRecorder()
	claudeRepararHandler(w, r)
	if instalacaoTipo() == "linux" {
		if w.Code != http.StatusBadRequest {
			t.Fatalf("no Linux não há config do Claude Desktop: %d", w.Code)
		}
		return
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("com o Claude aberto deveria recusar: %d", w.Code)
	}
}

func TestTesteExpiraEOnboarding(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo; ligacao.teste = nil }()
	_ = panelMetaCriar(panel.store.db)
	ligacao.teste = &testeClaude{Codigo: "1234", Inicio: time.Now().Add(-11 * time.Minute)}
	if testeResumo() != nil {
		t.Fatal("teste de mais de 10 min deveria sumir")
	}
	_, _ = panel.store.db.Exec(`INSERT INTO messages (id, chat_jid, content, timestamp, is_from_me) VALUES ('a','x@s.whatsapp.net',?, ?, 1)`,
		testeClaudePrefixo+"5555", time.Now())
	ligacao.teste = &testeClaude{Codigo: "5555", Inicio: time.Now(), Enviada: true}
	if tr := testeResumo(); tr["gravada"] != true || !strings.Contains(tr["pedido"].(string), "5555") {
		t.Fatalf("teste: %v", tr)
	}
	if o := onboardingResumo(); o["concluido"] != false || o["mensagens"] != 1 {
		t.Fatalf("onboarding: %v", o)
	}
	panelMetaSet("onboarding_concluido", "1")
	if onboardingResumo()["concluido"] != true {
		t.Fatal("concluir não gravou")
	}
	if c := novoCodigoTeste(); !codigoTesteRe.MatchString(c) {
		t.Fatalf("código: %q", c)
	}
}

func TestSinalDoMordomoNaoContaComoClaude(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo; ligacao.sinal = sinalClaude{} }()
	_ = panelMetaCriar(panel.store.db)
	ligacao.sinal = sinalClaude{}
	post := func(corpo string) {
		r := httptest.NewRequest(http.MethodPost, "/api/panel/claude-sinal", strings.NewReader(corpo))
		r.Header.Set("Content-Type", "application/json")
		claudeSinalHandler(httptest.NewRecorder(), r)
	}
	post(`{"ferramenta":"list_messages","cliente":"beatitude"}`)
	resp := map[string]interface{}{}
	ligacaoStatus(resp)
	if _, leu := resp["claude"].(map[string]interface{})["ultimo_sinal_secs"]; leu {
		t.Fatal("leitura do mordomo do Beatitude não pode acender o cartão do Claude")
	}
	post(`{"ferramenta":"list_chats","cliente":"claude-ai"}`)
	resp = map[string]interface{}{}
	ligacaoStatus(resp)
	if resp["claude"].(map[string]interface{})["cliente"] != "claude-ai" {
		t.Fatalf("claude: %v", resp["claude"])
	}
}

func TestOnboardingConcluidoNaoContaOBanco(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo; ligacaoCacheZerar() }()
	_ = panelMetaCriar(panel.store.db)
	panelMetaSet("onboarding_concluido", "1")
	if o := onboardingResumo(); o["concluido"] != true || o["mensagens"] != nil {
		t.Fatalf("onboarding concluído: %v", o)
	}
}
