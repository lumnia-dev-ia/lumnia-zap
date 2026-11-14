// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_bastao.go — o bastão (1.1.2, 14-11). O mordomo roda com um
// catálogo fechado: não abre os projetos do claude.ai, não lê arquivos do
// Mac, não usa conector fora da lista. Em 14-11 Diego pediu "entra no meu
// Claude e analisa um dos meus projetos" e o grupo só respondeu que não
// dava. Agora, quando o pedido passa do alcance, o mordomo devolve um bloco
// <bastao>{...}</bastao> e a ponte:
//   1. avisa no grupo, numa linha, que isso vai para o Claude dela;
//   2. manda, em mensagem separada e SEM a marca, o texto pronto para copiar
//      e colar numa conversa do Claude (nova ou num projeto). A conversa pode
//      ligar o computador no topo, se precisar do WhatsApp, de arquivos ou do
//      Chrome deste Mac;
//   3. registra o código (B12) no servidor do Beatitude, que só aceita
//      devolver_ao_grupo para códigos conhecidos e com menos de 24 h;
//   4. quando o Claude da conversa chama devolver_ao_grupo, a resposta entra
//      na fila do servidor e volta pela rodada da ponte
//      (/api/ponte/mensagens-do-grupo, campo "bastoes"), postada como
//      "◆ Beatitude · do seu Claude (B12)".
// Mensagem a terceiros nunca sai pelo bastão: o texto colado manda criar a
// ação (criar_acao), que espera o "sim N" dela.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	beatitudeBastaoValidade = 24 * time.Hour
	beatitudeBastaoMaxResp  = 1000
	// a mensagem que ela copia começa assim; a ponte ignora a própria
	// mensagem (não tem a marca ◆ para não poluir o texto colado)
	beatitudeBastaoPrefixo = "[Beatitude · pedido "
)

var beatitudeBastaoBlocoRe = regexp.MustCompile(`(?s)<bastao>\s*(.*?)\s*</bastao>`)
var beatitudeBastoesRe = regexp.MustCompile(`(?i)^\s*bast(õ|o)es\s*[?.!]?\s*$`)

func initBeatitudeBastao() {
	if panel.store == nil {
		return
	}
	_, _ = panel.store.db.Exec(`
	CREATE TABLE IF NOT EXISTS beatitude_bastoes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pedido TEXT,
		porque TEXT,
		criado_em TIMESTAMP,
		registrado_em TIMESTAMP,
		devolvido_em TIMESTAMP
	);`)
}

// beatitudeRegraBastao: a regra do prompt do mordomo (substitui, em 1.1.2,
// "diga que ela liga esse app em claude.ai").
const beatitudeRegraBastao = "Bastão: se o pedido precisa de algo fora do seu alcance (um projeto do claude.ai, arquivos do computador, o código de um app, um conector que não está nas suas ferramentas, ou um trabalho longo de análise), não tente, não diga só que não dá e não ofereça alternativa trabalhosa. " +
	"No fim da resposta ponha um bloco <bastao>{\"pedido\":\"o pedido dela, completo e autocontido, escrito como ela pediria ao Claude, com o contexto que você já tem (nomes, datas, o que já sabe, qual projeto)\",\"porque\":\"o que falta aqui, em poucas palavras (ex.: os seus projetos do claude.ai)\"}</bastao>. " +
	"A ponte avisa e manda o texto pronto para ela colar numa conversa do Claude; a resposta volta ao grupo. No texto visível, no máximo uma linha (ou nada). Um bloco por resposta. " +
	"Site que só precisa do Chrome é <navegador>, não bastão. Nunca chame devolver_ao_grupo: essa ferramenta é da conversa do Claude, não sua."

func beatitudeCodigoBastao(id int64) string { return fmt.Sprintf("B%d", id) }

// beatitudeTextoBastao: o que ela cola na conversa do Claude. Autocontido:
// a conversa nova não sabe nada do grupo.
func beatitudeTextoBastao(codigo, pedido string, comVolta bool) string {
	var b strings.Builder
	b.WriteString(beatitudeBastaoPrefixo + codigo + "]\n")
	b.WriteString(strings.TrimSpace(pedido))
	b.WriteString("\n\nResponda aqui.")
	if comVolta {
		b.WriteString(fmt.Sprintf(" No fim, chame a ferramenta devolver_ao_grupo do Beatitude com pedido \"%s\" e a resposta em até %d caracteres, em português, com formatação de WhatsApp.", codigo, beatitudeBastaoMaxResp))
	}
	b.WriteString("\nSe for preciso mandar mensagem a alguém, não envie: crie a ação com criar_acao do Beatitude (canal whatsapp). Ela vai esperar o meu \"sim\".")
	b.WriteString("\nSe precisar ler o WhatsApp, arquivos ou o Chrome do meu " + maquina() + " e o computador não estiver ligado a esta conversa, diga isso em uma linha e responda com o que der.")
	return b.String()
}

// beatitudeAvisoBastao: a linha com a marca que vem antes do texto a copiar.
func beatitudeAvisoBastao(codigo, porque string, comVolta bool) string {
	s := beatitudeMarca + ": "
	if p := strings.TrimSpace(porque); p != "" {
		s += "daqui eu não alcanço " + strings.TrimSuffix(p, ".") + ". "
	} else {
		s += "isso eu não alcanço daqui. "
	}
	s += "Copie a próxima mensagem e cole numa conversa do Claude, nova ou dentro de um projeto. " +
		"Se ele precisar do WhatsApp, de arquivos ou do Chrome deste " + maquina() + ", ligue o computador no topo da conversa antes de enviar."
	if comVolta {
		s += " A resposta volta aqui com o código *" + codigo + "*."
	}
	return s
}

// beatitudeSepararBastao tira o bloco da resposta. Devolve o texto visível,
// o aviso (com marca), o texto a copiar (sem marca) e o código. Sem bloco
// válido, aviso e texto vêm vazios.
func beatitudeSepararBastao(resposta string) (visivel, aviso, copiar, codigo string) {
	m := beatitudeBastaoBlocoRe.FindStringSubmatch(resposta)
	visivel = strings.TrimSpace(beatitudeBastaoBlocoRe.ReplaceAllString(resposta, ""))
	if m == nil || panel.store == nil {
		return visivel, "", "", ""
	}
	var p struct {
		Pedido string `json:"pedido"`
		Porque string `json:"porque"`
	}
	if json.Unmarshal([]byte(m[1]), &p) != nil || strings.TrimSpace(p.Pedido) == "" {
		return visivel, "", "", ""
	}
	pedido := beatitudeCurto(strings.TrimSpace(p.Pedido), 3000)
	porque := beatitudeCurto(strings.TrimSpace(p.Porque), 160)
	res, err := panel.store.db.Exec(`INSERT INTO beatitude_bastoes (pedido, porque, criado_em) VALUES (?, ?, ?)`, pedido, porque, time.Now())
	if err != nil {
		return visivel, "", "", ""
	}
	id, _ := res.LastInsertId()
	codigo = beatitudeCodigoBastao(id)
	comVolta := pontePareado()
	if comVolta {
		go beatitudeRegistrarBastao(id, codigo, pedido)
	}
	return visivel, beatitudeAvisoBastao(codigo, porque, comVolta), beatitudeTextoBastao(codigo, pedido, comVolta), codigo
}

// beatitudeRegistrarBastao avisa o servidor que o código existe, para que
// devolver_ao_grupo o aceite. Melhor esforço: se o servidor ainda não
// conhece a rota, a resposta fica só na conversa do Claude.
func beatitudeRegistrarBastao(id int64, codigo, pedido string) {
	_, err := ponteChamar("/api/ponte/bastao", map[string]interface{}{
		"codigo":    codigo,
		"resumo":    beatitudeCurto(pedido, 300),
		"expira_em": time.Now().Add(beatitudeBastaoValidade).UTC().Format(time.RFC3339),
	}, nil)
	if err != nil {
		if panel.logger != nil {
			panel.logger.Warnf("Beatitude: bastao %s nao registrado no servidor: %v", codigo, err)
		}
		return
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_bastoes SET registrado_em = ? WHERE id = ?`, time.Now(), id)
}

// ponteBastaoResposta: um item do campo "bastoes" de mensagens-do-grupo.
type ponteBastaoResposta struct {
	Codigo string `json:"codigo"`
	Texto  string `json:"texto"`
}

// beatitudeTextoDevolvido monta o que vai ao grupo quando a resposta volta.
func beatitudeTextoDevolvido(codigo, texto string) string {
	t := strings.TrimSpace(texto)
	if r := []rune(t); len(r) > beatitudeBastaoMaxResp+200 { // folga; o servidor já corta
		t = string(r[:beatitudeBastaoMaxResp+200]) + "…"
	}
	return beatitudeMarca + " · do seu Claude (" + codigo + ")\n\n" + t
}

// ponteBastoesDevolvidos posta as respostas que voltaram e confirma ao
// servidor, uma a uma, para não postar de novo.
func ponteBastoesDevolvidos(itens []ponteBastaoResposta) {
	for _, b := range itens {
		cod := strings.ToUpper(strings.TrimSpace(b.Codigo))
		if cod == "" || strings.TrimSpace(b.Texto) == "" {
			continue
		}
		beatitudeSend(beatitudeTextoDevolvido(cod, b.Texto))
		var id int64
		if _, err := fmt.Sscanf(cod, "B%d", &id); err == nil && panel.store != nil {
			_, _ = panel.store.db.Exec(`UPDATE beatitude_bastoes SET devolvido_em = ? WHERE id = ? AND devolvido_em IS NULL`, time.Now(), id)
		}
		if _, err := ponteChamar("/api/ponte/postado", map[string]interface{}{"tipo": "bastao", "codigo": cod}, nil); err != nil {
			ponteErro(err)
		}
	}
}

// beatitudeListarBastoes responde ao comando "bastões": o que está com o
// Claude dela, esperando volta, nas últimas 24 h.
func beatitudeListarBastoes(agora time.Time) []string {
	if panel.store == nil {
		return nil
	}
	rows, err := panel.store.db.Query(`SELECT id, pedido FROM beatitude_bastoes
		WHERE devolvido_em IS NULL AND criado_em >= ? ORDER BY id`, agora.Add(-beatitudeBastaoValidade))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var linhas []string
	for rows.Next() {
		var id int64
		var pedido string
		if rows.Scan(&id, &pedido) == nil {
			linhas = append(linhas, "*"+beatitudeCodigoBastao(id)+"*: "+beatitudeCurto(strings.ReplaceAll(pedido, "\n", " "), 120))
		}
	}
	if len(linhas) == 0 {
		return []string{beatitudeMarca + ": nenhum pedido esperando volta do seu Claude."}
	}
	return append([]string{beatitudeMarca + ": esperando volta do seu Claude (valem 24 h):"}, linhas...)
}

// beatitudeEhMensagemDoBastao: a própria mensagem a copiar, que a ponte
// postou sem a marca. Não é pedido.
func beatitudeEhMensagemDoBastao(texto string) bool {
	return strings.HasPrefix(strings.TrimSpace(texto), beatitudeBastaoPrefixo)
}
