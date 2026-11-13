package main

import (
	"strings"
	"testing"
	"time"
)

func TestFalaEhConversa(t *testing.T) {
	sim := []string{"Mandar um WhatsApp para a Lisiane dizendo te amo", "lembra que tenho dentista terça", "e o brief de hoje?", "123 pessoas vieram", "sim, pode marcar a reunião"}
	nao := []string{"", "sim 3", " SIM 12 ", "1", "12.", "ok", "pode enviar", "Sim!", beatitudeMarca + ": recebi.", "◆ Beatitude\n\nTexto"}
	for _, s := range sim {
		if !falaEhConversa(s) {
			t.Errorf("devia ser conversa: %q", s)
		}
	}
	for _, s := range nao {
		if falaEhConversa(s) {
			t.Errorf("nao devia ser conversa: %q", s)
		}
	}
}

func TestFalasSoVisivel(t *testing.T) {
	r := "Preparei.\n<rascunhos>[{\"n\":1}]</rascunhos>\n<navegador>{\"tarefa\":\"x\"}</navegador>\n<pix>abc</pix>"
	if got := falasSoVisivel(r); got != "Preparei." {
		t.Fatalf("visivel = %q", got)
	}
}

func TestFalasEnfileirarSubstituiComResposta(t *testing.T) {
	falas.mu.Lock()
	falas.pendentes = nil
	falas.mu.Unlock()
	f := falaNova("w1", time.Now(), "oi", "texto")
	falasEnfileirar([]falaGrupo{f, falaNova("", time.Now(), "sem id", "texto"), falaNova("w2", time.Now(), "", "texto")})
	f.RespostaMordomo = "ok"
	falasEnfileirar([]falaGrupo{f})
	falas.mu.Lock()
	defer falas.mu.Unlock()
	if len(falas.pendentes) != 1 || falas.pendentes[0].RespostaMordomo != "ok" || falas.pendentes[0].Processar {
		t.Fatalf("pendentes = %+v", falas.pendentes)
	}
	if !strings.HasSuffix(falas.pendentes[0].Em, "Z") {
		t.Fatalf("em deve ser UTC RFC3339: %s", falas.pendentes[0].Em)
	}
}

func TestFalasConfirmarLimpaSoAsEnviadas(t *testing.T) {
	falas.mu.Lock()
	falas.pendentes = []falaGrupo{{WaID: "a", Texto: "1"}, {WaID: "b", Texto: "2"}, {WaID: "a", Texto: "1", RespostaMordomo: "r"}}
	falas.mu.Unlock()
	falasConfirmar([]falaGrupo{{WaID: "a", Texto: "1"}})
	falas.mu.Lock()
	defer falas.mu.Unlock()
	if len(falas.pendentes) != 2 || falas.pendentes[0].WaID != "b" || falas.pendentes[1].RespostaMordomo != "r" {
		t.Fatalf("pendentes = %+v", falas.pendentes)
	}
}
