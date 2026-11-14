package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func storePreciso(t *testing.T) {
	t.Helper()
	antigo := panel.store
	panel.store = storeDeTeste(t)
	t.Cleanup(func() { panel.store = antigo })
	initBeatitudeNavegador()
	initBeatitudePreciso()
}

func TestPrecisoSeparaBloco(t *testing.T) {
	v, p := beatitudeSepararPreciso("Cheguei no login.\n<preciso>{\"site\":\"LATAM\",\"o_que\":\"entrar com a sua senha\"}</preciso>")
	if v != "Cheguei no login." || p == nil || p.Site != "LATAM" || p.OQue != "entrar com a sua senha" {
		t.Fatalf("v=%q p=%+v", v, p)
	}
	if _, p := beatitudeSepararPreciso("<preciso>{\"site\":\"x\"}</preciso>"); p != nil {
		t.Fatal("bloco sem o_que não vale")
	}
}

func TestAvisoPrecisoComLinkEContinua(t *testing.T) {
	s := beatitudeAvisoPreciso(7, beatitudePreciso{Site: "LATAM", OQue: "resolver a verificação de robô"}, "https://remotedesktop.google.com/access/session/x", true)
	for _, quer := range []string{"Ver a máquina", "https://remotedesktop.google.com", "*continua 7*", "nunca digito senha", "30 minutos"} {
		if !strings.Contains(s, quer) {
			t.Fatalf("falta %q em %q", quer, s)
		}
	}
	if s := beatitudeAvisoPreciso(7, beatitudePreciso{OQue: "x"}, "", false); strings.Contains(s, "continua") {
		t.Fatalf("sem sessão não oferece continua: %q", s)
	}
	if ponteEnderecoSeguro("javascript:alert(1)") != "" || ponteEnderecoSeguro("http://x") != "" {
		t.Fatal("só https vai ao grupo")
	}
}

func TestContinuaSoComTarefaPausada(t *testing.T) {
	storePreciso(t)
	agora := time.Now()
	if _, ok := beatitudeEhContinua("continua", agora); ok {
		t.Fatal("sem tarefa pausada, 'continua' é conversa")
	}
	res, _ := panel.store.db.Exec(`INSERT INTO beatitude_navegador (tarefa, criado_em, iniciado_em, sessao, pausado_em, precisa)
		VALUES ('comprar', ?, ?, 'sess-1', ?, 'entrar com a senha')`, agora, agora, agora)
	id, _ := res.LastInsertId()
	if got, ok := beatitudeEhContinua("Continua", agora); !ok || got != id {
		t.Fatalf("continua sem número: %d %v", got, ok)
	}
	if got, ok := beatitudeEhContinua("continua "+itoa(id), agora); !ok || got != id {
		t.Fatalf("continua N: %d %v", got, ok)
	}
	if _, ok := beatitudeEhContinua("continua a conversa sobre o jantar", agora); ok {
		t.Fatal("frase longa não é comando")
	}
	if _, ok := beatitudeEhContinua("continua", agora.Add(beatitudeNavPausaMax+time.Minute)); ok {
		t.Fatal("sem número, pausa vencida não conta")
	}
}

func TestPausaVencidaCancelaUmaVez(t *testing.T) {
	storePreciso(t)
	enviadas := 0
	antigo := beatitudeSendHook
	beatitudeSendHook = func(string) { enviadas++ }
	t.Cleanup(func() { beatitudeSendHook = antigo })

	pausa := time.Now().Add(-31 * time.Minute)
	res, _ := panel.store.db.Exec(`INSERT INTO beatitude_navegador (tarefa, criado_em, sessao, pausado_em) VALUES ('x', ?, 's', ?)`, pausa, pausa)
	id, _ := res.LastInsertId()
	beatitudeCancelarPausaVencida(id, pausa)
	beatitudeCancelarPausaVencida(id, pausa)
	var cancelado *time.Time
	_ = panel.store.db.QueryRow(`SELECT cancelado_em FROM beatitude_navegador WHERE id = ?`, id).Scan(&cancelado)
	if cancelado == nil || enviadas != 1 {
		t.Fatalf("cancelado=%v enviadas=%d", cancelado, enviadas)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
