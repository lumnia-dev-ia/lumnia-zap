package main

import (
	"strings"
	"testing"
	"time"
)

func TestBastaoSeparaAvisoETextoACopiar(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo }()
	initBeatitudeBastao()
	ponte.mu.Lock()
	token := ponte.cfg.Token
	ponte.cfg.Token = "" // outro teste pode ter deixado a ponte pareada
	ponte.mu.Unlock()
	defer func() { ponte.mu.Lock(); ponte.cfg.Token = token; ponte.mu.Unlock() }()

	resp := "Isso eu passo para o seu Claude.\n<bastao>{\"pedido\":\"Analise o projeto Timão e diga o que falta para o lançamento.\",\"porque\":\"os seus projetos do claude.ai\"}</bastao>"
	visivel, aviso, copiar, codigo := beatitudeSepararBastao(resp)
	if visivel != "Isso eu passo para o seu Claude." {
		t.Fatalf("visivel = %q", visivel)
	}
	if codigo != "B1" {
		t.Fatalf("codigo = %q", codigo)
	}
	if !strings.HasPrefix(aviso, beatitudeMarca) || !strings.Contains(aviso, "os seus projetos do claude.ai") {
		t.Fatalf("aviso = %q", aviso)
	}
	if strings.Contains(copiar, "◆") || !strings.HasPrefix(copiar, beatitudeBastaoPrefixo+"B1]") {
		t.Fatalf("texto a copiar sem marca e com o código no começo: %q", copiar)
	}
	if !strings.Contains(copiar, "criar_acao") || !strings.Contains(copiar, "Analise o projeto Timão") {
		t.Fatalf("texto a copiar incompleto: %q", copiar)
	}
	// a ponte ignora a própria mensagem a copiar
	if !beatitudeEhMensagemDoBastao(copiar) || falaEhConversa(copiar) {
		t.Fatal("a mensagem a copiar não pode virar pedido nem fala")
	}
	// sem ponte pareada, não promete a volta ao grupo
	if strings.Contains(copiar, "devolver_ao_grupo") {
		t.Fatal("sem ponte pareada o texto não deve pedir devolver_ao_grupo")
	}

	linhas := beatitudeListarBastoes(time.Now())
	if len(linhas) != 2 || !strings.Contains(linhas[1], "*B1*") {
		t.Fatalf("lista = %v", linhas)
	}
	if !beatitudeBastoesRe.MatchString("bastões") || !beatitudeBastoesRe.MatchString("Bastoes?") {
		t.Fatal("comando bastões não reconhecido")
	}
}

func TestBastaoComVoltaPedeDevolver(t *testing.T) {
	s := beatitudeTextoBastao("B7", "Resuma o projeto.", true)
	if !strings.Contains(s, "devolver_ao_grupo") || !strings.Contains(s, "\"B7\"") {
		t.Fatalf("texto = %q", s)
	}
	if got := beatitudeTextoDevolvido("B7", "Pronto."); !strings.HasPrefix(got, beatitudeMarca+" · do seu Claude (B7)") {
		t.Fatalf("devolvido = %q", got)
	}
}

func TestBastaoBlocoInvalidoNaoVaza(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo }()
	initBeatitudeBastao()
	visivel, aviso, copiar, _ := beatitudeSepararBastao("Ok.\n<bastao>{sem json}</bastao>")
	if visivel != "Ok." || aviso != "" || copiar != "" {
		t.Fatalf("bloco inválido: %q %q %q", visivel, aviso, copiar)
	}
}

func TestOQueAlcancoSaiDoCatalogo(t *testing.T) {
	s := beatitudeOQueAlcanco([]string{"timao"})
	if !strings.Contains(s, "Whisper") || !strings.Contains(s, "bastão") {
		t.Fatalf("alcance sem transcritor ou bastão: %q", s)
	}
	for _, c := range beatitudeConectoresAtivos([]string{"timao"}) {
		if !strings.Contains(s, c.Nome) {
			t.Fatalf("falta %s em %q", c.Nome, s)
		}
	}
	if strings.Contains(s, "Read AI") {
		t.Fatal("conector fora da máquina apareceu no alcance")
	}
	if !strings.Contains(beatitudeOQueAlcanco([]string{}), "nenhum conector extra") {
		t.Fatal("lista vazia deveria dizer nenhum conector")
	}
}
