// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// Package migrar passa a instalação clássica do Lumnia Zap (o .exe, com
// Tarefa Agendada e dados em %LOCALAPPDATA%\LumniaZap) para o pacote da
// Microsoft Store (doc 11, fase 3).
//
// Caso que originou (Douglas, 05/11): a versão da Store instalada por cima da
// clássica nunca subia — o servico.exe espera a porta 8080 ficar livre, e a
// ponte antiga a ocupava —, o LumniaZap.exe abria o painel ANTIGO (ele leu o
// QR ali achando que era o novo) e o Claude seguia lendo o banco antigo.
//
// A lógica fica aqui, sem dependência de Windows, para os testes rodarem em
// qualquer sistema; o que toca o sistema (tarefa, processos, porta, Claude)
// vem pela interface Sistema.
package migrar

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sistema: o que a migração pede ao Windows.
type Sistema interface {
	TarefaClassicaExiste() bool
	ApagarTarefaClassica() error
	EncerrarProcessosEm(pasta string) error // ponte, iniciar.cmd e servidor MCP clássicos
	EncerrarPonteDaStore() error
	PortaLivre(espera time.Duration) bool
	ClaudeAberto() bool
	FecharClaude() error
}

// Pastas: onde está cada coisa (preenchido por quem chama).
type Pastas struct {
	Classica   string   // %LOCALAPPDATA%\LumniaZap
	LocalState string   // %LOCALAPPDATA%\Packages\<PFN>\LocalState
	Startup    string   // %APPDATA%\...\Startup (atalho do fallback do .exe)
	Configs    []string // pastas com claude_desktop_config.json possíveis
	AliasMCP   string   // %LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe
}

func (p Pastas) storeClassico() string {
	return filepath.Join(p.Classica, "whatsapp-bridge", "store")
}
func (p Pastas) storePacote() string {
	return filepath.Join(p.LocalState, "whatsapp-bridge", "store")
}
func (p Pastas) marcador() string { return filepath.Join(p.LocalState, "migrado.json") }

// ArquivoPausa: enquanto existir, o servico.exe não sobe a ponte da Store.
func ArquivoPausa(localState string) string { return filepath.Join(localState, "pausa-migracao") }

// PausaMaxima: uma migração leva segundos. Pausa mais velha que isto é resto
// de uma migração interrompida (queda de energia, processo morto) e não pode
// impedir a ponte da Store de subir para sempre.
const PausaMaxima = 15 * time.Minute

// EmPausa diz se há migração em andamento; pausa vencida é removida.
func EmPausa(localState string, agora time.Time) bool {
	st, err := os.Stat(ArquivoPausa(localState))
	if err != nil {
		return false
	}
	if agora.Sub(st.ModTime()) > PausaMaxima {
		_ = os.Remove(ArquivoPausa(localState))
		return false
	}
	return true
}

// Pendente diz se há instalação clássica a migrar: banco clássico no disco
// (ou a Tarefa Agendada dela) e nenhuma migração feita antes.
func Pendente(s Sistema, p Pastas) bool {
	if _, err := os.Stat(p.marcador()); err == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(p.storeClassico(), "messages.db")); err == nil {
		return true
	}
	return s.TarefaClassicaExiste()
}

// Resultado vai para migrado.json e para a mensagem final.
type Resultado struct {
	De            string    `json:"de"`
	Quando        time.Time `json:"quando"`
	BytesCopiados int64     `json:"bytes_copiados"`
	StoreAnterior string    `json:"store_anterior,omitempty"` // backup do store do pacote, se havia
	Configs       []string  `json:"configs_repontadas,omitempty"`
	ConfigsFalhas []string  `json:"configs_falhas,omitempty"`
}

// Migrar faz a passagem. Ordem pensada para nunca deixar a pessoa sem
// histórico: nada é copiado antes de a porta estar livre, o store do pacote
// vira backup em vez de ser apagado e a pasta clássica fica intacta (só sem
// tarefa e sem config apontando para ela).
func Migrar(s Sistema, p Pastas, agora time.Time) (Resultado, error) {
	r := Resultado{De: p.Classica, Quando: agora}
	pausa := ArquivoPausa(p.LocalState)
	if err := os.MkdirAll(p.LocalState, 0o755); err != nil {
		return r, err
	}
	if err := os.WriteFile(pausa, []byte(agora.Format(time.RFC3339)), 0o644); err != nil {
		return r, err
	}
	defer os.Remove(pausa) // libera o servico.exe a subir a ponte da Store

	// 1. A clássica para de subir sozinha.
	if s.TarefaClassicaExiste() {
		if err := s.ApagarTarefaClassica(); err != nil {
			return r, fmt.Errorf("não consegui apagar a Tarefa Agendada \"Lumnia Zap\": %v", err)
		}
	}
	if p.Startup != "" {
		_ = os.Remove(filepath.Join(p.Startup, "Lumnia Zap.lnk"))
	}
	// 2. Ninguém segurando a porta nem os bancos.
	_ = s.EncerrarPonteDaStore()
	_ = s.EncerrarProcessosEm(p.Classica)
	if !s.PortaLivre(20 * time.Second) {
		return r, fmt.Errorf("a porta 8080 continuou ocupada; nada foi copiado. Reinicie o computador e abra o Lumnia Zap de novo")
	}
	// 3. Claude fechado antes de mexer na configuração dele (doc 12).
	if s.ClaudeAberto() {
		_ = s.FecharClaude()
	}
	// 4. Histórico: o do pacote vira backup; o clássico entra inteiro.
	origem, destino := p.storeClassico(), p.storePacote()
	if st, err := os.Stat(origem); err == nil && st.IsDir() {
		if _, err := os.Stat(destino); err == nil {
			r.StoreAnterior = destino + ".bak-" + agora.Format("20060102-150405")
			if err := os.Rename(destino, r.StoreAnterior); err != nil {
				return r, fmt.Errorf("não consegui guardar o histórico atual da Store: %v", err)
			}
		}
		n, err := copiarPasta(origem, destino)
		if err != nil {
			// desfaz: volta o store do pacote como estava
			_ = os.RemoveAll(destino)
			if r.StoreAnterior != "" {
				_ = os.Rename(r.StoreAnterior, destino)
				r.StoreAnterior = ""
			}
			return r, fmt.Errorf("a cópia do histórico falhou e foi desfeita: %v", err)
		}
		r.BytesCopiados = n
	}
	// 5. Claude: a entrada lumnia-zap passa a apontar para o alias da Store.
	for _, d := range p.Configs {
		ok, err := repontar(d, p.AliasMCP, agora)
		switch {
		case err != nil:
			r.ConfigsFalhas = append(r.ConfigsFalhas, d+": "+err.Error())
		case ok:
			r.Configs = append(r.Configs, d)
		}
	}
	// 6. Marcador: não pergunta de novo.
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(p.marcador(), b, 0o644); err != nil {
		return r, err
	}
	return r, nil
}

func copiarPasta(origem, destino string) (int64, error) {
	var total int64
	err := filepath.Walk(origem, func(caminho string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(origem, caminho)
		if err != nil {
			return err
		}
		alvo := filepath.Join(destino, rel)
		if info.IsDir() {
			return os.MkdirAll(alvo, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(caminho)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(alvo, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, in)
		total += n
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
	return total, err
}

// repontar troca a entrada lumnia-zap (se existir) pelo alias da Store, com
// backup na primeira vez, UTF-8 sem BOM e gravação atômica. Pasta sem config
// ou config sem a entrada: nada a fazer (a extensão .mcpb cuida do resto).
func repontar(dir, alias string, agora time.Time) (bool, error) {
	caminho := filepath.Join(dir, "claude_desktop_config.json")
	raw, err := os.ReadFile(caminho)
	if err != nil {
		return false, nil
	}
	limpo := strings.TrimPrefix(string(raw), "\xef\xbb\xbf")
	cfg := map[string]interface{}{}
	if strings.TrimSpace(limpo) != "" {
		if err := json.Unmarshal([]byte(limpo), &cfg); err != nil {
			return false, fmt.Errorf("arquivo ilegível")
		}
	}
	srv, _ := cfg["mcpServers"].(map[string]interface{})
	if _, ok := srv["lumnia-zap"]; !ok {
		return false, nil
	}
	if _, err := os.Stat(caminho + ".bak-lumnia-zap"); os.IsNotExist(err) {
		_ = os.WriteFile(caminho+".bak-lumnia-zap", raw, 0o600)
	}
	srv["lumnia-zap"] = map[string]interface{}{"command": alias}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := caminho + ".lumnia-novo"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, caminho); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// Conflito: gravado pelo servico.exe quando acha a 8080 ocupada por outra
// ponte; o painel da Store mostra quando conseguir subir.
func GravarConflito(localState, detalhe string, agora time.Time) {
	b, _ := json.Marshal(map[string]string{"em": agora.Format(time.RFC3339), "detalhe": detalhe})
	_ = os.MkdirAll(localState, 0o755)
	_ = os.WriteFile(filepath.Join(localState, "conflito.json"), b, 0o644)
}
