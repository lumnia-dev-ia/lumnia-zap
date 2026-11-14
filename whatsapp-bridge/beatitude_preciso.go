// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_preciso.go — quando o Chrome trava num login ou CAPTCHA (1.1.2,
// 14-11). Antes a tarefa só parava e dizia "em qual aba"; a pessoa, longe do
// Mac, não tinha como seguir. Agora:
//   1. o passo com o Chrome NUNCA digita senha nem código e NUNCA resolve
//      CAPTCHA: deixa a aba no ponto e devolve <preciso>{site, o_que}</preciso>;
//   2. a ponte guarda a sessão do Claude e avisa no grupo, com o link
//      "Ver a máquina" (o endereço da tela que ela salvou no app, vindo do
//      servidor no sinal de vida) e o comando *continua N*;
//   3. ela entra na máquina pelo celular, faz a parte dela e responde
//      *continua N*; a ponte retoma A MESMA sessão (claude -p --resume), que
//      já sabe onde parou;
//   4. parada há mais de 30 min, a tarefa é cancelada (com aviso).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const beatitudeNavPausaMax = 30 * time.Minute

const beatitudeRegraPreciso = "Login, código e CAPTCHA: nunca digite senha nem código de verificação e nunca resolva CAPTCHA ou verificação de robô. " +
	"Se a página pedir isso para seguir, deixe a aba aberta exatamente nesse ponto e termine a resposta com um bloco <preciso>{\"site\":\"nome do site\",\"o_que\":\"o que ela precisa fazer, em poucas palavras (ex.: entrar com a senha dela; resolver a verificação de robô; digitar o código que chegou por SMS)\"}</preciso>. " +
	"No texto visível, uma linha dizendo até onde chegou. Ela faz a parte dela no Mac e você continua desta mesma conversa."

var beatitudePrecisoBlocoRe = regexp.MustCompile(`(?s)<preciso>\s*(.*?)\s*</preciso>`)
var beatitudeContinuaRe = regexp.MustCompile(`(?i)^\s*(?:pode\s+)?continu(?:a|e|ar)\s*(\d+)?\s*[.!]?\s*$`)

type beatitudePreciso struct {
	Site string `json:"site"`
	OQue string `json:"o_que"`
}

func initBeatitudePreciso() {
	if panel.store == nil {
		return
	}
	for _, col := range []string{"sessao TEXT", "pausado_em TIMESTAMP", "precisa TEXT", "cancelado_em TIMESTAMP"} {
		_, _ = panel.store.db.Exec(`ALTER TABLE beatitude_navegador ADD COLUMN ` + col)
	}
}

// beatitudeSepararPreciso tira o bloco do texto. Bloco sem "o que" não vale.
func beatitudeSepararPreciso(texto string) (string, *beatitudePreciso) {
	m := beatitudePrecisoBlocoRe.FindStringSubmatch(texto)
	visivel := strings.TrimSpace(beatitudePrecisoBlocoRe.ReplaceAllString(texto, ""))
	if m == nil {
		return visivel, nil
	}
	var p beatitudePreciso
	if json.Unmarshal([]byte(m[1]), &p) != nil || strings.TrimSpace(p.OQue) == "" {
		return visivel, nil
	}
	p.Site = beatitudeCurto(strings.TrimSpace(p.Site), 80)
	p.OQue = beatitudeCurto(strings.TrimSpace(p.OQue), 200)
	return visivel, &p
}

// beatitudeAvisoPreciso: a mensagem ao grupo. Sem sessão não dá para
// continuar; ela termina a tarefa na máquina ou pede de novo.
func beatitudeAvisoPreciso(id int64, p beatitudePreciso, endereco string, comSessao bool) string {
	onde := "no Chrome"
	if p.Site != "" {
		onde = "em " + p.Site
	}
	s := beatitudeMarca + ": preciso de você " + onde + ": " + strings.TrimSuffix(p.OQue, ".") + "."
	if endereco != "" {
		s += "\nToque em *Ver a máquina*: " + endereco
	} else {
		s += "\nEntre no " + maquina() + " (pelo app de tela remota que você usa, ou na frente dele). Salve o endereço da tela no app do Beatitude para eu mandar o link aqui."
	}
	if comSessao {
		s += fmt.Sprintf("\nDepois responda *continua %d*. Eu nunca digito senha nem código. Em 30 minutos sem resposta, cancelo.", id)
	} else {
		s += "\nDepois termine por lá ou me peça de novo."
	}
	return s
}

func beatitudePausarNavegador(id int64, sessao string, p beatitudePreciso) {
	agora := time.Now()
	_, _ = panel.store.db.Exec(`UPDATE beatitude_navegador SET sessao = ?, pausado_em = ?, precisa = ? WHERE id = ?`,
		sessao, agora, p.OQue, id)
	beatitudeSend(beatitudeAvisoPreciso(id, p, ponteEnderecoTela(), sessao != ""))
	if sessao == "" {
		return
	}
	time.AfterFunc(beatitudeNavPausaMax, func() { beatitudeCancelarPausaVencida(id, agora) })
}

// beatitudeCancelarPausaVencida: se a MESMA pausa (mesmo instante) ainda está
// lá, cancela e avisa. Se ela continuou e a tarefa parou de novo, a pausa
// nova tem o seu próprio relógio.
func beatitudeCancelarPausaVencida(id int64, pausa time.Time) {
	res, err := panel.store.db.Exec(`UPDATE beatitude_navegador SET cancelado_em = ?
		WHERE id = ? AND pausado_em = ? AND cancelado_em IS NULL`, time.Now(), id, pausa)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		beatitudeSend(beatitudeMarca + fmt.Sprintf(": cancelei a tarefa %d no Chrome; ficou 30 minutos esperando você. Se ainda quiser, peça de novo.", id))
	}
}

// beatitudeNavPausada: a tarefa que espera "continua". Com número, aquela;
// sem número, a única pausada nos últimos 30 min.
func beatitudeNavPausada(n int64, agora time.Time) (int64, bool) {
	if panel.store == nil {
		return 0, false
	}
	desde := agora.Add(-beatitudeNavPausaMax)
	if n > 0 {
		var id int64
		err := panel.store.db.QueryRow(`SELECT id FROM beatitude_navegador WHERE id = ? AND pausado_em IS NOT NULL
			AND cancelado_em IS NULL AND COALESCE(sessao,'') != ''`, n).Scan(&id)
		return id, err == nil
	}
	rows, err := panel.store.db.Query(`SELECT id FROM beatitude_navegador WHERE pausado_em >= ?
		AND cancelado_em IS NULL AND COALESCE(sessao,'') != ''`, desde)
	if err != nil {
		return 0, false
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) != 1 {
		return 0, false
	}
	return ids[0], true
}

// beatitudeEhContinua: "continua", "continua 3". Só vale com tarefa pausada;
// fora disso segue para o mordomo como conversa.
func beatitudeEhContinua(texto string, agora time.Time) (int64, bool) {
	m := beatitudeContinuaRe.FindStringSubmatch(texto)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return beatitudeNavPausada(n, agora)
}

func beatitudeContinuarNavegador(id int64) {
	var tarefa, sessao, precisa string
	var leitura int
	var pausado *time.Time
	err := panel.store.db.QueryRow(`SELECT tarefa, COALESCE(leitura,0), COALESCE(sessao,''), COALESCE(precisa,''), pausado_em
		FROM beatitude_navegador WHERE id = ? AND cancelado_em IS NULL`, id).Scan(&tarefa, &leitura, &sessao, &precisa, &pausado)
	if err != nil || pausado == nil || sessao == "" {
		beatitudeSend(beatitudeMarca + ": essa tarefa não está esperando você. Se quiser, peça de novo.")
		return
	}
	if time.Since(*pausado) > beatitudeNavPausaMax {
		_, _ = panel.store.db.Exec(`UPDATE beatitude_navegador SET cancelado_em = ? WHERE id = ?`, time.Now(), id)
		beatitudeSend(beatitudeMarca + fmt.Sprintf(": a tarefa %d ficou mais de 30 minutos parada e foi cancelada. Peça de novo.", id))
		return
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_navegador SET pausado_em = NULL WHERE id = ?`, id)
	claude, err := beatitudeAcharClaude()
	if err != nil {
		beatitudeSend(beatitudeMarca + ": não encontrei o Claude Code neste " + maquina() + ".")
		return
	}
	beatitudeSend(beatitudeMarca + ": continuando no Chrome.")
	prompt := "Ela já fez a parte dela (" + precisa + "). Confira na mesma aba que a página seguiu e continue a tarefa de onde parou. " +
		"Se a mesma tela ainda estiver lá, ou aparecer outra que peça senha, código ou CAPTCHA, use o bloco <preciso> de novo."
	beatitudeNavRodar(claude, id, tarefa, prompt, beatitudeNavSistema(leitura == 1), []string{"--resume", sessao})
}

// beatitudeRodarPedidoNavegador: tarefa nova aprovada ou continuação.
func beatitudeRodarPedidoNavegador(p beatitudePedido) {
	if p.Continua {
		beatitudeContinuarNavegador(p.Navegador)
		return
	}
	beatitudeExecutarNavegador(p.Navegador)
}
