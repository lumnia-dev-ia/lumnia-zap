package main

// Mensagens agendadas do Beatitude (17-11).
//
// O caso: a Lisiane aprovou uma mensagem para as 8h do dia seguinte, a ponte
// respondeu "para desistir: cancela 1" e, quando ela pediu o cancelamento em
// frase livre ("Cancelar o envio da mensagem para a Jianny…"), o pedido foi
// para o mordomo. Ele olhou o app, não viu nada (o rascunho aprovado pelo
// grupo vive só aqui, em beatitude_rascunhos + panel_schedules) e disse que
// não havia mensagem agendada.
//
// O que este arquivo faz (portado para a 1.1.5 em 26-11, sobre a numeração
// única das programadas que entrou em 25-11):
//   1. A lista ("programadas", "agendadas", "o que vai sair?") é a de
//      beatitudeListarProgramadas; o número de cada uma é o do cartão.
//   2. Cancelamento em frase livre: acha a programada pelo número ou pelo
//      nome do destinatário e cancela sem chamar o Claude. Em dúvida, pergunta.
//   3. "cancela agendada N" / "cancela a programada N" é o mesmo que
//      "cancela N".
//   4. O mordomo recebe a lista no contexto (beatitudeBlocoProgramadas) e a
//      regra de não negar o que está nela (beatitude.go, regra "agendadas").
//   5. Espelho no app: a ponte manda as programadas ao servidor e aplica os
//      comandos que o app devolver (cancelar, agora, texto, hora). É o que
//      alimenta o quadro "Programadas" (beatitude_agendadas_ponte.go).
//
// Nada aqui envia mensagem a terceiros sem a aprovação que já existia: o
// único envio novo é "agora" vindo do app, que é o toque da própria pessoa.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// "agendadas", "programadas", "mensagens agendadas", "o que vai sair?"
	beatitudeAgendadasRe = regexp.MustCompile(`(?is)^\s*(?:(?:lista(?:r)?|ver|mostra(?:r)?|quais(?:\s+s[ãa]o)?)\s+(?:as\s+)?)?(?:(?:minhas\s+)?(?:mensagens\s+)?(?:agendadas?|programadas?)|o\s+que\s+(?:vai|est[áa])\s+(?:sair|agendad\p{L}*|programad\p{L}*))\s*[?.!]?\s*$`)
	// "cancela agendada 2", "cancelar a programada 1"
	beatitudeCancelaAgendadaRe = regexp.MustCompile(`(?is)^\s*(?:cancela(?:r)?|desmarca(?:r)?)\s+(?:a\s+)?(?:agendada|programada)\s+(\d{1,2})\s*[.!]?\s*$`)
	// intenção de cancelar em frase livre
	// Só ordem ou vontade ("cancela", "cancelar", "cancele", "desisti de mandar"),
	// nunca relato ("cancelei o voo", "a Marina cancelou").
	beatitudeCancelaLivreRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:cancel(?:a|ar|e|em|amento)|desmarc(?:a|ar|que)|desist(?:e|ir|i|o)|n[ãa]o\s+(?:precisa\s+(?:mais\s+)?)?(?:envi|mand)\p{L}*|n[ãa]o\s+(?:quero|vou)\s+(?:mais\s+)?(?:envi|mand)\p{L}*)(?:[^\p{L}]|$)`)
	// pedido de mensagem NOVA antes do verbo de cancelar ("manda pra Marina dizendo que cancela…")
	beatitudeCancelaNovaRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:mand|envi|escrev|respond|avis|prepar|diz|dig|fal)\p{L}*(?:[^\p{L}]|$)`)
	// o assunto é uma mensagem (forte) ou algo "agendado" (fraco)
	beatitudeCancelaForteRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:mensage\p{L}*|msg|envio\p{L}*|rascunho\p{L}*|whats\p{L}*|zap)(?:[^\p{L}]|$)`)
	beatitudeCancelaFracoRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:agendad\p{L}*|programad\p{L}*)(?:[^\p{L}]|$)`)
	// o assunto é outra coisa: fica com o mordomo
	beatitudeCancelaOutroRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:reuni[ãa]o|reuni[õo]es|evento\p{L}*|compromisso\p{L}*|consulta\p{L}*|voo\p{L}*|reserva\p{L}*|assinatura\p{L}*|pedido\p{L}*|compra\p{L}*|calend[áa]rio|lembrete\p{L}*)(?:[^\p{L}]|$)`)
	// "… e manda outra dizendo …": há um segundo pedido na mesma frase
	beatitudeCancelaMaisRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:e|mas|depois)\s+(?:envi|mand|escrev|agend|marc|avis|respond|prepar)\p{L}*`)
	beatitudeNumeroSoltoRe = regexp.MustCompile(`\d{1,2}`)
)

const beatitudeCancelaLivreMax = 400 // caracteres; pedido maior que isso é conversa

// beatitudeAgendadasAbertas: as programadas que ainda vão sair, pela ordem de
// saída (a mesma lista de "programadas").
func beatitudeAgendadasAbertas(agora time.Time) []beatitudeRascunhoLinha {
	return beatitudeProgramadasAbertas(agora)
}

// beatitudePendentesAbertos: rascunhos do último lote, dentro da janela, que
// ainda esperam decisão.
func beatitudePendentesAbertos(agora time.Time) []beatitudeRascunhoLinha {
	if panel.store == nil {
		return nil
	}
	rows, err := panel.store.db.Query(`SELECT id FROM beatitude_rascunhos
		WHERE enviado_em IS NULL AND agendamento_id IS NULL AND cancelado_em IS NULL AND criado_em >= ?
		  AND lote = (SELECT MAX(lote) FROM beatitude_rascunhos) ORDER BY n`, agora.Add(-beatitudeJanelaResposta))
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

// beatitudeEhListaAgendadas: "programadas", "agendadas", "o que vai sair?".
func beatitudeEhListaAgendadas(texto string) bool {
	return beatitudeProgramadasRe.MatchString(texto) || beatitudeAgendadasRe.MatchString(texto)
}

// beatitudeLinhaAgendada: "N · nome · quando: texto". N é o número do cartão.
func beatitudeLinhaAgendada(r beatitudeRascunhoLinha, agora time.Time) string {
	nome := beatitudeNomeDestino(r.jid)
	q := "sem horário"
	if r.enviarEm != nil {
		q = beatitudeQuando(*r.enviarEm, agora)
	}
	l := fmt.Sprintf("%d · %s · %s: %s", r.n, nome, q, beatitudeCurto(strings.Join(strings.Fields(r.texto), " "), 80))
	if r.anexoNome != "" {
		l += " (com " + r.anexoNome + ")"
	}
	return l
}

// beatitudeEhCancelaLivre: frase livre pedindo para cancelar uma MENSAGEM,
// quando há algo em aberto. Erra para o lado de não agir: sem palavra de
// mensagem, ou falando de reunião/evento, segue para o mordomo.
func beatitudeEhCancelaLivre(texto string) bool {
	if beatitudeCancelaAgendadaRe.MatchString(texto) {
		return true
	}
	if len([]rune(texto)) > beatitudeCancelaLivreMax || beatitudeCancelaRe.MatchString(texto) {
		return false // o comando exato já tem o seu caminho
	}
	loc := beatitudeCancelaLivreRe.FindStringIndex(texto)
	if loc == nil || beatitudeCancelaNovaRe.MatchString(texto[:loc[0]]) {
		return false
	}
	forte := beatitudeCancelaForteRe.MatchString(texto)
	fraco := beatitudeCancelaFracoRe.MatchString(texto)
	outro := beatitudeCancelaOutroRe.MatchString(texto)
	if !forte && (!fraco || outro) {
		return false
	}
	agora := time.Now()
	return len(beatitudeAgendadasAbertas(agora)) > 0 || len(beatitudePendentesAbertos(agora)) > 0
}

// beatitudeTokensDoNome: pedaços do nome que servem para achar a pessoa na
// frase ("Jianny Batista" -> jianny, batista), sem acento e em minúsculas.
func beatitudeTokensDoNome(nome string) []string {
	var out []string
	for _, p := range beatitudePalavraRe.FindAllString(strings.ToLower(ponteSemAcento(nome)), -1) {
		switch p {
		case "dos", "das", "del", "van", "von", "dra", "sra", "beatitude":
			continue
		}
		out = append(out, p)
	}
	return out
}

func beatitudeFraseCitaNome(frase, nome string) bool {
	palavras := map[string]bool{}
	for _, p := range beatitudePalavraRe.FindAllString(strings.ToLower(ponteSemAcento(frase)), -1) {
		palavras[p] = true
	}
	for _, t := range beatitudeTokensDoNome(nome) {
		if palavras[t] {
			return true
		}
	}
	return false
}

// beatitudeNumeroDaFrase: o número citado, ignorando horas e datas ("8h",
// "08:00", "30/11", "8 horas"). 0 quando não há ou quando há mais de um.
func beatitudeNumeroDaFrase(texto string) int {
	achado := 0
	for _, loc := range beatitudeNumeroSoltoRe.FindAllStringIndex(texto, -1) {
		antes, depois := "", strings.ToLower(texto[loc[1]:])
		if loc[0] > 0 {
			antes = texto[loc[0]-1 : loc[0]]
		}
		if antes == ":" || antes == "/" || antes == "h" || antes == "." || antes == ";" || (antes >= "0" && antes <= "9") {
			continue
		}
		d := strings.TrimLeft(depois, " ")
		if strings.HasPrefix(depois, ":") || strings.HasPrefix(depois, "/") || strings.HasPrefix(depois, ";") ||
			strings.HasPrefix(d, "h") || strings.HasPrefix(d, "da manh") || strings.HasPrefix(d, "da tarde") ||
			strings.HasPrefix(d, "da noite") || strings.HasPrefix(d, "min") || strings.HasPrefix(d, "dia") ||
			(len(depois) > 0 && depois[0] >= '0' && depois[0] <= '9') {
			continue
		}
		n, _ := strconv.Atoi(texto[loc[0]:loc[1]])
		if n == 0 {
			continue
		}
		if achado != 0 && achado != n {
			return 0
		}
		achado = n
	}
	return achado
}

// beatitudeCancelarLivre resolve o alvo e cancela. Devolve as linhas da
// resposta ao grupo (sem a marca).
func beatitudeCancelarLivre(texto string, agora time.Time) []string {
	agendadas := beatitudeAgendadasAbertas(agora)

	// "cancela agendada N": o N do cartão, como em "cancela N"
	if m := beatitudeCancelaAgendadaRe.FindStringSubmatch(texto); m != nil {
		n, _ := strconv.Atoi(m[1])
		for _, r := range agendadas {
			if r.n == n {
				return []string{beatitudeCancelarRascunho(r, agora, "cancela agendada "+m[1])}
			}
		}
		return append([]string{fmt.Sprintf("Não há programada %d.", n), ""}, beatitudeListarProgramadas(agora)[1:]...)
	}

	pendentes := beatitudePendentesAbertos(agora)
	todos := append(append([]beatitudeRascunhoLinha{}, pendentes...), agendadas...)
	if len(todos) == 0 {
		return []string{"Não há mensagem esperando envio neste " + maquina() + "."}
	}

	// 1) pelo nome de quem recebe
	var porNome []beatitudeRascunhoLinha
	for _, r := range todos {
		if beatitudeFraseCitaNome(texto, beatitudeNomeDestino(r.jid)) {
			porNome = append(porNome, r)
		}
	}
	// 2) pelo número do cartão
	var porNumero []beatitudeRascunhoLinha
	if n := beatitudeNumeroDaFrase(texto); n > 0 {
		base := todos
		if len(porNome) > 0 {
			base = porNome
		}
		for _, r := range base {
			if r.n == n {
				porNumero = append(porNumero, r)
			}
		}
	}

	var alvo *beatitudeRascunhoLinha
	switch {
	case len(porNumero) == 1:
		alvo = &porNumero[0]
	case len(porNome) == 1:
		alvo = &porNome[0]
	case len(porNome) == 0 && len(porNumero) == 0 && len(todos) == 1:
		alvo = &todos[0] // só existe uma: é essa
	}
	if alvo == nil {
		linhas := []string{"Qual delas? Diga o número:"}
		if len(pendentes) > 0 {
			for _, r := range pendentes {
				nome := beatitudeNomeDestino(r.jid)
				linhas = append(linhas, fmt.Sprintf("- rascunho %d · %s (ainda sem o seu sim): *cancela %d*", r.n, nome, r.n))
			}
		}
		for _, r := range agendadas {
			linhas = append(linhas, "- "+beatitudeLinhaAgendada(r, agora)+fmt.Sprintf(" → *cancela %d*", r.n))
		}
		return linhas
	}
	linhas := []string{beatitudeCancelarRascunho(*alvo, agora, "cancela (frase livre)")}
	if beatitudeCancelaMaisRe.MatchString(texto) {
		linhas = append(linhas, "O resto do pedido eu não li. Mande em outra mensagem.")
	}
	return linhas
}

// beatitudeCancelarRascunho tira do agendador (se estava lá) e marca o
// rascunho como cancelado. Usado pelo "cancela N", pela frase livre, pela
// reação ❌ e pelo comando vindo do app.
func beatitudeCancelarRascunho(r beatitudeRascunhoLinha, agora time.Time, origem string) string {
	nome := beatitudeNomeDestino(r.jid)
	rot := fmt.Sprintf("%d · %s", r.n, nome)
	switch {
	case r.enviado != nil:
		return rot + ": já tinha sido enviado"
	case r.cancelado != nil:
		return rot + ": já tinha sido cancelado"
	}
	if r.agendamento != nil {
		_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET enabled = 0, last_status = 'cancelada no Beatitude' WHERE id = ?`, *r.agendamento)
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET cancelado_em = ?, mudou_em = ? WHERE id = ?`, agora, agora, r.id)
	beatitudeRegistrar(agora, origem, "cancelado para "+nome, "cancelado", "")
	ponteAgendadasMudaram()
	if r.agendamento != nil && r.enviarEm != nil {
		return rot + ": cancelado, não vai sair (era " + beatitudeQuando(*r.enviarEm, agora) + ")"
	}
	return rot + ": cancelado, não vai sair"
}

func beatitudeExecutarAgendadas(texto string) {
	agora := time.Now()
	var linhas []string
	if beatitudeEhListaAgendadas(texto) {
		beatitudeSend(strings.Join(beatitudeListarProgramadas(agora), "\n"))
		return
	}
	linhas = beatitudeCancelarLivre(texto, agora)
	if len(linhas) == 0 {
		return
	}
	beatitudeSend(beatitudeMarca + "\n\n" + strings.Join(linhas, "\n"))
}

const beatitudeRegraAgendadas = "Mensagens programadas: o que sai sozinho no horário fica guardado na ponte e vem no contexto em \"Mensagens já programadas por este grupo\". Antes de dizer que não há mensagem programada, olhe esse bloco. Você não cancela nem muda horário: se ela pedir para cancelar, mudar ou conferir uma delas, diga em uma linha qual é (número, nome e hora) e o comando (*cancela N*, *envia N: novo texto*, *N agora*), ou *programadas* para ver a lista. Não cite telas do app nem da ponte, e não invente o número."
