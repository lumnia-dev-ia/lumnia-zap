package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func pacoteAssinado(t *testing.T, priv ed25519.PrivateKey, p beatitudePacoteRegras) []byte {
	t.Helper()
	corpo, _ := json.Marshal(p)
	b, _ := json.Marshal(beatitudeRegrasArquivo{
		Pacote:     base64.StdEncoding.EncodeToString(corpo),
		Assinatura: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, corpo)),
	})
	return b
}

func TestRegrasAssinaturaConfere(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	chave := base64.StdEncoding.EncodeToString(pub)
	dados := pacoteAssinado(t, priv, beatitudePacoteRegras{Versao: 2, Regras: map[string]string{"bastao": "novo"}})
	p, err := beatitudeAbrirRegras(dados, chave)
	if err != nil || p.Versao != 2 || p.Regras["bastao"] != "novo" {
		t.Fatalf("pacote válido: %v %+v", err, p)
	}

	// adulterado depois de assinado
	var arq beatitudeRegrasArquivo
	_ = json.Unmarshal(dados, &arq)
	corpo, _ := base64.StdEncoding.DecodeString(arq.Pacote)
	arq.Pacote = base64.StdEncoding.EncodeToString([]byte(strings.Replace(string(corpo), "novo", "mau", 1)))
	ruim, _ := json.Marshal(arq)
	if _, err := beatitudeAbrirRegras(ruim, chave); err == nil {
		t.Fatal("pacote adulterado tem de ser recusado")
	}

	// assinado por outra chave
	_, outra, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := beatitudeAbrirRegras(pacoteAssinado(t, outra, beatitudePacoteRegras{Versao: 3}), chave); err == nil {
		t.Fatal("outra chave tem de ser recusada")
	}
}

func TestRegrasAplicar(t *testing.T) {
	regras := []beatitudeRegra{
		{"tom", "tom antigo"},
		{"bastao", "bastão antigo"},
		{"viagem", "viagem antiga"},
		{"", "linha do conector"},
		{"agora", "Agora são 15:00"},
	}
	// sem pacote: tudo como compilado
	if got := strings.Join(beatitudeAplicarRegras(regras, nil, "g"), "|"); got != "tom antigo|bastão antigo|viagem antiga|linha do conector|Agora são 15:00" {
		t.Fatalf("sem pacote: %q", got)
	}
	p := &beatitudePacoteRegras{
		Versao: 1,
		Regras: map[string]string{"bastao": "bastão novo no {maquina} ({grupo})", "viagem": "", "agora": "não troca a hora"},
		Extras: []string{"regra extra"},
	}
	got := beatitudeAplicarRegras(regras, p, "1203@g.us")
	j := strings.Join(got, "|")
	if strings.Contains(j, "viagem") || strings.Contains(j, "bastão antigo") {
		t.Fatalf("troca e retirada: %q", j)
	}
	if !strings.Contains(j, "bastão novo no "+maquina()+" (1203@g.us)") {
		t.Fatalf("placeholders: %q", j)
	}
	if got[len(got)-2] != "regra extra" || got[len(got)-1] != "Agora são 15:00" {
		t.Fatalf("extra antes da hora, e a hora não se troca: %q", j)
	}
}

func TestRegrasConectorUsaTextoDoPacote(t *testing.T) {
	antigo := beatitudeRegrasAtual.pacote
	defer func() { beatitudeRegrasAtual.pacote = antigo }()
	c := beatitudeConector{ID: "lovable", Regra: "compilada"}
	beatitudeRegrasAtual.pacote = nil
	if beatitudeRegraDoConector(c) != "compilada" {
		t.Fatal("sem pacote vale a compilada")
	}
	beatitudeRegrasAtual.pacote = &beatitudePacoteRegras{Versao: 1, Conectores: map[string]string{"lovable": "do pacote"}}
	if beatitudeRegraDoConector(c) != "do pacote" {
		t.Fatal("o pacote troca o texto do conector")
	}
}

func TestRegrasServeParaEstaPonte(t *testing.T) {
	p := &beatitudePacoteRegras{Versao: 1, PonteMinima: "1.1.3"}
	casos := map[string]bool{"1.1.3": true, "1.2.0": true, "1.1.2": false, "nuvem-4f0152f": true, "": true}
	for v, quer := range casos {
		if beatitudeRegrasServe(p, v) != quer {
			t.Errorf("ponte %q: queria %v", v, quer)
		}
	}
}

func TestRegrasNaoVoltaParaPacoteMaisVelho(t *testing.T) {
	// o pacote publicado de verdade, assinado com a chave da Lumnia
	dados, err := os.ReadFile("../regras/mordomo.json")
	if err != nil {
		t.Fatal(err)
	}
	antigo := beatitudeRegrasAtual.pacote
	defer func() { beatitudeRegrasAtual.pacote = antigo }()
	beatitudeRegrasAtual.pacote = nil
	if err := beatitudeRegrasAceitar(dados, false); err != nil {
		t.Fatalf("o regras/mordomo.json do repositório tem de conferir com a chave compilada: %v", err)
	}
	pub := beatitudeRegrasAtual.pacote.Versao
	beatitudeRegrasAtual.pacote = &beatitudePacoteRegras{Versao: pub + 5}
	if err := beatitudeRegrasAceitar(dados, false); err == nil {
		t.Fatal("pacote mais velho que o em uso tem de ser recusado")
	}
}

// As chaves das regras são o contrato com o pacote: renomear uma quebra o
// pacote publicado sem aviso. Este teste fixa a lista.
func TestRegrasChavesEstaveis(t *testing.T) {
	fonte, err := os.ReadFile("beatitude.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"identidade", "tom", "formatacao", "so_le", "rascunhos", "horario", "anexo", "voz",
		"sem_instrucoes", "responder", "terceiros", "fazer_direto", "o_que_alcanco", "aprender", "caderno",
		"chrome_nunca", "navegador", "navegador_leitura", "viagem", "restaurante", "audio", "economia", "perfil",
		"ignorar_grupo", "pendentes", "temas", "bastao", "negada", "midia", "agora"} {
		if !strings.Contains(string(fonte), `{"`+k+`", `) {
			t.Errorf("regra %q sumiu do prompt do mordomo", k)
		}
	}
}
