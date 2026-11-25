// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_regras.go — as regras do mordomo sem nova versão do Zap (1.1.3).
//
// Até a 1.1.2, mudar uma frase do prompt do mordomo exigia compilar, assinar
// e notarizar o Zap para Mac e Windows. Agora o texto das regras pode vir de
// um pacote publicado no repositório (regras/mordomo.json), assinado com a
// chave da Lumnia (Ed25519). A ponte busca a cada 30 min, confere a
// assinatura com a chave pública compilada aqui e só então usa.
//
// O que o pacote PODE mudar: o texto das regras (por chave), acrescentar
// regras novas e o texto da regra de cada conector.
// O que ele NÃO alcança, de propósito: as ferramentas liberadas e negadas, a
// trava do "sim", os tetos, quem envia o quê. Isso fica no binário; um pacote
// adulterado ou mal escrito muda o que o mordomo diz, nunca o que ele pode
// fazer.
//
// Sem pacote, com pacote inválido, mais velho que o guardado, ou que pede uma
// ponte mais nova: valem os textos compilados. Nada quebra.
//
// Para publicar: edite regras/mordomo.fonte.json, aumente "versao", rode
//   bash scripts/assinar-regras.sh
// e faça o commit dos dois arquivos na main. Ver regras/LEIA-ME.md.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Chave pública da Lumnia para as regras. A privada fica fora do repositório.
const beatitudeRegrasChavePublica = "jLsYXoQ3EkISBnWbLXfSAQavlcz5/FKM9jwpCEBDgd4="

const (
	beatitudeRegrasURL      = "https://raw.githubusercontent.com/lumnia-dev-ia/lumnia-zap/main/regras/mordomo.json"
	beatitudeRegrasCache    = "store/regras-mordomo.json"
	beatitudeRegrasCada     = 30 * time.Minute
	beatitudeRegrasMaxBytes = 256 << 10
)

// beatitudeRegra: uma linha do prompt do mordomo com nome estável, para o
// pacote poder trocá-la sem depender da posição.
type beatitudeRegra struct {
	Chave string
	Texto string
}

type beatitudePacoteRegras struct {
	Versao      int               `json:"versao"`
	PonteMinima string            `json:"ponte_minima,omitempty"`
	Nota        string            `json:"nota,omitempty"`
	Regras      map[string]string `json:"regras,omitempty"`     // substitui pela chave; "" tira a regra
	Extras      []string          `json:"extras,omitempty"`     // acrescenta antes da hora atual
	Conectores  map[string]string `json:"conectores,omitempty"` // texto da regra do conector, pelo ID
}

// arquivo publicado: o pacote em base64 (os bytes exatos que foram assinados)
// e a assinatura. Assim nenhum reformatador de JSON quebra a conferência.
type beatitudeRegrasArquivo struct {
	Pacote     string `json:"pacote"`
	Assinatura string `json:"assinatura"`
}

var beatitudeRegrasAtual struct {
	mu     sync.RWMutex
	pacote *beatitudePacoteRegras
	erro   string
	visto  time.Time
}

// beatitudeAbrirRegras confere a assinatura e devolve o pacote.
func beatitudeAbrirRegras(dados []byte, chave string) (*beatitudePacoteRegras, error) {
	var arq beatitudeRegrasArquivo
	if err := json.Unmarshal(dados, &arq); err != nil {
		return nil, fmt.Errorf("arquivo ilegível: %v", err)
	}
	pub, err := base64.StdEncoding.DecodeString(chave)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("chave pública inválida")
	}
	corpo, err := base64.StdEncoding.DecodeString(arq.Pacote)
	if err != nil {
		return nil, fmt.Errorf("pacote ilegível")
	}
	sig, err := base64.StdEncoding.DecodeString(arq.Assinatura)
	if err != nil || !ed25519.Verify(pub, corpo, sig) {
		return nil, fmt.Errorf("assinatura não confere")
	}
	var p beatitudePacoteRegras
	if err := json.Unmarshal(corpo, &p); err != nil {
		return nil, fmt.Errorf("pacote ilegível: %v", err)
	}
	if p.Versao <= 0 {
		return nil, fmt.Errorf("pacote sem versão")
	}
	return &p, nil
}

// beatitudeRegrasServe: o pacote vale para esta ponte? Versão de nuvem
// ("nuvem-abc1234") e de desenvolvimento ("") aceitam qualquer ponte_minima.
func beatitudeRegrasServe(p *beatitudePacoteRegras, ponte string) bool {
	if p.PonteMinima == "" || !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(ponte) {
		return true
	}
	return atualizacaoComparar(ponte, p.PonteMinima) >= 0
}

// beatitudeRegrasAceitar troca o pacote em uso se o novo for válido, servir
// para esta ponte e não for mais velho que o atual (sem volta para trás).
func beatitudeRegrasAceitar(dados []byte, gravar bool) error {
	p, err := beatitudeAbrirRegras(dados, beatitudeRegrasChavePublica)
	if err != nil {
		return err
	}
	if !beatitudeRegrasServe(p, versao) {
		return fmt.Errorf("pacote %d pede a ponte %s", p.Versao, p.PonteMinima)
	}
	beatitudeRegrasAtual.mu.Lock()
	defer beatitudeRegrasAtual.mu.Unlock()
	if atual := beatitudeRegrasAtual.pacote; atual != nil && p.Versao < atual.Versao {
		return fmt.Errorf("pacote %d é mais velho que o %d em uso", p.Versao, atual.Versao)
	}
	beatitudeRegrasAtual.pacote = p
	beatitudeRegrasAtual.erro = ""
	if gravar {
		_ = os.WriteFile(beatitudeRegrasCache, dados, 0o600)
	}
	return nil
}

func beatitudeRegrasPacote() *beatitudePacoteRegras {
	beatitudeRegrasAtual.mu.RLock()
	defer beatitudeRegrasAtual.mu.RUnlock()
	return beatitudeRegrasAtual.pacote
}

// beatitudeRegrasBuscar baixa o arquivo publicado. Variável para os testes.
var beatitudeRegrasBuscar = func() ([]byte, error) {
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Get(beatitudeRegrasURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // ainda não publicado: vale o compilado
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, beatitudeRegrasMaxBytes))
}

func beatitudeRegrasAtualizar() {
	dados, err := beatitudeRegrasBuscar()
	beatitudeRegrasAtual.mu.Lock()
	beatitudeRegrasAtual.visto = time.Now()
	beatitudeRegrasAtual.mu.Unlock()
	if err == nil && dados != nil {
		err = beatitudeRegrasAceitar(dados, true)
	}
	if err != nil {
		beatitudeRegrasAtual.mu.Lock()
		beatitudeRegrasAtual.erro = err.Error()
		beatitudeRegrasAtual.mu.Unlock()
		if panel.logger != nil {
			panel.logger.Warnf("Beatitude: regras do mordomo: %v", err)
		}
	}
}

// initBeatitudeRegras: carrega o guardado e passa a buscar o publicado.
func initBeatitudeRegras() {
	if dados, err := os.ReadFile(beatitudeRegrasCache); err == nil {
		_ = beatitudeRegrasAceitar(dados, false)
	}
	go func() {
		time.Sleep(15 * time.Second) // deixa a configuração do módulo carregar
		for {
			beatitude.mu.Lock()
			ligado := beatitude.cfg.Ligado
			beatitude.mu.Unlock()
			if ligado { // quem usa o Zap sem o Beatitude não busca nada
				beatitudeRegrasAtualizar()
			}
			time.Sleep(beatitudeRegrasCada)
		}
	}()
}

// beatitudeRegrasResumo: para o painel e o sinal de vida.
func beatitudeRegrasResumo() map[string]interface{} {
	beatitudeRegrasAtual.mu.RLock()
	defer beatitudeRegrasAtual.mu.RUnlock()
	r := map[string]interface{}{"versao": 0, "erro": beatitudeRegrasAtual.erro}
	if p := beatitudeRegrasAtual.pacote; p != nil {
		r["versao"], r["nota"] = p.Versao, p.Nota
	}
	if !beatitudeRegrasAtual.visto.IsZero() {
		r["conferido_em"] = beatitudeRegrasAtual.visto
	}
	return r
}

// beatitudeAplicarRegras troca, tira e acrescenta as regras conforme o
// pacote e devolve as linhas do prompt. {maquina} e {grupo} no texto do
// pacote viram o nome da máquina e o JID do grupo.
func beatitudeAplicarRegras(regras []beatitudeRegra, p *beatitudePacoteRegras, grupo string) []string {
	troca := strings.NewReplacer("{maquina}", maquina(), "{grupo}", grupo)
	out := make([]string, 0, len(regras)+4)
	for _, r := range regras {
		if p != nil && r.Chave == "agora" {
			for _, e := range p.Extras {
				if strings.TrimSpace(e) != "" {
					out = append(out, troca.Replace(e))
				}
			}
		}
		if p != nil && r.Chave != "" && r.Chave != "agora" {
			if novo, ok := p.Regras[r.Chave]; ok {
				if strings.TrimSpace(novo) == "" {
					continue
				}
				out = append(out, troca.Replace(novo))
				continue
			}
		}
		out = append(out, r.Texto)
	}
	return out
}

// beatitudeRegraDoConector: o texto do pacote, se houver, senão o compilado.
func beatitudeRegraDoConector(c beatitudeConector) string {
	if p := beatitudeRegrasPacote(); p != nil {
		if t, ok := p.Conectores[c.ID]; ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return c.Regra
}
