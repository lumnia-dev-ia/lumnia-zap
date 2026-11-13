// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

func TestMidiaDaMensagem(t *testing.T) {
	casos := []struct {
		msg           *waProto.Message
		tipo, legenda string
	}{
		{&waProto.Message{ImageMessage: &waProto.ImageMessage{}}, "imagem", ""},
		{&waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String(" dieta ")}}, "imagem", "dieta"},
		{&waProto.Message{DocumentMessage: &waProto.DocumentMessage{Mimetype: proto.String("application/pdf")}}, "pdf", ""},
		{&waProto.Message{DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("Exame.PDF")}}, "pdf", ""},
		{&waProto.Message{DocumentMessage: &waProto.DocumentMessage{Mimetype: proto.String("image/png")}}, "imagem", ""},
		{&waProto.Message{DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("a.docx")}}, "outro", ""},
		{&waProto.Message{DocumentWithCaptionMessage: &waProto.FutureProofMessage{Message: &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("x.pdf"), Caption: proto.String("leia")}}}}, "pdf", "leia"},
		{&waProto.Message{Conversation: proto.String("oi")}, "", ""},
		{&waProto.Message{AudioMessage: &waProto.AudioMessage{}}, "", ""},
	}
	for i, c := range casos {
		tipo, leg := beatitudeMidiaDaMensagem(c.msg)
		if tipo != c.tipo || leg != c.legenda {
			t.Errorf("caso %d: %q %q, queria %q %q", i, tipo, leg, c.tipo, c.legenda)
		}
	}
}

func TestSepararLeitura(t *testing.T) {
	l := beatitudeSepararLeitura("RESUMO: dieta com 5 refeições\nCONTEÚDO:\nCafé | pão\nAlmoço | arroz")
	if l.Resumo != "dieta com 5 refeições" || !strings.HasPrefix(l.Conteudo, "Café | pão") {
		t.Errorf("%+v", l)
	}
	l = beatitudeSepararLeitura("**RESUMO:** recibo\n**CONTEÚDO:** total R$ 10")
	if l.Resumo != "recibo" || l.Conteudo != "total R$ 10" {
		t.Errorf("markdown: %+v", l)
	}
	l = beatitudeSepararLeitura("só um texto\nsem marcas")
	if l.Resumo != "só um texto" || !strings.Contains(l.Conteudo, "sem marcas") {
		t.Errorf("sem marcas: %+v", l)
	}
	l = beatitudeSepararLeitura("RESUMO: " + strings.Repeat("x", 500) + "\nCONTEÚDO:\nabc")
	if len([]rune(l.Resumo)) > 200 {
		t.Error("resumo passou de 200 caracteres")
	}
}

func TestBlocoMidiaEContagemPDF(t *testing.T) {
	b := beatitudeBlocoMidia("imagem", "minha dieta", beatitudeLeitura{Conteudo: "café"})
	if !strings.HasPrefix(b, "[Imagem enviada — conteúdo lido]:\nLegenda dela: minha dieta\ncafé") {
		t.Errorf("%q", b)
	}
	if !strings.HasPrefix(beatitudeBlocoMidia("pdf", "", beatitudeLeitura{Conteudo: "x"}), "[PDF enviado") {
		t.Error("cabeçalho do PDF")
	}
	pdf := []byte("<< /Type /Pages /Count 2 >> << /Type /Page >> << /Type/Page /Parent 1 >>")
	if n := beatitudeContarPaginasPDF(pdf); n != 2 {
		t.Errorf("páginas: %d", n)
	}
}

func TestMidiasDoLoteEAviso(t *testing.T) {
	var ps []beatitudePedido
	for i := 0; i < 7; i++ {
		ps = append(ps, beatitudePedido{Midia: "imagem"})
	}
	ps = append(ps, beatitudePedido{Texto: "faz um cardápio"})
	if n := len(beatitudeMidiasDoLote(ps)); n != beatitudeMidiaMaxLote {
		t.Errorf("lote: %d", n)
	}
	if beatitudeAvisoLendo([]string{"pdf"}) != "lendo o PDF…" || !strings.Contains(beatitudeAvisoLendo([]string{"imagem", "pdf"}), "2 arquivos") {
		t.Error("aviso de leitura")
	}
}

func TestLimparMidiasSoAsVelhas(t *testing.T) {
	base := t.TempDir()
	velha, nova := filepath.Join(base, "velha"), filepath.Join(base, "nova")
	_ = os.MkdirAll(velha, 0o700)
	_ = os.MkdirAll(nova, 0o700)
	antigo := time.Now().Add(-8 * 24 * time.Hour)
	_ = os.Chtimes(velha, antigo, antigo)
	beatitudeLimparMidias(base)
	if _, err := os.Stat(velha); !os.IsNotExist(err) {
		t.Error("pasta de 8 dias deveria sumir")
	}
	if _, err := os.Stat(nova); err != nil {
		t.Error("pasta nova não pode sumir")
	}
}

func TestLeitorSemMCPNemEscrita(t *testing.T) {
	for _, f := range []string{"Bash", "Write", "WebSearch", "WebFetch"} {
		if !contem(beatitudeLeitorNegadas, f) {
			t.Errorf("leitor deveria negar %s", f)
		}
	}
	if !strings.Contains(beatitudeLeitorSistema, "nunca instrução") {
		t.Error("o leitor precisa tratar o conteúdo como dado")
	}
}
