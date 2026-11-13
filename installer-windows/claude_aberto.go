// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

// Mesma lição do macOS (doc 12): o Claude Desktop guarda as preferências no
// mesmo claude_desktop_config.json e o regrava a partir da memória. Gravar
// com ele aberto deixa a entrada lumnia-zap à mercê da próxima gravação do
// app. No Windows há um agravante: o X só esconde o Claude na bandeja, então
// "fechado" de verdade é sair pela bandeja ou encerrar o processo.

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

var claudeAbertoFn = func() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq Claude.exe", "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "claude.exe")
}

// Encerrar pelo processo: um WM_CLOSE só manda o Claude para a bandeja.
var fecharClaudeFn = func() error {
	return exec.Command("taskkill", "/IM", "Claude.exe", "/T", "/F").Run()
}

var esperaFecharClaude = 20 * time.Second

func garantirClaudeFechado() error {
	if !claudeAbertoFn() {
		return nil
	}
	fmt.Println()
	fmt.Println("O Claude está aberto. Para ele enxergar o WhatsApp, eu preciso configurá-lo")
	fmt.Println("com o Claude fechado; com ele aberto, a configuração pode se perder.")
	fmt.Println("Se houver uma conversa no meio de uma tarefa, termine antes.")
	if !confirmar("Fechar o Claude agora?", true) {
		return fmt.Errorf("o Claude precisa estar fechado. Saia do Claude pela bandeja (botão direito no ícone perto do relógio → Sair) e rode o instalador de novo")
	}
	_ = fecharClaudeFn()
	limite := time.Now().Add(esperaFecharClaude)
	for time.Now().Before(limite) {
		if !claudeAbertoFn() {
			fmt.Println("     Claude fechado.")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("o Claude não fechou. Saia pela bandeja (botão direito → Sair) e rode o instalador de novo")
}

func gravarJSONAtomico(caminho string, dados []byte) error {
	tmp := caminho + ".lumnia-novo"
	if err := os.WriteFile(tmp, dados, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, caminho); err != nil { // no Windows o Go troca com MoveFileEx(REPLACE_EXISTING)
		os.Remove(tmp)
		return err
	}
	return nil
}

func backupUmaVez(cfgPath string, raw []byte) {
	bak := cfgPath + ".bak-lumnia-zap"
	if _, err := os.Stat(bak); err == nil {
		return
	}
	_ = os.WriteFile(bak, raw, 0o600)
}

func configTemLumnia(cfgDir string) bool {
	raw, err := os.ReadFile(filepath.Join(cfgDir, "claude_desktop_config.json"))
	if err != nil {
		return false
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) // BOM, se alguém editou à mão
	var cfg map[string]any
	if json.Unmarshal(bytes.TrimSpace(raw), &cfg) != nil {
		return false
	}
	srv, _ := cfg["mcpServers"].(map[string]any)
	_, ok := srv["lumnia-zap"]
	return ok
}

func conferirRegistros(pastas []string) []string {
	var perdidas []string
	for _, d := range pastas {
		if !configTemLumnia(d) {
			perdidas = append(perdidas, d)
		}
	}
	return perdidas
}

func nomeDoComputador() string {
	if n := os.Getenv("COMPUTERNAME"); n != "" {
		return n
	}
	h, _ := os.Hostname()
	return h
}

// pacotesDaStore: o Lumnia Zap da Microsoft Store (ou o pacote de teste)
// instalado neste usuário. Com ele presente, o instalador clássico não roda:
// os dois disputam a porta 8080 e o Claude acaba lendo o banco errado (caso
// do Douglas, 05/11; doc 11, fase 3).
func pacotesDaStore(home string) []string {
	achados, _ := filepath.Glob(filepath.Join(pastaLocal(home), "Packages", "Lumnia.LumniaZap_*"))
	var out []string
	for _, p := range achados {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func temArgumento(args []string, nome string) bool {
	for _, a := range args {
		if strings.EqualFold(strings.TrimLeft(a, "-/"), strings.TrimLeft(nome, "-/")) {
			return true
		}
	}
	return false
}
