// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// zelador.go — Claude e Chrome sempre abertos (1.1.2, 14-11). No Mac que
// serve de ponte, os dois apps são parte do serviço:
//   - o Claude (desktop) é o que deixa uma conversa do claude.ai, na nuvem,
//     ligar este computador e ler o WhatsApp, os arquivos e o Chrome — é
//     por onde volta o bastão (beatitude_bastao.go);
//   - o Chrome, com a extensão do Claude, é o navegador das tarefas
//     aprovadas no grupo (beatitude_navegador.go).
// Fechados, os dois falham em silêncio. O zelador confere a cada 5 minutos
// e reabre em segundo plano (open -gja: sem roubar o foco, escondido).
//
// Cuidados:
//   - só com o mordomo ligado: numa máquina que usa o Zap só como MCP, a
//     ponte não mexe nos apps da pessoa;
//   - o Claude só é reaberto depois de DUAS conferências seguidas fechado.
//     O instalador e o botão Reparar fecham o Claude para gravar a config
//     (installer/claude_aberto.go); reabrir no meio disso perderia a
//     entrada lumnia-zap. O Reparar ainda pausa o zelador por 30 min;
//   - depois de 3 tentativas sem sucesso, um aviso só ao grupo (e ao log),
//     até o app voltar.

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const (
	zeladorCada             = 5 * time.Minute
	zeladorFalhasParaAvisar = 3
)

type zeladorApp struct {
	Chave         string // claude, chrome
	Nome          string // nome do app para o open -a e para o texto
	Processo      string // nome exato do processo principal (pgrep -x)
	Caminho       string // onde o app costuma estar
	EsperaFechado int    // conferências seguidas fechado antes de reabrir
}

var zeladorApps = []zeladorApp{
	{Chave: "claude", Nome: "Claude", Processo: "Claude", Caminho: "/Applications/Claude.app", EsperaFechado: 2},
	{Chave: "chrome", Nome: "Google Chrome", Processo: "Google Chrome", Caminho: "/Applications/Google Chrome.app", EsperaFechado: 1},
}

var zelador struct {
	mu       sync.Mutex
	fechado  map[string]int // conferências seguidas fechado
	falhas   map[string]int // reaberturas que não pegaram
	avisado  map[string]bool
	pausaAte map[string]time.Time
}

// zeladorAberto e zeladorAbrir são variáveis para os testes trocarem.
var zeladorAberto = func(processo string) bool {
	return exec.Command("/usr/bin/pgrep", "-x", processo).Run() == nil
}

var zeladorAbrir = func(nome string) error {
	return exec.Command("/usr/bin/open", "-gja", nome).Run()
}

var zeladorInstalado = func(a zeladorApp) bool {
	if _, err := os.Stat(a.Caminho); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	_, err := os.Stat(home + a.Caminho) // ~/Applications
	return err == nil
}

// zeladorLigado: o mordomo está ligado nesta máquina.
var zeladorLigado = func() bool {
	beatitude.mu.Lock()
	defer beatitude.mu.Unlock()
	return beatitude.cfg.Ligado && beatitude.cfg.GrupoJID != ""
}

var zeladorAvisar = func(texto string) {
	if panel.logger != nil {
		panel.logger.Warnf("Zelador: %s", texto)
	}
	beatitudeSend(beatitudeMarca + ": " + texto)
}

func iniciarZelador() {
	if runtime.GOOS != "darwin" {
		return
	}
	go func() {
		time.Sleep(time.Minute) // deixa a ponte subir e conectar primeiro
		t := time.NewTicker(zeladorCada)
		defer t.Stop()
		for {
			zeladorRodada(time.Now())
			<-t.C
		}
	}()
}

// zeladorPausar suspende a reabertura de um app (o Reparar pede o Claude
// fechado).
func zeladorPausar(chave string, por time.Duration) {
	zelador.mu.Lock()
	defer zelador.mu.Unlock()
	if zelador.pausaAte == nil {
		zelador.pausaAte = map[string]time.Time{}
	}
	zelador.pausaAte[chave] = time.Now().Add(por)
}

func zeladorRodada(agora time.Time) {
	if !zeladorLigado() {
		return
	}
	zelador.mu.Lock()
	if zelador.fechado == nil {
		zelador.fechado, zelador.falhas, zelador.avisado = map[string]int{}, map[string]int{}, map[string]bool{}
	}
	if zelador.pausaAte == nil {
		zelador.pausaAte = map[string]time.Time{}
	}
	zelador.mu.Unlock()

	for _, a := range zeladorApps {
		if !zeladorInstalado(a) {
			continue // a prontidão mostra o que falta instalar
		}
		zelador.mu.Lock()
		if zeladorAberto(a.Processo) {
			if zelador.avisado[a.Chave] {
				zelador.avisado[a.Chave] = false
			}
			zelador.fechado[a.Chave], zelador.falhas[a.Chave] = 0, 0
			zelador.mu.Unlock()
			continue
		}
		zelador.fechado[a.Chave]++
		if agora.Before(zelador.pausaAte[a.Chave]) || zelador.fechado[a.Chave] < a.EsperaFechado {
			zelador.mu.Unlock()
			continue
		}
		zelador.mu.Unlock()

		_ = zeladorAbrir(a.Nome)

		zelador.mu.Lock()
		zelador.falhas[a.Chave]++
		avisar := zelador.falhas[a.Chave] >= zeladorFalhasParaAvisar && !zelador.avisado[a.Chave]
		if avisar {
			zelador.avisado[a.Chave] = true
		}
		zelador.mu.Unlock()
		if avisar {
			zeladorAvisar("o " + a.Nome + " está fechado neste " + maquina() + " e não consegui abrir sozinho. Abra-o e deixe aberto (pode minimizar); sem ele, " + zeladorParaQue(a.Chave) + ".")
		}
	}
	prontidaoInvalidar()
}

func zeladorParaQue(chave string) string {
	if chave == "claude" {
		return "as conversas do Claude não alcançam este " + maquina()
	}
	return "as tarefas no navegador não rodam"
}
