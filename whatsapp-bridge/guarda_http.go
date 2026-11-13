// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// Guarda de todas as rotas da ponte (1.1.1, achado da revisão).
//
// Escutar só em 127.0.0.1 não basta contra o navegador da própria pessoa:
//
//   - CSRF: qualquer site aberto podia mandar um formulário (text/plain com
//     corpo em formato JSON) para http://localhost:8080/api/send e a ponte
//     enviava a mensagem pelo WhatsApp dela; o mesmo valia para desvincular
//     e para os agendamentos.
//   - DNS rebinding: um domínio que passa a resolver para 127.0.0.1 vira
//     "mesma origem" do painel e consegue LER as respostas (contatos, status).
//
// Regras: o Host tem de ser o próprio localhost; e pedido que muda algo
// (não GET/HEAD) vindo de navegador só passa se a origem for o painel. Quem
// não é navegador (servidor MCP, curl, verificador) não manda Origin nem
// Sec-Fetch-Site e segue como sempre.

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func hostLocal(h string) bool {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func pedidoPermitido(r *http.Request) bool {
	if !hostLocal(r.Host) {
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !hostLocal(u.Host) {
			return false
		}
	}
	return true
}

func guardaLocal(prox http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pedidoPermitido(r) {
			http.Error(w, "pedido recusado: só o painel local e o servidor MCP desta máquina", http.StatusForbidden)
			return
		}
		prox.ServeHTTP(w, r)
	})
}
