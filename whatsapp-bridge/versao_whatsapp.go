package main

// Versão do WhatsApp Web que a ponte apresenta ao conectar (1.1.4).
//
// O que aconteceu em 23/11/2026, por volta das 20:05 (Brasília): o WhatsApp
// aposentou a revisão 2.3000.1043636855, que vinha compilada na whatsmeow de
// julho, e passou a recusar a conexão com "Client outdated (405)". No 405 a
// whatsmeow desliga a reconexão automática e só emite events.ClientOutdated,
// que a ponte não tratava: todas as instalações ficaram desconectadas, com a
// sessão intacta, e o painel dizia "Reconectando…" para sempre.
//
// Isso se repete de tempos em tempos (02/11, 14/09, julho). Daqui em diante:
//
//  1. Ao iniciar, e a cada 6 h, a ponte lê a revisão atual em web.whatsapp.com
//     (whatsmeow.GetLatestVersion) e passa a usá-la se for MAIS NOVA que a que
//     está em uso. Nunca desce: a compilada é o piso. Falhou a leitura, segue
//     com a que tem. Assim a próxima aposentadoria encontra a ponte já em dia.
//  2. No 405, o painel mostra que o WhatsApp recusou a versão (e que NÃO é
//     para ler o QR de novo), avisa o suporte pelo servidor do Beatitude quando
//     a máquina está pareada a ele, e tenta de novo em 5 s, 1 min, 5 min,
//     15 min, 30 min e depois a cada 30 min. Reconecta na mesma sessão assim
//     que houver revisão mais nova que a recusada; sem QR.
//  3. LUMNIA_WA_VERSAO=2.3000.N força uma revisão (suporte). Vale mesmo abaixo
//     da compilada e desliga a busca automática.
//
// O limite do que isto resolve: quando o WhatsApp muda o protocolo e não só o
// número, a revisão nova não basta e o remédio continua sendo atualizar a
// whatsmeow e publicar versão. O painel deixa isso claro depois de 1 h.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

const (
	waVersaoCache      = 30 * time.Minute // não consulta o site mais que isso
	waVersaoPeriodo    = 6 * time.Hour    // consulta de fundo, com a ponte conectada
	waVersaoTimeout    = 10 * time.Second
	waRecusaAvisoAposH = time.Hour // depois disso o painel fala em versão nova do Zap
)

// Intervalos entre tentativas depois de um 405; o último se repete.
var waRecusaEsperas = []time.Duration{
	5 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
}

type waVersaoEstado struct {
	mu sync.Mutex

	forcada     bool
	consultaEm  time.Time // última consulta ao site (com ou sem sucesso)
	consultaErr string
	origem      string // "compilada", "web.whatsapp.com", "LUMNIA_WA_VERSAO"

	// 405 em aberto
	recusada     store.WAVersionContainer
	recusadaEm   time.Time
	proximaEm    time.Time
	tentativas   int
	recuperando  bool
	alertaEnviou bool
}

var waVersao = waVersaoEstado{origem: "compilada"}

// Pontos de troca para os testes.
var (
	waBuscarVersao = func(ctx context.Context) (*store.WAVersionContainer, error) {
		return whatsmeow.GetLatestVersion(ctx, &http.Client{Timeout: waVersaoTimeout})
	}
	waReconectar = func() error {
		c := panel.client
		if c == nil {
			return fmt.Errorf("sem cliente")
		}
		if c.IsConnected() {
			return nil
		}
		return c.Connect()
	}
	waConectado = func() bool {
		c := panel.client
		return c != nil && c.IsConnected() && c.IsLoggedIn()
	}
	waAvisarSuporte = waAvisarSuporteBeatitude
	waAgora         = time.Now
)

// waVersaoMaisNova diz se a é maior que b, campo a campo.
func waVersaoMaisNova(a, b store.WAVersionContainer) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// waVersaoLer interpreta "2.3000.1055969240".
func waVersaoLer(s string) (store.WAVersionContainer, error) {
	var v store.WAVersionContainer
	partes := strings.Split(strings.TrimSpace(s), ".")
	if len(partes) != 3 {
		return v, fmt.Errorf("versão %q: esperado a.b.c", s)
	}
	for i, p := range partes {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return v, fmt.Errorf("versão %q: %v", s, err)
		}
		v[i] = uint32(n)
	}
	if v.IsZero() {
		return v, fmt.Errorf("versão zerada")
	}
	return v, nil
}

func waInfo(format string, args ...interface{}) {
	if panel.logger != nil {
		panel.logger.Infof(format, args...)
	}
}

func waAviso(format string, args ...interface{}) {
	if panel.logger != nil {
		panel.logger.Warnf(format, args...)
	}
}

// waVersaoIniciar roda antes da primeira conexão. Segura no máximo
// waVersaoTimeout: sem internet, a ponte sobe com a compilada.
func waVersaoIniciar() {
	if s := os.Getenv("LUMNIA_WA_VERSAO"); s != "" {
		v, err := waVersaoLer(s)
		if err != nil {
			waAviso("LUMNIA_WA_VERSAO ignorada: %v", err)
		} else {
			store.SetWAVersion(v)
			waVersao.mu.Lock()
			waVersao.forcada = true
			waVersao.origem = "LUMNIA_WA_VERSAO"
			waVersao.mu.Unlock()
			waInfo("Versão do WhatsApp Web forçada por LUMNIA_WA_VERSAO: %s", v.String())
			return
		}
	}
	waInfo("Versão do WhatsApp Web compilada: %s", store.GetWAVersion().String())
	waVersaoAtualizar(true)
	go func() {
		for {
			time.Sleep(waVersaoPeriodo)
			waVersaoAtualizar(false)
		}
	}()
}

// waVersaoAtualizar consulta o site (respeitando o cache, salvo forcar) e
// adota a revisão se for mais nova que a em uso. Devolve true se mudou.
func waVersaoAtualizar(forcar bool) bool {
	waVersao.mu.Lock()
	if waVersao.forcada {
		waVersao.mu.Unlock()
		return false
	}
	if !forcar && !waVersao.consultaEm.IsZero() && waAgora().Sub(waVersao.consultaEm) < waVersaoCache {
		waVersao.mu.Unlock()
		return false
	}
	waVersao.consultaEm = waAgora()
	waVersao.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), waVersaoTimeout)
	defer cancel()
	nova, err := waBuscarVersao(ctx)

	waVersao.mu.Lock()
	defer waVersao.mu.Unlock()
	if err != nil || nova == nil || nova.IsZero() {
		if err == nil {
			err = fmt.Errorf("resposta vazia")
		}
		waVersao.consultaErr = err.Error()
		waAviso("Não consegui ler a versão atual do WhatsApp Web (sigo com %s): %v",
			store.GetWAVersion().String(), err)
		return false
	}
	waVersao.consultaErr = ""
	atual := store.GetWAVersion()
	if !waVersaoMaisNova(*nova, atual) {
		return false
	}
	store.SetWAVersion(*nova)
	waVersao.origem = "web.whatsapp.com"
	waInfo("Versão do WhatsApp Web: %s → %s", atual.String(), nova.String())
	return true
}

// waVersaoRecusada trata events.ClientOutdated.
func waVersaoRecusada() {
	atual := store.GetWAVersion()
	waVersao.mu.Lock()
	if waVersao.recusadaEm.IsZero() {
		waVersao.recusadaEm = waAgora()
		waVersao.alertaEnviou = false
	}
	waVersao.recusada = atual
	jaRodando := waVersao.recuperando
	waVersao.recuperando = true
	waVersao.mu.Unlock()

	waAviso("O WhatsApp recusou a versão %s (405). A sessão continua salva; tentando com uma versão mais nova.", atual.String())
	if !jaRodando {
		go waRecuperar()
	}
}

// waRecuperar: um laço só por vez. Sai quando conecta.
func waRecuperar() {
	defer func() {
		waVersao.mu.Lock()
		waVersao.recuperando = false
		waVersao.proximaEm = time.Time{}
		waVersao.mu.Unlock()
	}()
	for i := 0; ; i++ {
		espera := waRecusaEsperas[len(waRecusaEsperas)-1]
		if i < len(waRecusaEsperas) {
			espera = waRecusaEsperas[i]
		}
		waVersao.mu.Lock()
		waVersao.proximaEm = waAgora().Add(espera)
		waVersao.tentativas = i
		waVersao.mu.Unlock()
		waDormir(espera)

		if waConectado() {
			waVersaoConectou()
			return
		}
		waAvisarSeDemorou()

		waVersaoAtualizar(true)
		waVersao.mu.Lock()
		recusada := waVersao.recusada
		waVersao.mu.Unlock()
		if !waVersaoMaisNova(store.GetWAVersion(), recusada) {
			continue // ainda não há revisão mais nova; reconectar daria 405 de novo
		}
		waInfo("Reconectando com a versão %s (a recusada era %s)", store.GetWAVersion().String(), recusada.String())
		if err := waReconectar(); err != nil {
			waAviso("Reconexão depois do 405 falhou: %v", err)
			continue
		}
		// Se der 405 de novo, waVersaoRecusada atualiza "recusada" e este laço
		// segue (recuperando continua true). Se conectar, Connected limpa tudo.
		waDormir(15 * time.Second)
		if waConectado() {
			waVersaoConectou()
			return
		}
	}
}

var waDormir = time.Sleep

// waVersaoConectou trata events.Connected.
func waVersaoConectou() {
	waVersao.mu.Lock()
	tinha := !waVersao.recusadaEm.IsZero()
	waVersao.recusadaEm = time.Time{}
	waVersao.recusada = store.WAVersionContainer{}
	waVersao.tentativas = 0
	waVersao.alertaEnviou = false
	waVersao.mu.Unlock()
	if tinha {
		waInfo("Conectado de novo depois do 405, com a versão %s", store.GetWAVersion().String())
	}
}

func waAvisarSeDemorou() {
	waVersao.mu.Lock()
	desde := waVersao.recusadaEm
	if desde.IsZero() || waVersao.alertaEnviou || waAgora().Sub(desde) < 5*time.Minute {
		waVersao.mu.Unlock()
		return
	}
	waVersao.alertaEnviou = true
	recusada := waVersao.recusada
	waVersao.mu.Unlock()
	go waAvisarSuporte(recusada, desde)
}

// waAvisarSuporteBeatitude usa o mesmo /api/ponte/alerta do verificador da
// nuvem. Só funciona onde a ponte está pareada ao Beatitude (Mac e Linux).
func waAvisarSuporteBeatitude(recusada store.WAVersionContainer, desde time.Time) {
	if !beatitudeDisponivel() || !pontePareado() {
		return
	}
	host, _ := os.Hostname()
	corpo := map[string]interface{}{
		"maquina": fmt.Sprintf("Lumnia Zap no %s (%s)", maquina(), host),
		"versao":  versaoPonte(),
		"itens": []map[string]string{{
			"peca":   "WhatsApp",
			"estado": "versão recusada (405) desde " + desde.In(panelLoc).Format("02/01 15:04"),
			"fazer": beatitudeCurto("O WhatsApp recusou a versão "+recusada.String()+
				" e a ponte ainda não achou uma mais nova. Não ler o QR: a sessão está salva. "+
				"Se passar de 1 h, publicar o Zap com a whatsmeow atual (doc 15).", 300),
		}},
	}
	if st, err := ponteChamar("/api/ponte/alerta", corpo, nil); err != nil || st >= 300 {
		waAviso("Aviso de 405 ao suporte não saiu (%d, %v)", st, err)
	}
}

// waVersaoStatus entra no /api/panel/status.
func waVersaoStatus(resp map[string]interface{}) {
	waVersao.mu.Lock()
	defer waVersao.mu.Unlock()
	info := map[string]interface{}{
		"em_uso": store.GetWAVersion().String(),
		"origem": waVersao.origem,
	}
	if waVersao.consultaErr != "" {
		info["erro_consulta"] = waVersao.consultaErr
	}
	resp["whatsapp_web"] = info
	if waVersao.recusadaEm.IsZero() {
		return
	}
	r := map[string]interface{}{
		"versao":     waVersao.recusada.String(),
		"desde_secs": int(waAgora().Sub(waVersao.recusadaEm).Seconds()),
		"demorando":  waAgora().Sub(waVersao.recusadaEm) >= waRecusaAvisoAposH,
	}
	if !waVersao.proximaEm.IsZero() {
		s := int(waVersao.proximaEm.Sub(waAgora()).Seconds())
		if s < 0 {
			s = 0
		}
		r["proxima_secs"] = s
	}
	resp["whatsapp_recusou"] = r
}
