// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_falas.go — espelha no app (Conversas) o que a pessoa fala no
// grupo Beatitude e o que o mordomo respondeu, pela rota da ponte
// POST /api/ponte/falas-do-grupo (contrato em docs/ponte-falas-do-grupo.md
// do app). Tres portas, um so cerebro: o que foi pedido pelo WhatsApp aparece
// no app com a marca "via WhatsApp" e pode continuar por la.
//
// Travas:
//   - so falas da PROPRIA pessoa (IsFromMe) no grupo Beatitude; nunca uma
//     mensagem de terceiro, nunca outro chat;
//   - o que o modulo mesmo escreveu no grupo (prefixo beatitudeMarca / "◆")
//     nunca e enviado como fala;
//   - "sim N", comandos e aprovacoes do navegador nao sao falas: o app ja
//     registra o que eles fazem;
//   - processar e sempre false: quem trata o pedido e o mordomo daqui; o app
//     so espelha e nunca executa de novo.

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	falasRota          = "/api/ponte/falas-do-grupo"
	falasBackfillDias  = 7
	falasLote          = 200
	falasTetoTexto     = 2000
	falasTetoResposta  = 8000
	falasTetoPendentes = 500
)

type falaAcaoLocal struct {
	Tipo   string `json:"tipo"`
	Resumo string `json:"resumo"`
}

type falaGrupo struct {
	WaID            string          `json:"wa_id"`
	Em              string          `json:"em"`
	Texto           string          `json:"texto"`
	Tipo            string          `json:"tipo"`
	RespostaMordomo string          `json:"resposta_mordomo,omitempty"`
	AcoesLocais     []falaAcaoLocal `json:"acoes_locais,omitempty"`
	Processar       bool            `json:"processar"`
}

var falas struct {
	mu        sync.Mutex
	pendentes []falaGrupo // o que ainda nao chegou ao app (rede, app fora); sai no proximo laco da ponte
	enviando  bool
	backfill  bool // backfill ja tentado neste processo
}

func falaNova(waID string, em time.Time, texto, tipo string) falaGrupo {
	return falaGrupo{WaID: waID, Em: em.UTC().Format(time.RFC3339), Texto: beatitudeCurto(strings.TrimSpace(texto), falasTetoTexto), Tipo: tipo}
}

// falasRegistrar enfileira falas recem-chegadas (sem resposta ainda) e tenta
// mandar na hora. Sai cedo sem ponte pareada.
func falasRegistrar(lista []falaGrupo) {
	if len(lista) == 0 || !pontePareado() {
		return
	}
	falasEnfileirar(lista)
	go falasFlush()
}

// falasResponder complementa a ultima fala do lote com o que o mordomo
// respondeu; o app junta pelo wa_id, sem duplicar.
func falasResponder(f falaGrupo, resposta string, acoes []falaAcaoLocal) {
	if f.WaID == "" || !pontePareado() {
		return
	}
	f.RespostaMordomo = beatitudeCurto(strings.TrimSpace(resposta), falasTetoResposta)
	f.AcoesLocais = acoes
	falasEnfileirar([]falaGrupo{f})
	go falasFlush()
}

func falasEnfileirar(lista []falaGrupo) {
	falas.mu.Lock()
	defer falas.mu.Unlock()
	for _, f := range lista {
		if f.WaID == "" || f.Texto == "" {
			continue
		}
		f.Processar = false
		trocou := false
		for i := range falas.pendentes { // a versao com resposta substitui a sem resposta
			if falas.pendentes[i].WaID == f.WaID && f.RespostaMordomo != "" {
				falas.pendentes[i] = f
				trocou = true
				break
			}
		}
		if !trocou {
			falas.pendentes = append(falas.pendentes, f)
		}
	}
	if n := len(falas.pendentes); n > falasTetoPendentes {
		falas.pendentes = falas.pendentes[n-falasTetoPendentes:]
	}
}

// falasFlush manda o que esta pendente, em lotes, e so limpa o que o app
// confirmou. Chamado apos cada fala e a cada laco da ponte.
func falasFlush() {
	falas.mu.Lock()
	if falas.enviando || len(falas.pendentes) == 0 {
		falas.mu.Unlock()
		return
	}
	falas.enviando = true
	lote := append([]falaGrupo(nil), falas.pendentes...)
	falas.mu.Unlock()
	defer func() {
		falas.mu.Lock()
		falas.enviando = false
		falas.mu.Unlock()
	}()
	for i := 0; i < len(lote); i += falasLote {
		fim := i + falasLote
		if fim > len(lote) {
			fim = len(lote)
		}
		if err := falasEnviar(lote[i:fim], false); err != nil {
			if panel.logger != nil {
				panel.logger.Warnf("Beatitude: falas do grupo nao chegaram ao app (%d pendentes): %v", len(lote)-i, err)
			}
			return // fica pendente; o laco da ponte tenta de novo
		}
		falasConfirmar(lote[i:fim])
	}
}

func falasConfirmar(enviadas []falaGrupo) {
	falas.mu.Lock()
	defer falas.mu.Unlock()
	resto := falas.pendentes[:0]
	for _, p := range falas.pendentes {
		mesma := false
		for _, e := range enviadas {
			if e.WaID == p.WaID && e.RespostaMordomo == p.RespostaMordomo {
				mesma = true
				break
			}
		}
		if !mesma {
			resto = append(resto, p)
		}
	}
	falas.pendentes = resto
}

func falasEnviar(lista []falaGrupo, backfill bool) error {
	var r struct {
		OK        bool `json:"ok"`
		Recebidas int  `json:"recebidas"`
		Novas     int  `json:"novas"`
	}
	_, err := ponteChamar(falasRota, map[string]interface{}{"falas": lista, "backfill": backfill}, &r)
	if err != nil {
		return err
	}
	if panel.logger != nil && r.Novas > 0 {
		panel.logger.Infof("Beatitude: %d fala(s) do grupo no app (%d novas)", r.Recebidas, r.Novas)
	}
	return nil
}

// falaEhConversa diz se uma mensagem da pessoa no grupo e um pedido (vale
// espelhar) ou um comando que o app ja conhece por outro caminho.
func falaEhConversa(texto string) bool {
	t := strings.TrimSpace(texto)
	if t == "" || strings.HasPrefix(t, beatitudeMarca) || strings.HasPrefix(t, "◆") {
		return false
	}
	if falaEhRuido(t) {
		return false
	}
	if beatitudeEhAprovacaoNavegador(t) || beatitudeEhComando(t) {
		return false
	}
	return true
}

var falaSoNumeroRe = regexp.MustCompile(`^\s*\d{1,2}\s*[.!]?\s*$`)

// falaEhRuido: "1", "sim 3", "ok", "pode" — respostas curtas a algo que o
// grupo ofereceu. Nao sao conversa; o app ja registra o efeito delas.
func falaEhRuido(texto string) bool {
	t := strings.TrimSpace(texto)
	if t == "" {
		return true
	}
	if _, ok := ponteEhSimN(t); ok {
		return true
	}
	return falaSoNumeroRe.MatchString(t) || beatitudeSimRe.MatchString(t)
}

// falasBackfill manda ao app, uma vez por instalacao, as falas dos ultimos
// dias no grupo (com a resposta do mordomo quando ha registro em
// beatitude_runs). Roda no laco da ponte; grava a data em beatitude.json.
func falasBackfill() {
	falas.mu.Lock()
	if falas.backfill {
		falas.mu.Unlock()
		return
	}
	falas.backfill = true
	falas.mu.Unlock()

	beatitude.mu.Lock()
	cfg := beatitude.cfg
	beatitude.mu.Unlock()
	if !cfg.Ligado || cfg.GrupoJID == "" || !cfg.FalasBackfillEm.IsZero() || !pontePareado() || panel.store == nil {
		return
	}
	desde := time.Now().Add(-falasBackfillDias * 24 * time.Hour)
	rows, err := panel.store.db.Query(
		`SELECT id, content, timestamp FROM messages
		  WHERE chat_jid = ? AND is_from_me = 1 AND timestamp >= ? AND content != ''
		  ORDER BY timestamp ASC`, cfg.GrupoJID, desde)
	if err != nil {
		if panel.logger != nil {
			panel.logger.Warnf("Beatitude: backfill das falas nao leu as mensagens: %v", err)
		}
		return
	}
	var lista []falaGrupo
	for rows.Next() {
		var id, content string
		var em time.Time
		if err := rows.Scan(&id, &content, &em); err != nil {
			continue
		}
		if !falaEhConversa(content) {
			continue
		}
		f := falaNova(id, em, content, "texto")
		f.RespostaMordomo = falasRespostaRegistrada(content, em)
		lista = append(lista, f)
	}
	rows.Close()
	if len(lista) == 0 {
		falasMarcarBackfill()
		return
	}
	for i := 0; i < len(lista); i += falasLote {
		fim := i + falasLote
		if fim > len(lista) {
			fim = len(lista)
		}
		if err := falasEnviar(lista[i:fim], true); err != nil {
			if panel.logger != nil {
				panel.logger.Warnf("Beatitude: backfill das falas falhou (tenta no proximo inicio): %v", err)
			}
			falas.mu.Lock()
			falas.backfill = false
			falas.mu.Unlock()
			return
		}
	}
	if panel.logger != nil {
		panel.logger.Infof("Beatitude: backfill de %d fala(s) do grupo enviado ao app", len(lista))
	}
	falasMarcarBackfill()
}

func falasMarcarBackfill() {
	beatitude.mu.Lock()
	beatitude.cfg.FalasBackfillEm = time.Now()
	_ = beatitudeSalvar()
	beatitude.mu.Unlock()
}

// falasRespostaRegistrada acha em beatitude_runs a resposta do mordomo a um
// pedido do grupo: o run cujo pedido contem a fala e comecou depois dela.
func falasRespostaRegistrada(texto string, em time.Time) string {
	if panel.store == nil {
		return ""
	}
	var resposta string
	err := panel.store.db.QueryRow(
		`SELECT resposta FROM beatitude_runs
		  WHERE status = 'ok' AND resposta != '' AND iniciado_em >= ? AND iniciado_em <= ? AND instr(pedido, ?) > 0
		  ORDER BY iniciado_em ASC LIMIT 1`, em.Add(-time.Minute), em.Add(30*time.Minute), strings.TrimSpace(texto)).Scan(&resposta)
	if err != nil {
		return ""
	}
	return beatitudeCurto(falasSoVisivel(resposta), falasTetoResposta)
}

// falasSoVisivel tira os blocos que a ponte interpreta (rascunhos, navegador,
// pix) sem nenhum efeito colateral: so o texto que a pessoa viu no grupo.
func falasSoVisivel(resposta string) string {
	t := beatitudeBlocoRe.ReplaceAllString(resposta, "")
	t = beatitudeNavBlocoRe.ReplaceAllString(t, "")
	t = beatitudePixBlocoRe.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}
