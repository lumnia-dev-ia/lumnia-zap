package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/store"
)

// waTesteAmbiente isola o estado global e os pontos de troca.
func waTesteAmbiente(t *testing.T) {
	t.Helper()
	orig := store.GetWAVersion()
	busca, recon, conect, aviso, agora, dormir := waBuscarVersao, waReconectar, waConectado, waAvisarSuporte, waAgora, waDormir
	t.Cleanup(func() {
		store.SetWAVersion(orig)
		waBuscarVersao, waReconectar, waConectado, waAvisarSuporte, waAgora, waDormir = busca, recon, conect, aviso, agora, dormir
		waVersao = waVersaoEstado{origem: "compilada"}
	})
	waVersao = waVersaoEstado{origem: "compilada"}
	waAvisarSuporte = func(store.WAVersionContainer, time.Time) {}
	waDormir = func(time.Duration) {}
}

func TestWAVersaoMaisNova(t *testing.T) {
	casos := []struct {
		a, b store.WAVersionContainer
		quer bool
	}{
		{store.WAVersionContainer{2, 3000, 2}, store.WAVersionContainer{2, 3000, 1}, true},
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 1}, false},
		{store.WAVersionContainer{2, 3000, 1}, store.WAVersionContainer{2, 3000, 2}, false},
		{store.WAVersionContainer{2, 3001, 0}, store.WAVersionContainer{2, 3000, 9}, true},
		{store.WAVersionContainer{3, 0, 0}, store.WAVersionContainer{2, 9999, 9}, true},
	}
	for _, c := range casos {
		if got := waVersaoMaisNova(c.a, c.b); got != c.quer {
			t.Errorf("%v > %v: %v, queria %v", c.a, c.b, got, c.quer)
		}
	}
}

func TestWAVersaoLer(t *testing.T) {
	v, err := waVersaoLer(" 2.3000.1055969240 ")
	if err != nil || v != (store.WAVersionContainer{2, 3000, 1055969240}) {
		t.Fatalf("got %v %v", v, err)
	}
	for _, ruim := range []string{"", "2.3000", "2.x.1", "0.0.0", "2.3000.99999999999"} {
		if _, err := waVersaoLer(ruim); err == nil {
			t.Errorf("%q deveria falhar", ruim)
		}
	}
}

func TestWAVersaoAtualizarSobeNuncaDesce(t *testing.T) {
	waTesteAmbiente(t)
	base := store.WAVersionContainer{2, 3000, 1000}
	store.SetWAVersion(base)

	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) {
		return &store.WAVersionContainer{2, 3000, 900}, nil // mais velha
	}
	if waVersaoAtualizar(true) || store.GetWAVersion() != base {
		t.Fatalf("desceu para %v", store.GetWAVersion())
	}

	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) {
		return nil, errors.New("sem internet")
	}
	if waVersaoAtualizar(true) || store.GetWAVersion() != base {
		t.Fatalf("erro mudou a versão: %v", store.GetWAVersion())
	}
	if waVersao.consultaErr == "" {
		t.Fatal("erro da consulta não ficou registrado")
	}

	nova := store.WAVersionContainer{2, 3000, 1100}
	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) { return &nova, nil }
	if !waVersaoAtualizar(true) || store.GetWAVersion() != nova {
		t.Fatalf("não subiu: %v", store.GetWAVersion())
	}
	if waVersao.origem != "web.whatsapp.com" || waVersao.consultaErr != "" {
		t.Fatalf("estado: %+v", waVersao)
	}
	if got := store.BaseClientPayload.GetUserAgent().GetAppVersion().GetTertiary(); got != 1100 {
		t.Fatalf("handshake não leva a versão nova: %d", got)
	}
}

func TestWAVersaoAtualizarRespeitaCache(t *testing.T) {
	waTesteAmbiente(t)
	chamadas := 0
	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) {
		chamadas++
		return nil, errors.New("x")
	}
	agora := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	waAgora = func() time.Time { return agora }
	waVersaoAtualizar(false)
	waVersaoAtualizar(false)
	if chamadas != 1 {
		t.Fatalf("cache não segurou: %d", chamadas)
	}
	waVersaoAtualizar(true)
	if chamadas != 2 {
		t.Fatalf("forçar não passou do cache: %d", chamadas)
	}
	agora = agora.Add(waVersaoCache + time.Second)
	waVersaoAtualizar(false)
	if chamadas != 3 {
		t.Fatalf("cache não expirou: %d", chamadas)
	}
}

func TestWAVersaoForcadaPorAmbiente(t *testing.T) {
	waTesteAmbiente(t)
	t.Setenv("LUMNIA_WA_VERSAO", "2.3000.5")
	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) {
		t.Fatal("não devia consultar o site com versão forçada")
		return nil, nil
	}
	waVersaoIniciar()
	if store.GetWAVersion() != (store.WAVersionContainer{2, 3000, 5}) || !waVersao.forcada {
		t.Fatalf("versão forçada não valeu: %v", store.GetWAVersion())
	}
	if waVersaoAtualizar(true) {
		t.Fatal("atualizou por cima da forçada")
	}
}

// O caso de 23/11: 405, o site ainda devolve a recusada por um tempo, depois
// uma mais nova; a ponte só reconecta quando há a mais nova.
func TestWARecuperaDepoisDo405(t *testing.T) {
	waTesteAmbiente(t)
	recusada := store.WAVersionContainer{2, 3000, 1043636855}
	store.SetWAVersion(recusada)

	var mu sync.Mutex
	consultas, reconexoes := 0, 0
	conectado := false
	waBuscarVersao = func(context.Context) (*store.WAVersionContainer, error) {
		mu.Lock()
		defer mu.Unlock()
		consultas++
		if consultas < 3 {
			v := recusada
			return &v, nil
		}
		return &store.WAVersionContainer{2, 3000, 1055969240}, nil
	}
	waReconectar = func() error {
		mu.Lock()
		defer mu.Unlock()
		reconexoes++
		conectado = true
		return nil
	}
	waConectado = func() bool {
		mu.Lock()
		defer mu.Unlock()
		return conectado
	}

	waVersaoRecusada()
	st := map[string]interface{}{}
	waVersaoStatus(st)
	if st["whatsapp_recusou"] == nil {
		t.Fatal("painel não mostra o 405")
	}
	waVersaoRecusada() // 405 repetido não abre segundo laço

	prazo := time.Now().Add(3 * time.Second)
	for {
		waVersao.mu.Lock()
		rodando := waVersao.recuperando
		waVersao.mu.Unlock()
		if !rodando {
			break
		}
		if time.Now().After(prazo) {
			t.Fatal("laço de recuperação não terminou")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if reconexoes != 1 {
		t.Fatalf("reconexões: %d (queria 1, só depois da versão nova)", reconexoes)
	}
	if consultas != 3 {
		t.Fatalf("consultas: %d", consultas)
	}
	if store.GetWAVersion() != (store.WAVersionContainer{2, 3000, 1055969240}) {
		t.Fatalf("versão final %v", store.GetWAVersion())
	}
	st = map[string]interface{}{}
	waVersaoStatus(st)
	if st["whatsapp_recusou"] != nil {
		t.Fatal("405 continua no painel depois de conectar")
	}
}

func TestWAAvisaSuporteUmaVezDepoisDe5Min(t *testing.T) {
	waTesteAmbiente(t)
	agora := time.Date(2026, 11, 23, 23, 6, 0, 0, time.UTC)
	waAgora = func() time.Time { return agora }
	avisos := make(chan store.WAVersionContainer, 5)
	waAvisarSuporte = func(v store.WAVersionContainer, _ time.Time) { avisos <- v }

	waVersao.recusadaEm = agora
	waVersao.recusada = store.WAVersionContainer{2, 3000, 1}
	waAvisarSeDemorou()
	agora = agora.Add(6 * time.Minute)
	waAvisarSeDemorou()
	waAvisarSeDemorou()
	time.Sleep(20 * time.Millisecond)
	if n := len(avisos); n != 1 {
		t.Fatalf("avisos: %d", n)
	}
	agora = agora.Add(2 * time.Hour)
	st := map[string]interface{}{}
	waVersaoStatus(st)
	if r := st["whatsapp_recusou"].(map[string]interface{}); r["demorando"] != true {
		t.Fatalf("depois de 1 h o painel devia pedir atualização: %v", r)
	}
}
