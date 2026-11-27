// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_extras.go — icone do grupo, voz (transcricao local) e envio com
// aprovacao explicita ("envia 1").
//
// Desde 25-11, mensagem com horario a mais de 5 minutos ja nasce programada
// (beatitudeFolgaSemSim); o resto abaixo vale para o que espera o sim.
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
	for _, col := range []string{"enviar_em TIMESTAMP", "agendamento_id INTEGER", "so_agora INTEGER DEFAULT 0", "cancelado_em TIMESTAMP",
		"anexo_msg_id TEXT", "anexo_nome TEXT", "falha TEXT", "mudou_em TIMESTAMP"} {
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
	// O Whisper so usa o fim da dica (~220 tokens) quando ela e longa: o
	// vocabulario fixo vem depois dos nomes para nunca ser cortado.
	if len(nomes) > 30 {
		nomes = nomes[:30]
	}
	return "Pedido ao assistente Beatitude. Contatos: " + strings.Join(nomes, ", ") +
		". Termos: " + strings.Join(beatitudeVocabularioVoz, ", ") +
		". E-mails como diego@lumnia.dev e nome@gmail.com. Agenda, almoço, reunião, pendentes, temas."
}

// beatitudeVocabularioVoz: nomes de produtos e empresas que o Whisper erra
// (14-11: "Elevren Labs"). Curto de proposito; os nomes de pessoas vem das
// conversas.
var beatitudeVocabularioVoz = []string{
	"Beatitude", "Lumnia", "Lumnia Zap", "Claude", "Anthropic", "Lovable", "ElevenLabs",
	"Timão", "Katelline", "Evoque", "Amigo", "Prontus", "KCEA", "Rhesus", "Culture Compass",
	"WhatsApp", "Gmail", "Google Drive", "Outlook",
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
	Anexo    string `json:"anexo,omitempty"`     // codigo da lista de arquivos do grupo (A1…), beatitude_anexos.go
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

// Mensagem programada sem "sim" (25-11). O que ela pede para um horario a
// mais de beatitudeFolgaSemSim de distancia ja nasce programado: a ponte
// agenda na hora e mostra o cartao com o texto. Ela muda com "envia N: texto"
// e desiste com "cancela N" ou ❌. Com menos folga que isso nao da tempo de
// desfazer, entao segue pedindo o sim; envio imediato tambem.
const beatitudeFolgaSemSim = 5 * time.Minute

// beatitudeRascunhosInfo: o que a ponte fez com o bloco de rascunhos.
type beatitudeRascunhosInfo struct {
	Visivel     string // texto do mordomo, sem o bloco e sem a marca repetida
	Cartao      string // o que a PONTE escreve: nome real, horario, texto e como mudar
	Programados int    // ja agendados, saem sozinhos no horario
	Pendentes   int    // esperam o sim (ou "agora", ou um horario)
	Resumo      string // uma linha por mensagem, SEM o texto (vai ao app)
}

// beatitudeSepararRascunhos: forma curta, usada onde so o texto importa.
func beatitudeSepararRascunhos(resposta, grupo, pedido string, anexos ...beatitudeAnexo) (string, string) {
	info := beatitudeTratarRascunhos(resposta, grupo, pedido, anexos...)
	return info.Visivel, info.Cartao
}

var (
	// o mordomo imita o historico do grupo e comeca a resposta com a marca
	beatitudeMarcaNoInicioRe = regexp.MustCompile(`^(?:\s*◆\s*Beatitude\s*:?\s*)+`)
	// resposta que fala de um rascunho sem trazer o bloco: nada foi montado
	beatitudePrometeRascunhoRe = regexp.MustCompile(`(?i)\brascunho\b|aprovar o envio|\bprogramei\b|ficou programad|deixei programad`)
	beatitudeFalaDeAcaoRe      = regexp.MustCompile(`(?i)\bação \*?\d|\bsim \*?\d`)
)

func beatitudeTirarMarca(s string) string {
	return strings.TrimSpace(beatitudeMarcaNoInicioRe.ReplaceAllString(s, ""))
}

const (
	beatitudeAvisoSemBloco   = "O texto da mensagem não veio nesta resposta, então *nada foi preparado nem programado*. Peça de novo."
	beatitudeAvisoBlocoRuim  = "O texto da mensagem veio com defeito e eu descartei: *nada foi preparado nem programado*. Peça de novo."
	beatitudeAvisoSemDestino = "Não achei no WhatsApp a conversa nem o número de %s, então *nada foi preparado para esse destino*. Diga o número com DDD ou encaminhe o contato para este grupo e peça de novo."
)

// beatitudeTratarRascunhos tira o bloco da resposta do Claude, confere cada
// destino, grava os validos num lote novo e ja agenda os que tem horario com
// folga. Nunca fica calado: rascunho descartado vira aviso no cartao.
func beatitudeTratarRascunhos(resposta, grupo, pedido string, anexos ...beatitudeAnexo) beatitudeRascunhosInfo {
	m := beatitudeBlocoRe.FindStringSubmatch(resposta)
	info := beatitudeRascunhosInfo{Visivel: beatitudeTirarMarca(beatitudeBlocoRe.ReplaceAllString(resposta, ""))}
	if m == nil {
		if beatitudePrometeRascunhoRe.MatchString(info.Visivel) && !beatitudeFalaDeAcaoRe.MatchString(info.Visivel) {
			info.Cartao = beatitudeAvisoSemBloco
		}
		return info
	}
	var lista []beatitudeRascunho
	if err := json.Unmarshal([]byte(m[1]), &lista); err != nil || len(lista) == 0 {
		info.Cartao = beatitudeAvisoBlocoRuim
		return info
	}
	agora := time.Now()
	comHorario := beatitudePedidoTemHorario(pedido)
	lote := agora.UnixNano()

	// Numeracao continua: os numeros das programadas em aberto ficam
	// reservados, para "cancela 3" valer sempre para uma mensagem so. Se o
	// mordomo numerou por cima deles, a ponte desloca o lote inteiro.
	desloca := 0
	if maior := beatitudeMaiorNumeroAberto(agora); maior > 0 {
		menor := 0
		for _, r := range lista {
			if r.N > 0 && (menor == 0 || r.N < menor) {
				menor = r.N
			}
		}
		if menor > 0 && menor <= maior {
			desloca = maior - menor + 1
		}
	}

	var b strings.Builder
	var resumo []string
	var pendentes, programados []int
	agendados, travados := 0, 0
	var semArquivo, semDestino []string // rascunhos que cairam, com o motivo dito no cartao
	var unicoQuando time.Time
	for _, r := range lista {
		texto := strings.TrimSpace(r.Texto)
		if r.N <= 0 || texto == "" || len([]rune(texto)) > 4000 || r.ChatJID == grupo {
			continue
		}
		jid, semConversa, ok := beatitudeResolverDestino(r.ChatJID)
		if !ok { // destino inventado ou desconhecido: cai, e o cartao diz
			semDestino = append(semDestino, beatitudeRotuloDestino(r.ChatJID))
			continue
		}
		nome := beatitudeNomeDestino(jid)
		n := r.N + desloca
		var anexoID, anexoNome interface{}
		var anexoMsgID, anexoTxt string
		if strings.TrimSpace(r.Anexo) != "" {
			a, ok := beatitudeAcharAnexo(anexos, r.Anexo)
			if !ok { // codigo inventado: nao manda o texto prometendo um arquivo que nao vai
				semArquivo = append(semArquivo, nome)
				continue
			}
			anexoID, anexoNome = a.MsgID, a.Nome
			anexoMsgID, anexoTxt = a.MsgID, a.Nome
		}
		quando, soAgora, motivo := beatitudeClassificarHorario(r.EnviarEm, comHorario, agora)
		var enviarEm interface{}
		if !quando.IsZero() {
			enviarEm = quando
		}
		res, err := panel.store.db.Exec(
			`INSERT INTO beatitude_rascunhos (lote, n, chat_jid, texto, criado_em, enviar_em, so_agora, anexo_msg_id, anexo_nome)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			lote, n, jid, texto, agora, enviarEm, soAgora, anexoID, anexoNome)
		if err != nil {
			continue
		}
		id, _ := res.LastInsertId()

		// Horario com folga, conversa conhecida: ja fica programada. Numero
		// sem conversa anterior nunca sai sem o sim (pode ser numero errado).
		programada := false
		if !quando.IsZero() && quando.Sub(agora) > beatitudeFolgaSemSim && !semConversa {
			linha := beatitudeRascunhoLinha{id: id, n: n, jid: jid, texto: texto, enviarEm: &quando,
				anexoMsgID: anexoMsgID, anexoNome: anexoTxt}
			if caminho, cerr := beatitudeCaminhoDoRascunho(linha); cerr == nil {
				if ag, aerr := beatitudeAgendar(linha, nome, texto, caminho); aerr == nil {
					_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET agendamento_id = ? WHERE id = ?`, ag, id)
					beatitudeRegistrar(agora, "programa "+strconv.Itoa(n), "programado para "+nome+" em "+quando.In(panelLoc).Format("02/01 15:04"), "agendado", "")
					programada = true
				}
			}
		}

		cab := fmt.Sprintf("*%d · %s*", n, nome)
		linhaResumo := "Mensagem para " + nome
		if semConversa {
			cab += " · número sem conversa anterior"
		}
		switch {
		case programada:
			programados = append(programados, n)
			cab += " · programada para *" + beatitudeQuando(quando, agora) + "*"
			linhaResumo += " programada para " + beatitudeQuando(quando, agora)
		case !quando.IsZero():
			pendentes = append(pendentes, n)
			agendados++
			unicoQuando = quando
			cab += " · sai *" + beatitudeQuando(quando, agora) + "*"
			linhaResumo += " para " + beatitudeQuando(quando, agora) + ", esperando o seu sim"
		case soAgora:
			pendentes = append(pendentes, n)
			travados++
			cab += " · " + motivo
			linhaResumo += " sem horário para sair"
		default:
			pendentes = append(pendentes, n)
			linhaResumo += " esperando o seu sim"
		}
		if anexoNome != nil {
			cab += " · com o arquivo *" + anexoTxt + "*"
		}
		fmt.Fprintf(&b, "%s\n%s\n\n", cab, texto)
		resumo = append(resumo, linhaResumo+".")
	}

	var avisos []string
	if len(semDestino) > 0 {
		avisos = append(avisos, fmt.Sprintf(beatitudeAvisoSemDestino, strings.Join(semDestino, ", ")))
	}
	if len(semArquivo) > 0 {
		avisos = append(avisos, "Não achei o arquivo para "+strings.Join(semArquivo, ", ")+
			". Mande ou encaminhe o arquivo para este grupo e repita o pedido.")
	}
	info.Programados, info.Pendentes = len(programados), len(pendentes)
	info.Resumo = strings.Join(append(resumo, avisos...), "\n")
	if info.Programados+info.Pendentes == 0 {
		if len(avisos) == 0 { // o bloco veio, mas nenhum item servia
			avisos = []string{beatitudeAvisoBlocoRuim}
			info.Resumo = beatitudeAvisoBlocoRuim
		}
		info.Cartao = strings.Join(avisos, "\n")
		return info
	}

	var rodape []string
	switch {
	case len(programados) == 1:
		p := strconv.Itoa(programados[0])
		rodape = append(rodape, "Já está programada e sai sozinha no horário. Para mudar o texto: *envia "+p+": seu texto*. Para desistir: *cancela "+p+"* ou ❌.")
	case len(programados) > 1:
		p := strconv.Itoa(programados[0])
		rodape = append(rodape, "As programadas saem sozinhas no horário. Para mudar um texto: *envia "+p+": seu texto*. Para desistir: *cancela "+p+"*. Para ver todas: *programadas*.")
	}
	if len(pendentes) > 0 {
		p := strconv.Itoa(pendentes[0])
		so := len(pendentes) == 1
		switch {
		case so && agendados == 1:
			rodape = append(rodape, "Para aprovar, responda *sim* (ou *"+p+"*) ou reaja com 👍: sai "+beatitudeQuando(unicoQuando, agora)+", não antes. Para mudar o texto: *envia "+p+": seu texto*. ❌ cancela. Nada sai sem isso.")
		case so && travados == 1:
			rodape = append(rodape, "Você citou um horário e este rascunho não tem horário para sair, então o *sim* não envia. Diga o horário (ex.: *às 8h30*), ou responda *agora* para enviar já.")
		case so:
			rodape = append(rodape, "Para enviar, responda *sim* (ou *"+p+"*) ou reaja com 👍. Para mudar o texto: *envia "+p+": seu texto*. ❌ cancela. Nada sai sem isso.")
		default:
			if len(pendentes) >= beatitudeEnqueteMinimo {
				rodape = append(rodape, "Marque na enquete abaixo os que devem sair e confirme com 👍 nela (ou escreva *envia*). Também vale responder com os números: *"+p+"* ou *todos*. Para mudar o texto: *envia "+p+": seu texto*. Nada sai sem isso.")
			} else if len(programados) > 0 {
				rodape = append(rodape, "Os outros esperam o seu sim: responda com o número (*"+p+"*) ou *todos*. Para mudar o texto: *envia "+p+": seu texto*.")
			} else {
				rodape = append(rodape, "Para aprovar, responda com o número: *"+p+"*, *"+p+" e "+strconv.Itoa(pendentes[1])+"* ou *todos*, ou reaja com 👍 para aprovar todos deste cartão. Para mudar o texto: *envia "+p+": seu texto*. Nada sai sem isso.")
			}
			if agendados > 0 {
				rodape = append(rodape, "Os que têm horário só saem no horário.")
			}
			if travados > 0 {
				rodape = append(rodape, "Os marcados \"sem horário marcado\" ou com horário que passou não saem com o número: diga o horário, ou responda *"+p+" agora* para enviar já.")
			}
		}
	}
	b.WriteString(strings.Join(append(rodape, avisos...), "\n"))
	info.Cartao = strings.TrimSpace(b.String())
	return info
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

// ---------------------------------------------------------------- destino
//
// Tres ganchos, trocados nos testes: a agenda de contatos do WhatsApp, o
// outro identificador da mesma pessoa (telefone <-> LID) e a consulta ao
// WhatsApp sobre um numero.

var beatitudeContatoNome = func(jid string) string {
	if panel.client == nil || panel.client.Store == nil || panel.client.Store.Contacts == nil {
		return ""
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := panel.client.Store.Contacts.GetContact(ctx, j)
	if err != nil || !c.Found {
		return ""
	}
	for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return ""
}

var beatitudeJIDAlternativo = func(jid string) string {
	if panel.client == nil || panel.client.Store == nil {
		return ""
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return ""
	}
	alt, err := panel.client.Store.GetAltJID(context.Background(), j)
	if err != nil || alt.IsEmpty() {
		return ""
	}
	return alt.String()
}

var beatitudeNumeroNoWhatsApp = func(tel string) (string, error) {
	if panel.client == nil {
		return "", fmt.Errorf("sem conexão")
	}
	return ponteJIDDoTelefone(tel)
}

// beatitudeTelefoneDoJID: os digitos, so para JID de telefone bem formado.
func beatitudeTelefoneDoJID(jid string) string {
	partes := strings.SplitN(jid, "@", 2)
	if len(partes) != 2 || partes[1] != types.DefaultUserServer || len(partes[0]) < 10 || len(partes[0]) > 15 {
		return ""
	}
	for _, c := range partes[0] {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return partes[0]
}

// beatitudeResolverDestino confere o destino de um rascunho. Aceita a
// conversa que esta no banco (pelo JID dado ou pelo outro identificador da
// mesma pessoa) e, desde 25-11, um telefone que o WhatsApp confirma mesmo
// sem conversa anterior (contato encaminhado, numero ditado): esse volta
// com semConversa, e o chamador nunca o envia sem o sim.
func beatitudeResolverDestino(jid string) (final string, semConversa, ok bool) {
	jid = strings.TrimSpace(jid)
	if _, existe := beatitudeNomeDoChat(jid); existe {
		return jid, false, true
	}
	if !strings.Contains(jid, "@") || strings.HasSuffix(jid, "@g.us") {
		return "", false, false
	}
	if alt := beatitudeJIDAlternativo(jid); alt != "" {
		if _, existe := beatitudeNomeDoChat(alt); existe {
			return alt, false, true
		}
	}
	tel := beatitudeTelefoneDoJID(jid)
	if tel == "" {
		return "", false, false
	}
	certo, err := beatitudeNumeroNoWhatsApp(tel)
	if err != nil || certo == "" {
		return "", false, false
	}
	if _, existe := beatitudeNomeDoChat(certo); existe { // o WhatsApp devolveu o JID de uma conversa conhecida
		return certo, false, true
	}
	return certo, true, true
}

// beatitudeTelefoneLegivel: "5511950380882" -> "+55 11 95038-0882".
func beatitudeTelefoneLegivel(tel string) string {
	tel = strings.TrimLeft(tel, "+")
	if strings.HasPrefix(tel, "55") && (len(tel) == 12 || len(tel) == 13) {
		resto := tel[4:]
		return "+55 " + tel[2:4] + " " + resto[:len(resto)-4] + "-" + resto[len(resto)-4:]
	}
	return "+" + tel
}

// beatitudeRotuloDestino: como citar no aviso um destino que caiu.
func beatitudeRotuloDestino(jid string) string {
	if tel := beatitudeTelefoneDoJID(jid); tel != "" {
		return beatitudeTelefoneLegivel(tel)
	}
	if strings.TrimSpace(jid) == "" {
		return "um destino sem identificação"
	}
	return "\"" + beatitudeCurto(jid, 40) + "\""
}

// beatitudeNomeDestino: o nome que aparece no cartao e nas confirmacoes.
// Nome da conversa; sem ele, o nome na agenda de contatos; sem ele, o
// telefone legivel (antes saia "5511950380882").
func beatitudeNomeDestino(jid string) string {
	bruto := strings.SplitN(jid, "@", 2)[0]
	nome, _ := beatitudeNomeDoChat(jid)
	if nome != "" && nome != bruto {
		return nome
	}
	if n := beatitudeContatoNome(jid); n != "" {
		return n
	}
	if alt := beatitudeJIDAlternativo(jid); alt != "" {
		if n, _ := beatitudeNomeDoChat(alt); n != "" && n != strings.SplitN(alt, "@", 2)[0] {
			return n
		}
		if n := beatitudeContatoNome(alt); n != "" {
			return n
		}
		if tel := beatitudeTelefoneDoJID(alt); tel != "" {
			return beatitudeTelefoneLegivel(tel)
		}
	}
	if tel := beatitudeTelefoneDoJID(jid); tel != "" {
		return beatitudeTelefoneLegivel(tel)
	}
	return bruto
}

// ---------------------------------------------------------------- programadas

const beatitudeSQLProgramadaAberta = `agendamento_id IS NOT NULL AND enviado_em IS NULL AND cancelado_em IS NULL AND enviar_em >= ?`

// beatitudeMaiorNumeroAberto: o maior numero entre as programadas que ainda
// vao sair. Zero quando nao ha nenhuma (a numeracao volta ao 1).
func beatitudeMaiorNumeroAberto(agora time.Time) int {
	if panel.store == nil {
		return 0
	}
	var n int
	_ = panel.store.db.QueryRow(`SELECT COALESCE(MAX(n),0) FROM beatitude_rascunhos WHERE `+beatitudeSQLProgramadaAberta,
		agora.Add(-time.Hour)).Scan(&n)
	return n
}

// beatitudeProgramadasAbertas: as que ainda vao sair, pela ordem de saida.
func beatitudeProgramadasAbertas(agora time.Time) []beatitudeRascunhoLinha {
	if panel.store == nil {
		return nil
	}
	rows, err := panel.store.db.Query(`SELECT id FROM beatitude_rascunhos WHERE `+beatitudeSQLProgramadaAberta+
		` ORDER BY enviar_em, n`, agora.Add(-time.Hour))
	if err != nil {
		return nil
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []beatitudeRascunhoLinha
	for _, id := range ids {
		if r, err := beatitudeLerRascunho(`id = ?`, id); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// "programadas", "agendadas", "quais as mensagens programadas?"
var beatitudeProgramadasRe = regexp.MustCompile(`(?i)^\s*(?:(?:quais|ver|veja|lista|listar|mostra|mostrar|me\s+mostra)\s+)?(?:(?:as|minhas|são\s+as|sao\s+as)\s+)?(?:mensagens\s+)?(?:programadas|agendadas)\s*[?.!]?\s*$`)

// beatitudeListarProgramadas responde "programadas" sem chamar o Claude.
func beatitudeListarProgramadas(agora time.Time) []string {
	abertas := beatitudeProgramadasAbertas(agora)
	if len(abertas) == 0 {
		return []string{beatitudeMarca + ": nenhuma mensagem programada por este grupo."}
	}
	linhas := []string{beatitudeMarca, "", "*Programadas*"}
	for _, r := range abertas {
		q := ""
		if r.enviarEm != nil {
			q = " · " + beatitudeQuando(*r.enviarEm, agora)
		}
		arq := ""
		if r.anexoNome != "" {
			arq = " · com " + r.anexoNome
		}
		linhas = append(linhas, fmt.Sprintf("*%d · %s*%s%s\n%s", r.n, beatitudeNomeDestino(r.jid), q, arq, beatitudeCurto(strings.ReplaceAll(r.texto, "\n", " "), 140)))
	}
	linhas = append(linhas, "", "Para mudar o texto: *envia N: seu texto*. Para desistir: *cancela N*. Para mandar já: *N agora*.")
	return linhas
}

// beatitudeBlocoProgramadas entra no pedido ao Claude: ele nao cancela nem
// muda nada, mas sabe dizer o numero certo a ela.
func beatitudeBlocoProgramadas(agora time.Time) string {
	abertas := beatitudeProgramadasAbertas(agora)
	if len(abertas) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Mensagens já programadas por este grupo (a ponte envia sozinha no horário):\n")
	maior := 0
	for _, r := range abertas {
		q := ""
		if r.enviarEm != nil {
			q = r.enviarEm.In(panelLoc).Format("02/01 15:04")
		}
		fmt.Fprintf(&b, "- %d · %s · %s · %s\n", r.n, beatitudeNomeDestino(r.jid), q, beatitudeCurto(strings.ReplaceAll(r.texto, "\n", " "), 100))
		if r.n > maior {
			maior = r.n
		}
	}
	fmt.Fprintf(&b, "Esses números estão ocupados: numere os rascunhos novos a partir de %d. Para ela mudar ou desistir de uma programada, diga o comando (*envia N: novo texto*, *cancela N*); você não muda nem cancela.", maior+1)
	return b.String()
}

var (
	// "envia 1", "manda 1 e 3", "pode enviar todos", e tambem so "1", "1 e 3", "todos"
	beatitudeEnviaRe      = regexp.MustCompile(`(?is)^\s*(?:(?:pode\s+)?(?:envi(?:a|ar|e)|mand(?:a|ar|e))\s+)?(todos|tudo|\d+(?:[\s,e]+\d+)*)\s*[.!]?\s*$`)
	beatitudeEnviaTextoRe = regexp.MustCompile(`(?is)^\s*(?:envi(?:a|ar|e)|mand(?:a|ar|e))\s+(\d+)\s*:\s*(.+)$`)
	// "sim", "pode", "ok", "pode enviar", "manda": so quando ha UM rascunho pendente
	beatitudeSimRe = regexp.MustCompile(`(?is)^\s*(sim|s|ok|pode|pode\s+enviar|pode\s+mandar|envia|manda|enviar|mandar)\s*[.!]?\s*$`)
	// "SIM, enviar amanha, 8h", "ok pode mandar", "sim, envia às 8h30": um sim
	// seguido só de palavras de envio e de horário (25-11). "sim, mas troca o
	// texto" não casa e segue para o Claude.
	beatitudeSimLongoRe = regexp.MustCompile(`(?is)^\s*(?:sim|ok|pode)\b[\s,.;!-]*(?:(?:por\s+favor|pode|isso|ent[ãa]o|envi(?:a|ar|e)|mand(?:a|ar|e)|amanh[ãa]|hoje|cedo|de\s+manh[ãa]|(?:[àa]s\s+)?(?:[01]?\d|2[0-3])\s*(?:[:;.h]\s*[0-5]\d|h|\s*horas?)|[àa]s\s+(?:[01]?\d|2[0-3]))[\s,.;!-]*)*$`)
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
	if !beatitudeEnviaRe.MatchString(texto) && !beatitudeEhSim(texto) {
		return false
	}
	if beatitudeEnviaRe.MatchString(texto) && regexp.MustCompile(`(?i)^\s*(pode\s+)?(envi|mand)`).MatchString(texto) {
		return true // "envia 1" explicito: sempre comando (responde se nao houver rascunho)
	}
	pend := beatitudePendentes(beatitudeJanelaResposta)
	if !beatitudeEnviaRe.MatchString(texto) { // "sim" e parecidos: so com UM rascunho esperando
		return pend == 1
	}
	return pend > 0
}

// beatitudeEhSim: "sim", "ok", "pode enviar" e o sim com horario junto.
func beatitudeEhSim(texto string) bool {
	return beatitudeSimRe.MatchString(texto) || beatitudeSimLongoRe.MatchString(texto)
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
	anexoMsgID  string
	anexoNome   string
}

func beatitudeLerRascunho(where string, args ...interface{}) (beatitudeRascunhoLinha, error) {
	var r beatitudeRascunhoLinha
	err := panel.store.db.QueryRow(
		`SELECT id, n, chat_jid, texto, enviado_em, cancelado_em, enviar_em, agendamento_id, COALESCE(so_agora,0),
		        COALESCE(anexo_msg_id,''), COALESCE(anexo_nome,'')
		 FROM beatitude_rascunhos WHERE `+where, args...).
		Scan(&r.id, &r.n, &r.jid, &r.texto, &r.enviado, &r.cancelado, &r.enviarEm, &r.agendamento, &r.soAgora,
			&r.anexoMsgID, &r.anexoNome)
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

	type alvo = beatitudeAlvo
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
			return []string{beatitudeMarca + ": diga qual: *cancela 1*, *cancela 2*… (*programadas* lista o que vai sair)"}
		}
	case modoAgora:
		if m := beatitudeAgoraRe.FindStringSubmatch(texto); m[1] != "" {
			n, _ := strconv.Atoi(m[1])
			alvos = append(alvos, alvo{n: n})
		} else if alvos = unico(); alvos == nil {
			return []string{beatitudeMarca + ": diga qual: *1 agora*, *2 agora*…"}
		}
	case beatitudeEhSim(texto) && !beatitudeEnviaRe.MatchString(texto) && !beatitudeEnviaTextoRe.MatchString(texto):
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
	if ultimoLote == 0 && (len(alvos) == 0 || alvos[0].id == 0) && beatitudeAgendadosAbertos() == 0 {
		return []string{beatitudeMarca + ": não há rascunho esperando envio. Peça \"pendentes\" primeiro."}
	}
	if len(alvos) == 0 {
		return []string{beatitudeMarca + ": não achei o que enviar. Use, por exemplo, *envia 1*."}
	}
	return beatitudeAplicar(alvos, ultimoLote, modoAgora, modoCancela, agora)
}

// beatitudeAlvo: um rascunho apontado por um comando. Com id, vale o id (de
// qualquer lote); sem id, vale o N do ultimo lote.
type beatitudeAlvo struct {
	n     int
	id    int64
	texto string // vazio = usar o rascunho
}

// beatitudeAplicar executa envio, agendamento, "agora" ou cancelamento nos
// alvos. Usado pelos comandos do grupo e pelos comandos vindos do app.
func beatitudeAplicar(alvos []beatitudeAlvo, ultimoLote int64, modoAgora, modoCancela bool, agora time.Time) []string {
	var linhas []string
	for _, a := range alvos {
		var r beatitudeRascunhoLinha
		var err error
		if a.id > 0 {
			r, err = beatitudeLerRascunho(`id = ?`, a.id)
		} else {
			r, err = beatitudeAcharPorNumero(ultimoLote, a.n, agora)
		}
		if err != nil {
			linhas = append(linhas, fmt.Sprintf("%d: não existe na última lista nem entre as programadas", a.n))
			continue
		}
		nome := beatitudeNomeDestino(r.jid)
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
			linhas = append(linhas, beatitudeCancelarRascunho(r, agora, "cancela "+strconv.Itoa(r.n)))
			continue
		}

		final := r.texto
		if a.texto != "" {
			final = a.texto
		}

		// programada e esperando a hora: "envia N: texto" troca o texto e
		// mantem o horario; fora isso, so "agora" ou "cancela" mexem nela
		if r.agendamento != nil && !modoAgora && a.texto != "" {
			q := ""
			if r.enviarEm != nil {
				q = ", sai *" + beatitudeQuando(*r.enviarEm, agora) + "*"
			}
			if _, err := panel.store.db.Exec(`UPDATE panel_schedules SET message = ? WHERE id = ? AND enabled = 1`, final, *r.agendamento); err != nil {
				linhas = append(linhas, rot+": não consegui trocar o texto ("+err.Error()+"). Segue o texto anterior.")
				continue
			}
			_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET texto = ?, mudou_em = ? WHERE id = ?`, final, agora, r.id)
			ponteAgendadasMudaram()
			linhas = append(linhas, rot+": texto trocado"+q+".\n"+final)
			beatitudeRegistrar(agora, "muda "+strconv.Itoa(r.n), "texto trocado para "+nome, "agendado", "")
			continue
		}
		if r.agendamento != nil && !modoAgora {
			q := ""
			if r.enviarEm != nil {
				q = " (" + beatitudeQuando(*r.enviarEm, agora) + ")"
			}
			linhas = append(linhas, rot+": já está programada e sai no horário"+q+". Para mandar já: *"+strconv.Itoa(r.n)+" agora*; para desistir: *cancela "+strconv.Itoa(r.n)+"*")
			continue
		}

		if !modoAgora {
			if r.enviarEm != nil && r.enviarEm.After(agora) {
				caminho, err := beatitudeCaminhoDoRascunho(r)
				if err != nil {
					linhas = append(linhas, rot+": não agendei, o arquivo "+r.anexoNome+" não está disponível ("+err.Error()+"). Mande o arquivo de novo no grupo e repita o pedido.")
					continue
				}
				id, err := beatitudeAgendar(r, nome, final, caminho)
				if err != nil {
					linhas = append(linhas, rot+": não consegui agendar ("+err.Error()+"). Nada foi enviado.")
					continue
				}
				_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET agendamento_id = ?, texto = ?, mudou_em = ? WHERE id = ?`, id, final, agora, r.id)
				ponteAgendadasMudaram()
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
		// o arquivo e conferido antes: se nao estiver disponivel, nem o texto sai
		caminho, err := beatitudeCaminhoDoRascunho(r)
		if err != nil {
			linhas = append(linhas, rot+": não enviei, o arquivo "+r.anexoNome+" não está disponível ("+err.Error()+"). Mande o arquivo de novo no grupo e repita o pedido.")
			continue
		}
		ok, msg := panelSend(r.jid, final, "beatitude-aprovado")
		if !ok {
			linhas = append(linhas, fmt.Sprintf("%s: não foi (%s)", rot, msg))
			continue
		}
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ?, mudou_em = ?, texto = ? WHERE id = ?`, time.Now(), time.Now(), final, r.id)
		if r.agendamento != nil {
			ponteAgendadasMudaram()
		}
		if caminho != "" {
			if ok, msg := panelSendArquivo(r.jid, caminho, "beatitude-aprovado"); !ok {
				linhas = append(linhas, rot+": texto enviado, mas o arquivo "+r.anexoNome+" não foi ("+msg+"). Anexe à mão na conversa.")
				beatitudeRegistrar(time.Now(), "envia "+strconv.Itoa(r.n), "enviado para "+nome+" sem o arquivo", "erro", msg)
				continue
			}
			linhas = append(linhas, rot+": enviado, com o arquivo "+r.anexoNome)
			beatitudeRegistrar(time.Now(), "envia "+strconv.Itoa(r.n), "enviado para "+nome+" com "+r.anexoNome, "envio", "")
			continue
		}
		linhas = append(linhas, rot+": enviado")
		beatitudeRegistrar(time.Now(), "envia "+strconv.Itoa(r.n), "enviado para "+nome, "envio", "")
	}
	return linhas
}

// beatitudeAgendar cria o agendamento unico no agendador do painel.
func beatitudeAgendar(r beatitudeRascunhoLinha, nome, texto, caminho string) (int64, error) {
	if r.enviarEm == nil {
		return 0, fmt.Errorf("sem horário")
	}
	q := r.enviarEm.In(panelLoc)
	var midia interface{}
	if caminho != "" {
		midia = caminho
	}
	res, err := panel.store.db.Exec(
		`INSERT INTO panel_schedules
		 (recipient, display, message, mode, at_time, on_date, enabled, created_at, media_path)
		 VALUES (?, ?, ?, 'once', ?, ?, 1, ?, ?)`,
		r.jid, "Beatitude · "+nome, texto, q.Format("15:04"), q.Format("2006-01-02"), time.Now(), midia)
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
	nome := beatitudeNomeDestino(r.jid)
	hora := quando.In(panelLoc).Format("15:04")
	if status == "enviada" {
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ?, mudou_em = ? WHERE id = ?`, time.Now(), time.Now(), r.id)
		ponteAgendadasMudaram()
		beatitudeRegistrar(time.Now(), "agendado "+strconv.Itoa(r.n), "enviado para "+nome+" às "+hora, "envio", "")
		if r.anexoNome != "" {
			beatitudeSend(beatitudeMarca + ": enviei às " + hora + " para " + nome + ", com o arquivo " + r.anexoNome + ", como combinado.")
			return
		}
		beatitudeSend(beatitudeMarca + ": enviei às " + hora + " para " + nome + ", como combinado.")
		return
	}
	if strings.HasPrefix(status, "enviada sem o arquivo") { // o texto saiu; o arquivo nao
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ?, mudou_em = ? WHERE id = ?`, time.Now(), time.Now(), r.id)
		ponteAgendadasMudaram()
		beatitudeRegistrar(time.Now(), "agendado "+strconv.Itoa(r.n), "enviado para "+nome+" às "+hora+" sem o arquivo", "erro", status)
		beatitudeSend(beatitudeMarca + ": às " + hora + " enviei o texto para " + nome + ", mas o arquivo " + r.anexoNome +
			" *não foi* (" + strings.TrimPrefix(status, "enviada sem o arquivo: ") + "). Anexe à mão na conversa.")
		return
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET cancelado_em = ?, mudou_em = ?, falha = ? WHERE id = ?`, time.Now(), time.Now(), beatitudeCurto(status, 200), r.id)
	ponteAgendadasMudaram()
	beatitudeRegistrar(time.Now(), "agendado "+strconv.Itoa(r.n), "não enviado para "+nome, "erro", status)
	beatitudeSend(beatitudeMarca + ": a mensagem das " + hora + " para " + nome + " *não saiu* (" + status + "). Se ainda quiser, me peça de novo.")
}

// beatitudeCaminhoDoRascunho: "" quando o rascunho nao leva arquivo.
func beatitudeCaminhoDoRascunho(r beatitudeRascunhoLinha) (string, error) {
	if r.anexoMsgID == "" {
		return "", nil
	}
	beatitude.mu.Lock()
	grupo := beatitude.cfg.GrupoJID
	beatitude.mu.Unlock()
	return beatitudeArquivoDoAnexo(r.anexoMsgID, grupo)
}

// beatitudeAcharPorNumero: o rascunho de numero n. Primeiro no ultimo lote;
// se la nao existe ou ja foi resolvido, entre as programadas em aberto (os
// numeros delas nao se repetem, ver beatitudeTratarRascunhos).
func beatitudeAcharPorNumero(ultimoLote int64, n int, agora time.Time) (beatitudeRascunhoLinha, error) {
	r, err := beatitudeLerRascunho(`lote = ? AND n = ?`, ultimoLote, n)
	if err == nil && r.enviado == nil && r.cancelado == nil {
		return r, nil
	}
	if p, perr := beatitudeLerRascunho(`n = ? AND `+beatitudeSQLProgramadaAberta+` ORDER BY id DESC LIMIT 1`, n, agora.Add(-time.Hour)); perr == nil {
		return p, nil
	}
	return r, err
}
