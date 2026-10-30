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

func TestPanelHTMLParaPlataforma_Mac(t *testing.T) {
	if !bytes.Equal(panelHTMLParaPlataforma("darwin"), panelHTML) {
		t.Fatal("no Mac o painel é o HTML embutido, sem trocas")
	}
}

func TestPanelHTMLParaPlataforma_Windows(t *testing.T) {
	w := panelHTMLParaPlataforma("windows")
	if !bytes.Contains(w, []byte(`<body data-beatitude="0">`)) || bytes.Contains(w, []byte(`data-beatitude="1"`)) {
		t.Fatal("Windows: marca do Beatitude deveria virar 0")
	}
	for _, mac := range []string{"launchctl", "Library/Application", "o Mac ligado", "Se o Mac estiver", "neste Mac\""} {
		if bytes.Contains(w, []byte(mac)) {
			t.Fatalf("Windows: sobrou texto de Mac: %q", mac)
		}
	}
	for _, novo := range []string{"Menu Iniciar", "o computador ligado", "Se o computador estiver"} {
		if !bytes.Contains(w, []byte(novo)) {
			t.Fatalf("Windows: faltou o texto %q", novo)
		}
	}
	if bytes.Equal(panelHTML, w) {
		t.Fatal("a troca não pode alterar o HTML embutido")
	}
}

func TestPanelHTMLParaPlataforma_Linux(t *testing.T) {
	l := panelHTMLParaPlataforma("linux")
	if !bytes.Contains(l, []byte(`data-beatitude="1"`)) {
		t.Fatal("Linux: o Beatitude existe, a marca fica em 1")
	}
	if bytes.Contains(l, []byte("o Mac ligado")) {
		t.Fatal("Linux: texto de Mac no agendamento")
	}
}
