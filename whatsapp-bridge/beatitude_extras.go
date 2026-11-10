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
	// Rascunho com horario (10-11). "duplicate column" e esperado em bancos ja migrados.
	for _, col := range []string{"enviar_em TIMESTAMP", "agendamento_id INTEGER", "so_agora INTEGER DEFAULT 0", "cancelado_em TIMESTAMP"} {
		if _, err := panel.store.db.Exec("ALTER TABLE beatitude_rascunhos ADD COLUMN " + col); err != nil &&
			!strings.Contains(err.Error(), "duplicate column") && panel.logger != nil {
			panel.logger.Warnf("Beatitude: falha ao acrescentar coluna %s: %v", col, err)
		}
	}
	panelAoRodarAgendamento = beatitudeAoRodarAgendamento
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
//
// Rascunho com horario (10-11). Quando ela pede "as 8h30 responde a Marina",
// o rascunho traz "enviar_em" e o "sim" APROVA, mas a mensagem so sai no
// horario: a ponte cria um agendamento unico no agendador do painel
// (panel_schedules), que ja roda a cada 30 s e nao envia o que passou da
// hora. Funciona para qualquer conversa do WhatsApp, esteja a pessoa em
// Pessoas no app ou nao.
//
// Trava: se o pedido cita um horario e o rascunho veio SEM horario, o "sim"
// nao envia; ela precisa dizer "agora" (ou dar o horario). Foi o que faltou
// na madrugada de 10-11: o rascunho comum saiu as 04:36 no lugar das 08:30.

var beatitudeBlocoRe = regexp.MustCompile(`(?s)<rascunhos>\s*(.*?)\s*</rascunhos>`)

type beatitudeRascunho struct {
	N        int    `json:"n"`
	ChatJID  string `json:"chat_jid"`
	Texto    string `json:"texto"`
	EnviarEm string `json:"enviar_em,omitempty"` // "AAAA-MM-DD HH:MM", horario de Brasilia
}

// ate quando no futuro um rascunho pode ser agendado
const beatitudeHorizonteAgenda = 7 * 24 * time.Hour

// Horario citado no pedido: "8:30", "8;30", "8h", "8h30", "8 horas", "23.59",
// "meio-dia", "meia-noite". Erra para o lado seguro: na duvida pede "agora".
var beatitudeHorarioRe = regexp.MustCompile(`(?i)(?:^|[^\d])(?:[01]?\d|2[0-3])\s*(?:[:;.h]\s*[0-5]\d(?:[^\d]|$)|h(?:[^a-z\d]|$)|horas?\b)|\bmei[oa][\s-]*(?:dia|noite)\b`)

func beatitudePedidoTemHorario(pedido string) bool {
	return beatitudeHorarioRe.MatchString(pedido)
}

// beatitudeLerHorario le "enviar_em" no fuso do painel (America/Sao_Paulo).
func beatitudeLerHorario(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, f := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(f, s, panelLoc); err == nil {
			return t.Truncate(time.Minute), true
		}
	}
	return time.Time{}, false
}

// beatitudeQuando escreve "hoje às 08:30", "amanhã às 07:00" ou "12/11 às 09:00".
func beatitudeQuando(t, agora time.Time) string {
	t, agora = t.In(panelLoc), agora.In(panelLoc)
	dia := func(x time.Time) string { return x.Format("2006-01-02") }
	switch dia(t) {
	case dia(agora):
		return "hoje às " + t.Format("15:04")
	case dia(agora.AddDate(0, 0, 1)):
		return "amanhã às " + t.Format("15:04")
	}
	return t.Format("02/01") + " às " + t.Format("15:04")
}

// beatitudeClassificarHorario decide o que o "sim" faz com um rascunho:
// agenda (horario futuro valido), exige "agora" (pedido com horario e
// rascunho sem horario, ou horario que ja passou) ou envia na hora.
func beatitudeClassificarHorario(enviarEm string, pedidoComHorario bool, agora time.Time) (quando time.Time, soAgora bool, motivo string) {
	t, ok := beatitudeLerHorario(enviarEm)
	switch {
	case ok && t.After(agora) && t.Sub(agora) <= beatitudeHorizonteAgenda:
		return t, false, ""
	case ok && !t.After(agora):
		return time.Time{}, true, "o horário pedido (" + t.In(panelLoc).Format("15:04") + ") já passou"
	case ok:
		return time.Time{}, true, "o horário pedido passa de 7 dias"
	case pedidoComHorario:
		return time.Time{}, true, "sem horário marcado"
	}
	return time.Time{}, false, ""
}

// beatitudeSepararRascunhos tira o bloco da resposta do Claude, confere cada
// destino e grava os validos num lote novo. Devolve o texto visivel e o
// trecho que a PONTE escreve (nome real da conversa + texto exato + horario).
func beatitudeSepararRascunhos(resposta, grupo, pedido string) (string, string) {
	m := beatitudeBlocoRe.FindStringSubmatch(resposta)
	visivel := strings.TrimSpace(beatitudeBlocoRe.ReplaceAllString(resposta, ""))
	if m == nil {
		return visivel, ""
	}
	var lista []beatitudeRascunho
	if err := json.Unmarshal([]byte(m[1]), &lista); err != nil || len(lista) == 0 {
		return visivel, ""
	}
	agora := time.Now()
	comHorario := beatitudePedidoTemHorario(pedido)
	lote := agora.UnixNano()
	var b strings.Builder
	validos, agendados, travados := 0, 0, 0
	var unicoQuando time.Time
	for _, r := range lista {
		texto := strings.TrimSpace(r.Texto)
		if r.N <= 0 || texto == "" || len([]rune(texto)) > 4000 || r.ChatJID == grupo {
			continue
		}
		nome, existe := beatitudeNomeDoChat(r.ChatJID)
		if !existe { // destino inventado ou desconhecido: descarta
			continue
		}
		quando, soAgora, motivo := beatitudeClassificarHorario(r.EnviarEm, comHorario, agora)
		var enviarEm interface{}
		if !quando.IsZero() {
			enviarEm = quando
		}
		_, err := panel.store.db.Exec(
			`INSERT INTO beatitude_rascunhos (lote, n, chat_jid, texto, criado_em, enviar_em, so_agora) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			lote, r.N, r.ChatJID, texto, agora, enviarEm, soAgora)
		if err != nil {
			continue
		}
		validos++
		cab := fmt.Sprintf("*%d · %s*", r.N, nome)
		switch {
		case !quando.IsZero():
			agendados++
			unicoQuando = quando
			cab += " · sai *" + beatitudeQuando(quando, agora) + "*"
		case soAgora:
			travados++
			cab += " · " + motivo
		}
		fmt.Fprintf(&b, "%s\n%s\n\n", cab, texto)
	}
	if validos == 0 {
		return visivel, ""
	}
	switch {
	case validos == 1 && agendados == 1:
		b.WriteString("Para aprovar, responda *sim* (ou *1*): sai " + beatitudeQuando(unicoQuando, agora) + ", não antes. Para mudar o texto: *envia 1: seu texto*. Nada sai sem isso.")
	case validos == 1 && travados == 1:
		b.WriteString("Você citou um horário e este rascunho não tem horário para sair, então o *sim* não envia. Diga o horário (ex.: *às 8h30*), ou responda *agora* para enviar já.")
	case validos == 1:
		b.WriteString("Para enviar, responda *sim* (ou *1*). Para mudar o texto: *envia 1: seu texto*. Nada sai sem isso.")
	default:
		b.WriteString("Para aprovar, responda com o número: *1*, *1 e 3* ou *todos*. Para mudar o texto: *envia 2: seu texto*. Nada sai sem isso.")
		if agendados > 0 {
			b.WriteString("\nOs que têm horário só saem no horário.")
		}
		if travados > 0 {
			b.WriteString("\nOs marcados \"sem horário marcado\" ou com horário que passou não saem com o número: diga o horário, ou responda *2 agora* para enviar já.")
		}
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
	// "agora", "1 agora", "envia agora", "manda o 2 agora": envia ja, ignorando horario
	beatitudeAgoraRe = regexp.MustCompile(`(?is)^\s*(?:(?:pode\s+)?(?:envi(?:a|ar|e)|mand(?:a|ar|e))\s+)?(?:o\s+)?(\d+)?\s*(?:já\s+|ja\s+)?agora\s*(?:mesmo)?\s*[.!]?\s*$`)
	// "cancela", "cancela 1", "não envia", "nao manda o 2": desiste (tambem do agendado)
	beatitudeCancelaRe = regexp.MustCompile(`(?is)^\s*(?:cancela(?:r)?|desmarca(?:r)?|n[ãa]o\s+(?:envi(?:a|e)|mand(?:a|e)))\s*(?:o\s+)?(\d+)?\s*[.!]?\s*$`)
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
	if beatitudeCancelaRe.MatchString(texto) {
		return beatitudePendentes(beatitudeJanelaResposta) > 0 || beatitudeAgendadosAbertos() > 0
	}
	if beatitudeAgoraRe.MatchString(texto) {
		return beatitudePendentes(beatitudeJanelaResposta) > 0 || beatitudeAgendadosAbertos() > 0
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

// beatitudePendentes conta rascunhos do ultimo lote, dentro da janela, que
// ainda esperam decisao (nem enviados, nem agendados, nem cancelados).
func beatitudePendentes(janela time.Duration) int {
	if panel.store == nil {
		return 0
	}
	var n int
	_ = panel.store.db.QueryRow(`
		SELECT COUNT(*) FROM beatitude_rascunhos
		WHERE enviado_em IS NULL AND agendamento_id IS NULL AND cancelado_em IS NULL
		  AND criado_em >= ?
		  AND lote = (SELECT MAX(lote) FROM beatitude_rascunhos)`,
		time.Now().Add(-janela)).Scan(&n)
	return n
}

// beatitudeAgendadosAbertos conta rascunhos aprovados que esperam o horario.
func beatitudeAgendadosAbertos() int {
	if panel.store == nil {
		return 0
	}
	var n int
	_ = panel.store.db.QueryRow(`
		SELECT COUNT(*) FROM beatitude_rascunhos
		WHERE agendamento_id IS NOT NULL AND enviado_em IS NULL AND cancelado_em IS NULL
		  AND enviar_em >= ?`, time.Now().Add(-time.Hour)).Scan(&n)
	return n
}

type beatitudeRascunhoLinha struct {
	id          int64
	n           int
	jid         string
	texto       string
	enviado     *time.Time
	cancelado   *time.Time
	enviarEm    *time.Time
	agendamento *int64
	soAgora     bool
}

func beatitudeLerRascunho(where string, args ...interface{}) (beatitudeRascunhoLinha, error) {
	var r beatitudeRascunhoLinha
	err := panel.store.db.QueryRow(
		`SELECT id, n, chat_jid, texto, enviado_em, cancelado_em, enviar_em, agendamento_id, COALESCE(so_agora,0)
		 FROM beatitude_rascunhos WHERE `+where, args...).
		Scan(&r.id, &r.n, &r.jid, &r.texto, &r.enviado, &r.cancelado, &r.enviarEm, &r.agendamento, &r.soAgora)
	return r, err
}

func beatitudeExecutarComando(texto, grupo string) {
	linhas := beatitudeComando(texto, time.Now())
	if len(linhas) == 0 {
		return
	}
	beatitudeSend(beatitudeMarca + "\n\n" + strings.Join(linhas, "\n"))
}

// beatitudeComando executa "sim", "envia N", "N agora", "cancela N" e devolve
// as linhas da resposta ao grupo. Separado do envio ao grupo para os testes.
func beatitudeComando(texto string, agora time.Time) []string {
	var ultimoLote int64
	_ = panel.store.db.QueryRow(
		`SELECT COALESCE(MAX(lote),0) FROM beatitude_rascunhos WHERE criado_em >= ?`,
		agora.Add(-beatitudeValidadeRascunho)).Scan(&ultimoLote)

	modoAgora := beatitudeAgoraRe.MatchString(texto) && !beatitudeEnviaTextoRe.MatchString(texto)
	modoCancela := beatitudeCancelaRe.MatchString(texto)

	type alvo struct {
		n     int
		id    int64  // quando o alvo vem de fora do ultimo lote (agendado antigo)
		texto string // vazio = usar o rascunho
	}
	var alvos []alvo

	// sem numero, "agora"/"cancela" valem para o unico rascunho em aberto: o
	// pendente do ultimo lote ou, nao havendo, o unico agendado esperando hora
	unico := func() []alvo {
		var ids []int64
		rows, err := panel.store.db.Query(`SELECT id FROM beatitude_rascunhos
			WHERE lote = ? AND enviado_em IS NULL AND cancelado_em IS NULL AND agendamento_id IS NULL`, ultimoLote)
		if err == nil {
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
		if len(ids) == 0 {
			rows, err := panel.store.db.Query(`SELECT id FROM beatitude_rascunhos
				WHERE agendamento_id IS NOT NULL AND enviado_em IS NULL AND cancelado_em IS NULL AND enviar_em >= ?`,
				agora.Add(-time.Hour))
			if err == nil {
				for rows.Next() {
					var id int64
					if rows.Scan(&id) == nil {
						ids = append(ids, id)
					}
				}
				rows.Close()
			}
		}
		if len(ids) != 1 {
			return nil
		}
		return []alvo{{id: ids[0]}}
	}

	switch {
	case modoCancela:
		if m := beatitudeCancelaRe.FindStringSubmatch(texto); m[1] != "" {
			n, _ := strconv.Atoi(m[1])
			alvos = append(alvos, alvo{n: n})
		} else if alvos = unico(); alvos == nil {
			return []string{beatitudeMarca + ": diga qual: *cancela 1*, *cancela 2*…"}
		}
	case modoAgora:
		if m := beatitudeAgoraRe.FindStringSubmatch(texto); m[1] != "" {
			n, _ := strconv.Atoi(m[1])
			alvos = append(alvos, alvo{n: n})
		} else if alvos = unico(); alvos == nil {
			return []string{beatitudeMarca + ": diga qual: *1 agora*, *2 agora*…"}
		}
	case beatitudeSimRe.MatchString(texto):
		var n int
		_ = panel.store.db.QueryRow(`SELECT n FROM beatitude_rascunhos WHERE lote = ? AND enviado_em IS NULL
			AND agendamento_id IS NULL AND cancelado_em IS NULL ORDER BY n LIMIT 1`, ultimoLote).Scan(&n)
		if n > 0 {
			alvos = append(alvos, alvo{n: n})
		}
	default:
		if m := beatitudeEnviaTextoRe.FindStringSubmatch(texto); m != nil {
			n, _ := strconv.Atoi(m[1])
			alvos = append(alvos, alvo{n: n, texto: strings.TrimSpace(m[2])})
		} else if m := beatitudeEnviaRe.FindStringSubmatch(texto); m != nil {
			sel := strings.ToLower(m[1])
			if sel == "todos" || sel == "tudo" {
				rows, err := panel.store.db.Query(`SELECT n FROM beatitude_rascunhos WHERE lote = ? AND enviado_em IS NULL
					AND agendamento_id IS NULL AND cancelado_em IS NULL ORDER BY n`, ultimoLote)
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
	}
	if ultimoLote == 0 && (len(alvos) == 0 || alvos[0].id == 0) {
		return []string{beatitudeMarca + ": não há rascunho esperando envio. Peça \"pendentes\" primeiro."}
	}
	if len(alvos) == 0 {
		return []string{beatitudeMarca + ": não achei o que enviar. Use, por exemplo, *envia 1*."}
	}

	var linhas []string
	for _, a := range alvos {
		var r beatitudeRascunhoLinha
		var err error
		if a.id > 0 {
			r, err = beatitudeLerRascunho(`id = ?`, a.id)
		} else {
			r, err = beatitudeLerRascunho(`lote = ? AND n = ?`, ultimoLote, a.n)
		}
		if err != nil {
			linhas = append(linhas, fmt.Sprintf("%d: não existe na última lista", a.n))
			continue
		}
		nome, _ := beatitudeNomeDoChat(r.jid)
		rot := fmt.Sprintf("%d · %s", r.n, nome)
		switch {
		case r.enviado != nil:
			linhas = append(linhas, rot+": já tinha sido enviado")
			continue
		case r.cancelado != nil:
			linhas = append(linhas, rot+": já tinha sido cancelado")
			continue
		}

		if modoCancela {
			if r.agendamento != nil {
				_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET enabled = 0, last_status = 'cancelada no grupo Beatitude' WHERE id = ?`, *r.agendamento)
			}
			_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET cancelado_em = ? WHERE id = ?`, agora, r.id)
			linhas = append(linhas, rot+": cancelado, não vai sair")
			beatitudeRegistrar(agora, "cancela "+strconv.Itoa(r.n), "cancelado para "+nome, "cancelado", "")
			continue
		}

		final := r.texto
		if a.texto != "" {
			final = a.texto
		}

		// ja aprovado e esperando a hora: so "agora" ou "cancela" mexem nele
		if r.agendamento != nil && !modoAgora {
			q := ""
			if r.enviarEm != nil {
				q = " (" + beatitudeQuando(*r.enviarEm, agora) + ")"
			}
			linhas = append(linhas, rot+": já está aprovado e sai no horário"+q+". Para mandar já: *"+strconv.Itoa(r.n)+" agora*; para desistir: *cancela "+strconv.Itoa(r.n)+"*")
			continue
		}

		if !modoAgora {
			if r.enviarEm != nil && r.enviarEm.After(agora) {
				id, err := beatitudeAgendar(r, nome, final)
				if err != nil {
					linhas = append(linhas, rot+": não consegui agendar ("+err.Error()+"). Nada foi enviado.")
					continue
				}
				_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET agendamento_id = ?, texto = ? WHERE id = ?`, id, final, r.id)
				linhas = append(linhas, rot+": aprovado, sai *"+beatitudeQuando(*r.enviarEm, agora)+"*. Para desistir: *cancela "+strconv.Itoa(r.n)+"*")
				beatitudeRegistrar(agora, "agenda "+strconv.Itoa(r.n), "agendado para "+nome+" em "+r.enviarEm.In(panelLoc).Format("02/01 15:04"), "agendado", "")
				continue
			}
			if r.enviarEm != nil || r.soAgora {
				motivo := "você citou um horário e este não tem horário marcado"
				if r.enviarEm != nil {
					motivo = "o horário (" + r.enviarEm.In(panelLoc).Format("15:04") + ") já passou"
				}
				linhas = append(linhas, rot+": não enviei, "+motivo+". Para mandar já: *"+strconv.Itoa(r.n)+" agora*; ou diga o novo horário")
				continue
			}
		}

		if r.agendamento != nil { // "agora" num agendado: tira do agendador antes de mandar
			_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET enabled = 0, last_status = 'enviada antes, pelo grupo Beatitude' WHERE id = ?`, *r.agendamento)
		}
		ok, msg := panelSend(r.jid, final, "beatitude-aprovado")
		if !ok {
			linhas = append(linhas, fmt.Sprintf("%s: não foi (%s)", rot, msg))
			continue
		}
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ?, texto = ? WHERE id = ?`, time.Now(), final, r.id)
		linhas = append(linhas, rot+": enviado")
		beatitudeRegistrar(time.Now(), "envia "+strconv.Itoa(r.n), "enviado para "+nome, "envio", "")
	}
	return linhas
}

// beatitudeAgendar cria o agendamento unico no agendador do painel.
func beatitudeAgendar(r beatitudeRascunhoLinha, nome, texto string) (int64, error) {
	if r.enviarEm == nil {
		return 0, fmt.Errorf("sem horário")
	}
	q := r.enviarEm.In(panelLoc)
	res, err := panel.store.db.Exec(
		`INSERT INTO panel_schedules
		 (recipient, display, message, mode, at_time, on_date, enabled, created_at)
		 VALUES (?, ?, ?, 'once', ?, ?, 1, ?)`,
		r.jid, "Beatitude · "+nome, texto, q.Format("15:04"), q.Format("2006-01-02"), time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// beatitudeAoRodarAgendamento e chamado pelo agendador do painel depois de
// cada envio agendado. Se for um rascunho do Beatitude, marca e avisa no grupo.
func beatitudeAoRodarAgendamento(id int64, status string, quando time.Time) {
	if panel.store == nil {
		return
	}
	r, err := beatitudeLerRascunho(`agendamento_id = ? AND enviado_em IS NULL AND cancelado_em IS NULL`, id)
	if err != nil {
		return
	}
	nome, _ := beatitudeNomeDoChat(r.jid)
	hora := quando.In(panelLoc).Format("15:04")
	if status == "enviada" {
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ? WHERE id = ?`, time.Now(), r.id)
		beatitudeRegistrar(time.Now(), "agendado "+strconv.Itoa(r.n), "enviado para "+nome+" às "+hora, "envio", "")
		beatitudeSend(beatitudeMarca + ": enviei às " + hora + " para " + nome + ", como combinado.")
		return
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET cancelado_em = ? WHERE id = ?`, time.Now(), r.id)
	beatitudeRegistrar(time.Now(), "agendado "+strconv.Itoa(r.n), "não enviado para "+nome, "erro", status)
	beatitudeSend(beatitudeMarca + ": a mensagem das " + hora + " para " + nome + " *não saiu* (" + status + "). Se ainda quiser, me peça de novo.")
}
