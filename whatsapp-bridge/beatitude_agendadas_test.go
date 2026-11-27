package main

import (
	"strings"
	"testing"
	"time"
)

// agendarParaTeste cria um rascunho com horário para o chat dado. Desde
// 25-11, horário a mais de 5 minutos já nasce programado, sem o "sim".
// Devolve o horário.
func agendarParaTeste(t *testing.T, jid, texto string, daqui time.Duration) time.Time {
	t.Helper()
	quando := time.Now().In(panelLoc).Add(daqui).Truncate(time.Minute)
	antes := len(beatitudeAgendadasAbertas(time.Now()))
	resp := `<rascunhos>[{"n":1,"chat_jid":"` + jid + `","texto":"` + texto + `","enviar_em":"` + quando.Format("2006-01-02 15:04") + `"}]</rascunhos>`
	_, cartao := beatitudeSepararRascunhos(resp, "1203@g.us", "amanhã às "+quando.Format("15:04")+" manda")
	if len(beatitudeAgendadasAbertas(time.Now())) != antes+1 {
		t.Fatalf("deveria nascer programada: %q", cartao)
	}
	time.Sleep(2 * time.Millisecond)
	return quando
}

func agendamentosLigados(t *testing.T) int {
	t.Helper()
	var n int
	_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM panel_schedules WHERE enabled = 1`).Scan(&n)
	return n
}

// O caso da Lisiane em 17-11: aprovou para as 8h e pediu o cancelamento em
// frase livre. Tem de cancelar sem chamar o Claude.
func TestCancelamentoEmFraseLivre(t *testing.T) {
	defer storeAgendado(t)()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Bom dia, Marina! Manda os vídeos ao Ricardo?", 10*time.Hour)

	frases := []string{
		"Cancelar o envio da mensagem para a Marina para ela enviar os vídeos ao Ricardo; ela já enviou",
		"Está aqui a mensagem agendada para amanhã, 8h para a Marina. Cancelar o envio",
		"cancela a mensagem da marina",
		"não precisa mais mandar a mensagem pra Marina",
		"desiste do envio de amanhã",
	}
	for _, f := range frases {
		if !beatitudeEhCancelaLivre(f) {
			t.Errorf("deveria reconhecer o cancelamento: %q", f)
		}
	}
	// o que NÃO é cancelamento de mensagem segue para o mordomo
	for _, f := range []string{
		"Cancelar a reunião agendada com a Marina amanhã",
		"cancela o evento das 15h",
		"a Marina cancelou o jantar",
		"manda mensagem pra Marina dizendo que cancelei o voo",
		"manda mensagem pra Marina dizendo para cancelar o envio dos vídeos",
		"a Marina cancelou a mensagem que ia mandar",
		"cancelei a mensagem ontem",
		"pendentes",
	} {
		if beatitudeEhCancelaLivre(f) {
			t.Errorf("não deveria tratar como cancelamento de mensagem: %q", f)
		}
	}

	linhas := strings.Join(beatitudeCancelarLivre(frases[0], time.Now()), "\n")
	if !strings.Contains(linhas, "Marina Helito") || !strings.Contains(linhas, "cancelado, não vai sair") {
		t.Fatalf("deveria cancelar a da Marina: %q", linhas)
	}
	if agendamentosLigados(t) != 0 {
		t.Fatal("o agendamento deveria ter sido desligado")
	}
	if len(beatitudeAgendadasAbertas(time.Now())) != 0 {
		t.Fatal("não deveria sobrar agendada")
	}
	// depois de cancelada, a frase não é mais comando (nada em aberto)
	if beatitudeEhCancelaLivre(frases[0]) {
		t.Fatal("sem nada em aberto, a frase volta a ser conversa")
	}
}

func TestCancelamentoComDuasAgendadasPedeQualOuAchaPeloNome(t *testing.T) {
	defer storeAgendado(t)()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	agendarParaTeste(t, "5511888880000@s.whatsapp.net", "Oi Cris", 6*time.Hour)
	if n := len(beatitudeAgendadasAbertas(time.Now())); n != 2 {
		t.Fatalf("esperava 2 agendadas, veio %d", n)
	}

	// sem nome: pergunta e ensina o comando de cada uma; nada é cancelado
	linhas := strings.Join(beatitudeCancelarLivre("cancela a mensagem agendada", time.Now()), "\n")
	if !strings.Contains(linhas, "Qual delas") || !strings.Contains(linhas, "*cancela 1*") || !strings.Contains(linhas, "*cancela 2*") {
		t.Fatalf("deveria perguntar qual: %q", linhas)
	}
	if agendamentosLigados(t) != 2 {
		t.Fatal("na dúvida, nada pode ser cancelado")
	}

	// com o nome: cancela só a dela
	linhas = strings.Join(beatitudeCancelarLivre("cancelar o envio pra Cris, ela já respondeu", time.Now()), "\n")
	if !strings.Contains(linhas, "Cris Meirelles") || !strings.Contains(linhas, "cancelado") {
		t.Fatalf("deveria cancelar a da Cris: %q", linhas)
	}
	abertas := beatitudeAgendadasAbertas(time.Now())
	if len(abertas) != 1 || abertas[0].jid != "5511999990000@s.whatsapp.net" {
		t.Fatalf("deveria sobrar só a da Marina: %+v", abertas)
	}
}

// Desde 25-11 cada programada tem o seu número. "cancela agendada N" é o
// mesmo que "cancela N"; a lista é a de "programadas".
func TestCancelaAgendadaPeloNumeroEListagem(t *testing.T) {
	defer storeAgendado(t)()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	agendarParaTeste(t, "5511888880000@s.whatsapp.net", "Oi Cris", 6*time.Hour)

	if !beatitudeEhListaAgendadas("agendadas") || !beatitudeEhListaAgendadas("Mensagens programadas?") || !beatitudeEhListaAgendadas("o que vai sair?") {
		t.Fatal("deveria reconhecer o pedido da lista")
	}
	if beatitudeEhListaAgendadas("quero ver as reuniões agendadas da semana") {
		t.Fatal("frase longa sobre outra coisa não é o comando")
	}
	lista := strings.Join(beatitudeListarProgramadas(time.Now()), "\n")
	if !strings.Contains(lista, "1 · Marina Helito") || !strings.Contains(lista, "2 · Cris Meirelles") {
		t.Fatalf("lista na ordem do horário: %q", lista)
	}

	if !beatitudeEhCancelaLivre("cancela agendada 1") {
		t.Fatal("cancela agendada 1 deveria ser comando")
	}
	linhas := strings.Join(beatitudeCancelarLivre("cancela agendada 1", time.Now()), "\n")
	if !strings.Contains(linhas, "Marina Helito") || !strings.Contains(linhas, "cancelado") {
		t.Fatalf("deveria cancelar a 1: %q", linhas)
	}
	linhas = strings.Join(beatitudeCancelarLivre("cancela agendada 5", time.Now()), "\n")
	if !strings.Contains(linhas, "Não há programada 5") || !strings.Contains(linhas, "Cris Meirelles") {
		t.Fatalf("número que não existe mostra a lista: %q", linhas)
	}
	if agendamentosLigados(t) != 1 {
		t.Fatal("deveria sobrar um agendamento ligado")
	}
}

// "cancela 1" é o que a ponte ensina ao aprovar. Tem de continuar valendo
// depois que outro pedido cria um lote novo.
func TestCancelaNAlcancaAgendadaDeLoteAntigo(t *testing.T) {
	defer storeAgendado(t)()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	// lote novo, rascunho 1 enviado na hora (sem horário): o "1" do último lote já foi
	beatitudeSepararRascunhos(`<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris"}]</rascunhos>`, "1203@g.us", "responde a Cris")
	_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviado_em = ? WHERE chat_jid = '5511888880000@s.whatsapp.net'`, time.Now())

	linhas := strings.Join(beatitudeComando("cancela 1", time.Now()), "\n")
	if !strings.Contains(linhas, "Marina Helito") || !strings.Contains(linhas, "cancelado") {
		t.Fatalf("cancela 1 deveria alcançar a agendada da Marina: %q", linhas)
	}
	if agendamentosLigados(t) != 0 {
		t.Fatal("o agendamento deveria ter sido desligado")
	}
}

func TestNumeroDaFraseIgnoraHoraEData(t *testing.T) {
	casos := map[string]int{
		"cancela a mensagem 2":                        2,
		"cancela a mensagem das 8h":                   0,
		"cancela a mensagem de amanhã às 08:00":       0,
		"cancela o envio do dia 30/11":                0,
		"Está agendada para amanhã, 8h. Cancelar a 3": 3,
		"cancela a 1 e a 2":                           0,
		"cancela o envio das 8 horas":                 0,
		"cancela a mensagem da marina":                0,
	}
	for frase, quer := range casos {
		if got := beatitudeNumeroDaFrase(frase); got != quer {
			t.Errorf("%q: quer %d, veio %d", frase, quer, got)
		}
	}
}

func TestRegraAgendadasCitaOBlocoDoContexto(t *testing.T) {
	defer storeAgendado(t)()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	b := beatitudeBlocoProgramadas(time.Now())
	// a regra manda o mordomo olhar o bloco pelo título que ele de fato tem
	if !strings.Contains(b, "Mensagens já programadas por este grupo") || !strings.Contains(beatitudeRegraAgendadas, "Mensagens já programadas por este grupo") {
		t.Fatalf("regra e bloco desencontrados: %q", b)
	}
	if !strings.Contains(beatitudeRegraAgendadas, "*cancela N*") || strings.Contains(beatitudeRegraAgendadas, "cancela agendada") {
		t.Fatal("a regra deve ensinar o comando de número único")
	}
}

// ---------------------------------------------------------------- espelho no app

func TestItensDoEspelho(t *testing.T) {
	defer storeAgendado(t)()
	quando := agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	itens := ponteItensAgendadas(time.Now())
	if len(itens) != 1 {
		t.Fatalf("esperava 1 item, veio %d", len(itens))
	}
	it := itens[0]
	if it.Estado != "agendado" || it.DestinoNome != "Marina Helito" || it.Texto != "Oi Marina" || !strings.HasPrefix(it.Ref, "r") || it.Numero != 1 {
		t.Fatalf("item: %+v", it)
	}
	if got, _ := time.Parse(time.RFC3339, it.EnviarEm); !got.Equal(quando) {
		t.Fatalf("enviar_em: %s, queria %s", it.EnviarEm, quando)
	}
	// cancelada continua na lista (por 48 h), com o estado novo
	beatitudeCancelarLivre("cancela a mensagem da marina", time.Now())
	itens = ponteItensAgendadas(time.Now())
	if len(itens) != 1 || itens[0].Estado != "cancelado" {
		t.Fatalf("depois de cancelar: %+v", itens)
	}
	// rascunho comum (sem horário) nunca vai ao app por esta rota
	beatitudeSepararRascunhos(`<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris"}]</rascunhos>`, "1203@g.us", "responde a Cris")
	if n := len(ponteItensAgendadas(time.Now())); n != 1 {
		t.Fatalf("rascunho sem horário não entra: %d itens", n)
	}
}

func TestComandosDoApp(t *testing.T) {
	defer storeAgendado(t)()
	ponteAgend.mu.Lock()
	ponteAgend.feitos, ponteAgend.vistos = nil, nil
	ponteAgend.mu.Unlock()
	agendarParaTeste(t, "5511999990000@s.whatsapp.net", "Oi Marina", 5*time.Hour)
	ref := ponteItensAgendadas(time.Now())[0].Ref
	agora := time.Now()

	// texto
	ls := ponteAplicarComandosAgendadas([]ponteComandoAgendada{{ID: "c1", Ref: ref, Tipo: "texto", Valor: "Oi Marina, tudo bem?"}}, agora)
	var msg string
	_ = panel.store.db.QueryRow(`SELECT message FROM panel_schedules`).Scan(&msg)
	if msg != "Oi Marina, tudo bem?" || len(ls) != 1 || !strings.Contains(ls[0], "texto trocado") {
		t.Fatalf("texto: %q %v", msg, ls)
	}
	// hora
	nova := agora.In(panelLoc).Add(26 * time.Hour).Truncate(time.Minute)
	ls = ponteAplicarComandosAgendadas([]ponteComandoAgendada{{ID: "c2", Ref: ref, Tipo: "hora", Valor: nova.UTC().Format(time.RFC3339)}}, agora)
	var onDate, atTime string
	_ = panel.store.db.QueryRow(`SELECT on_date, at_time FROM panel_schedules`).Scan(&onDate, &atTime)
	if onDate != nova.Format("2006-01-02") || atTime != nova.Format("15:04") || !strings.Contains(strings.Join(ls, " "), "horário trocado") {
		t.Fatalf("hora: %s %s %v", onDate, atTime, ls)
	}
	if got := ponteItensAgendadas(time.Now())[0].EnviarEm; got != nova.UTC().Format(time.RFC3339) {
		t.Fatalf("o espelho deveria levar o horário novo: %s", got)
	}
	// hora no passado e além de 7 dias: recusa e não mexe
	ls = ponteAplicarComandosAgendadas([]ponteComandoAgendada{
		{ID: "c3", Ref: ref, Tipo: "hora", Valor: agora.Add(-time.Hour).UTC().Format(time.RFC3339)},
		{ID: "c4", Ref: ref, Tipo: "hora", Valor: agora.Add(9 * 24 * time.Hour).UTC().Format(time.RFC3339)},
		{ID: "c5", Ref: "r9999", Tipo: "cancelar"},
		{ID: "c6", Ref: ref, Tipo: "apagar-tudo"},
	}, agora)
	if len(ls) != 4 || !strings.Contains(strings.Join(ls, "|"), "já passou") || !strings.Contains(strings.Join(ls, "|"), "7 dias") {
		t.Fatalf("recusas: %v", ls)
	}
	if agendamentosLigados(t) != 1 {
		t.Fatal("recusas não podem desligar o agendamento")
	}
	// cancelar; repetido não cancela de novo nem falha
	ls = ponteAplicarComandosAgendadas([]ponteComandoAgendada{{ID: "c7", Ref: ref, Tipo: "cancelar"}}, agora)
	if agendamentosLigados(t) != 0 || !strings.Contains(strings.Join(ls, " "), "cancelado") {
		t.Fatalf("cancelar: %v", ls)
	}
	ls = ponteAplicarComandosAgendadas([]ponteComandoAgendada{{ID: "c7", Ref: ref, Tipo: "cancelar"}, {ID: "c8", Ref: ref, Tipo: "cancelar"}}, agora)
	if len(ls) != 1 || !strings.Contains(ls[0], "já estava cancelado") {
		t.Fatalf("cancelar de novo: %v", ls)
	}

	// confirmações guardadas para a próxima chamada: uma por comando
	ponteAgend.mu.Lock()
	feitos := append([]ponteComandoFeito{}, ponteAgend.feitos...)
	ponteAgend.mu.Unlock()
	ok := map[string]bool{}
	for _, f := range feitos {
		ok[f.ID] = f.OK
	}
	for id, quer := range map[string]bool{"c1": true, "c2": true, "c3": false, "c4": false, "c5": false, "c6": false, "c7": true, "c8": true} {
		if got, tem := ok[id]; !tem || got != quer {
			t.Errorf("confirmação de %s: quer %v, veio %v (tem=%v)", id, quer, got, tem)
		}
	}
}
