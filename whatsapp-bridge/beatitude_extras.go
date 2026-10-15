// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_extras.go — icone do grupo, voz (transcricao local) e envio com
// aprovacao explicita ("envia 1").
//
// Envio com aprovacao: o Claude continua SEM ferramenta de envio. Ele devolve
// os rascunhos num bloco <rascunhos>; a ponte confere cada destino no banco
// local, mostra no grupo o NOME REAL da conversa e o texto exato, e so envia
// quando a propria pessoa responde no grupo "envia 1" (ou "envia 1 e 3",
// "envia todos", "envia 2: texto novo"). Quem envia e a ponte, por ordem
// direta da pessoa, nunca o modelo.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

//go:embed beatitude-icone.jpg
var beatitudeIcone []byte

const beatitudeValidadeRascunho = 24 * time.Hour

func initBeatitudeExtras() {
	if panel.store == nil {
		return
	}
	_, err := panel.store.db.Exec(`
	CREATE TABLE IF NOT EXISTS beatitude_rascunhos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		lote INTEGER,
		n INTEGER,
		chat_jid TEXT,
		texto TEXT,
		criado_em TIMESTAMP,
		enviado_em TIMESTAMP
	);`)
	if err != nil && panel.logger != nil {
		panel.logger.Warnf("Beatitude: falha ao criar tabela de rascunhos: %v", err)
	}
}

// ---------------------------------------------------------------- icone

func beatitudePorIcone(grupo string) {
	if panel.client == nil || len(beatitudeIcone) == 0 {
		return
	}
	jid, err := types.ParseJID(grupo)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// so no grupo que a propria ponte criou: nunca troca um icone que a
	// pessoa escolheu
	if _, err := panel.client.SetGroupPhoto(ctx, jid, beatitudeIcone); err != nil {
		panel.logger.Warnf("Beatitude: não consegui pôr o ícone no grupo: %v", err)
		beatitude.mu.Lock()
		beatitude.ultimo = "ícone do grupo: " + err.Error()
		beatitude.mu.Unlock()
	}
}

// ---------------------------------------------------------------- voz

// Transcricao no proprio Mac, offline (faster-whisper via uv, que o Zap ja
// instala). A voz nao sai da maquina. Na primeira vez baixa o modelo.
const beatitudeTranscritorAvulsoPy = `import sys
from faster_whisper import WhisperModel
dica = sys.argv[3] if len(sys.argv) > 3 else None
m = WhisperModel("small", device="cpu", compute_type="int8", download_root=sys.argv[2])
segs, _ = m.transcribe(sys.argv[1], language="pt", vad_filter=True, initial_prompt=dica)
print(" ".join(s.text.strip() for s in segs).strip())
`

func beatitudeAcharUV() string {
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".local", "bin", "uv"),
		"/opt/homebrew/bin/uv",
		"/usr/local/bin/uv",
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func beatitudeTranscrever(grupo, msgID string) (string, error) {
	ok, _, _, caminho, err := downloadMedia(panel.client, panel.store, msgID, grupo)
	if !ok || err != nil {
		return "", fmt.Errorf("não consegui baixar o áudio: %v", err)
	}
	// Passo 6 (15-10): primeiro o residente (modelo ja carregado); se ele
	// falhar, o caminho avulso de sempre, que sobe tudo do zero.
	dica := beatitudeDicaTranscricao()
	texto, rerr := beatitudeTranscritorPedir(caminho, dica)
	if rerr == nil {
		if texto == "" {
			return "", fmt.Errorf("não entendi nada no áudio")
		}
		return texto, nil
	}
	if panel.logger != nil {
		panel.logger.Warnf("Beatitude: transcritor residente falhou (%v); usando o avulso", rerr)
	}
	return beatitudeTranscreverAvulso(caminho, dica)
}

// beatitudeTranscreverAvulso: um processo por audio (o caminho original).
func beatitudeTranscreverAvulso(caminho, dica string) (string, error) {
	uv := beatitudeAcharUV()
	if uv == "" {
		return "", fmt.Errorf("uv não encontrado")
	}
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, beatitudeWorkDir)
	_ = os.MkdirAll(filepath.Join(dir, "modelos"), 0o700)
	script := filepath.Join(dir, "transcrever.py")
	if err := os.WriteFile(script, []byte(beatitudeTranscritorAvulsoPy), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, "run", "--no-project", "--python", "3.11",
		"--with", "faster-whisper", "python", script, caminho, filepath.Join(dir, "modelos"), dica)
	cmd.Env = beatitudeAmbiente()
	out, err := cmd.Output()
	if err != nil {
		if ee, isExit := err.(*exec.ExitError); isExit {
			return "", fmt.Errorf("transcrição falhou: %s", beatitudeCurto(string(ee.Stderr), 200))
		}
		return "", fmt.Errorf("transcrição falhou: %v", err)
	}
	texto := strings.TrimSpace(string(out))
	if texto == "" {
		return "", fmt.Errorf("não entendi nada no áudio")
	}
	return texto, nil
}

// beatitudeDicaTranscricao da ao Whisper os nomes das conversas mais ativas,
// para ele acertar "Lisiane" em vez de "Liziane", e o jeito de ditar e-mail.
func beatitudeDicaTranscricao() string {
	nomes := []string{}
	if panel.store != nil {
		rows, err := panel.store.db.Query(`SELECT name FROM chats WHERE name IS NOT NULL AND name != '' AND jid NOT LIKE '%@g.us' ORDER BY last_message_time DESC LIMIT 40`)
		if err == nil {
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil && !strings.Contains(n, "@") {
					nomes = append(nomes, n)
				}
			}
			rows.Close()
		}
	}
	return "Pedido ao assistente Beatitude. Contatos: " + strings.Join(nomes, ", ") +
		". E-mails como diego@lumnia.dev e nome@gmail.com. Agenda, almoço, reunião, pendentes, temas."
}

// ---------------------------------------------------------------- rascunhos

var beatitudeBlocoRe = regexp.MustCompile(`(?s)<rascunhos>\s*(.*?)\s*</rascunhos>`)

type beatitudeRascunho struct {
	N       int    `json:"n"`
	ChatJID string `json:"chat_jid"`
	Texto   string `json:"texto"`
}

// beatitudeSepararRascunhos tira o bloco da resposta do Claude, confere cada
// destino e grava os validos num lote novo. Devolve o texto visivel e o
// trecho que a PONTE escreve (nome real da conversa + texto exato).
func beatitudeSepararRascunhos(resposta, grupo string) (string, string) {
	m := beatitudeBlocoRe.FindStringSubmatch(resposta)
	visivel := strings.TrimSpace(beatitudeBlocoRe.ReplaceAllString(resposta, ""))
	if m == nil {
		return visivel, ""
	}
	var lista []beatitudeRascunho
	if err := json.Unmarshal([]byte(m[1]), &lista); err != nil || len(lista) == 0 {
		return visivel, ""
	}
	lote := time.Now().UnixNano()
	var b strings.Builder
	validos := 0
	for _, r := range lista {
		texto := strings.TrimSpace(r.Texto)
		if r.N <= 0 || texto == "" || len([]rune(texto)) > 4000 || r.ChatJID == grupo {
			continue
		}
		nome, existe := beatitudeNomeDoChat(r.ChatJID)
		if !existe { // destino inventado ou desconhecido: descarta
			continue
		}
		_, err := panel.store.db.Exec(
			`INSERT INTO beatitude_rascunhos (lote, n, chat_jid, texto, criado_em) VALUES (?, ?, ?, ?, ?)`,
			lote, r.N, r.ChatJID, texto, time.Now())
		if err != nil {
			continue
		}
		validos++
		fmt.Fprintf(&b, "*%d · %s*\n%s\n\n", r.N, nome, texto)
	}
	if validos == 0 {
		return visivel, ""
	}
	if validos == 1 {
		b.WriteString("Para enviar, responda *sim* (ou *1*). Para mudar o texto: *envia 1: seu texto*. Nada sai sem isso.")
	} else {
		b.WriteString("Para enviar, responda com o número: *1*, *1 e 3* ou *todos*. Para mudar o texto: *envia 2: seu texto*. Nada sai sem isso.")
	}
	return visivel, strings.TrimSpace(b.String())
}

func beatitudeNomeDoChat(jid string) (string, bool) {
	if panel.store == nil || !strings.Contains(jid, "@") {
		return "", false
	}
	var nome string
	err := panel.store.db.QueryRow(`SELECT COALESCE(name,'') FROM chats WHERE jid = ?`, jid).Scan(&nome)
	if err != nil {
		return "", false
	}
	if nome == "" {
		nome = strings.SplitN(jid, "@", 2)[0]
	}
	return nome, true
}

var (
	// "envia 1", "manda 1 e 3", "pode enviar todos", e tambem so "1", "1 e 3", "todos"
	beatitudeEnviaRe      = regexp.MustCompile(`(?is)^\s*(?:(?:pode\s+)?(?:envi(?:a|ar|e)|mand(?:a|ar|e))\s+)?(todos|tudo|\d+(?:[\s,e]+\d+)*)\s*[.!]?\s*$`)
	beatitudeEnviaTextoRe = regexp.MustCompile(`(?is)^\s*(?:envi(?:a|ar|e)|mand(?:a|ar|e))\s+(\d+)\s*:\s*(.+)$`)
	// "sim", "pode", "ok", "pode enviar", "manda": so quando ha UM rascunho pendente
	beatitudeSimRe = regexp.MustCompile(`(?is)^\s*(sim|s|ok|pode|pode\s+enviar|pode\s+mandar|envia|manda|enviar|mandar)\s*[.!]?\s*$`)
)

// janela em que respostas curtas ("1", "sim") valem como aprovacao
const beatitudeJanelaResposta = 2 * time.Hour

// beatitudeEhComando reconhece a aprovacao sem chamar o Claude. Respostas
// curtas so contam se houver rascunho pendente recente; fora disso, "1" ou
// "sim" seguem para o Claude como conversa normal.
func beatitudeEhComando(texto string) bool {
	if beatitudeEnviaTextoRe.MatchString(texto) {
		return true
	}
	if !beatitudeEnviaRe.MatchString(texto) && !beatitudeSimRe.MatchString(texto) {
		return false
	}
	if beatitudeEnviaRe.MatchString(texto) && regexp.MustCompile(`(?i)^\s*(pode\s+)?(envi|mand)`).MatchString(texto) {
		return true // "envia 1" explicito: sempre comando (responde se nao houver rascunho)
	}
	pend := beatitudePendentes(beatitudeJanelaResposta)
	if beatitudeSimRe.MatchString(texto) {
		return pend == 1
	}
	return pend > 0
}

// beatitudePendentes conta rascunhos nao enviados do ultimo lote dentro da janela.
func beatitudePendentes(janela time.Duration) int {
	if panel.store == nil {
		return 0
	}
	var n int
	_ = panel.store.db.QueryRow(`
		SELECT COUNT(*) FROM beatitude_rascunhos
		WHERE enviado_em IS NULL AND criado_em >= ?
		  AND lote = (SELECT MAX(lote) FROM beatitude_rascunhos)`,
		time.Now().Add(-janela)).Scan(&n)
	return n
}

func beatitudeExecutarComando(texto, grupo string) {
	var ultimoLote int64
	_ = panel.store.db.QueryRow(
		`SELECT COALESCE(MAX(lote),0) FROM beatitude_rascunhos WHERE criado_em >= ?`,
		time.Now().Add(-beatitudeValidadeRascunho)).Scan(&ultimoLote)
	if ultimoLote == 0 {
		beatitudeSend(beatitudeMarca + ": não há rascunho esperando envio. Peça \"pendentes\" primeiro.")
		return
	}

	type alvo struct {
		n     int
		texto string // vazio = usar o rascunho
	}
	var alvos []alvo
	if beatitudeSimRe.MatchString(texto) {
		var n int
		_ = panel.store.db.QueryRow(`SELECT n FROM beatitude_rascunhos WHERE lote = ? AND enviado_em IS NULL ORDER BY n LIMIT 1`, ultimoLote).Scan(&n)
		if n > 0 {
			alvos = append(alvos, alvo{n: n})
		}
	} else if m := beatitudeEnviaTextoRe.FindStringSubmatch(texto); m != nil {
		n, _ := strconv.Atoi(m[1])
		alvos = append(alvos, alvo{n, strings.TrimSpace(m[2])})
	} else if m := beatitudeEnviaRe.FindStringSubmatch(texto); m != nil {
		sel := strings.ToLower(m[1])
		if sel == "todos" || sel == "tudo" {
			rows, err := panel.store.db.Query(`SELECT n FROM beatitude_rascunhos WHERE lote = ? AND enviado_em IS NULL ORDER BY n`, ultimoLote)
			if err == nil {
				for rows.Next() {
					var n int
					if rows.Scan(&n) == nil {
						alvos = append(alvos, alvo{n: n})
					}
				}
				rows.Close()
			}
		} else {
			for _, p := range regexp.MustCompile(`\d+`).FindAllString(sel, -1) {
				n, _ := strconv.Atoi(p)
				alvos = append(alvos, alvo{n: n})
			}
		}
	}
	if len(alvos) == 0 {
		beatitudeSend(beatitudeMarca + ": não achei o que enviar. Use, por exemplo, *envia 1*.")
		return
	}

	var linhas []string
	for _, a := range alvos {
		var id int64
		var jid, rascunho string
		var enviado *time.Time
		err := panel.store.db.QueryRow(
			`SELECT id, chat_jid, texto, enviado_em FROM beatitude_rascunhos WHERE lote = ? AND n = ?`,
			ultimoLote, a.n).Scan(&id, &jid, &rascunho, &enviado)
		if err != nil {
			linhas = append(linhas, fmt.Sprintf("%d: não existe na última lista", a.n))
			continue
		}
		nome, _ := beatitudeNomeDoChat(jid)
		if enviado != nil {
			linhas = append(linhas, fmt.Sprintf("%d · %s: já tinha sido enviado", a.n, nome))
			continue
		}
		final := rascunho
		if a.texto != "" {
			final = a.texto
		}
		ok, msg := panelSend(jid, final, "beatitude-aprovado")
		if !ok {
			linhas = append(linhas, fmt.Sprintf("%d · %s: não foi (%s)", a.n, nome, msg))
			continue
		}
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ?, texto = ? WHERE id = ?`, time.Now(), final, id)
		linhas = append(linhas, fmt.Sprintf("%d · %s: enviado", a.n, nome))
		beatitudeRegistrar(time.Now(), "envia "+strconv.Itoa(a.n), "enviado para "+nome, "envio", "")
	}
	beatitudeSend(beatitudeMarca + "\n\n" + strings.Join(linhas, "\n"))
}
