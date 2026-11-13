// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardaLocal(t *testing.T) {
	casos := []struct {
		nome, metodo, host string
		cab                map[string]string
		passa              bool
	}{
		{"painel lendo", "GET", "localhost:8080", nil, true},
		{"painel enviando", "POST", "localhost:8080", map[string]string{"Origin": "http://localhost:8080", "Sec-Fetch-Site": "same-origin"}, true},
		{"painel por 127.0.0.1", "POST", "127.0.0.1:8080", map[string]string{"Origin": "http://127.0.0.1:8080"}, true},
		{"servidor MCP (sem Origin)", "POST", "127.0.0.1:8080", nil, true},
		{"ipv6 local", "GET", "[::1]:8080", nil, true},
		{"site qualquer mandando formulário", "POST", "localhost:8080", map[string]string{"Origin": "https://site-qualquer.example", "Sec-Fetch-Site": "cross-site"}, false},
		{"origem nula (iframe/arquivo)", "POST", "localhost:8080", map[string]string{"Origin": "null"}, false},
		{"só Sec-Fetch-Site cruzado", "POST", "localhost:8080", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"DNS rebinding lendo", "GET", "ataque.example:8080", nil, false},
		{"DNS rebinding enviando", "POST", "ataque.example:8080", map[string]string{"Origin": "http://ataque.example:8080"}, false},
	}
	ok := guardaLocal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, c := range casos {
		r := httptest.NewRequest(c.metodo, "http://"+c.host+"/api/send", nil)
		r.Host = c.host
		for k, v := range c.cab {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		ok.ServeHTTP(w, r)
		if (w.Code == 200) != c.passa {
			t.Errorf("%s: código %d, deveria passar=%v", c.nome, w.Code, c.passa)
		}
	}
}
