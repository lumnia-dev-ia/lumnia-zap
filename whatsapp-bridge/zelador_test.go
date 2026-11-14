package main

import (
	"strings"
	"testing"
	"time"
)

func zeladorDeTeste(t *testing.T, aberto map[string]bool) (*[]string, *[]string) {
	t.Helper()
	a, ab, inst, lig, av := zeladorAberto, zeladorAbrir, zeladorInstalado, zeladorLigado, zeladorAvisar
	t.Cleanup(func() {
		zeladorAberto, zeladorAbrir, zeladorInstalado, zeladorLigado, zeladorAvisar = a, ab, inst, lig, av
		zelador.fechado, zelador.falhas, zelador.avisado, zelador.pausaAte = nil, nil, nil, nil
	})
	zelador.fechado, zelador.falhas, zelador.avisado, zelador.pausaAte = nil, nil, nil, nil
	var abertos, avisos []string
	zeladorAberto = func(p string) bool { return aberto[p] }
	zeladorAbrir = func(n string) error { abertos = append(abertos, n); return nil }
	zeladorInstalado = func(zeladorApp) bool { return true }
	zeladorLigado = func() bool { return true }
	zeladorAvisar = func(s string) { avisos = append(avisos, s) }
	return &abertos, &avisos
}

func TestZeladorEsperaDuasRodadasParaReabrirOClaude(t *testing.T) {
	abertos, _ := zeladorDeTeste(t, map[string]bool{"Google Chrome": true})
	agora := time.Now()
	zeladorRodada(agora)
	if len(*abertos) != 0 {
		t.Fatalf("na primeira rodada fechado não reabre o Claude (instalador pode estar gravando): %v", *abertos)
	}
	zeladorRodada(agora.Add(zeladorCada))
	if len(*abertos) != 1 || (*abertos)[0] != "Claude" {
		t.Fatalf("na segunda rodada deveria abrir o Claude: %v", *abertos)
	}
}

func TestZeladorChromeAbreNaHoraEPausaRespeitada(t *testing.T) {
	abertos, _ := zeladorDeTeste(t, map[string]bool{})
	zeladorPausar("claude", time.Hour)
	agora := time.Now()
	zeladorRodada(agora)
	zeladorRodada(agora.Add(zeladorCada))
	for _, n := range *abertos {
		if n == "Claude" {
			t.Fatal("Claude reaberto durante a pausa do Reparar")
		}
	}
	if len(*abertos) == 0 || (*abertos)[0] != "Google Chrome" {
		t.Fatalf("Chrome deveria abrir já na primeira rodada: %v", *abertos)
	}
}

func TestZeladorAvisaUmaVezDepoisDeTresFalhas(t *testing.T) {
	_, avisos := zeladorDeTeste(t, map[string]bool{"Claude": true})
	agora := time.Now()
	for i := 0; i < 6; i++ {
		zeladorRodada(agora.Add(time.Duration(i) * zeladorCada))
	}
	if len(*avisos) != 1 || !strings.Contains((*avisos)[0], "Google Chrome") {
		t.Fatalf("avisos = %v", *avisos)
	}
}

func TestZeladorDesligadoNaoMexe(t *testing.T) {
	abertos, _ := zeladorDeTeste(t, map[string]bool{})
	zeladorLigado = func() bool { return false }
	zeladorRodada(time.Now())
	zeladorRodada(time.Now().Add(zeladorCada))
	if len(*abertos) != 0 {
		t.Fatalf("com o mordomo desligado o zelador não abre nada: %v", *abertos)
	}
}

func TestProntidaoAvisoNaoDerrubaPronta(t *testing.T) {
	itens := []prontidaoItem{{Chave: "a", OK: true}, {Chave: "filevault_desligado", OK: false, Aviso: true}}
	if !prontidaoTudoOK(itens) {
		t.Fatal("aviso amarelo não pode tirar o pronta")
	}
	itens = append(itens, prontidaoItem{Chave: "claude_aberto", OK: false})
	if prontidaoTudoOK(itens) {
		t.Fatal("Claude fechado tira o pronta")
	}
}
