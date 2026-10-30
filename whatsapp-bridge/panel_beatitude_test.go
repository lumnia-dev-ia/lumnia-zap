package main

import (
	"bytes"
	"testing"
)

// O painel nasce com o cartão do Beatitude escondido e com a marca que o
// Windows troca; sem a marca, o Windows passaria a mostrar o link do módulo.
func TestPanelHTML_BeatitudeEscondidoEMarcado(t *testing.T) {
	if n := bytes.Count(panelHTML, []byte(`<body data-beatitude="1">`)); n != 1 {
		t.Fatalf(`esperava um <body data-beatitude="1">, achei %d`, n)
	}
	if !bytes.Contains(panelHTML, []byte(`id="beatitude" style="display:none"`)) {
		t.Fatal("o cartão do Beatitude precisa nascer escondido")
	}
	if !bytes.Contains(panelHTML, []byte(`href="/beatitude"`)) {
		t.Fatal("falta o link discreto para /beatitude")
	}
}

func TestPanelHTMLParaPlataforma(t *testing.T) {
	com := panelHTMLParaPlataforma(true)
	if !bytes.Contains(com, []byte(`data-beatitude="1"`)) {
		t.Fatal("Mac/Linux: marca deveria ficar em 1")
	}
	sem := panelHTMLParaPlataforma(false)
	if !bytes.Contains(sem, []byte(`<body data-beatitude="0">`)) || bytes.Contains(sem, []byte(`data-beatitude="1"`)) {
		t.Fatal("Windows: marca deveria virar 0")
	}
	if !bytes.Equal(panelHTML, com) {
		t.Fatal("a versão com Beatitude não pode alterar o HTML embutido")
	}
}
