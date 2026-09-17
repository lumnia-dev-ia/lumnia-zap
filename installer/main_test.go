package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

// Gera as telas reais para inspecao visual, usando as funcoes de producao.
func TestPaginas(t *testing.T) {
	dir := "/tmp/telas"
	os.MkdirAll(dir, 0o755)
	paginaAberta = true // nao tenta abrir navegador

	paginaPath = dir + "/1-inicio.html"
	passoAtual = 0
	escreverPagina(cabecalhoPagina(1), corpoPassos(), "")

	paginaPath = dir + "/2-meio.html"
	etapa(3)

	paginaPath = dir + "/3-fim.html"
	progressoConcluir(resumoHTML(
		[]string{
			"/Users/exemplo/Library/Application Support/Claude",
			"/Users/exemplo/Library/Application Support/Claude Desktop",
		},
		true, "", "/Users/exemplo/Desktop/Lumnia-Zap-instalacao.txt"))

	paginaPath = dir + "/4-erro.html"
	passoAtual = 6
	progressoFalhar("o serviço não respondeu a tempo.\n\nEstado do serviço:\n  state = spawn scheduled\n  last exit reason = OS_REASON_CODESIGNING\n  runs = 2380\n\nO sistema está matando a ponte por assinatura de código.")

	// a tela de erro nao pode continuar recarregando sozinha
	b, _ := os.ReadFile(dir + "/4-erro.html")
	if strings.Contains(string(b), "http-equiv=\"refresh\"") {
		t.Error("a tela de erro nao deveria ter meta refresh")
	}
}

// O rename tem de trocar o inode - e o ponto todo da correcao.
func TestEscreverArquivoTrocaInode(t *testing.T) {
	d := t.TempDir()
	alvo := d + "/bin"
	os.WriteFile(alvo, []byte("velho"), 0o755)
	i1 := inode(t, alvo)
	if err := escreverArquivo(alvo, strings.NewReader("novo"), 0o755); err != nil {
		t.Fatal(err)
	}
	i2 := inode(t, alvo)
	if i1 == i2 {
		t.Fatalf("inode nao mudou (%d): o arquivo foi sobrescrito no lugar", i1)
	}
	b, _ := os.ReadFile(alvo)
	if string(b) != "novo" {
		t.Fatalf("conteudo errado: %q", b)
	}
	st, _ := os.Stat(alvo)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("permissao errada: %v", st.Mode().Perm())
	}
	if _, err := os.Stat(alvo + ".novo"); err == nil {
		t.Error("sobrou arquivo temporario")
	}
}

// Arquivo somente-leitura no pacote nao pode travar a proxima instalacao.
func TestEscreverArquivoSobreSomenteLeitura(t *testing.T) {
	d := t.TempDir()
	alvo := d + "/LICENSE"
	os.WriteFile(alvo, []byte("velho"), 0o444)
	if err := escreverArquivo(alvo, strings.NewReader("novo"), 0o444); err != nil {
		t.Fatalf("falhou sobre arquivo 0444: %v", err)
	}
	if err := escreverArquivo(alvo, strings.NewReader("mais novo"), 0o444); err != nil {
		t.Fatalf("falhou na segunda passada: %v", err)
	}
}

// O log nao pode vazar conteudo de conversa para a tela de erro.
func TestUltimasLinhasNaoVazaMensagem(t *testing.T) {
	d := t.TempDir()
	f := d + "/bridge.log"
	os.WriteFile(f, []byte(
		"11:00:00 [Client INFO] Starting WhatsApp client...\n"+
			"[2026-08-05 15:09:41] ← 190494734872770: segredo do meu pai\n"+
			"████ QR ████\n"+
			"11:00:01 [Client ERROR] Falha ao conectar\n"), 0o644)
	got := ultimasLinhas(f, 10)
	for _, proibido := range []string{"segredo", "190494734872770", "█"} {
		if strings.Contains(got, proibido) {
			t.Errorf("vazou %q em:\n%s", proibido, got)
		}
	}
	if !strings.Contains(got, "Falha ao conectar") {
		t.Error("perdeu a linha de diagnostico")
	}
}

func inode(t *testing.T, caminho string) uint64 {
	t.Helper()
	st, err := os.Stat(caminho)
	if err != nil {
		t.Fatal(err)
	}
	return st.Sys().(*syscall.Stat_t).Ino
}
