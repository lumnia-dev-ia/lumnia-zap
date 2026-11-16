// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_reacoes.go — aprovar com 👍 e escolher numa enquete (16-11, 1.1.3).
//
// Até 3 rascunhos: 👍 no cartão do mordomo aprova os rascunhos daquele
// cartão; ❌ cancela. Com 4 ou mais, vai junto uma enquete aberta (pode marcar
// vários): a pessoa marca os que devem sair e confirma com 👍 na enquete ou
// escrevendo "envia". Marcar não envia nada: o voto chega a cada toque e a
// ponte não sabe quando ela terminou.
//
// A reação e o voto não são um caminho novo de envio. Eles viram o mesmo
// comando de texto ("envia 1 3", "cancela 2") e passam pelas mesmas travas do
// beatitudeComando: horário, "sem horário marcado", arquivo indisponível,
// teto por hora. Valem só no cartão da última lista e dentro da janela de
// resposta.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const (
	beatitudeEnqueteMinimo = 4  // a partir de quantos rascunhos vai a enquete
	beatitudeEnqueteMax    = 12 // limite de opções de uma enquete do WhatsApp
)

var (
	beatitudeNumeroCartaoRe = regexp.MustCompile(`(?m)^\*(\d+) · `)
	beatitudeReacaoVistas   sync.Map // id da reação/voto → já tratado (reconexão repete eventos)
)

func initBeatitudeReacoes() {
	if panel.store == nil {
		return
	}
	_, _ = panel.store.db.Exec(`CREATE TABLE IF NOT EXISTS beatitude_cartoes (
		msg_id    TEXT PRIMARY KEY,
		lote      INTEGER NOT NULL,
		tipo      TEXT NOT NULL,      -- cartao | enquete
		numeros   TEXT NOT NULL,      -- "1,2,3"
		opcoes    TEXT,               -- enquete: JSON com o texto de cada opção, na ordem de numeros
		selecao   TEXT DEFAULT '',    -- enquete: números marcados no último voto
		criado_em TIMESTAMP NOT NULL
	)`)
}

func beatitudeEhJoinha(r string) bool {
	return strings.HasPrefix(r, "👍") // com ou sem tom de pele
}

func beatitudeEhCancelaReacao(r string) bool {
	return r == "❌" || r == "✖️" || r == "🚫"
}

// ---------------------------------------------------------------- envio

// beatitudeEnviarGrupo manda ao grupo e devolve o ID da mensagem.
// Variável para os testes.
var beatitudeEnviarGrupo = func(m *waProto.Message, texto string) (string, error) {
	beatitude.mu.Lock()
	grupo := beatitude.cfg.GrupoJID
	beatitude.mu.Unlock()
	if beatitudeSendHook != nil { // testes antigos interceptam só o texto
		beatitudeSendHook(texto)
		return "", nil
	}
	if grupo == "" || panel.client == nil || !panel.client.IsConnected() {
		return "", fmt.Errorf("ponte sem conexão")
	}
	jid, err := types.ParseJID(grupo)
	if err != nil {
		return "", err
	}
	resp, err := panel.client.SendMessage(context.Background(), jid, m)
	if err != nil {
		return "", err
	}
	recordAPISend(grupo, "beatitude")
	if m.GetConversation() != "" {
		gravarEnviada(panel.client, jid, resp.ID, texto, resp.Timestamp)
	}
	return string(resp.ID), nil
}

// beatitudeEnviarCartoes manda o cartão dos rascunhos (em partes, se for
// longo), guarda o ID de cada parte e, com 4 ou mais rascunhos, a enquete.
// desde: o início da rodada; os rascunhos criados depois são deste lote.
func beatitudeEnviarCartoes(rascunhos string, desde time.Time) {
	lote, numeros := beatitudeLoteDesde(desde)
	for _, parte := range beatitudePartir(rascunhos, beatitudeMaxChars) {
		texto := beatitudeMarca + "\n\n" + parte
		id, err := beatitudeEnviarGrupo(&waProto.Message{Conversation: proto.String(texto)}, texto)
		if err != nil {
			if panel.logger != nil {
				panel.logger.Warnf("Beatitude: falha ao enviar o cartão: %v", err)
			}
			continue
		}
		if id == "" || lote == 0 {
			continue
		}
		var daParte []int
		for _, m := range beatitudeNumeroCartaoRe.FindAllStringSubmatch(parte, -1) {
			n, _ := strconv.Atoi(m[1])
			daParte = append(daParte, n)
		}
		if len(daParte) > 0 {
			beatitudeGuardarCartao(id, lote, "cartao", daParte, nil)
		}
	}
	if lote == 0 || len(numeros) < beatitudeEnqueteMinimo {
		return
	}
	opcoes := beatitudeOpcoesEnquete(lote, numeros)
	if len(opcoes) > beatitudeEnqueteMax {
		opcoes, numeros = opcoes[:beatitudeEnqueteMax], numeros[:beatitudeEnqueteMax]
	}
	nome := "Quais envio? Marque e confirme com 👍 aqui"
	var poll *waProto.Message
	if panel.client != nil {
		poll = panel.client.BuildPollCreation(nome, opcoes, 0)
	} else {
		poll = (&whatsmeow.Client{}).BuildPollCreation(nome, opcoes, 0)
	}
	id, err := beatitudeEnviarGrupo(poll, "")
	if err != nil {
		if panel.logger != nil {
			panel.logger.Warnf("Beatitude: falha ao enviar a enquete: %v", err)
		}
		beatitudeSend(beatitudeMarca + ": não consegui mandar a enquete. Responda com os números: *1 e 3* ou *todos*.")
		return
	}
	if id != "" {
		beatitudeGuardarCartao(id, lote, "enquete", numeros, opcoes)
	}
}

func beatitudeLoteDesde(desde time.Time) (int64, []int) {
	var lote int64
	_ = panel.store.db.QueryRow(`SELECT COALESCE(MAX(lote),0) FROM beatitude_rascunhos WHERE criado_em >= ?`, desde).Scan(&lote)
	if lote == 0 {
		return 0, nil
	}
	rows, err := panel.store.db.Query(`SELECT n FROM beatitude_rascunhos WHERE lote = ? ORDER BY n`, lote)
	if err != nil {
		return lote, nil
	}
	defer rows.Close()
	var ns []int
	for rows.Next() {
		var n int
		if rows.Scan(&n) == nil {
			ns = append(ns, n)
		}
	}
	return lote, ns
}

// beatitudeOpcoesEnquete: "1 · Rodrigo · sai às 20:00". Opção curta; o texto
// inteiro está no cartão logo acima. Opções repetidas não são aceitas pelo
// WhatsApp, e o número no início garante que não se repetem.
func beatitudeOpcoesEnquete(lote int64, numeros []int) []string {
	agora := time.Now()
	var out []string
	for _, n := range numeros {
		r, err := beatitudeLerRascunho(`lote = ? AND n = ?`, lote, n)
		nome := "?"
		if err == nil {
			nome, _ = beatitudeNomeDoChat(r.jid)
		}
		op := fmt.Sprintf("%d · %s", n, nome)
		if err == nil && r.enviarEm != nil {
			op += " · sai " + beatitudeQuando(*r.enviarEm, agora)
		}
		if err == nil && r.anexoNome != "" {
			op += " · com arquivo"
		}
		if len([]rune(op)) > 90 {
			op = string([]rune(op)[:89]) + "…"
		}
		out = append(out, op)
	}
	return out
}

func beatitudeGuardarCartao(id string, lote int64, tipo string, numeros []int, opcoes []string) {
	var op interface{}
	if opcoes != nil {
		b, _ := json.Marshal(opcoes)
		op = string(b)
	}
	_, _ = panel.store.db.Exec(`INSERT OR REPLACE INTO beatitude_cartoes (msg_id, lote, tipo, numeros, opcoes, criado_em)
		VALUES (?, ?, ?, ?, ?, ?)`, id, lote, tipo, beatitudeJuntarNumeros(numeros), op, time.Now())
}

type beatitudeCartao struct {
	id       string
	lote     int64
	tipo     string
	numeros  []int
	opcoes   []string
	selecao  []int
	criadoEm time.Time
}

func beatitudeLerCartao(id string) (beatitudeCartao, bool) {
	var c beatitudeCartao
	var nums, sel string
	var op *string
	err := panel.store.db.QueryRow(`SELECT msg_id, lote, tipo, numeros, opcoes, COALESCE(selecao,''), criado_em
		FROM beatitude_cartoes WHERE msg_id = ?`, id).Scan(&c.id, &c.lote, &c.tipo, &nums, &op, &sel, &c.criadoEm)
	if err != nil {
		return c, false
	}
	c.numeros, c.selecao = beatitudeSepararNumeros(nums), beatitudeSepararNumeros(sel)
	if op != nil {
		_ = json.Unmarshal([]byte(*op), &c.opcoes)
	}
	return c, true
}

// beatitudeEnqueteAberta: a enquete do último lote, ainda dentro da janela.
func beatitudeEnqueteAberta(agora time.Time) (beatitudeCartao, bool) {
	var id string
	err := panel.store.db.QueryRow(`SELECT msg_id FROM beatitude_cartoes WHERE tipo = 'enquete'
		AND lote = (SELECT MAX(lote) FROM beatitude_rascunhos) AND criado_em >= ?
		ORDER BY criado_em DESC LIMIT 1`, agora.Add(-beatitudeJanelaResposta)).Scan(&id)
	if err != nil {
		return beatitudeCartao{}, false
	}
	return beatitudeLerCartao(id)
}

func beatitudeJuntarNumeros(ns []int) string {
	p := make([]string, len(ns))
	for i, n := range ns {
		p[i] = strconv.Itoa(n)
	}
	return strings.Join(p, ",")
}

func beatitudeSepararNumeros(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func beatitudeListaNumeros(ns []int) string {
	p := make([]string, len(ns))
	for i, n := range ns {
		p[i] = strconv.Itoa(n)
	}
	if len(p) == 1 {
		return p[0]
	}
	return strings.Join(p[:len(p)-1], ", ") + " e " + p[len(p)-1]
}

// ---------------------------------------------------------------- recebimento

// beatitudeAoReagir: reação da pessoa no grupo. alvo é a mensagem reagida.
func beatitudeAoReagir(reacaoID, alvo, emoji string, hora time.Time) []string {
	if emoji == "" { // reação retirada: nada a fazer (o agendado se cancela com ❌)
		return nil
	}
	if _, visto := beatitudeReacaoVistas.LoadOrStore(reacaoID, true); visto {
		return nil
	}
	if !beatitudeEhJoinha(emoji) && !beatitudeEhCancelaReacao(emoji) {
		return nil // outras reações não significam nada para a ponte
	}
	c, ok := beatitudeLerCartao(alvo)
	if !ok {
		return nil // reação em mensagem que não é cartão
	}
	if hora.Sub(c.criadoEm) > beatitudeJanelaResposta {
		return []string{"essa lista é de mais de 2 horas atrás. Peça de novo, que eu refaço."}
	}
	var ultimo int64
	_ = panel.store.db.QueryRow(`SELECT COALESCE(MAX(lote),0) FROM beatitude_rascunhos`).Scan(&ultimo)
	if c.lote != ultimo {
		return []string{"essa lista não é a mais recente. Use a última que mandei."}
	}
	if beatitudeEhCancelaReacao(emoji) {
		if c.tipo != "cartao" {
			return nil
		}
		var linhas []string
		for _, n := range c.numeros {
			linhas = append(linhas, beatitudeSemMarca(beatitudeComando("cancela "+strconv.Itoa(n), time.Now()))...)
		}
		return linhas
	}
	alvos := c.numeros
	if c.tipo == "enquete" {
		if len(c.selecao) == 0 {
			return []string{"nada marcado na enquete. Marque os que devem sair e confirme com 👍 nela."}
		}
		alvos = c.selecao
	} else if len(c.numeros) >= beatitudeEnqueteMinimo || beatitudeCartaoComEnquete(c.lote) {
		return []string{"com tantos rascunhos, marque na enquete os que devem sair e confirme com 👍 nela."}
	}
	return beatitudeComando("envia "+beatitudeListaNumeros(alvos), time.Now())
}

func beatitudeCartaoComEnquete(lote int64) bool {
	var n int
	_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM beatitude_cartoes WHERE lote = ? AND tipo = 'enquete'`, lote).Scan(&n)
	return n > 0
}

// beatitudeAoVotar: o voto chega a cada toque, com a seleção inteira.
// Só guarda; quem envia é o 👍 ou o "envia".
func beatitudeAoVotar(enqueteID string, escolhidas [][]byte) {
	c, ok := beatitudeLerCartao(enqueteID)
	if !ok || c.tipo != "enquete" {
		return
	}
	hashes := whatsmeow.HashPollOptions(c.opcoes)
	var sel []int
	for i, h := range hashes {
		for _, e := range escolhidas {
			if bytes.Equal(h, e) && i < len(c.numeros) {
				sel = append(sel, c.numeros[i])
			}
		}
	}
	sort.Ints(sel)
	_, _ = panel.store.db.Exec(`UPDATE beatitude_cartoes SET selecao = ? WHERE msg_id = ?`, beatitudeJuntarNumeros(sel), enqueteID)
}

// beatitudeConfirmarEnquete: "envia", "sim" ou "ok" com uma enquete aberta.
func beatitudeConfirmarEnquete(agora time.Time) ([]string, bool) {
	c, ok := beatitudeEnqueteAberta(agora)
	if !ok {
		return nil, false
	}
	if len(c.selecao) == 0 {
		return []string{"nada marcado na enquete. Marque os que devem sair e confirme com 👍 nela, ou responda com os números."}, true
	}
	return beatitudeComando("envia "+beatitudeListaNumeros(c.selecao), agora), true
}

// beatitudeOnReacaoOuVoto: chamado por beatitudeOnMessage (com beatitude.mu
// preso) para reação e voto da pessoa no grupo. Devolve true se tratou.
func beatitudeOnReacaoOuVoto(msg *events.Message) bool {
	if r := msg.Message.GetReactionMessage(); r != nil {
		id, alvo, emoji, hora := string(msg.Info.ID), r.GetKey().GetID(), r.GetText(), msg.Info.Timestamp
		go func() {
			if linhas := beatitudeAoReagir(id, alvo, emoji, hora); len(linhas) > 0 {
				beatitudeSend(beatitudeMarca + "\n\n" + strings.Join(beatitudeSemMarca(linhas), "\n"))
			}
		}()
		return true
	}
	if u := msg.Message.GetPollUpdateMessage(); u != nil {
		id := string(msg.Info.ID)
		alvo := u.GetPollCreationMessageKey().GetID()
		go func() {
			if _, visto := beatitudeReacaoVistas.LoadOrStore(id, true); visto || panel.client == nil {
				return
			}
			voto, err := panel.client.DecryptPollVote(context.Background(), msg)
			if err != nil {
				if panel.logger != nil {
					panel.logger.Warnf("Beatitude: voto da enquete ilegível: %v", err)
				}
				return
			}
			beatitudeAoVotar(alvo, voto.GetSelectedOptions())
		}()
		return true
	}
	return false
}

func beatitudeSemMarca(linhas []string) []string {
	out := make([]string, 0, len(linhas))
	for _, l := range linhas {
		out = append(out, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l, beatitudeMarca), ":")))
	}
	return out
}
