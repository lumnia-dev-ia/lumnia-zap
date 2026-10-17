// Copyright (c) 2026 Lumnia - Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// "Pronta para rodar sozinha": quatro conferencias que dizem se a ponte vai
// continuar no ar sem ninguem por perto. Nasceram do Mac do pai do Diego
// (17-10): o item em segundo plano estava desligado e o macOS encerrava a
// sessao a cada 60 min sem uso, e nenhum dos dois era visivel para quem
// instalou. Nenhuma conferencia pede senha de administrador; o que exige
// senha fica como instrucao para a pessoa.
type conferencia struct {
	nome string
	ok   bool
	// detalhe curto do que foi visto (vai para o diario e o relatorio).
	visto string
	// o que fazer quando falha; vazio quando ok.
	fazer string
}

// prontidao roda as quatro conferencias. Nao decide nada: quem chama mostra.
func prontidao() []conferencia {
	return []conferencia{
		servicoHabilitado(),
		servicoCarregado(),
		semEncerramentoAutomatico(),
		semRepouso(),
	}
}

func prontidaoOK(cs []conferencia) bool {
	for _, c := range cs {
		if !c.ok {
			return false
		}
	}
	return true
}

// servicoHabilitado: o launchd aceita subir o job? Quando a pessoa desliga o
// item em Ajustes > Geral > Itens de Inicio, o label entra na lista de
// desabilitados do dominio gui e nem RunAtLoad nem bootstrap sobem nada.
func servicoHabilitado() conferencia {
	uid := fmt.Sprint(os.Getuid())
	out, _ := exec.Command("/bin/launchctl", "print-disabled", "gui/"+uid).CombinedOutput()
	desligado := regexp.MustCompile(`"` + regexp.QuoteMeta(label) + `"\s*=>\s*(disabled|true)`).
		Match(out)
	c := conferencia{nome: "Item em segundo plano ligado", ok: !desligado,
		visto: "launchctl print-disabled: " + map[bool]string{true: "desabilitado", false: "habilitado"}[desligado]}
	if desligado {
		c.fazer = "Em Ajustes > Geral > Itens de Início, ligue \"" + produto + "\". " +
			"Desligado, a ponte não volta quando o Mac reinicia ou a sessão é encerrada."
	}
	return c
}

// servicoCarregado: a ponte esta rodando pelo launchd (e nao na mao)?
func servicoCarregado() conferencia {
	uid := fmt.Sprint(os.Getuid())
	out, err := exec.Command("/bin/launchctl", "print", "gui/"+uid+"/"+label).CombinedOutput()
	estado := ""
	for _, l := range strings.Split(string(out), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "state = ") {
			estado = strings.TrimPrefix(t, "state = ")
		}
	}
	ok := err == nil && estado == "running"
	c := conferencia{nome: "Serviço carregado pelo sistema", ok: ok}
	if err != nil {
		c.visto = "launchctl print: o serviço não está carregado"
	} else {
		c.visto = "launchctl print: state = " + estado
	}
	if !ok {
		c.fazer = "Abra o " + produto + " (Aplicativos) uma vez; ele recarrega o serviço. " +
			"Se continuar assim, rode o instalador de novo."
	}
	return c
}

// semEncerramentoAutomatico: "Encerrar sessao automaticamente apos
// inatividade" (Privacidade e Seguranca > Avancado). Ligado, derruba todos
// os LaunchAgents da sessao, a ponte inclusive. No Mac do pai estava em 60 min.
func semEncerramentoAutomatico() conferencia {
	out, err := exec.Command("/usr/bin/defaults", "read",
		"/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay").CombinedOutput()
	segundos := 0
	if err == nil {
		segundos, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	c := conferencia{nome: "Encerramento automático de sessão desligado", ok: segundos <= 0}
	if segundos <= 0 {
		c.visto = "AutoLogOutDelay ausente ou 0"
	} else {
		c.visto = fmt.Sprintf("AutoLogOutDelay = %d s (%d min)", segundos, segundos/60)
		c.fazer = fmt.Sprintf("Este Mac encerra a sessão sozinho após %d min sem uso, e a ponte para junto. "+
			"Desligue em Ajustes > Privacidade e Segurança > Avançado… > "+
			"\"Encerrar sessão automaticamente após inatividade\" (pede a senha de administrador). "+
			"Com a sessão sempre aberta, ligue o bloqueio de tela com senha.", segundos/60)
	}
	return c
}

// semRepouso: o Mac dorme? Dormindo, a ponte nao recebe nem manda nada.
// pmset -g mostra o perfil em uso; "sleep 0" e o que o roteiro de instalacao
// pede (sudo pmset -a sleep 0).
func semRepouso() conferencia {
	out, err := exec.Command("/usr/bin/pmset", "-g").CombinedOutput()
	minutos := -1
	if err == nil {
		re := regexp.MustCompile(`(?m)^\s*sleep\s+(\d+)`)
		if m := re.FindSubmatch(out); m != nil {
			minutos, _ = strconv.Atoi(string(m[1]))
		}
	}
	c := conferencia{nome: "Mac sem repouso", ok: minutos == 0}
	switch {
	case minutos < 0:
		c.visto = "pmset -g: não consegui ler"
		c.fazer = "Confira em Ajustes > Economia de Energia (ou Bateria) que o Mac não entra em repouso; " +
			"ou, no Terminal: sudo pmset -a sleep 0"
	case minutos == 0:
		c.visto = "pmset -g: sleep 0"
	default:
		c.visto = fmt.Sprintf("pmset -g: sleep %d", minutos)
		c.fazer = fmt.Sprintf("Este Mac entra em repouso após %d min, e a ponte para junto. "+
			"No Terminal: sudo pmset -a sleep 0 (pede a senha de administrador).", minutos)
	}
	return c
}

// prontidaoTexto monta o bloco para o diario, o relatorio e o alerta final.
func prontidaoTexto(cs []conferencia) string {
	var b strings.Builder
	b.WriteString("Pronta para rodar sozinha: ")
	if prontidaoOK(cs) {
		b.WriteString("sim\n")
	} else {
		b.WriteString("ainda não\n")
	}
	for _, c := range cs {
		marca := "✓"
		if !c.ok {
			marca = "✗"
		}
		fmt.Fprintf(&b, "  %s %s (%s)\n", marca, c.nome, c.visto)
		if c.fazer != "" {
			fmt.Fprintf(&b, "      → %s\n", c.fazer)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
