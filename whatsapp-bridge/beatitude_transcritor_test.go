package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Stub que fala o protocolo do residente: {"pronto":true} e depois uma
// resposta por linha, ecoando o nome do arquivo como "texto".
const stubTranscritor = `import sys, json, time
print(json.dumps({"pronto": True}), flush=True)
for linha in sys.stdin:
    req = json.loads(linha)
    if req["arquivo"].endswith("lento"):
        time.sleep(5)
    if req["arquivo"].endswith("ruim"):
        print(json.dumps({"erro": "arquivo corrompido"}), flush=True); continue
    print(json.dumps({"texto": "ouvi " + req["arquivo"] + " / " + req.get("dica", "")}), flush=True)
`

func TestTranscritorResidente(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sem python3")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub.py")
	if err := os.WriteFile(stub, []byte(stubTranscritor), 0o600); err != nil {
		t.Fatal(err)
	}
	antigoCmd := beatitudeTranscritorComando
	beatitudeTranscritorComando = func(uv, script, modelos string) *exec.Cmd { return exec.Command("python3", stub) }
	defer func() {
		beatitudeTranscritorComando = antigoCmd
		beatitudeTranscritor.mu.Lock()
		beatitudeTranscritorMatar()
		beatitudeTranscritor.mu.Unlock()
	}()
	// beatitudeAcharUV precisa achar algo: aponta para um arquivo qualquer
	home, _ := os.UserHomeDir()
	fakeUV := filepath.Join(home, ".local", "bin", "uv")
	if _, err := os.Stat(fakeUV); err != nil {
		_ = os.MkdirAll(filepath.Dir(fakeUV), 0o700)
		if err := os.WriteFile(fakeUV, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Skip("não consegui criar o uv falso")
		}
		defer os.Remove(fakeUV)
	}
	cwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(cwd)

	inicio := time.Now()
	txt, err := beatitudeTranscritorPedir("/tmp/a.ogg", "Contatos: Lisiane")
	if err != nil || !strings.HasPrefix(txt, "ouvi /tmp/a.ogg") || !strings.Contains(txt, "Lisiane") {
		t.Fatalf("primeiro pedido: %q, %v", txt, err)
	}
	if time.Since(inicio) > 10*time.Second {
		t.Errorf("subida lenta demais: %s", time.Since(inicio))
	}
	// segundo pedido reusa o processo
	est := beatitudeTranscritorEstado()
	if !est["pronto"].(bool) {
		t.Fatal("devia estar pronto")
	}
	if txt, err = beatitudeTranscritorPedir("/tmp/b.ogg", ""); err != nil || txt != "ouvi /tmp/b.ogg /" {
		t.Fatalf("segundo pedido: %q, %v", txt, err)
	}
	if beatitudeTranscritorEstado()["pedidos"].(int) != 2 {
		t.Error("contador de pedidos")
	}
	// erro do modelo volta como erro, processo segue vivo
	if _, err = beatitudeTranscritorPedir("/tmp/ruim", ""); err == nil || !strings.Contains(err.Error(), "corrompido") {
		t.Fatalf("erro esperado: %v", err)
	}
	if !beatitudeTranscritorEstado()["pronto"].(bool) {
		t.Fatal("erro de transcrição não devia derrubar o processo")
	}
}

func TestLerLinhaTimeout(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	br := bufioReader(r)
	inicio := time.Now()
	if _, err := beatitudeLerLinha(br, 300*time.Millisecond); err == nil {
		t.Fatal("devia estourar")
	}
	if time.Since(inicio) > 2*time.Second {
		t.Error("demorou demais para estourar")
	}
}

func bufioReader(f *os.File) *bufio.Reader { return bufio.NewReader(f) }
