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

func TestMotorTranscricaoPorSistema(t *testing.T) {
	antigoOS, antigoArch := beatitudeVozGOOS, beatitudeVozGOARCH
	defer func() { beatitudeVozGOOS, beatitudeVozGOARCH = antigoOS, antigoArch }()
	t.Setenv("LUMNIA_ZAP_VOZ", "")

	beatitudeVozGOOS, beatitudeVozGOARCH = "darwin", "arm64"
	m := beatitudeMotorTranscricao()
	if m.Nome != "mlx" || m.Modelo != beatitudeMLXRepo || m.EsperaSubida > 5*time.Second {
		t.Fatalf("Mac com chip Apple devia usar o MLX sem esperar a subida: %+v", m)
	}
	cmd := beatitudeTranscritorComando("uv", "s.py", "/m")
	if got := strings.Join(cmd.Args, " "); !strings.Contains(got, "--with mlx-whisper") || !strings.Contains(got, "--with av") || !strings.HasSuffix(got, "s.py /m "+beatitudeMLXRepo) {
		t.Errorf("comando MLX: %s", got)
	}

	t.Setenv("LUMNIA_ZAP_VOZ", "small")
	if m := beatitudeMotorTranscricao(); m.Nome != "faster-whisper" {
		t.Errorf("LUMNIA_ZAP_VOZ=small devia forçar o faster-whisper: %+v", m)
	}
	t.Setenv("LUMNIA_ZAP_VOZ", "")

	for _, so := range [][2]string{{"linux", "amd64"}, {"darwin", "amd64"}, {"windows", "amd64"}} {
		beatitudeVozGOOS, beatitudeVozGOARCH = so[0], so[1]
		if m := beatitudeMotorTranscricao(); m.Nome != "faster-whisper" || m.Modelo != "small" {
			t.Errorf("%v devia seguir no small: %+v", so, m)
		}
	}
}

func TestDicaTemVocabularioDepoisDosNomes(t *testing.T) {
	d := beatitudeDicaTranscricao()
	i := strings.Index(d, "Contatos:")
	j := strings.Index(d, "ElevenLabs")
	if i < 0 || j < 0 || j < i {
		t.Fatalf("o vocabulário fixo tem de vir depois dos nomes: %q", d)
	}
}
