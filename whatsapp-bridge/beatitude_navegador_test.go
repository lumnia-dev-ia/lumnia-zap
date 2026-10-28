// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestSepararPix(t *testing.T) {
	codigo := "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR5913Azul Linhas6009Sao Paulo62070503***6304ABCD"
	visivel, pix := beatitudeSepararPix("Compra montada: ida 18h05, volta 21h30, R$ 760,00. PIX vale até 19h52.\n\n<pix>\n" + codigo[:40] + "\n" + codigo[40:] + "\n</pix>")
	if pix != codigo {
		t.Errorf("codigo errado: %q", pix)
	}
	if visivel != "Compra montada: ida 18h05, volta 21h30, R$ 760,00. PIX vale até 19h52." {
		t.Errorf("visivel errado: %q", visivel)
	}
	if _, pix := beatitudeSepararPix("sem bloco"); pix != "" {
		t.Errorf("sem bloco devia dar vazio")
	}
	if v, pix := beatitudeSepararPix("x <pix>isto não é um pix</pix>"); pix != "" || v != "x" {
		t.Errorf("bloco invalido devia ser descartado: %q %q", v, pix)
	}
}
