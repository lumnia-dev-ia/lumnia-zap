// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

// O Claude Desktop guarda as próprias preferências no MESMO
// claude_desktop_config.json em que o instalador registra o servidor MCP, e
// regrava o arquivo inteiro a partir do que tem na memória. Gravar com o
// Claude aberto e só depois pedir Cmd+Q deixava a entrada lumnia-zap à mercê
// da próxima gravação do app — inclusive a do próprio Cmd+Q (doc 12, 12/11:
// Mac mini com a ponte no ar e a config sem mcpServers). Daí a ordem nova:
// fechar o Claude ANTES de gravar, conferir depois, e só então pedir para
// abrir.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// claudeAbertoFn e fecharClaudeFn são variáveis para os testes trocarem.
var claudeAbertoFn = func() bool {
	// -x: só o processo principal "Claude" (os "Claude Helper" não casam;
	// o Claude Code de linha de comando chama "claude", em minúsculas).
	return exec.Command("/usr/bin/pgrep", "-x", "Claude").Run() == nil
}

var fecharClaudeFn = func() error {
	_, err := osa(`tell application "Claude" to quit`)
	return err
}

var esperaFecharClaude = 30 * time.Second

// garantirClaudeFechado: se o Claude está aberto, explica e pede para fechar.
// "Fechar o Claude para mim" manda o quit (o Claude pode pedir confirmação se
// houver algo rodando); "Cancelar" encerra a instalação sem mexer em nada.
func garantirClaudeFechado() error {
	if !claudeAbertoFn() {
		return nil
	}
	anotar("Claude aberto: pedindo para fechar antes de gravar a configuração")
	if !perguntarBotoes("Feche o Claude antes de continuar",
		"O Claude está aberto. Para ele enxergar o WhatsApp, eu preciso configurá-lo com o Claude fechado; "+
			"com ele aberto, a configuração pode se perder.\n\n"+
			"Se houver uma conversa no meio de uma tarefa, termine antes. Depois eu aviso quando puder abrir de novo.",
		"Cancelar", "Fechar o Claude para mim") {
		return fmt.Errorf("instalação cancelada: o Claude precisa estar fechado. Feche o Claude (Cmd+Q) e rode o instalador de novo")
	}
	if err := fecharClaudeFn(); err != nil {
		anotar("Pedido de quit ao Claude falhou: %v", err)
	}
	limite := time.Now().Add(esperaFecharClaude)
	for time.Now().Before(limite) {
		if !claudeAbertoFn() {
			anotar("Claude fechado")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("o Claude não fechou. Feche o Claude com Cmd+Q (confirme se ele perguntar) e rode o instalador de novo")
}

// gravarJSONAtomico grava num temporário ao lado e troca com rename: o
// Claude nunca lê um arquivo pela metade.
func gravarJSONAtomico(caminho string, dados []byte) error {
	tmp := caminho + ".lumnia-novo"
	if err := os.WriteFile(tmp, dados, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, caminho); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// backupUmaVez guarda a configuração como estava ANTES da primeira instalação
// do Zap. Reinstalações não sobrescrevem: o original é o que vale guardar.
func backupUmaVez(cfgPath string, raw []byte) {
	bak := cfgPath + ".bak-lumnia-zap"
	if _, err := os.Stat(bak); err == nil {
		return
	}
	_ = os.WriteFile(bak, raw, 0o600)
}

// configTemLumnia diz se a configuração da pasta tem a entrada lumnia-zap.
func configTemLumnia(cfgDir string) bool {
	raw, err := os.ReadFile(filepath.Join(cfgDir, arquivoConfigClaude))
	if err != nil {
		return false
	}
	var cfg map[string]any
	if json.Unmarshal(bytes.TrimSpace(raw), &cfg) != nil {
		return false
	}
	srv, _ := cfg["mcpServers"].(map[string]any)
	_, ok := srv["lumnia-zap"]
	return ok
}

// conferirRegistros relê as configurações gravadas e devolve as que perderam
// a entrada. Chamado no fim da instalação, uns 40 s depois de gravar.
func conferirRegistros(pastas []string) []string {
	var perdidas []string
	for _, d := range pastas {
		if !configTemLumnia(d) {
			perdidas = append(perdidas, d)
		}
	}
	return perdidas
}

// nomeDoComputador: "Mac mini do Diego" (ComputerName) ou o hostname. Vai na
// tela final e no relatório: quem tem dois computadores precisa saber em qual
// instalou (doc 02, caso do Thiago).
func nomeDoComputador() string {
	if out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			return n
		}
	}
	h, _ := os.Hostname()
	return h
}

// perguntarBotoes: alerta com dois botões; true = o da direita (padrão).
func perguntarBotoes(titulo, msg, cancelar, continuar string) bool {
	_, err := osa(fmt.Sprintf(
		`display alert "%s" message "%s" buttons {"%s","%s"} default button "%s" cancel button "%s"`,
		escapar(titulo), escapar(msg), escapar(cancelar), escapar(continuar), escapar(continuar), escapar(cancelar)))
	return err == nil
}

// arrumarPastaDaPonte trata os dois restos do incidente de 22–24/09 antes da
// extração:
//   - um ARQUIVO chamado whatsapp-bridge no lugar da pasta (o binário copiado
//     por cima): vai para whatsapp-bridge.binario-<data>, senão a extração
//     falha com "not a directory";
//   - histórico em whatsapp-bridge.antigo/store sem whatsapp-bridge/store:
//     volta para o lugar, senão a ponte cria um store/ vazio e o histórico
//     some de vista em silêncio.
func arrumarPastaDaPonte(destino string) error {
	ponte := filepath.Join(destino, "whatsapp-bridge")
	if st, err := os.Lstat(ponte); err == nil && !st.IsDir() {
		novo := ponte + ".binario-" + time.Now().Format("20060102-150405")
		if err := os.Rename(ponte, novo); err != nil {
			return fmt.Errorf("há um arquivo whatsapp-bridge no lugar da pasta da ponte e não consegui movê-lo (%v). "+
				"Renomeie %s e rode o instalador de novo", err, ponte)
		}
		anotar("Arquivo solto no lugar da pasta da ponte movido para %s", filepath.Base(novo))
	}
	antigo := filepath.Join(destino, "whatsapp-bridge.antigo", "store")
	atual := filepath.Join(ponte, "store")
	if st, err := os.Stat(antigo); err == nil && st.IsDir() {
		if _, err := os.Stat(atual); os.IsNotExist(err) {
			if err := os.MkdirAll(ponte, 0o755); err != nil {
				return err
			}
			if err := os.Rename(antigo, atual); err != nil {
				return fmt.Errorf("o histórico do WhatsApp está em %s, fora do lugar, e não consegui movê-lo (%v). "+
					"Mova essa pasta para %s e rode o instalador de novo", antigo, err, atual)
			}
			anotar("Histórico recuperado de whatsapp-bridge.antigo/store para whatsapp-bridge/store")
		}
	}
	return nil
}
