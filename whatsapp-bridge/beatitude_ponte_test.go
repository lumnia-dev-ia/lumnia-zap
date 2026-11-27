package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// 25-11: beatitude.lumnia.dev passou a redirecionar (307) para beatitude.ai.
// O Go descartava o token no outro dominio e o app respondia 401
// (token_invalido) a cada minuto. A ponte agora para e diz para onde foi.
func TestPonteRedirecionamentoParaOutroDominio(t *testing.T) {
	var chegouNoNovo bool
	novo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chegouNoNovo = true
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "erro": "token_invalido"})
	}))
	defer novo.Close()
	antigo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, novo.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer antigo.Close()
	ponte.cfg = ponteConfig{Token: "bz_teste_0123456789", URL: antigo.URL}
	defer func() { ponte.cfg = ponteConfig{} }()

	_, err := ponteChamar("/api/ponte/sinal-de-vida", map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("redirecionamento para outro domínio tinha que falhar")
	}
	e, ok := err.(*ponteErroEndereco)
	if !ok {
		t.Fatalf("queria ponteErroEndereco, veio %T: %v", err, err)
	}
	if e.Para != novo.URL || !strings.Contains(err.Error(), "mudou de endereço") {
		t.Fatalf("mensagem não diz para onde foi: %v", err)
	}
	if strings.Contains(err.Error(), "token_invalido") {
		t.Fatalf("não pode virar token_invalido: %v", err)
	}
	if chegouNoNovo {
		t.Fatal("a chamada não devia chegar ao outro domínio sem o token")
	}

	// ponteRequisitar (GET e multipart) segue a mesma regra.
	if _, err := ponteRequisitar(http.MethodGet, "/api/ponte/pessoas", "", nil, nil); err == nil {
		t.Fatal("ponteRequisitar tinha que falhar no redirecionamento")
	} else if _, ok := err.(*ponteErroEndereco); !ok {
		t.Fatalf("ponteRequisitar: queria ponteErroEndereco, veio %T: %v", err, err)
	}
}

// Redirecionamento no mesmo host continua valendo, com o token.
func TestPonteRedirecionamentoMesmoHost(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/velho" {
			http.Redirect(w, r, "/api/ponte/sinal-de-vida", http.StatusTemporaryRedirect)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()
	ponte.cfg = ponteConfig{Token: "bz_teste_0123456789", URL: srv.URL}
	defer func() { ponte.cfg = ponteConfig{} }()

	if _, err := ponteChamar("/velho", map[string]interface{}{}, nil); err != nil {
		t.Fatalf("mesmo host devia seguir: %v", err)
	}
	if auth != "Bearer bz_teste_0123456789" {
		t.Fatalf("token não chegou depois do redirecionamento: %q", auth)
	}
}

func TestPonteBase(t *testing.T) {
	casos := map[string]string{
		"":                              "https://beatitude.ai",
		"https://beatitude.lumnia.dev":  "https://beatitude.ai",
		"https://Beatitude.Lumnia.dev/": "https://beatitude.ai",
		"https://beatitude.ai/":         "https://beatitude.ai",
		"http://127.0.0.1:8099":         "http://127.0.0.1:8099",
	}
	for in, quer := range casos {
		if got := ponteBase(in); got != quer {
			t.Errorf("ponteBase(%q) = %q, queria %q", in, got, quer)
		}
	}
	if ponteURLPadrao != "https://beatitude.ai" || beatitudeMCPURL != "https://beatitude.ai/mcp" {
		t.Fatalf("endereços padrão: %s / %s", ponteURLPadrao, beatitudeMCPURL)
	}
}
