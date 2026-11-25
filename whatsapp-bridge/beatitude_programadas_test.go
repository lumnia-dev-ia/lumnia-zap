package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Os casos de 24-11 (grupo da Lisiane): pedido de mensagem com horário que
// voltava só com a descrição, rascunho descartado em silêncio, todo rascunho
// com o número 1 e "SIM, enviar amanhã, 8h" que não era entendido.

func rascunhoPara(n int, jid, texto string, quando time.Time) string {
	h := ""
	if !quando.IsZero() {
		h = `,"enviar_em":"` + quando.In(panelLoc).Format("2006-01-02 15:04") + `"`
	}
	return fmt.Sprintf(`<rascunhos>[{"n":%d,"chat_jid":"%s","texto":"%s"%s}]</rascunhos>`, n, jid, texto, h)
}

func TestNumeracaoContinuaECancelaPorNumero(t *testing.T) {
	defer storeAgendado(t)()
	amanha := time.Now().In(panelLoc).Add(20 * time.Hour)
	// três pedidos seguidos, e o mordomo numera todos como 1
	_, c1 := beatitudeSepararRascunhos(rascunhoPara(1, "5511999990000@s.whatsapp.net", "Oi Marina", amanha), "1203@g.us", "amanhã 8h manda")
	time.Sleep(2 * time.Millisecond)
	_, c2 := beatitudeSepararRascunhos(rascunhoPara(1, "5511888880000@s.whatsapp.net", "Oi Cris", amanha), "1203@g.us", "amanhã 8h manda")
	if !strings.HasPrefix(c1, "*1 · Marina Helito*") || !strings.HasPrefix(c2, "*2 · Cris Meirelles*") || !strings.Contains(c2, "*cancela 2*") {
		t.Fatalf("a segunda programada deveria ser a 2:\n%s\n---\n%s", c1, c2)
	}
	if b := beatitudeBlocoProgramadas(time.Now()); !strings.Contains(b, "- 1 · Marina Helito") || !strings.Contains(b, "a partir de 3") {
		t.Fatalf("o pedido ao Claude deveria listar as programadas e o próximo número: %q", b)
	}
	// "cancela 1" alcança a primeira, que não é do último lote
	if !beatitudeEhComando("cancela 1") {
		t.Fatal("cancela 1 deveria ser comando com programadas abertas")
	}
	l := strings.Join(beatitudeComando("cancela 1", time.Now()), "\n")
	if !strings.Contains(l, "1 · Marina Helito: cancelado") {
		t.Fatalf("cancela 1: %q", l)
	}
	var ligados int
	_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM panel_schedules WHERE enabled = 1 AND display LIKE '%Cris%'`).Scan(&ligados)
	if ligados != 1 {
		t.Fatal("a da Cris continua programada")
	}
	lista := strings.Join(beatitudeListarProgramadas(time.Now()), "\n")
	if strings.Contains(lista, "Marina") || !strings.Contains(lista, "*2 · Cris Meirelles*") || !strings.Contains(lista, "Oi Cris") {
		t.Fatalf("programadas: %q", lista)
	}
	for _, s := range []string{"programadas", "Agendadas?", "quais as mensagens programadas?", "ver programadas"} {
		if !beatitudeProgramadasRe.MatchString(s) {
			t.Errorf("%q deveria listar as programadas", s)
		}
	}
	if beatitudeProgramadasRe.MatchString("as mensagens programadas para o Pedro saíram?") {
		t.Error("pergunta comprida segue para o Claude")
	}
}

func TestEnviaNTextoTrocaOTextoDaProgramada(t *testing.T) {
	defer storeAgendado(t)()
	amanha := time.Now().In(panelLoc).Add(20 * time.Hour)
	beatitudeSepararRascunhos(rascunhoPara(1, "5511999990000@s.whatsapp.net", "Oi Marina", amanha), "1203@g.us", "amanhã 8h manda")
	l := strings.Join(beatitudeComando("envia 1: Bom dia, Marina! Tudo certo para sábado?", time.Now()), "\n")
	var msg string
	var ligado int
	_ = panel.store.db.QueryRow(`SELECT message, enabled FROM panel_schedules`).Scan(&msg, &ligado)
	if !strings.Contains(l, "texto trocado") || msg != "Bom dia, Marina! Tudo certo para sábado?" || ligado != 1 {
		t.Fatalf("o texto deveria mudar e o horário ficar: %q / %q / %d", l, msg, ligado)
	}
	var n int
	_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM panel_schedules`).Scan(&n)
	if n != 1 {
		t.Fatalf("um agendamento só, sem duplicar: %d", n)
	}
}

func TestRascunhoDescartadoAvisa(t *testing.T) {
	defer storeAgendado(t)()
	antigo := beatitudeNumeroNoWhatsApp
	beatitudeNumeroNoWhatsApp = func(string) (string, error) { return "", fmt.Errorf("numero_sem_whatsapp") }
	defer func() { beatitudeNumeroNoWhatsApp = antigo }()

	amanha := time.Now().In(panelLoc).Add(20 * time.Hour)
	v, c := beatitudeSepararRascunhos("◆ Beatitude\n\n"+rascunhoPara(1, "5511949420441@s.whatsapp.net", "Olá", amanha), "1203@g.us", "amanhã 8h manda pro Kayque")
	if v != "" || !strings.Contains(c, "+55 11 94942-0441") || !strings.Contains(c, "nada foi preparado") {
		t.Fatalf("destino que não existe vira aviso, não silêncio: %q / %q", v, c)
	}
	// resposta que fala de rascunho sem trazer o bloco
	info := beatitudeTratarRascunhos("◆ Beatitude\n\n◆ Beatitude\nRascunho para o Fagotti, com envio amanhã às 8h.", "1203@g.us", "amanhã 8h manda")
	if info.Visivel != "Rascunho para o Fagotti, com envio amanhã às 8h." || !strings.Contains(info.Cartao, "nada foi preparado") {
		t.Fatalf("sem bloco: %q / %q", info.Visivel, info.Cartao)
	}
	// conversa comum e ação do app não ganham aviso
	for _, s := range []string{"Sua agenda de amanhã está livre até as 14h.", "O rascunho do convite ficou como ação *1*: responda *sim 1*."} {
		if i := beatitudeTratarRascunhos(s, "1203@g.us", "x"); i.Cartao != "" {
			t.Errorf("%q não deveria ganhar aviso: %q", s, i.Cartao)
		}
	}
	if i := beatitudeTratarRascunhos("Vai. <rascunhos>[{quebrado</rascunhos>", "1203@g.us", "x"); !strings.Contains(i.Cartao, "defeito") {
		t.Errorf("bloco quebrado deveria avisar: %q", i.Cartao)
	}
}

// O Kayque de 24-11: contato encaminhado, sem conversa anterior.
func TestNumeroSemConversaEsperaOSim(t *testing.T) {
	defer storeAgendado(t)()
	antigoNum, antigoNome := beatitudeNumeroNoWhatsApp, beatitudeContatoNome
	beatitudeNumeroNoWhatsApp = func(tel string) (string, error) { return tel + "@s.whatsapp.net", nil }
	beatitudeContatoNome = func(string) string { return "" }
	defer func() { beatitudeNumeroNoWhatsApp, beatitudeContatoNome = antigoNum, antigoNome }()

	amanha := time.Now().In(panelLoc).Add(20 * time.Hour)
	info := beatitudeTratarRascunhos(rascunhoPara(1, "5511949420441@s.whatsapp.net", "Bom dia, Kayque!", amanha), "1203@g.us", "amanhã 8h manda pro Kayque")
	if info.Programados != 0 || info.Pendentes != 1 || !strings.Contains(info.Cartao, "*1 · +55 11 94942-0441* · número sem conversa anterior") ||
		!strings.Contains(info.Cartao, "Para aprovar") {
		t.Fatalf("número sem conversa mostra o telefone e espera o sim: %+v", info)
	}
	if strings.Contains(info.Resumo, "Bom dia") {
		t.Fatalf("o resumo que vai ao app não leva o texto da mensagem: %q", info.Resumo)
	}
	// o caso real: ela respondeu "SIM, enviar amanha, 8h"
	if !beatitudeEhComando("SIM, enviar amanha, 8h") {
		t.Fatal("sim com horário junto deveria aprovar")
	}
	l := strings.Join(beatitudeComando("SIM, enviar amanha, 8h", time.Now()), "\n")
	var dest string
	_ = panel.store.db.QueryRow(`SELECT recipient FROM panel_schedules WHERE enabled = 1`).Scan(&dest)
	if !strings.Contains(l, "aprovado, sai") || dest != "5511949420441@s.whatsapp.net" {
		t.Fatalf("o sim agenda para o número: %q %q", l, dest)
	}
}

func TestSimComTextoJunto(t *testing.T) {
	sim := []string{"sim", "SIM, enviar amanha, 8h", "ok pode mandar", "Sim, envia às 8h30", "pode, manda hoje às 14", "sim, pode enviar"}
	nao := []string{"sim, mas troca o texto", "sim 3", "ok, e a Marina?", "pode ser dia 4?", "sim, envia para o Pedro também"}
	for _, s := range sim {
		if !beatitudeEhSim(s) {
			t.Errorf("%q deveria valer como sim", s)
		}
	}
	for _, s := range nao {
		if beatitudeEhSim(s) {
			t.Errorf("%q não é um sim puro", s)
		}
	}
}

func TestNomeDoDestinoSemNomeNaConversa(t *testing.T) {
	defer storeAgendado(t)()
	_, _ = panel.store.db.Exec(`INSERT INTO chats VALUES ('5511950380882@s.whatsapp.net', '', ?)`, time.Now())
	antigo := beatitudeContatoNome
	defer func() { beatitudeContatoNome = antigo }()
	beatitudeContatoNome = func(string) string { return "" }
	if n := beatitudeNomeDestino("5511950380882@s.whatsapp.net"); n != "+55 11 95038-0882" {
		t.Fatalf("sem nome, o telefone legível: %q", n)
	}
	beatitudeContatoNome = func(string) string { return "Fagotti" }
	if n := beatitudeNomeDestino("5511950380882@s.whatsapp.net"); n != "Fagotti" {
		t.Fatalf("o nome da agenda de contatos: %q", n)
	}
	if n := beatitudeNomeDestino("5511999990000@s.whatsapp.net"); n != "Marina Helito" {
		t.Fatalf("o nome da conversa continua valendo: %q", n)
	}
}

func TestListaMistaProgramadaEPendente(t *testing.T) {
	defer storeAgendado(t)()
	amanha := time.Now().In(panelLoc).Add(20 * time.Hour).Format("2006-01-02 15:04")
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511999990000@s.whatsapp.net","texto":"Oi Marina","enviar_em":"` + amanha + `"},` +
		`{"n":2,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris"}]</rascunhos>`
	info := beatitudeTratarRascunhos(resp, "1203@g.us", "responde as duas")
	if info.Programados != 1 || info.Pendentes != 1 || !strings.Contains(info.Cartao, "*cancela 1*") || !strings.Contains(info.Cartao, "(ou *2*)") {
		t.Fatalf("uma programada e uma esperando o sim: %+v", info)
	}
	if !beatitudeEhComando("sim") { // um só esperando: o sim vale para ele
		t.Fatal("sim deveria aprovar o que espera")
	}
}
