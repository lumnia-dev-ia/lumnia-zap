package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

type grupoDeTeste struct {
	textos   []string
	enquetes []*waProto.PollCreationMessage
	ids      []string
}

// storeReacoes: banco de teste, cinco contatos, envio ao grupo e aos
// contatos interceptados.
func storeReacoes(t *testing.T) (*grupoDeTeste, *[]envioDeTeste, func()) {
	t.Helper()
	envios, _, fim := storeAnexos(t)
	initBeatitudeReacoes()
	for i := 1; i <= 5; i++ {
		_, _ = panel.store.db.Exec(`INSERT INTO chats VALUES (?, ?, ?)`,
			fmt.Sprintf("551170000000%d@s.whatsapp.net", i), fmt.Sprintf("Pessoa %d", i), time.Now())
	}
	g := &grupoDeTeste{}
	antigo := beatitudeEnviarGrupo
	beatitudeEnviarGrupo = func(m *waProto.Message, texto string) (string, error) {
		id := fmt.Sprintf("MSG%d", len(g.ids)+1)
		g.ids = append(g.ids, id)
		if p := m.GetPollCreationMessage(); p != nil {
			g.enquetes = append(g.enquetes, p)
		} else {
			g.textos = append(g.textos, texto)
		}
		return id, nil
	}
	beatitudeReacaoVistas.Range(func(k, _ interface{}) bool { beatitudeReacaoVistas.Delete(k); return true })
	return g, envios, func() { beatitudeEnviarGrupo = antigo; fim() }
}

func rascunhosDe(n int, extra string) string {
	var p []string
	for i := 1; i <= n; i++ {
		p = append(p, fmt.Sprintf(`{"n":%d,"chat_jid":"551170000000%d@s.whatsapp.net","texto":"Oi %d"%s}`, i, i, i, extra))
	}
	return "<rascunhos>[" + strings.Join(p, ",") + "]</rascunhos>"
}

func cartaoComJoinha(t *testing.T, n int, extra, pedido string) (*grupoDeTeste, *[]envioDeTeste, func()) {
	t.Helper()
	g, envios, fim := storeReacoes(t)
	desde := time.Now()
	_, cartao := beatitudeSepararRascunhos(rascunhosDe(n, extra), "1203@g.us", pedido)
	beatitudeEnviarCartoes(cartao, desde)
	return g, envios, fim
}

func TestJoinhaNoCartaoAprovaOsRascunhos(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 2, "", "responde os dois")
	defer fim()
	if len(g.textos) != 1 || len(g.enquetes) != 0 {
		t.Fatalf("até 3 rascunhos: só o cartão, sem enquete: %d textos, %d enquetes", len(g.textos), len(g.enquetes))
	}
	if !strings.Contains(g.textos[0], "reaja com 👍 para aprovar todos") {
		t.Fatalf("o cartão deveria ensinar o 👍: %q", g.textos[0])
	}
	linhas := strings.Join(beatitudeAoReagir("R1", g.ids[0], "👍🏽", time.Now()), "\n")
	if !strings.Contains(linhas, "1 · Pessoa 1: enviado") || !strings.Contains(linhas, "2 · Pessoa 2: enviado") {
		t.Fatalf("linhas: %q", linhas)
	}
	if len(*envios) != 2 {
		t.Fatalf("dois envios: %+v", *envios)
	}
	// o mesmo evento de novo (reconexão) não faz nada
	if l := beatitudeAoReagir("R1", g.ids[0], "👍", time.Now()); l != nil {
		t.Fatalf("reação repetida: %q", l)
	}
}

func TestOutrasReacoesNaoFazemNada(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 1, "", "responde")
	defer fim()
	for i, e := range []string{"❤️", "😂", "🙏", ""} {
		if l := beatitudeAoReagir(fmt.Sprintf("R%d", i), g.ids[0], e, time.Now()); l != nil {
			t.Fatalf("%q não pode aprovar: %q", e, l)
		}
	}
	// 👍 numa mensagem que não é cartão
	if l := beatitudeAoReagir("RX", "OUTRA", "👍", time.Now()); l != nil {
		t.Fatalf("👍 fora do cartão: %q", l)
	}
	if len(*envios) != 0 {
		t.Fatal("nada sai")
	}
}

func TestXisNoCartaoCancela(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 2, "", "responde")
	defer fim()
	linhas := strings.Join(beatitudeAoReagir("R1", g.ids[0], "❌", time.Now()), "\n")
	if !strings.Contains(linhas, "cancelado") || len(*envios) != 0 {
		t.Fatalf("❌ cancela e não envia: %q %+v", linhas, *envios)
	}
	if beatitudePendentes(beatitudeJanelaResposta) != 0 {
		t.Fatal("nada pendente depois do ❌")
	}
}

func TestJoinhaRespeitaOHorario(t *testing.T) {
	quando := time.Now().In(panelLoc).Add(3 * time.Hour)
	g, envios, fim := cartaoComJoinha(t, 1, `,"enviar_em":"`+quando.Format("2006-01-02 15:04")+`"`, "às "+quando.Format("15:04")+" manda")
	defer fim()
	linhas := strings.Join(beatitudeAoReagir("R1", g.ids[0], "👍", time.Now()), "\n")
	if !strings.Contains(linhas, "aprovado, sai") || len(*envios) != 0 {
		t.Fatalf("com horário, 👍 agenda e não envia agora: %q %+v", linhas, *envios)
	}
}

func TestEnqueteComCincoRascunhos(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 5, "", "pendentes")
	defer fim()
	if len(g.enquetes) != 1 {
		t.Fatalf("com 5 rascunhos vai uma enquete: %d", len(g.enquetes))
	}
	p := g.enquetes[0]
	if p.GetSelectableOptionsCount() != 0 || len(p.GetOptions()) != 5 || p.GetOptions()[2].GetOptionName() != "3 · Pessoa 3" {
		t.Fatalf("enquete aberta com uma opção por rascunho: %+v", p)
	}
	if !strings.Contains(g.textos[0], "Marque na enquete abaixo") {
		t.Fatalf("o cartão aponta para a enquete: %q", g.textos[0])
	}
	cartaoID, enqueteID := g.ids[0], g.ids[1]

	// 👍 no cartão, com enquete: pede para usar a enquete
	if l := strings.Join(beatitudeAoReagir("R1", cartaoID, "👍", time.Now()), ""); !strings.Contains(l, "marque na enquete") {
		t.Fatalf("👍 no cartão com enquete: %q", l)
	}
	// 👍 na enquete sem nada marcado
	if l := strings.Join(beatitudeAoReagir("R2", enqueteID, "👍", time.Now()), ""); !strings.Contains(l, "nada marcado") {
		t.Fatalf("enquete vazia: %q", l)
	}
	// marca 1 e 3, depois troca para 1, 3 e 5, depois desmarca o 5: vale o último voto
	nomes := []string{}
	for _, o := range p.GetOptions() {
		nomes = append(nomes, o.GetOptionName())
	}
	h := whatsmeow.HashPollOptions(nomes)
	beatitudeAoVotar(enqueteID, [][]byte{h[0], h[2]})
	beatitudeAoVotar(enqueteID, [][]byte{h[0], h[2], h[4]})
	beatitudeAoVotar(enqueteID, [][]byte{h[2], h[0]})
	if len(*envios) != 0 {
		t.Fatal("marcar não envia")
	}
	linhas := strings.Join(beatitudeAoReagir("R3", enqueteID, "👍", time.Now()), "\n")
	if !strings.Contains(linhas, "1 · Pessoa 1: enviado") || !strings.Contains(linhas, "3 · Pessoa 3: enviado") || strings.Contains(linhas, "Pessoa 5") {
		t.Fatalf("só os marcados: %q", linhas)
	}
	if len(*envios) != 2 {
		t.Fatalf("envios: %+v", *envios)
	}
	// "envia" depois: os já enviados não saem de novo
	l, ok := beatitudeConfirmarEnquete(time.Now())
	if !ok || !strings.Contains(strings.Join(l, "\n"), "já tinha sido enviado") || len(*envios) != 2 {
		t.Fatalf("envia repetido: %v %q %+v", ok, l, *envios)
	}
}

func TestEnviaPorTextoComEnquete(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 4, "", "pendentes")
	defer fim()
	nomes := []string{}
	for _, o := range g.enquetes[0].GetOptions() {
		nomes = append(nomes, o.GetOptionName())
	}
	beatitudeAoVotar(g.ids[1], [][]byte{whatsmeow.HashPollOptions(nomes)[3]})
	l, ok := beatitudeConfirmarEnquete(time.Now())
	if !ok || !strings.Contains(strings.Join(l, ""), "4 · Pessoa 4: enviado") || len(*envios) != 1 {
		t.Fatalf("envia manda o marcado: %q %+v", l, *envios)
	}
}

func TestReacaoEmListaAntigaRecusada(t *testing.T) {
	g, envios, fim := cartaoComJoinha(t, 1, "", "responde")
	defer fim()
	antigo := g.ids[0]
	time.Sleep(2 * time.Millisecond)
	desde := time.Now()
	_, cartao := beatitudeSepararRascunhos(rascunhosDe(1, ""), "1203@g.us", "responde de novo")
	beatitudeEnviarCartoes(cartao, desde)
	l := strings.Join(beatitudeAoReagir("R1", antigo, "👍", time.Now()), "")
	if !strings.Contains(l, "não é a mais recente") || len(*envios) != 0 {
		t.Fatalf("lista antiga: %q %+v", l, *envios)
	}
}
