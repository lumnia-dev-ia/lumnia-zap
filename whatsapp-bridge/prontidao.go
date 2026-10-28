// Copyright (c) 2026 Lumnia - Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// "Pronta para rodar sozinha" (1.0.8): a ponte confere, a cada 5 minutos, se
// este Mac vai mante-la no ar sem ninguem por perto. Mesma lista do
// instalador (installer/prontidao.go, copia deliberada: os dois modulos nao
// compartilham codigo). Nasceu do Mac do pai do Diego em 17-10: o item em
// segundo plano estava desligado e o macOS encerrava a sessao a cada 60 min
// sem uso, e ninguem via. Aqui o painel mostra, e o sinal de vida leva ao
// servidor, que pode avisar pelo app quando a ponte sumir.
//
// Limite: desligada, a ponte nao roda e nao avisa nada. Este arquivo so
// consegue mostrar o problema ANTES de ele derrubar a ponte.

const prontidaoServico = "dev.lumnia.zap"

type prontidaoItem struct {
	Chave string `json:"chave"`
	Nome  string `json:"nome"`
	OK    bool   `json:"ok"`
	Visto string `json:"visto"`
	Fazer string `json:"fazer,omitempty"`
}

var prontidaoCache struct {
	mu    sync.Mutex
	itens []prontidaoItem
	em    time.Time
}

const prontidaoCada = 5 * time.Minute

// prontidaoItens devolve a ultima conferencia (refaz se passou de 5 min ou
// se agora=true). Fora do macOS devolve nil: nada a conferir.
func prontidaoItens(agora bool) []prontidaoItem {
	if runtime.GOOS != "darwin" {
		return nil
	}
	prontidaoCache.mu.Lock()
	defer prontidaoCache.mu.Unlock()
	if !agora && prontidaoCache.itens != nil && time.Since(prontidaoCache.em) < prontidaoCada {
		return prontidaoCache.itens
	}
	prontidaoCache.itens = []prontidaoItem{
		prontidaoServicoHabilitado(),
		prontidaoServicoCarregado(),
		prontidaoSemEncerramentoAutomatico(),
		prontidaoSemRepouso(),
	}
	prontidaoCache.em = time.Now()
	return prontidaoCache.itens
}

func prontidaoTudoOK(itens []prontidaoItem) bool {
	for _, c := range itens {
		if !c.OK {
			return false
		}
	}
	return true
}

// prontidaoResumo e o que vai no /api/panel/status.
func prontidaoResumo() map[string]interface{} {
	itens := prontidaoItens(false)
	if itens == nil {
		return nil
	}
	return map[string]interface{}{
		"ok":    prontidaoTudoOK(itens),
		"itens": itens,
	}
}

// prontidaoEstado e o que a ponte manda no sinal de vida (campo `estado`,
// previsto no blueprint de 14-10). Chaves curtas e estaveis: o servidor
// guarda e o app mostra em Instalar. Nil fora do macOS.
func prontidaoEstado() map[string]interface{} {
	itens := prontidaoItens(false)
	if itens == nil {
		return nil
	}
	e := map[string]interface{}{"plataforma": "mac", "pronta": prontidaoTudoOK(itens)}
	for _, c := range itens {
		e[c.Chave] = c.OK
	}
	return e
}

func prontidaoServicoHabilitado() prontidaoItem {
	uid := fmt.Sprint(os.Getuid())
	out, _ := exec.Command("/bin/launchctl", "print-disabled", "gui/"+uid).CombinedOutput()
	desligado := regexp.MustCompile(`"` + regexp.QuoteMeta(prontidaoServico) + `"\s*=>\s*(disabled|true)`).Match(out)
	c := prontidaoItem{Chave: "item_ligado", Nome: "Item em segundo plano ligado", OK: !desligado}
	if desligado {
		c.Visto = "desligado em Itens de Início"
		c.Fazer = "Em Ajustes > Geral > Itens de Início, ligue \"Lumnia Zap\". Desligado, a ponte não volta quando o Mac reinicia ou a sessão é encerrada."
	} else {
		c.Visto = "habilitado no launchd"
	}
	return c
}

func prontidaoServicoCarregado() prontidaoItem {
	uid := fmt.Sprint(os.Getuid())
	out, err := exec.Command("/bin/launchctl", "print", "gui/"+uid+"/"+prontidaoServico).CombinedOutput()
	estado, pid := prontidaoEstadoLaunchd(string(out))
	// "running" é o normal. Com pid no primeiro nível o processo está de pé,
	// mesmo que o launchd descreva o estado com outra palavra.
	ok := err == nil && (estado == "running" || pid > 0)
	c := prontidaoItem{Chave: "servico_carregado", Nome: "Serviço carregado pelo sistema", OK: ok}
	switch {
	case err != nil:
		c.Visto = "a ponte está rodando, mas não pelo launchd (foi iniciada à mão)"
		c.Fazer = "Feche esta ponte e abra o Lumnia Zap (Aplicativos): ele sobe o serviço do jeito certo, que volta sozinho depois de reinício."
	case ok && estado == "running":
		c.Visto = "launchd: running"
	case ok:
		c.Visto = fmt.Sprintf("launchd: %s, processo %d no ar", estado, pid)
	default:
		c.Visto = "launchd: " + estado
		c.Fazer = "Abra o Lumnia Zap (Aplicativos) uma vez; ele recarrega o serviço."
	}
	return c
}

// prontidaoEstadoLaunchd lê o estado e o pid do serviço na saída de
// `launchctl print`. Só contam as chaves do primeiro nível do bloco: os blocos
// aninhados (endpoints, gatilhos de evento) também têm linhas "state = ..." e
// a leitura antiga, que ficava com a última ocorrência, pegava uma delas e
// marcava ✕ com a ponte rodando ("launchd: active", 28/10/2026).
func prontidaoEstadoLaunchd(saida string) (estado string, pid int) {
	nivel := ""
	for _, l := range strings.Split(saida, "\n") {
		t := strings.TrimLeft(l, " \t")
		ind := l[:len(l)-len(t)]
		t = strings.TrimSpace(t)
		if t == "" || ind == "" {
			continue
		}
		if nivel == "" {
			nivel = ind // a primeira linha recuada marca o primeiro nível
		}
		if ind != nivel {
			continue
		}
		switch {
		case estado == "" && strings.HasPrefix(t, "state = "):
			estado = strings.TrimPrefix(t, "state = ")
		case pid == 0 && strings.HasPrefix(t, "pid = "):
			pid, _ = strconv.Atoi(strings.TrimPrefix(t, "pid = "))
		}
	}
	return estado, pid
}

func prontidaoSemEncerramentoAutomatico() prontidaoItem {
	out, err := exec.Command("/usr/bin/defaults", "read",
		"/Library/Preferences/.GlobalPreferences", "com.apple.autologout.AutoLogOutDelay").CombinedOutput()
	segundos := 0
	if err == nil {
		segundos, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	c := prontidaoItem{Chave: "sem_autologout", Nome: "Encerramento automático de sessão desligado", OK: segundos <= 0}
	if segundos <= 0 {
		c.Visto = "desligado"
	} else {
		c.Visto = fmt.Sprintf("ligado: encerra após %d min sem uso", segundos/60)
		c.Fazer = "A ponte para junto com a sessão. Desligue em Ajustes > Privacidade e Segurança > Avançado… > " +
			"\"Encerrar sessão automaticamente após inatividade\" (pede a senha de administrador) e deixe o bloqueio de tela com senha ligado."
	}
	return c
}

func prontidaoSemRepouso() prontidaoItem {
	out, err := exec.Command("/usr/bin/pmset", "-g").CombinedOutput()
	minutos := -1
	if err == nil {
		if m := regexp.MustCompile(`(?m)^\s*sleep\s+(\d+)`).FindSubmatch(out); m != nil {
			minutos, _ = strconv.Atoi(string(m[1]))
		}
	}
	c := prontidaoItem{Chave: "sem_repouso", Nome: "Mac sem repouso", OK: minutos == 0}
	switch {
	case minutos < 0:
		c.Visto = "não consegui ler o pmset"
		c.Fazer = "Confira em Ajustes > Economia de Energia que o Mac não entra em repouso; ou, no Terminal: sudo pmset -a sleep 0"
	case minutos == 0:
		c.Visto = "o Mac não dorme"
	default:
		c.Visto = fmt.Sprintf("entra em repouso após %d min", minutos)
		c.Fazer = "Dormindo, a ponte não recebe nem manda nada. No Terminal: sudo pmset -a sleep 0 (pede a senha de administrador)."
	}
	return c
}

// /api/panel/prontidao?agora=1 refaz a conferencia na hora (botao Verificar).
// /api/panel/prontidao/itens-de-inicio abre o painel de Itens de Inicio.
func panelProntidaoHandler(w http.ResponseWriter, r *http.Request) {
	itens := prontidaoItens(r.URL.Query().Get("agora") == "1")
	writeJSON(w, map[string]interface{}{"ok": prontidaoTudoOK(itens), "itens": itens})
}

func panelProntidaoAbrirHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	if runtime.GOOS == "darwin" {
		_ = exec.Command("/usr/bin/open", "x-apple.systempreferences:com.apple.LoginItems-Settings.extension").Run()
	}
	writeJSON(w, map[string]bool{"ok": true})
}
