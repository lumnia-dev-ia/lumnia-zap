// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// agenda_apple_ponte.go — leva a agenda do iPhone ao servidor do Beatitude.
//
// O brief da manha roda na nuvem da Anthropic e nao enxerga este computador.
// Entao a ponte, que ja fala com o servidor pelo contrato /api/ponte/*,
// deposita de tempos em tempos os compromissos de hoje e dos proximos sete
// dias em /api/ponte/agenda. O servidor guarda por membro; o roteiro
// brief_diario le de la. Nada de notas: titulo, horario, calendario, local e
// nomes de convidados, o que "O dia" precisa.
//
// Cadencia: a cada ponteAgendaCada quando a agenda esta conectada, e logo
// depois de um compromisso criado pela ponte. Se o servidor ainda nao tiver a
// rota (404), espera ponteAgendaRecuo antes de tentar de novo, sem log a
// cada minuto.

import (
	"context"
	"sync"
	"time"
)

const (
	ponteAgendaRota  = "/api/ponte/agenda"
	ponteAgendaCada  = 30 * time.Minute
	ponteAgendaRecuo = 6 * time.Hour
	ponteAgendaDias  = 7
)

var ponteAgenda struct {
	mu        sync.Mutex
	ultimoEm  time.Time
	proximoEm time.Time // vazio = quando der; preenchido pelo recuo
	forcar    bool
	ultimoErr string
}

// ponteAgendaDepois pede um deposito no proximo ciclo (depois de agendaCriar).
func ponteAgendaDepois() {
	ponteAgenda.mu.Lock()
	ponteAgenda.forcar = true
	ponteAgenda.proximoEm = time.Time{}
	ponteAgenda.mu.Unlock()
	ponteAcordar()
}

type ponteEventoAgenda struct {
	Titulo     string   `json:"titulo"`
	Inicio     string   `json:"inicio"`
	Fim        string   `json:"fim"`
	Quando     string   `json:"quando"`
	DiaInteiro bool     `json:"dia_inteiro"`
	Calendario string   `json:"calendario"`
	Local      string   `json:"local,omitempty"`
	Convidados []string `json:"convidados,omitempty"`
	Beatitude  bool     `json:"criado_pelo_beatitude"`
}

// ponteAgendaCorpo monta o que vai ao servidor: sem notas, sem UID.
func ponteAgendaCorpo(evs []agendaEvento, de, ate time.Time) map[string]interface{} {
	lista := make([]ponteEventoAgenda, 0, len(evs))
	for _, ev := range evs {
		lista = append(lista, ponteEventoAgenda{
			Titulo: ev.Titulo, Inicio: ev.Inicio, Fim: ev.Fim, Quando: ev.Quando, DiaInteiro: ev.DiaInteiro,
			Calendario: ev.Calendario, Local: ev.Local, Convidados: ev.Convidados, Beatitude: ev.Beatitude,
		})
	}
	return map[string]interface{}{
		"fonte":   "apple",
		"fuso":    time.Local.String(),
		"de":      de.Format(time.RFC3339),
		"ate":     ate.Format(time.RFC3339),
		"lido_em": time.Now().Format(time.RFC3339),
		"eventos": lista,
	}
}

// ponteEnviarAgenda e chamada em toda rodada da ponte; decide sozinha se e hora.
func ponteEnviarAgenda() {
	if !agendaConectada() {
		return
	}
	ponteAgenda.mu.Lock()
	agora := time.Now()
	hora := ponteAgenda.forcar ||
		(agora.After(ponteAgenda.proximoEm) && agora.Sub(ponteAgenda.ultimoEm) >= ponteAgendaCada)
	ponteAgenda.forcar = false
	ponteAgenda.mu.Unlock()
	if !hora {
		return
	}

	loc := time.Local
	hoje := time.Date(agora.Year(), agora.Month(), agora.Day(), 0, 0, 0, 0, loc)
	de := hoje
	ate := hoje.AddDate(0, 0, ponteAgendaDias).Add(24*time.Hour - time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	evs, err := agendaListar(ctx, de, ate, "")
	if err != nil {
		// estado e log ja registrados por agendaListar; nao insiste
		ponteAgenda.mu.Lock()
		ponteAgenda.ultimoEm = agora
		ponteAgenda.ultimoErr = agendaErroSeguro(err)
		ponteAgenda.mu.Unlock()
		return
	}
	status, err := ponteChamar(ponteAgendaRota, ponteAgendaCorpo(evs, de, ate), nil)
	ponteAgenda.mu.Lock()
	defer ponteAgenda.mu.Unlock()
	ponteAgenda.ultimoEm = agora
	if err != nil {
		ponteAgenda.ultimoErr = beatitudeCurto(err.Error(), 200)
		if status == 404 || status == 405 {
			// servidor sem a rota ainda: volta daqui a algumas horas
			ponteAgenda.proximoEm = agora.Add(ponteAgendaRecuo)
			if panel.logger != nil {
				panel.logger.Infof("Agenda da Apple: o servidor ainda não aceita %s; tento de novo em %s", ponteAgendaRota, ponteAgendaRecuo)
			}
			return
		}
		ponteErro(err)
		return
	}
	ponteAgenda.ultimoErr = ""
	if panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: %d compromisso(s) depositado(s) no app (hoje a +%d dias)", len(evs), ponteAgendaDias)
	}
}
