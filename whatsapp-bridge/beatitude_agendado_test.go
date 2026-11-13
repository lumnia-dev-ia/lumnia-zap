package main

import (
	"strings"
	"testing"
	"time"
)

// storeAgendado: banco em memoria com mensagens, rascunhos (ja migrados),
// agendador do painel e beatitude_runs.
func storeAgendado(t *testing.T) func() {
	t.Helper()
	antigo, antigoHook := panel.store, panelAoRodarAgendamento
	panel.store = storeDeTeste(t)
	if err := ensurePanelTables(panel.store); err != nil {
		t.Fatal(err)
	}
	if _, err := panel.store.db.Exec(`CREATE TABLE beatitude_runs (id INTEGER PRIMARY KEY AUTOINCREMENT,
		iniciado_em TIMESTAMP, terminado_em TIMESTAMP, pedido TEXT, resposta TEXT, status TEXT, erro TEXT)`); err != nil {
		t.Fatal(err)
	}
	initBeatitudeExtras()
	initBeatitudeExtras() // segunda vez: migracao idempotente
	_, _ = panel.store.db.Exec(`INSERT INTO chats VALUES ('5511999990000@s.whatsapp.net', 'Marina Helito', ?)`, time.Now())
	_, _ = panel.store.db.Exec(`INSERT INTO chats VALUES ('5511888880000@s.whatsapp.net', 'Cris Meirelles', ?)`, time.Now())
	return func() { panel.store, panelAoRodarAgendamento = antigo, antigoHook }
}

func TestPedidoTemHorario(t *testing.T) {
	com := []string{
		"As 8:30 responde a mensagem para a Marina que sábado ela irá aproveitar a Giulia",
		"Prepare uma mensagem para enviar para Cris hoje, 8 horas, em resposta",
		"Era para enviar 8;30",
		"Hoje, 23:59 enviar uma mensagem",
		"manda às 7h pro Pedro",
		"amanhã 7h30 manda bom dia",
		"manda ao meio-dia",
	}
	sem := []string{
		"responde a Marina que sábado ela vai aproveitar a Giulia",
		"pendentes",
		"manda os 2 rascunhos",
		"o voo LA3456 sai cedo",
		"5 anos de namoro",
	}
	for _, p := range com {
		if !beatitudePedidoTemHorario(p) {
			t.Errorf("deveria achar horário em %q", p)
		}
	}
	for _, p := range sem {
		if beatitudePedidoTemHorario(p) {
			t.Errorf("não deveria achar horário em %q", p)
		}
	}
}

func TestClassificarHorario(t *testing.T) {
	agora := time.Date(2026, 11, 10, 4, 35, 0, 0, panelLoc)
	if q, so, _ := beatitudeClassificarHorario("2026-11-10 08:30", true, agora); q.IsZero() || so {
		t.Fatalf("08:30 futuro deveria agendar: %v %v", q, so)
	}
	if q, so, m := beatitudeClassificarHorario("2026-11-10 04:00", true, agora); !q.IsZero() || !so || !strings.Contains(m, "já passou") {
		t.Fatalf("horário passado deveria travar: %v %v %q", q, so, m)
	}
	if q, so, _ := beatitudeClassificarHorario("", true, agora); !q.IsZero() || !so {
		t.Fatal("pedido com horário e rascunho sem horário deveria travar")
	}
	if q, so, _ := beatitudeClassificarHorario("", false, agora); !q.IsZero() || so {
		t.Fatal("pedido sem horário: envio comum")
	}
	if _, so, _ := beatitudeClassificarHorario("2026-11-30 08:30", true, agora); !so {
		t.Fatal("mais de 7 dias deveria travar")
	}
	if q, _, _ := beatitudeClassificarHorario("2026-11-10T08:30", true, agora); q.Hour() != 8 || q.Minute() != 30 {
		t.Fatalf("formato com T: %v", q)
	}
}

func TestRascunhoComHorarioAprovaEAgenda(t *testing.T) {
	defer storeAgendado(t)()
	agora := time.Now().In(panelLoc)
	quando := agora.Add(3 * time.Hour).Truncate(time.Minute)
	resp := `Vai para a Marina. <rascunhos>[{"n":1,"chat_jid":"5511999990000@s.whatsapp.net","texto":"Oi Marina, sábado você aproveita a Giulia!","enviar_em":"` +
		quando.Format("2006-01-02 15:04") + `"}]</rascunhos>`
	visivel, ponte := beatitudeSepararRascunhos(resp, "1203@g.us", "às "+quando.Format("15:04")+" responde a Marina")
	if visivel != "Vai para a Marina." {
		t.Fatalf("visivel: %q", visivel)
	}
	if !strings.Contains(ponte, "sai *") || !strings.Contains(ponte, quando.Format("15:04")) || !strings.Contains(ponte, "não antes") {
		t.Fatalf("a ponte deveria mostrar o horário: %q", ponte)
	}
	if !beatitudeEhComando("sim") {
		t.Fatal("sim deveria ser comando com um rascunho pendente")
	}
	linhas := strings.Join(beatitudeComando("sim", time.Now()), "\n")
	if !strings.Contains(linhas, "aprovado, sai") {
		t.Fatalf("sim deveria aprovar e agendar: %q", linhas)
	}
	var onDate, atTime, msg string
	var enabled int
	if err := panel.store.db.QueryRow(`SELECT on_date, at_time, message, enabled FROM panel_schedules`).Scan(&onDate, &atTime, &msg, &enabled); err != nil {
		t.Fatal(err)
	}
	if onDate != quando.Format("2006-01-02") || atTime != quando.Format("15:04") || enabled != 1 || !strings.Contains(msg, "Giulia") {
		t.Fatalf("agendamento errado: %s %s %d %q", onDate, atTime, enabled, msg)
	}
	var enviado *time.Time
	_ = panel.store.db.QueryRow(`SELECT enviado_em FROM beatitude_rascunhos`).Scan(&enviado)
	if enviado != nil {
		t.Fatal("não pode ter enviado na hora")
	}
	// depois de aprovado, "sim" de novo nao reenvia nem duplica
	if beatitudePendentes(beatitudeJanelaResposta) != 0 {
		t.Fatal("agendado não é mais pendente")
	}
	// "cancela" sem numero acha o unico agendado e desliga o agendamento
	if !beatitudeEhComando("cancela") {
		t.Fatal("cancela deveria ser comando com um agendado aberto")
	}
	linhas = strings.Join(beatitudeComando("cancela", time.Now()), "\n")
	if !strings.Contains(linhas, "cancelado") {
		t.Fatalf("cancela: %q", linhas)
	}
	_ = panel.store.db.QueryRow(`SELECT enabled FROM panel_schedules`).Scan(&enabled)
	if enabled != 0 {
		t.Fatal("o agendamento deveria ter sido desligado")
	}
}

func TestPedidoComHorarioSemEnviarEmNaoEnviaComSim(t *testing.T) {
	defer storeAgendado(t)()
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511999990000@s.whatsapp.net","texto":"Oi Marina"}]</rascunhos>`
	_, ponte := beatitudeSepararRascunhos(resp, "1203@g.us", "As 8:30 responde a mensagem para a Marina")
	if !strings.Contains(ponte, "não envia") || !strings.Contains(ponte, "*agora*") {
		t.Fatalf("deveria avisar que o sim não envia: %q", ponte)
	}
	linhas := strings.Join(beatitudeComando("sim", time.Now()), "\n")
	if !strings.Contains(linhas, "não enviei") {
		t.Fatalf("sim não pode enviar: %q", linhas)
	}
	linhas = strings.Join(beatitudeComando("1", time.Now()), "\n")
	if !strings.Contains(linhas, "não enviei") {
		t.Fatalf("1 não pode enviar: %q", linhas)
	}
	var enviado *time.Time
	_ = panel.store.db.QueryRow(`SELECT enviado_em FROM beatitude_rascunhos`).Scan(&enviado)
	if enviado != nil {
		t.Fatal("nada pode ter saído")
	}
	if !beatitudeEhComando("agora") || !beatitudeEhComando("1 agora") || !beatitudeEhComando("envia agora") {
		t.Fatal("agora deveria ser comando")
	}
}

func TestRascunhoSemHorarioSegueComoAntes(t *testing.T) {
	defer storeAgendado(t)()
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511999990000@s.whatsapp.net","texto":"Oi Marina"},{"n":2,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris"}]</rascunhos>`
	_, ponte := beatitudeSepararRascunhos(resp, "1203@g.us", "pendentes")
	if strings.Contains(ponte, "sai *") || strings.Contains(ponte, "sem horário") {
		t.Fatalf("sem horário não muda nada: %q", ponte)
	}
	if beatitudePendentes(beatitudeJanelaResposta) != 2 {
		t.Fatal("dois pendentes")
	}
	if beatitudeEhComando("sim") {
		t.Fatal("com dois pendentes, sim sozinho não é comando (como antes)")
	}
}

func TestAvisoDoAgendadorMarcaEnviado(t *testing.T) {
	defer storeAgendado(t)()
	quando := time.Now().In(panelLoc).Add(2 * time.Hour)
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris","enviar_em":"` + quando.Format("2006-01-02 15:04") + `"}]</rascunhos>`
	beatitudeSepararRascunhos(resp, "1203@g.us", "manda às 8h")
	beatitudeComando("1", time.Now())
	var id int64
	if err := panel.store.db.QueryRow(`SELECT id FROM panel_schedules`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if panelAoRodarAgendamento == nil {
		t.Fatal("o módulo deveria registrar o aviso no agendador")
	}
	panelAoRodarAgendamento(id, "enviada", quando)
	var enviado *time.Time
	_ = panel.store.db.QueryRow(`SELECT enviado_em FROM beatitude_rascunhos`).Scan(&enviado)
	if enviado == nil {
		t.Fatal("deveria marcar enviado")
	}
	if beatitudeAgendadosAbertos() != 0 {
		t.Fatal("nada mais em aberto")
	}
}

func TestTetoContaSoPedidosDoGrupo(t *testing.T) {
	defer storeAgendado(t)()
	base := time.Now().Add(-50 * time.Minute)
	ins := func(i int, pedido string) {
		_, _ = panel.store.db.Exec(`INSERT INTO beatitude_runs (iniciado_em, pedido, status) VALUES (?, ?, 'ok')`,
			base.Add(time.Duration(i)*time.Minute), pedido)
	}
	for i := 0; i < 40; i++ {
		ins(i, "preparar: Diego")
		ins(i, "navegador: voos")
	}
	if n, _ := beatitudeTeto(); n != 0 {
		t.Fatalf("preparo e navegador não contam: %d", n)
	}
	for i := 0; i < beatitudeMaxPorHora; i++ {
		ins(i, "pedido dela")
	}
	_, _ = panel.store.db.Exec(`INSERT INTO beatitude_runs (iniciado_em, pedido, status) VALUES (?, 'pedido dela', 'erro')`, base)
	n, libera := beatitudeTeto()
	if n != beatitudeMaxPorHora {
		t.Fatalf("conta %d", n)
	}
	esperado := base.Add(time.Hour)
	if d := libera.Sub(esperado); d < -time.Second || d > time.Second {
		t.Fatalf("libera em %v, esperado %v", libera, esperado)
	}
	aviso := beatitudeAvisoTeto(libera, true)
	if !strings.Contains(aviso, "Libero às *") || !strings.Contains(aviso, "Não ouvi o áudio") {
		t.Fatalf("aviso: %q", aviso)
	}
}

func TestLiberaEm(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 4, 0, 0, 0, panelLoc)
	var hs []time.Time
	for i := 0; i < 32; i++ {
		hs = append(hs, t0.Add(time.Duration(i)*time.Minute))
	}
	if got := beatitudeLiberaEm(hs, 30); !got.Equal(t0.Add(2*time.Minute + time.Hour)) {
		t.Fatalf("32 pedidos, teto 30: libera quando sai o 3º mais antigo; veio %v", got)
	}
	if !beatitudeLiberaEm(hs[:10], 30).IsZero() {
		t.Fatal("abaixo do teto: zero")
	}
}

func TestEnviosDoGrupoNaoContamNoTetoDoPainel(t *testing.T) {
	defer storeAgendado(t)()
	for i := 0; i < 50; i++ {
		recordAPISend("1203@g.us", "beatitude")
	}
	recordAPISend("5511999990000@s.whatsapp.net", "beatitude-aprovado")
	if n := apiSendsSince(time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("só o envio a terceiro conta: %d", n)
	}
}
