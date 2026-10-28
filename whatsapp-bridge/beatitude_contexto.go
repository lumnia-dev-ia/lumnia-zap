// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_contexto.go — o que a ponte entrega PRONTO ao Claude para ele
// nao gastar rodadas descobrindo (passos 2 e 3 do plano de 15-10).
//
// Medido em 15-10: "pendentes" levou 19 turnos e 47 s de modelo; um pedido
// de mensagem para a esposa, 8 turnos. Quase tudo era o Claude chamando
// list_chats, search_contacts e list_messages uma conversa por vez. A ponte
// tem o mesmo banco a um SELECT de distancia, entao ela monta:
//
//   1. "quem e quem": as pessoas do circulo (as que o app conhece) com o nome
//      do WhatsApp, o chat_jid e a data da ultima mensagem — o Claude nao
//      precisa procurar a conversa;
//   2. para "pendentes": as conversas candidatas das ultimas 72 h com as
//      ultimas mensagens de cada uma — o Claude responde em uma rodada;
//   3. para um pedido que cita alguem pelo nome: as ultimas mensagens daquela
//      conversa.
//
// Tudo isso ja era legivel pelo Claude pelas ferramentas; nada novo sai do
// Mac. As ferramentas continuam disponiveis para o que a pre-leitura nao
// cobriu. Quem esta fora do circulo entra so no item 2 (o Claude ja podia
// ler essas conversas) e nunca no item 1.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	beatitudeCtxJanelaPendentes = 72 * time.Hour
	beatitudeCtxMaxPendentes    = 12
	beatitudeCtxMsgsPendente    = 6
	beatitudeCtxMsgsCitada      = 15
	beatitudeCtxMaxCitadas      = 3
	beatitudeCtxMaxChars        = 180 // por mensagem injetada
)

var (
	beatitudePendentesRe = regexp.MustCompile(`(?i)\bpendente`)
	beatitudePalavraRe   = regexp.MustCompile(`[\p{L}]{3,}`)
)

// beatitudeContextoPronto monta o bloco "Contexto pronto" para o prompt.
// Vazio quando nao ha nada util (ponte nao pareada, banco vazio).
func beatitudeContextoPronto(grupo, pedido string) string {
	var partes []string
	if q := beatitudeQuemEQuem(grupo); q != "" {
		partes = append(partes, q)
	}
	if beatitudePendentesRe.MatchString(pedido) {
		if p := beatitudeCandidatasPendentes(grupo); p != "" {
			partes = append(partes, p)
		}
	}
	if c := beatitudeConversasCitadas(grupo, pedido); c != "" {
		partes = append(partes, c)
	}
	if len(partes) == 0 {
		return ""
	}
	return "Contexto pronto (lido pela ponte neste " + maquina() + " agora; use antes de chamar ferramentas):\n\n" +
		strings.Join(partes, "\n\n")
}

type beatitudeChatInfo struct {
	JID    string
	Nome   string
	Ultima time.Time
}

// beatitudeChatsIndividuais: conversas individuais com nome, da mais recente
// para a mais antiga, limitadas a n. Exclui o grupo Beatitude.
func beatitudeChatsIndividuais(grupo string, n int) []beatitudeChatInfo {
	if panel.store == nil {
		return nil
	}
	rows, err := panel.store.db.Query(`
		SELECT jid, COALESCE(name,''), COALESCE(last_message_time, '')
		FROM chats
		WHERE jid NOT LIKE '%@g.us' AND jid NOT LIKE '%@broadcast' AND jid NOT LIKE '%@newsletter'
		  AND jid != ? AND jid != 'status@broadcast'
		ORDER BY last_message_time DESC LIMIT ?`, grupo, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []beatitudeChatInfo
	for rows.Next() {
		var c beatitudeChatInfo
		var em interface{}
		if rows.Scan(&c.JID, &c.Nome, &em) != nil {
			continue
		}
		c.Ultima, _ = ponteTempoSQL(em)
		if c.Nome == "" || strings.Contains(c.Nome, "@") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// beatitudeQuemEQuem: pessoas do circulo (telefones que o app conhece) com
// nome do WhatsApp, chat_jid e ha quanto tempo foi a ultima mensagem.
// Sem parentesco: isso o app sabe e a ponte nao; o prompt manda chamar
// ler_perfil so quando o pedido depender disso.
func beatitudeQuemEQuem(grupo string) string {
	circulo := ponteTelefonesDoCirculo()
	if len(circulo) == 0 {
		return ""
	}
	var linhas []string
	for _, c := range beatitudeChatsIndividuais(grupo, 400) {
		tel := strings.TrimLeft(ponteTelefoneDoChat(c.JID), "+")
		if tel == "" || !circulo[tel] {
			continue
		}
		linhas = append(linhas, fmt.Sprintf("- %s · chat_jid %s · última mensagem %s", c.Nome, c.JID, beatitudeHaQuanto(c.Ultima)))
		if len(linhas) >= 40 {
			break
		}
	}
	if len(linhas) == 0 {
		return ""
	}
	return "Pessoas do círculo (nome no WhatsApp · chat_jid para list_messages · última mensagem):\n" + strings.Join(linhas, "\n")
}

// beatitudeCandidatasPendentes: conversas individuais das ultimas 72 h cuja
// ultima mensagem nao e da pessoa, com as ultimas mensagens de cada uma.
// A triagem (o que pede resposta, o que e aviso) fica com o Claude.
func beatitudeCandidatasPendentes(grupo string) string {
	if panel.store == nil {
		return ""
	}
	limite := time.Now().Add(-beatitudeCtxJanelaPendentes)
	rows, err := panel.store.db.Query(`
		SELECT m.chat_jid, COALESCE(c.name,''), MAX(m.timestamp) AS em
		FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE m.timestamp >= ?
		  AND m.chat_jid NOT LIKE '%@g.us' AND m.chat_jid NOT LIKE '%@broadcast'
		  AND m.chat_jid NOT LIKE '%@newsletter' AND m.chat_jid != ? AND m.chat_jid != 'status@broadcast'
		GROUP BY m.chat_jid
		HAVING (SELECT is_from_me FROM messages WHERE chat_jid = m.chat_jid ORDER BY timestamp DESC LIMIT 1) = 0
		ORDER BY em DESC LIMIT ?`, limite, grupo, beatitudeCtxMaxPendentes)
	if err != nil {
		return ""
	}
	type cand struct {
		jid, nome string
		em        time.Time
	}
	var cands []cand
	for rows.Next() {
		var c cand
		var bruto interface{} // MAX(timestamp) volta como texto; ver ponteTempoSQL
		if rows.Scan(&c.jid, &c.nome, &bruto) == nil {
			c.em, _ = ponteTempoSQL(bruto)
			if c.nome == "" {
				c.nome = strings.SplitN(c.jid, "@", 2)[0]
			}
			cands = append(cands, c)
		}
	}
	rows.Close()
	if len(cands) == 0 {
		return "Pendentes: nenhuma conversa individual das últimas 72 h terminou com mensagem de outra pessoa. Diga isso em uma linha; só chame ferramentas se quiser olhar grupos pequenos."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Candidatas a pendente (conversas individuais das últimas 72 h em que a última mensagem é da outra pessoa; %d no total, mais recente primeiro). Triagem é sua: ignore aviso, propaganda e o que não pede resposta. Não chame list_chats nem list_messages para estas; já estão aqui.\n", len(cands))
	for i, c := range cands {
		fmt.Fprintf(&b, "\n%d. %s · chat_jid %s · última %s\n", i+1, c.nome, c.jid, beatitudeHaQuanto(c.em))
		b.WriteString(beatitudeUltimasMensagens(c.jid, beatitudeCtxMsgsPendente))
	}
	return b.String()
}

// beatitudeConversasCitadas: se o pedido cita o nome de alguem com conversa
// individual, as ultimas mensagens dessa conversa. Ate 3 conversas.
func beatitudeConversasCitadas(grupo, pedido string) string {
	palavras := map[string]bool{}
	for _, p := range beatitudePalavraRe.FindAllString(strings.ToLower(ponteSemAcento(pedido)), -1) {
		palavras[p] = true
	}
	if len(palavras) == 0 {
		return ""
	}
	type hit struct {
		c      beatitudeChatInfo
		pontos int
	}
	var hits []hit
	for _, c := range beatitudeChatsIndividuais(grupo, 400) {
		pontos := 0
		for _, t := range beatitudePalavraRe.FindAllString(strings.ToLower(ponteSemAcento(c.Nome)), -1) {
			if palavras[t] {
				pontos++
			}
		}
		if pontos > 0 {
			hits = append(hits, hit{c, pontos})
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].pontos != hits[j].pontos {
			return hits[i].pontos > hits[j].pontos
		}
		return hits[i].c.Ultima.After(hits[j].c.Ultima)
	})
	if len(hits) > beatitudeCtxMaxCitadas {
		hits = hits[:beatitudeCtxMaxCitadas]
	}
	var b strings.Builder
	b.WriteString("Conversas de quem o pedido cita (as últimas mensagens; se houver mais de uma pessoa com esse nome, pergunte qual antes de agir):\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "\n%s · chat_jid %s · última %s\n", h.c.Nome, h.c.JID, beatitudeHaQuanto(h.c.Ultima))
		b.WriteString(beatitudeUltimasMensagens(h.c.JID, beatitudeCtxMsgsCitada))
	}
	return b.String()
}

// beatitudeUltimasMensagens: as ultimas n mensagens da conversa, mais antiga
// primeiro, uma por linha, texto cortado em beatitudeCtxMaxChars.
func beatitudeUltimasMensagens(jid string, n int) string {
	if panel.store == nil {
		return ""
	}
	msgs, err := panel.store.GetMessages(jid, n)
	if err != nil || len(msgs) == 0 {
		return "   (sem mensagens guardadas)\n"
	}
	var b strings.Builder
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		quem := "ela/ele"
		if m.IsFromMe {
			quem = "eu"
		}
		texto := strings.TrimSpace(m.Content)
		if texto == "" && m.MediaType != "" {
			texto = "[" + m.MediaType + "]"
		}
		fmt.Fprintf(&b, "   [%s] %s: %s\n", m.Time.In(panelLoc).Format("02/01 15:04"), quem, beatitudeCurto(texto, beatitudeCtxMaxChars))
	}
	return b.String()
}

// ponteTempoSQL converte o que o sqlite devolve para MAX()/MIN() de uma
// coluna TIMESTAMP. O driver (mattn/go-sqlite3) so parseia texto em time.Time
// quando a coluna e declarada TIMESTAMP; uma agregacao perde o tipo declarado
// e volta como string, e o Scan direto em *time.Time falha. Aceita time.Time
// (coluna direta), string (agregacao) e nil (sem linhas).
func ponteTempoSQL(v interface{}) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, !x.IsZero()
	case string:
		s := strings.TrimSpace(x)
		for _, layout := range []string{
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02T15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			time.RFC3339Nano,
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	case []byte:
		return ponteTempoSQL(string(x))
	}
	return time.Time{}, false
}

func beatitudeHaQuanto(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("há %d dias", int(d.Hours()/24))
	}
}
