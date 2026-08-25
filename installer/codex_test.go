package main

// Testes do registro no Codex. A biblioteca TOML entra AQUI, e só aqui, para
// provar que o texto gerado é TOML válido — o código de produção edita o
// arquivo como texto justamente para preservar comentários do usuário.
//
// Este arquivo é MANTIDO IDÊNTICO em installer/ e installer-windows/.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const (
	uvTeste  = "/Users/alguem/.local/bin/uv"
	dirTeste = "/Users/alguem/Library/Application Support/LumniaZap/whatsapp-mcp-server"
)

// parse valida que o resultado é TOML e devolve a árvore para inspeção.
func parse(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("o resultado não é TOML válido: %v\n---\n%s", err, b)
	}
	return m
}

// servidor navega até mcp_servers.lumnia-zap na árvore parseada.
func servidor(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	srvs, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("faltou a tabela mcp_servers")
	}
	s, ok := srvs["lumnia-zap"].(map[string]any)
	if !ok {
		t.Fatal("faltou mcp_servers.lumnia-zap")
	}
	return s
}

func conferirServidor(t *testing.T, m map[string]any) {
	t.Helper()
	s := servidor(t, m)
	if s["command"] != uvTeste {
		t.Errorf("command = %v, esperava %q", s["command"], uvTeste)
	}
	args, _ := s["args"].([]any)
	if len(args) != 4 || args[0] != "--directory" || args[1] != dirTeste ||
		args[2] != "run" || args[3] != "main.py" {
		t.Errorf("args errados: %v", args)
	}
	tools, _ := s["tools"].(map[string]any)
	for _, nome := range []string{"send_message", "send_file", "send_audio_message"} {
		tool, _ := tools[nome].(map[string]any)
		if tool == nil || tool["approval_mode"] != "prompt" {
			t.Errorf("faltou approval_mode=prompt em tools.%s (tools=%v)", nome, tools)
		}
	}
}

// Cenário 1: não existe config.toml — cria configuração válida.
func TestCodexConfigInexistente(t *testing.T) {
	novo := atualizarConfigCodex(nil, uvTeste, dirTeste)
	conferirServidor(t, parse(t, novo))
}

// Cenário 2: configuração existente com outras preferências, comentários e
// outros MCPs — tudo preservado, lumnia-zap adicionado.
func TestCodexPreservaConfigExistente(t *testing.T) {
	existente := `# minha config do codex — não mexa!
model = "gpt-5.2-codex"
approval_policy = "on-request"

[sandbox_workspace_write]
network_access = true

# meu servidor de banco de dados
[mcp_servers.postgres]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-postgres"]

[mcp_servers.github]
command = "github-mcp"
`
	novo := atualizarConfigCodex([]byte(existente), uvTeste, dirTeste)
	m := parse(t, novo)
	conferirServidor(t, m)

	if m["model"] != "gpt-5.2-codex" || m["approval_policy"] != "on-request" {
		t.Error("perdeu preferências de topo do usuário")
	}
	srvs := m["mcp_servers"].(map[string]any)
	if _, ok := srvs["postgres"]; !ok {
		t.Error("perdeu o MCP postgres do usuário")
	}
	if _, ok := srvs["github"]; !ok {
		t.Error("perdeu o MCP github do usuário")
	}
	for _, comentario := range []string{"# minha config do codex — não mexa!", "# meu servidor de banco de dados"} {
		if !strings.Contains(string(novo), comentario) {
			t.Errorf("perdeu o comentário do usuário: %s", comentario)
		}
	}
}

// Cenário 3: lumnia-zap já existe com caminho antigo — atualiza só ele.
func TestCodexAtualizaCaminhoAntigo(t *testing.T) {
	existente := `[mcp_servers.lumnia-zap]
command = "/caminho/antigo/uv"
args = ["--directory", "/caminho/antigo/whatsapp-mcp-server", "run", "main.py"]

[mcp_servers.outro]
command = "outro-mcp"
`
	novo := atualizarConfigCodex([]byte(existente), uvTeste, dirTeste)
	m := parse(t, novo)
	conferirServidor(t, m)
	if strings.Contains(string(novo), "/caminho/antigo") {
		t.Error("sobrou o caminho antigo no arquivo")
	}
	if _, ok := m["mcp_servers"].(map[string]any)["outro"]; !ok {
		t.Error("perdeu o MCP vizinho ao atualizar o lumnia-zap")
	}
}

// Cenário 4: idempotência — rodar duas (ou dez) vezes não duplica nada.
func TestCodexIdempotente(t *testing.T) {
	base := []byte("model = \"gpt-5.2-codex\"\n\n[mcp_servers.postgres]\ncommand = \"npx\"\n")
	umaVez := atualizarConfigCodex(base, uvTeste, dirTeste)
	deznove := umaVez
	for i := 0; i < 9; i++ {
		deznove = atualizarConfigCodex(deznove, uvTeste, dirTeste)
	}
	if string(umaVez) != string(deznove) {
		t.Fatalf("não é idempotente:\n--- 1x ---\n%s\n--- 10x ---\n%s", umaVez, deznove)
	}
	if n := strings.Count(string(deznove), "[mcp_servers.lumnia-zap]"); n != 1 {
		t.Fatalf("seção duplicada: apareceu %d vezes", n)
	}
}

// Variações de grafia que também precisam ser reconhecidas como nossas:
// nome entre aspas e chave inline dentro de [mcp_servers].
func TestCodexRemoveGrafiasAlternativas(t *testing.T) {
	casos := []string{
		"[mcp_servers.\"lumnia-zap\"]\ncommand = \"/velho/uv\"\n",
		"[mcp_servers]\nlumnia-zap = { command = \"/velho/uv\", args = [] }\n",
		"mcp_servers.lumnia-zap.command = \"/velho/uv\"\n",
	}
	for _, existente := range casos {
		novo := atualizarConfigCodex([]byte(existente), uvTeste, dirTeste)
		if strings.Contains(string(novo), "/velho/uv") {
			t.Errorf("não removeu a forma antiga:\n--- entrada ---\n%s--- saída ---\n%s", existente, novo)
			continue
		}
		conferirServidor(t, parse(t, novo))
	}
}

// Caminhos com contrabarra (Windows) e aspas têm de virar TOML válido.
func TestCodexCaminhoWindows(t *testing.T) {
	uvWin := `C:\Users\João d'Ávila\.local\bin\uv.exe`
	dirWin := `C:\Users\João d'Ávila\AppData\Local\LumniaZap\whatsapp-mcp-server`
	novo := atualizarConfigCodex(nil, uvWin, dirWin)
	m := parse(t, novo)
	s := servidor(t, m)
	if s["command"] != uvWin {
		t.Errorf("command = %v, esperava %q", s["command"], uvWin)
	}
	if args, _ := s["args"].([]any); len(args) != 4 || args[1] != dirWin {
		t.Errorf("args errados: %v", s["args"])
	}
}

// registrarNoCodex de ponta a ponta: cria, faz backup só quando muda algo e
// não reescreve quando já está certo.
func TestRegistrarNoCodex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	cfg := filepath.Join(dir, "config.toml")

	// primeira instalação: arquivo não existe
	if err := registrarNoCodex("/home/qualquer", "/Users/alguem/Library/Application Support/LumniaZap", uvTeste); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parse(t, b1)
	if _, err := os.Stat(cfg + ".bak-lumnia-zap"); err == nil {
		t.Error("gerou backup sem existir arquivo anterior")
	}

	// segunda rodada: nada muda, nem backup aparece
	antes, _ := os.Stat(cfg)
	if err := registrarNoCodex("/home/qualquer", "/Users/alguem/Library/Application Support/LumniaZap", uvTeste); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(cfg)
	if string(b1) != string(b2) {
		t.Error("reinstalar mudou o arquivo")
	}
	if _, err := os.Stat(cfg + ".bak-lumnia-zap"); err == nil {
		t.Error("gerou backup numa rodada que não mudou nada")
	}
	depois, _ := os.Stat(cfg)
	if !antes.ModTime().Equal(depois.ModTime()) {
		t.Error("reescreveu o arquivo sem necessidade")
	}

	// usuário mexeu no arquivo: atualização preserva e faz backup
	seu := "# nota do usuário\nmodel = \"gpt-5.2-codex\"\n" + string(b2)
	os.WriteFile(cfg, []byte(seu), 0o600)
	if err := registrarNoCodex("/home/qualquer", "/Users/alguem/Library/Application Support/LumniaZap", uvTeste); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b3), "# nota do usuário") {
		t.Error("perdeu a edição do usuário")
	}
	bak, err := os.ReadFile(cfg + ".bak-lumnia-zap")
	if err != nil || string(bak) != seu {
		t.Error("backup não guardou o arquivo como estava antes")
	}
	conferirServidor(t, parse(t, b3))
}

func TestTomlStr(t *testing.T) {
	casos := map[string]string{
		`simples`:            `"simples"`,
		`C:\Users\uv.exe`:    `"C:\\Users\\uv.exe"`,
		`com "aspas" dentro`: `"com \"aspas\" dentro"`,
		"tab\taqui":          `"tab\taqui"`,
	}
	for entrada, esperado := range casos {
		if got := tomlStr(entrada); got != esperado {
			t.Errorf("tomlStr(%q) = %s, esperava %s", entrada, got, esperado)
		}
	}
}
