package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPonteEhSimN(t *testing.T) {
	ok := map[string]int{"sim 3": 3, "Sim 12": 12, "  SÍM 7 ": 7, "sim\t1": 1}
	for txt, n := range ok {
		got, valido := ponteEhSimN(txt)
		if !valido || got != n {
			t.Errorf("%q: queria %d, veio %d/%v", txt, n, got, valido)
		}
	}
	for _, txt := range []string{"sim", "sim 3 por favor", "pode enviar 3", "sim 0", "sim 100", "3", "sim três", "não sim 3", "sim 3!"} {
		if _, valido := ponteEhSimN(txt); valido {
			t.Errorf("%q não devia valer como aprovação", txt)
		}
	}
}

func TestPonteChamarTokenEErros(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path == "/api/ponte/aprovar" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "erro": "texto_nao_e_sim_n"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "visto_em": "x"})
	}))
	defer srv.Close()
	ponte.cfg = ponteConfig{Token: "bz_teste_0123456789", URL: srv.URL}

	if _, err := ponteChamar("/api/ponte/sinal-de-vida", map[string]interface{}{}, nil); err != nil {
		t.Fatalf("sinal-de-vida: %v", err)
	}
	if auth != "Bearer bz_teste_0123456789" {
		t.Fatalf("header errado: %q", auth)
	}
	status, err := ponteChamar("/api/ponte/aprovar", map[string]interface{}{"numero": 3}, nil)
	if status != http.StatusUnprocessableEntity || err == nil {
		t.Fatalf("queria 422 com erro, veio %d %v", status, err)
	}
	if e, ok := err.(*ponteErroHTTP); !ok || e.Codigo != "texto_nao_e_sim_n" {
		t.Fatalf("código do erro não veio: %v", err)
	}

	ponte.cfg = ponteConfig{}
	if _, err := ponteChamar("/api/ponte/sinal-de-vida", map[string]interface{}{}, nil); err == nil {
		t.Fatal("sem token não pode chamar o servidor")
	}
}

func TestPonteRascunhoValido(t *testing.T) {
	if ponteRascunhoValido("") || ponteRascunhoValido("◆ Beatitude: oi") {
		t.Fatal("vazio ou marca da ponte não vale")
	}
	longo := make([]rune, 1001)
	for i := range longo {
		longo[i] = 'a'
	}
	if ponteRascunhoValido(string(longo)) {
		t.Fatal("mais de 1000 caracteres não vale")
	}
	if !ponteRascunhoValido("Confirmo o jantar de sábado.") {
		t.Fatal("rascunho normal devia valer")
	}
}
