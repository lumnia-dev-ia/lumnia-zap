// Suporte ao OpenAI Codex — registra o servidor MCP lumnia-zap no
// config.toml do Codex (~/.codex/config.toml, ou $CODEX_HOME/config.toml),
// preservando todo o resto do arquivo.
//
// Este arquivo é MANTIDO IDÊNTICO em installer/ e installer-windows/. Os dois
// instaladores são módulos Go separados de propósito (zero dependência entre
// eles), então a lógica comum vive duplicada — o mesmo padrão já usado por
// registrarNoClaude/gravarConfig. Se mudar aqui, mude lá. O que difere por
// sistema é só a detecção, em codex_detectar.go de cada módulo.
//
// Por que edição por seções em vez de uma biblioteca TOML: o config.toml é do
// USUÁRIO — pode ter comentários, outros servidores MCP e preferências em
// qualquer ordem. As bibliotecas TOML de Go (BurntSushi, pelletier/go-toml v2)
// descartam comentários ao reserializar, o que apagaria anotações da pessoa.
// Então o arquivo é tratado como texto: removemos apenas as seções e chaves do
// lumnia-zap, onde quer que estejam, e anexamos a versão nova no fim; todas as
// outras linhas passam intactas, byte a byte. Uma biblioteca TOML é usada SÓ
// nos testes (codex_test.go), para garantir que o resultado é TOML válido.
//
// Formato conforme a documentação oficial do Codex (developers.openai.com/codex):
//
//	[mcp_servers.<id>]              command/args do servidor stdio
//	[mcp_servers.<id>.tools.<tool>] approval_mode por ferramenta (Codex >= 0.144)
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexHome devolve a pasta de configuração do Codex, respeitando a variável
// CODEX_HOME documentada pela OpenAI (padrão: ~/.codex nos dois sistemas).
func codexHome(home string) string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

// registrarNoCodex adiciona/atualiza o servidor MCP lumnia-zap no config.toml
// do Codex. Idempotente: se o arquivo já está exatamente como deveria, não
// escreve nada (nem backup). Quando vai mexer num arquivo existente, guarda
// uma cópia em config.toml.bak-lumnia-zap antes — igual ao fluxo do Claude.
func registrarNoCodex(home, destino, uv string) error {
	dir := codexHome(home)
	cfgPath := filepath.Join(dir, "config.toml")

	atual, err := os.ReadFile(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("não consegui ler %s: %v", cfgPath, err)
	}

	novo := atualizarConfigCodex(atual, uv, filepath.Join(destino, "whatsapp-mcp-server"))
	if len(atual) > 0 && string(novo) == string(atual) {
		return nil // já está assim; não mexe
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if len(atual) > 0 {
		_ = os.WriteFile(cfgPath+".bak-lumnia-zap", atual, 0o600)
	}
	return os.WriteFile(cfgPath, novo, 0o600)
}

// atualizarConfigCodex recebe o conteúdo atual do config.toml (nil/vazio se o
// arquivo não existe) e devolve o novo conteúdo: tudo que não é do lumnia-zap
// preservado na ordem original, e as seções do lumnia-zap regravadas no fim.
func atualizarConfigCodex(atual []byte, uv, dirServidor string) []byte {
	base := strings.TrimRight(removerSecoesLumniaZap(string(atual)), " \t\r\n")
	bloco := blocoLumniaZap(uv, dirServidor)
	if base == "" {
		return []byte(bloco)
	}
	return []byte(base + "\n\n" + bloco)
}

// blocoLumniaZap monta as seções TOML do lumnia-zap. Os comentários ficam
// DEPOIS do cabeçalho da seção de propósito: assim pertencem à seção e são
// removidos junto com ela na próxima atualização, mantendo a operação
// idempotente.
func blocoLumniaZap(uv, dirServidor string) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.lumnia-zap]\n")
	b.WriteString("# Gerado pelo instalador do Lumnia Zap — seu WhatsApp local via MCP.\n")
	b.WriteString("# Rodar o instalador de novo recria/atualiza APENAS as seções lumnia-zap;\n")
	b.WriteString("# o resto deste arquivo é preservado.\n")
	fmt.Fprintf(&b, "command = %s\n", tomlStr(uv))
	fmt.Fprintf(&b, "args = [\"--directory\", %s, \"run\", \"main.py\"]\n", tomlStr(dirServidor))
	b.WriteString("\n")
	b.WriteString("# Ferramentas que ENVIAM algo pelo seu WhatsApp pedem confirmação antes de\n")
	b.WriteString("# executar; as de leitura seguem a política de aprovação normal do Codex.\n")
	b.WriteString("# (Aprovação por ferramenta existe a partir do Codex 0.144, de julho/2026.)\n")
	for i, t := range []string{"send_message", "send_file", "send_audio_message"} {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[mcp_servers.lumnia-zap.tools.%s]\napproval_mode = \"prompt\"\n", t)
	}
	return b.String()
}

// removerSecoesLumniaZap devolve o texto sem nada que pertença ao lumnia-zap:
//
//   - tabelas [mcp_servers.lumnia-zap] e subtabelas ([...tools.send_message]),
//     com aspas ou sem ([mcp_servers."lumnia-zap"]);
//   - a chave inline `lumnia-zap = {...}` dentro de uma tabela [mcp_servers];
//   - chaves pontilhadas na raiz, tipo `mcp_servers.lumnia-zap.command = ...`.
//
// Todas as outras linhas — inclusive comentários e linhas em branco — passam
// sem alteração.
func removerSecoesLumniaZap(texto string) string {
	if texto == "" {
		return ""
	}
	linhas := strings.Split(texto, "\n")
	out := make([]string, 0, len(linhas))
	var tabela []string // caminho da tabela atual; vazio = raiz
	pulando := false    // dentro de uma tabela do lumnia-zap

	for _, l := range linhas {
		if caminho, ok := cabecalhoTabela(l); ok {
			tabela = caminho
			pulando = ehLumniaZap(caminho)
			if pulando {
				continue
			}
			out = append(out, l)
			continue
		}
		if pulando {
			continue
		}
		if i := strings.Index(l, "="); i > 0 {
			chave := append(append([]string{}, tabela...), caminhoChave(strings.TrimSpace(l[:i]))...)
			if ehLumniaZap(chave) {
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// ehLumniaZap diz se um caminho de tabela/chave pertence ao nosso servidor.
func ehLumniaZap(caminho []string) bool {
	return len(caminho) >= 2 && caminho[0] == "mcp_servers" && caminho[1] == "lumnia-zap"
}

// cabecalhoTabela reconhece linhas [tabela] e [[tabela]] e devolve o caminho
// normalizado (sem aspas). Nomes de tabela contendo "]" dentro de aspas são
// um caso patológico que não tratamos — nenhum cliente MCP gera isso.
func cabecalhoTabela(linha string) ([]string, bool) {
	t := strings.TrimSpace(linha)
	if !strings.HasPrefix(t, "[") {
		return nil, false
	}
	t = strings.TrimPrefix(t, "[")
	t = strings.TrimPrefix(t, "[") // [[array de tabelas]]
	fim := strings.Index(t, "]")
	if fim < 0 {
		return nil, false
	}
	return caminhoChave(t[:fim]), true
}

// caminhoChave separa um caminho TOML pontilhado em partes, respeitando
// aspas: `mcp_servers."lumnia-zap".tools` vira [mcp_servers lumnia-zap tools].
func caminhoChave(s string) []string {
	var partes []string
	var atual strings.Builder
	var aspas byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case aspas != 0:
			if c == aspas {
				aspas = 0
			} else {
				atual.WriteByte(c)
			}
		case c == '"' || c == '\'':
			aspas = c
		case c == '.':
			partes = append(partes, strings.TrimSpace(atual.String()))
			atual.Reset()
		default:
			atual.WriteByte(c)
		}
	}
	partes = append(partes, strings.TrimSpace(atual.String()))
	return partes
}

// tomlStr codifica uma string como TOML basic string. Essencial no Windows,
// onde caminhos têm contrabarras que precisam de escape ("C:\\Users\\...").
func tomlStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
