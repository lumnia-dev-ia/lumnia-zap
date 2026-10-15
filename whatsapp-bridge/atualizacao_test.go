// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAtualizacaoComparar(t *testing.T) {
	casos := []struct {
		a, b string
		quer int
	}{
		{"1.0.6", "1.0.5", 1}, {"1.0.10", "1.0.9", 1}, {"1.0.6", "1.0.6", 0}, {"v1.1.0", "1.0.99", 1}, {"1.0.5", "1.0.6", -1}, {"2", "1.9.9", 1},
	}
	for _, c := range casos {
		got := atualizacaoComparar(c.a, c.b)
		if (got > 0) != (c.quer > 0) || (got == 0) != (c.quer == 0) {
			t.Errorf("comparar(%q,%q) = %d, queria sinal de %d", c.a, c.b, got, c.quer)
		}
	}
}

func TestAtualizacaoUltimaVersao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("esperava HEAD, veio %s", r.Method)
		}
		w.Header().Set("Location", "https://github.com/lumnia-dev-ia/lumnia-zap/releases/tag/v1.0.7")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	old := atualizacaoLatestURL
	atualizacaoLatestURL = srv.URL + "/releases/latest"
	defer func() { atualizacaoLatestURL = old }()
	v, err := atualizacaoUltimaVersao()
	if err != nil || v != "1.0.7" {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

// pacoteFalso monta um lumnia-zap-*.tar.gz como o release.sh gera.
func pacoteFalso(t *testing.T, versao string, comPonte bool) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(nome string, conteudo []byte, modo int64) {
		_ = tw.WriteHeader(&tar.Header{Name: nome, Mode: modo, Size: int64(len(conteudo)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(conteudo)
	}
	if comPonte {
		add("lumnia-zap/whatsapp-bridge/whatsapp-bridge", bytes.Repeat([]byte("B"), 2<<20), 0o755)
	}
	add("lumnia-zap/whatsapp-bridge/._whatsapp-bridge", []byte("lixo do macOS"), 0o644)
	add("lumnia-zap/whatsapp-mcp-server/main.py", []byte("print('novo')\n"), 0o644)
	add("lumnia-zap/whatsapp-mcp-server/agenda.py", []byte("# agenda\n"), 0o644)
	add("lumnia-zap/VERSION", []byte(versao+"\n"), 0o644)
	add("lumnia-zap/../fora.txt", []byte("x"), 0o644) // filepath.Clean tira o "..": vira "fora.txt" na raiz, permitido
	_ = tw.Close()
	_ = gz.Close()
	f := filepath.Join(t.TempDir(), "pacote.tar.gz")
	if err := os.WriteFile(f, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAtualizacaoExtrairETrocar(t *testing.T) {
	destino := t.TempDir()
	// instalacao existente
	_ = os.MkdirAll(filepath.Join(destino, "whatsapp-bridge", "store"), 0o755)
	_ = os.MkdirAll(filepath.Join(destino, "whatsapp-mcp-server"), 0o755)
	_ = os.WriteFile(filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge"), []byte("velho"), 0o755)
	_ = os.WriteFile(filepath.Join(destino, "whatsapp-bridge", "store", "apple.json"), []byte("{}"), 0o600)
	_ = os.WriteFile(filepath.Join(destino, "whatsapp-mcp-server", "main.py"), []byte("print('velho')\n"), 0o644)
	_ = os.WriteFile(filepath.Join(destino, "VERSION"), []byte("1.0.6\n"), 0o644)

	// pacote sem a ponte: nada muda
	tmp := filepath.Join(destino, "atualizacao-x")
	_ = os.MkdirAll(tmp, 0o755)
	if err := atualizacaoExtrair(pacoteFalso(t, "1.0.7", false), tmp); err != nil {
		t.Fatal(err)
	}
	if err := atualizacaoTrocar(tmp, destino, "1.0.7"); err == nil {
		t.Fatal("pacote sem a ponte devia ser recusado")
	}
	if b, _ := os.ReadFile(filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge")); string(b) != "velho" {
		t.Fatal("a ponte foi trocada por um pacote invalido")
	}
	_ = os.RemoveAll(tmp)

	// versao diferente da pedida: recusa
	tmp = filepath.Join(destino, "atualizacao-y")
	_ = os.MkdirAll(tmp, 0o755)
	if err := atualizacaoExtrair(pacoteFalso(t, "1.0.8", true), tmp); err != nil {
		t.Fatal(err)
	}
	if err := atualizacaoTrocar(tmp, destino, "1.0.7"); err == nil {
		t.Fatal("versao errada devia ser recusada")
	}
	_ = os.RemoveAll(tmp)

	// pacote bom
	tmp = filepath.Join(destino, "atualizacao-z")
	_ = os.MkdirAll(tmp, 0o755)
	if err := atualizacaoExtrair(pacoteFalso(t, "1.0.7", true), tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "whatsapp-bridge", "._whatsapp-bridge")); !os.IsNotExist(err) {
		t.Error("._ devia ser ignorado")
	}
	if err := atualizacaoTrocar(tmp, destino, "1.0.7"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge"))
	if err != nil || st.Size() != 2<<20 || st.Mode().Perm() != 0o755 {
		t.Errorf("ponte nova: %v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge.anterior")); string(b) != "velho" {
		t.Error("o binario anterior devia ficar guardado")
	}
	if b, _ := os.ReadFile(filepath.Join(destino, "whatsapp-mcp-server", "main.py")); string(b) != "print('novo')\n" {
		t.Error("main.py nao foi trocado")
	}
	if _, err := os.Stat(filepath.Join(destino, "whatsapp-mcp-server", "agenda.py")); err != nil {
		t.Error("agenda.py novo nao foi criado")
	}
	if b, _ := os.ReadFile(filepath.Join(destino, "VERSION")); string(b) != "1.0.7\n" {
		t.Errorf("VERSION: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(destino, "whatsapp-bridge", "store", "apple.json")); string(b) != "{}" {
		t.Error("o store/ nao pode ser tocado")
	}
	if restos, _ := filepath.Glob(filepath.Join(destino, "*", "*.novo")); len(restos) != 0 {
		t.Errorf("sobrou .novo: %v", restos)
	}
}
