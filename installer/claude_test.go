package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Monta uma pasta de configuracao do Claude com o conteudo dado.
func semearConfig(t *testing.T, dir, conteudo string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	alvo := filepath.Join(dir, arquivoConfigClaude)
	if err := os.WriteFile(alvo, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lerConfig(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, arquivoConfigClaude))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("config inválido: %v", err)
	}
	return cfg
}

func servidorLumnia(t *testing.T, dir string) map[string]any {
	t.Helper()
	cfg := lerConfig(t, dir)
	servidores, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers ausente em %s", dir)
	}
	srv, ok := servidores["lumnia-zap"].(map[string]any)
	if !ok {
		t.Fatalf("lumnia-zap ausente em %s", dir)
	}
	return srv
}

// Mac limpo: so a pasta canonica, criada na hora.
func TestConfigsDoClaudeCanonica(t *testing.T) {
	home := t.TempDir()
	pastas := configsDoClaude(home)
	if len(pastas) != 1 {
		t.Fatalf("esperava 1 pasta, veio %d: %v", len(pastas), pastas)
	}
	esperado := filepath.Join(home, "Library", "Application Support", "Claude")
	if pastas[0] != esperado {
		t.Fatalf("esperava %s, veio %s", esperado, pastas[0])
	}
}

// O caso que motivou tudo: duas instalacoes do Claude lado a lado.
func TestConfigsDoClaudeAchaInstalacaoParalela(t *testing.T) {
	home := t.TempDir()
	suporte := filepath.Join(home, "Library", "Application Support")
	paralela := filepath.Join(suporte, "Claude Desktop")
	semearConfig(t, paralela, `{"preferences":{"tema":"escuro"}}`)

	// Pasta com prefixo Claude mas SEM config nao conta.
	if err := os.MkdirAll(filepath.Join(suporte, "ClaudeSobras"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pasta de outro app tambem nao.
	semearConfig(t, filepath.Join(suporte, "OutroApp"), `{}`)

	pastas := configsDoClaude(home)
	if len(pastas) != 2 {
		t.Fatalf("esperava 2 pastas, veio %d: %v", len(pastas), pastas)
	}
	var achou bool
	for _, p := range pastas {
		if p == paralela {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("não achou a instalação paralela: %v", pastas)
	}
}

// Versao em sandbox (App Store) vive sob ~/Library/Containers.
func TestConfigsDoClaudeAchaSandbox(t *testing.T) {
	home := t.TempDir()
	sandbox := filepath.Join(home, "Library", "Containers", "com.anthropic.claude",
		"Data", "Library", "Application Support", "Claude")
	semearConfig(t, sandbox, `{}`)

	pastas := configsDoClaude(home)
	var achou bool
	for _, p := range pastas {
		if p == sandbox {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("não achou a instalação em sandbox: %v", pastas)
	}
}

// Grava nas duas instalacoes e preserva o que ja estava em cada uma.
func TestRegistrarNoClaudeGravaEmTodasEPreserva(t *testing.T) {
	home := t.TempDir()
	suporte := filepath.Join(home, "Library", "Application Support")
	canonica := filepath.Join(suporte, "Claude")
	paralela := filepath.Join(suporte, "Claude Desktop")

	semearConfig(t, canonica, `{"preferences":{"idioma":"pt-BR"},
		"mcpServers":{"outro-mcp":{"command":"/bin/echo"}}}`)
	semearConfig(t, paralela, `{"preferences":{"tema":"escuro"}}`)

	destino := filepath.Join(home, "Library", "Application Support", "LumniaZap")
	uv := filepath.Join(home, ".local", "bin", "uv")

	gravadas, err := registrarNoClaude(home, destino, uv)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(gravadas) != 2 {
		t.Fatalf("esperava gravar em 2, gravou em %d: %v", len(gravadas), gravadas)
	}

	for _, dir := range []string{canonica, paralela} {
		srv := servidorLumnia(t, dir)
		if srv["command"] != uv {
			t.Errorf("%s: command errado: %v", dir, srv["command"])
		}
		args, _ := srv["args"].([]any)
		if len(args) != 4 || args[1] != filepath.Join(destino, "whatsapp-mcp-server") {
			t.Errorf("%s: args errados: %v", dir, args)
		}
		// As preferencias do usuario nao podem ter sumido.
		if _, ok := lerConfig(t, dir)["preferences"]; !ok {
			t.Errorf("%s: preferences foi apagado", dir)
		}
	}

	// Outro MCP que ja existia continua la.
	servidores := lerConfig(t, canonica)["mcpServers"].(map[string]any)
	if _, ok := servidores["outro-mcp"]; !ok {
		t.Error("canônica: outro-mcp foi apagado")
	}
}

// Rodar de novo nao duplica nem estraga nada.
func TestRegistrarNoClaudeIdempotente(t *testing.T) {
	home := t.TempDir()
	destino := filepath.Join(home, "LumniaZap")
	uv := "/usr/local/bin/uv"

	for i := 0; i < 3; i++ {
		if _, err := registrarNoClaude(home, destino, uv); err != nil {
			t.Fatalf("passada %d: %v", i, err)
		}
	}
	canonica := filepath.Join(home, "Library", "Application Support", "Claude")
	servidores := lerConfig(t, canonica)["mcpServers"].(map[string]any)
	if len(servidores) != 1 {
		t.Fatalf("esperava 1 servidor, veio %d: %v", len(servidores), servidores)
	}
}

// Config corrompido em UMA instalacao nao pode derrubar a instalacao inteira.
func TestRegistrarNoClaudeTolerauMaCorrompida(t *testing.T) {
	home := t.TempDir()
	suporte := filepath.Join(home, "Library", "Application Support")
	semearConfig(t, filepath.Join(suporte, "Claude Desktop"), `{ isto nao e json`)

	gravadas, err := registrarNoClaude(home, filepath.Join(home, "LumniaZap"), "/bin/uv")
	if err != nil {
		t.Fatalf("não devia falhar: %v", err)
	}
	if len(gravadas) != 1 {
		t.Fatalf("esperava 1 gravação (a canônica), veio %d: %v", len(gravadas), gravadas)
	}
}

// O diario nao pode vazar nada alem de caminhos — e precisa citar as pastas.
func TestDiarioRegistraOndeGravou(t *testing.T) {
	diario = nil
	t.Cleanup(func() { diario = nil })

	home := t.TempDir()
	if _, err := registrarNoClaude(home, filepath.Join(home, "LumniaZap"), "/bin/uv"); err != nil {
		t.Fatal(err)
	}
	texto := diarioTexto()
	if !strings.Contains(texto, "Application Support/Claude") {
		t.Fatalf("o diário não diz onde gravou:\n%s", texto)
	}
}

// detectarClaude nao pode dizer "nao tem Claude" num Mac que tem config gravado.
func TestDetectarClaudePorConfigExistente(t *testing.T) {
	home := t.TempDir()
	semearConfig(t, filepath.Join(home, "Library", "Application Support", "Claude"), `{}`)
	if !detectarClaude(home) {
		t.Fatal("devia ter detectado o Claude pela configuração existente")
	}
}
