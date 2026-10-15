// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// agenda_apple_api.go — rotas locais da Agenda da Apple e a pagina /apple.
//
//   GET  /apple                      pagina de configuracao (sem link no painel comum)
//   GET  /api/agenda/estado          estado, calendarios, principal (nunca a senha)
//   GET  /api/agenda/listar          ?de&ate&calendario
//   GET  /api/agenda/buscar          ?texto&de&ate
//   GET  /api/agenda/livre           ?de&ate&duracao_min&janela&dias_semana
//   POST /api/agenda/criar           JSON agendaPedidoCriar
//   POST /api/agenda/testar          (painel) {apple_id, senha_app} -> calendarios, sem gravar
//   POST /api/agenda/configurar      (painel) {apple_id, senha_app?, ler:{url:bool}, principal}
//   POST /api/agenda/desconectar     (painel)
//
// Tudo so em localhost, como o resto do painel. As rotas que mudam a
// configuracao exigem o cabecalho do painel (beatitudeExigePainel).

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed apple.html
var appleHTML []byte

func registerAgendaRoutes() {
	h := func(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isLocalRequest(r) {
				http.Error(w, "apenas localhost", http.StatusForbidden)
				return
			}
			fn(w, r)
		}
	}
	http.HandleFunc("/apple", h(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(appleHTML)
	}))
	http.HandleFunc("/api/agenda/estado", h(agendaEstadoHandler))
	http.HandleFunc("/api/agenda/listar", h(agendaListarHandler))
	http.HandleFunc("/api/agenda/buscar", h(agendaBuscarHandler))
	http.HandleFunc("/api/agenda/livre", h(agendaLivreHandler))
	http.HandleFunc("/api/agenda/criar", h(agendaCriarHandler))
	http.HandleFunc("/api/agenda/testar", h(agendaTestarHandler))
	http.HandleFunc("/api/agenda/configurar", h(agendaConfigurarHandler))
	http.HandleFunc("/api/agenda/desconectar", h(agendaDesconectarHandler))
}

func agendaErroJSON(w http.ResponseWriter, err error, status int) {
	w.WriteHeader(status)
	writeJSON(w, map[string]interface{}{"ok": false, "erro": agendaErroSeguro(err)})
}

func agendaContexto(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 90*time.Second)
}

func agendaEstadoHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, agendaConfigPublica())
}

// agendaIntervalo le ?de&ate com os padroes dados (em dias a partir de hoje).
func agendaIntervalo(r *http.Request, padraoDe, padraoAte int) (time.Time, time.Time, error) {
	loc := time.Local
	hoje := time.Now().In(loc)
	hoje = time.Date(hoje.Year(), hoje.Month(), hoje.Day(), 0, 0, 0, 0, loc)
	de := hoje.AddDate(0, 0, padraoDe)
	ate := hoje.AddDate(0, 0, padraoAte).Add(24*time.Hour - time.Second)
	var err error
	if s := r.URL.Query().Get("de"); s != "" {
		if de, err = agendaLerData(s, false, loc); err != nil {
			return de, ate, err
		}
	}
	if s := r.URL.Query().Get("ate"); s != "" {
		if ate, err = agendaLerData(s, true, loc); err != nil {
			return de, ate, err
		}
	}
	return de, ate, nil
}

func agendaStatusDoErro(err error) int {
	var ae *agendaErro
	if errors.As(err, &ae) {
		switch ae.Estado {
		case agendaEstadoSenha:
			return http.StatusUnauthorized
		case agendaEstadoSinal:
			return http.StatusBadGateway
		}
	}
	return http.StatusBadRequest
}

func agendaListarHandler(w http.ResponseWriter, r *http.Request) {
	de, ate, err := agendaIntervalo(r, 0, 7)
	if err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := agendaContexto(r)
	defer cancel()
	evs, err := agendaListar(ctx, de, ate, r.URL.Query().Get("calendario"))
	if err != nil {
		agendaErroJSON(w, err, agendaStatusDoErro(err))
		return
	}
	if evs == nil {
		evs = []agendaEvento{}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "de": de.Format(time.RFC3339), "ate": ate.Format(time.RFC3339),
		"fuso": time.Local.String(), "eventos": evs,
	})
}

func agendaBuscarHandler(w http.ResponseWriter, r *http.Request) {
	de, ate, err := agendaIntervalo(r, -365, 365)
	if err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	// busca de 2 anos excede a janela maxima de uma leitura: parte em duas
	ctx, cancel := agendaContexto(r)
	defer cancel()
	texto := r.URL.Query().Get("texto")
	var evs []agendaEvento
	for _, faixa := range agendaPartirJanela(de, ate) {
		parte, err := agendaBuscar(ctx, texto, faixa[0], faixa[1])
		if err != nil {
			agendaErroJSON(w, err, agendaStatusDoErro(err))
			return
		}
		evs = append(evs, parte...)
	}
	if len(evs) > agendaMaxBusca {
		evs = evs[:agendaMaxBusca]
	}
	if evs == nil {
		evs = []agendaEvento{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "eventos": evs})
}

// agendaPartirJanela divide um intervalo em faixas de no maximo agendaJanelaMax.
func agendaPartirJanela(de, ate time.Time) [][2]time.Time {
	var out [][2]time.Time
	for ini := de; ini.Before(ate); {
		fim := ini.Add(agendaJanelaMax)
		if fim.After(ate) {
			fim = ate
		}
		out = append(out, [2]time.Time{ini, fim})
		ini = fim
	}
	return out
}

func agendaLivreHandler(w http.ResponseWriter, r *http.Request) {
	de, ate, err := agendaIntervalo(r, 0, 7)
	if err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	dur := 60
	if s := q.Get("duracao_min"); s != "" {
		if dur, err = strconv.Atoi(s); err != nil || dur < 5 || dur > 12*60 {
			agendaErroJSON(w, errors.New("duracao_min entre 5 e 720"), http.StatusBadRequest)
			return
		}
	}
	jIni, jFim := "08:00", "20:00"
	if s := q.Get("janela"); s != "" {
		partes := strings.SplitN(s, "-", 2)
		if len(partes) == 2 {
			jIni, jFim = strings.TrimSpace(partes[0]), strings.TrimSpace(partes[1])
		}
	}
	dias := map[int]bool{}
	if s := q.Get("dias_semana"); s != "" {
		for _, d := range strings.Split(strings.ToLower(ponteSemAcento(s)), ",") {
			d = strings.TrimSpace(d)
			for i, nome := range []string{"dom", "seg", "ter", "qua", "qui", "sex", "sab"} {
				if d == nome || d == strconv.Itoa(i) {
					dias[i] = true
				}
			}
			if d == "uteis" {
				for i := 1; i <= 5; i++ {
					dias[i] = true
				}
			}
		}
	}
	ctx, cancel := agendaContexto(r)
	defer cancel()
	evs, err := agendaListar(ctx, de, ate, "")
	if err != nil {
		agendaErroJSON(w, err, agendaStatusDoErro(err))
		return
	}
	livres := agendaLivre(evs, de, ate, time.Duration(dur)*time.Minute, jIni, jFim, dias, time.Local)
	if livres == nil {
		livres = []agendaJanelaLivre{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "janela": jIni + "-" + jFim, "duracao_min": dur, "livres": livres})
}

func agendaCriarHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var p agendaPedidoCriar
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := agendaContexto(r)
	defer cancel()
	ev, err := agendaCriar(ctx, p)
	if err != nil {
		agendaErroJSON(w, err, agendaStatusDoErro(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "evento": ev})
}

// --- configuracao pela pagina (exige o cabecalho do painel)

func agendaTestarHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	var p struct {
		AppleID  string `json:"apple_id"`
		SenhaApp string `json:"senha_app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := agendaContexto(r)
	defer cancel()
	home, cals, err := agendaTestar(ctx, p.AppleID, p.SenhaApp)
	if err != nil {
		agendaErroJSON(w, err, agendaStatusDoErro(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "home_url": home, "calendarios": cals})
}

func agendaConfigurarHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	var p struct {
		AppleID   string          `json:"apple_id"`
		SenhaApp  string          `json:"senha_app"`
		Ler       map[string]bool `json:"ler"`
		Principal string          `json:"principal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		agendaErroJSON(w, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := agendaContexto(r)
	defer cancel()
	if err := agendaConfigurar(ctx, p.AppleID, p.SenhaApp, p.Ler, p.Principal); err != nil {
		agendaErroJSON(w, err, agendaStatusDoErro(err))
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "config": agendaConfigPublica()})
}

func agendaDesconectarHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	agendaDesconectar()
	writeJSON(w, map[string]interface{}{"ok": true})
}
