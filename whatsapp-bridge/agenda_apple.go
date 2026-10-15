// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// agenda_apple.go — Agenda da Apple (iCloud) pelo computador da pessoa.
//
// O que faz: le e cria compromissos no calendario do iPhone da pessoa,
// falando CalDAV com o iCloud (caldav.icloud.com) com uma SENHA ESPECIFICA
// DE APP que ela gera na conta Apple. Nada de EventKit: um codigo so para
// Mac e Linux.
//
// Quem nao configurou nao percebe: sem store/apple.json nao ha ferramenta
// no MCP, nao ha link no painel e nenhuma chamada sai para a Apple.
//
// Travas (no codigo, nao no prompt):
//   - so agenda: o cliente fala apenas com o calendar-home-set do CalDAV;
//     nao ha IMAP nem CardDAV neste binario, a senha nao vai a mais nada;
//   - so leitura, salvo agendaCriar; nao existe editar nem apagar, nem do
//     que o proprio Beatitude criou;
//   - criar com convidados nao existe na v1 (no iCloud, gravar com ATTENDEE
//     ja dispara o convite): a chamada e recusada com erro claro;
//   - escreve so no calendario principal ou num calendario marcado "ler"
//     e nomeado no pedido; nunca num calendario somente leitura;
//   - a senha fica em store/apple.json (0600), nunca sai da maquina, nunca
//     aparece no log;
//   - 401/403 -> estado senha_invalida e nenhuma nova tentativa ate a pessoa
//     salvar de novo; rede -> sem_sinal, sem apagar nada.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
)

const (
	agendaConfigPath  = "store/apple.json"
	agendaCriadosPath = "store/apple_criados.json"
	agendaICloudURL   = "https://caldav.icloud.com/"
	agendaCacheTTL    = 10 * time.Minute
	agendaJanelaMax   = 400 * 24 * time.Hour
	agendaMaxEventos  = 500
	agendaMaxBusca    = 50
	agendaMaxLivres   = 30
	agendaTimeoutHTTP = 40 * time.Second
	agendaUIDPrefixo  = "beatitude-"
	agendaUserAgent   = "LumniaZap/agenda (CalDAV)"

	agendaEstadoNao   = "nao"
	agendaEstadoOK    = "conectada"
	agendaEstadoSenha = "senha_invalida"
	agendaEstadoSinal = "sem_sinal"
)

type agendaCalendario struct {
	URL       string `json:"url"`
	Nome      string `json:"nome"`
	Cor       string `json:"cor,omitempty"`
	Ler       bool   `json:"ler"`
	SoLeitura bool   `json:"so_leitura"`
}

type agendaConfig struct {
	AppleID       string             `json:"apple_id"`
	SenhaApp      string             `json:"senha_app"`
	HomeURL       string             `json:"home_url"`
	Calendarios   []agendaCalendario `json:"calendarios"`
	Principal     string             `json:"principal"`
	VerificadoEm  time.Time          `json:"verificado_em"`
	Estado        string             `json:"estado"`
	UltimaLeitura time.Time          `json:"ultima_leitura,omitempty"`
	UltimoErro    string             `json:"ultimo_erro,omitempty"`
}

// agendaEvento e o que sai para o MCP e para a ponte. Datas em RFC 3339 no
// fuso da maquina; "quando" e a forma legivel ("qui 16/10 14:00–15:00").
type agendaEvento struct {
	UID        string   `json:"uid"`
	Titulo     string   `json:"titulo"`
	Inicio     string   `json:"inicio"`
	Fim        string   `json:"fim"`
	Quando     string   `json:"quando"`
	DiaInteiro bool     `json:"dia_inteiro"`
	Calendario string   `json:"calendario"`
	Local      string   `json:"local,omitempty"`
	Convidados []string `json:"convidados,omitempty"`
	Notas      string   `json:"notas,omitempty"`
	Beatitude  bool     `json:"criado_pelo_beatitude"`
	Livre      bool     `json:"-"` // TRANSP:TRANSPARENT (nao ocupa a pessoa)

	inicio, fim time.Time
}

type agendaJanelaLivre struct {
	Inicio string `json:"inicio"`
	Fim    string `json:"fim"`
	Quando string `json:"quando"`
	Min    int    `json:"minutos"`
}

type agendaPedidoCriar struct {
	Titulo     string   `json:"titulo"`
	Inicio     string   `json:"inicio"`
	Fim        string   `json:"fim"`
	Calendario string   `json:"calendario,omitempty"`
	Local      string   `json:"local,omitempty"`
	Notas      string   `json:"notas,omitempty"`
	AlertaMin  int      `json:"alerta_min,omitempty"`
	Convidados []string `json:"convidados,omitempty"`
	DiaInteiro bool     `json:"dia_inteiro,omitempty"`
}

// agendaErro carrega o estado que o erro implica (senha_invalida, sem_sinal
// ou vazio quando e um erro de uso, que nao muda o estado da conexao).
type agendaErro struct {
	Estado string
	Err    error
}

func (e *agendaErro) Error() string { return e.Err.Error() }
func (e *agendaErro) Unwrap() error { return e.Err }

func agendaErroSenha(err error) error { return &agendaErro{Estado: agendaEstadoSenha, Err: err} }
func agendaErroSinal(err error) error { return &agendaErro{Estado: agendaEstadoSinal, Err: err} }

// agendaDiscoverURL e onde a descoberta comeca; variavel so para o teste
// apontar para um servidor falso.
var agendaDiscoverURL = agendaICloudURL

type agendaCacheItem struct {
	em      time.Time
	eventos []agendaEvento
}

var agenda struct {
	mu      sync.Mutex
	cfg     agendaConfig
	cache   map[string]agendaCacheItem
	criados map[string]bool // uid -> criado pelo Beatitude
}

// ------------------------------------------------------------ configuracao

func initAgendaApple() {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	agenda.cache = map[string]agendaCacheItem{}
	agenda.criados = map[string]bool{}
	if b, err := os.ReadFile(agendaConfigPath); err == nil {
		_ = json.Unmarshal(b, &agenda.cfg)
	}
	if b, err := os.ReadFile(agendaCriadosPath); err == nil {
		var lista []string
		if json.Unmarshal(b, &lista) == nil {
			for _, u := range lista {
				agenda.criados[u] = true
			}
		}
	}
	if agenda.cfg.SenhaApp != "" && agenda.cfg.Estado == "" {
		agenda.cfg.Estado = agendaEstadoOK
	}
	if agenda.cfg.SenhaApp != "" && panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: configurada (%s), estado %s, %d calendário(s)",
			agenda.cfg.AppleID, agenda.cfg.Estado, len(agenda.cfg.Calendarios))
	}
}

// agendaConfigurada: ha senha guardada (a pessoa passou pela pagina).
func agendaConfigurada() bool {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	return agenda.cfg.SenhaApp != "" && agenda.cfg.HomeURL != ""
}

// agendaConectada: configurada e sem senha invalida. Com sem_sinal ainda
// tentamos (a rede pode ter voltado).
func agendaConectada() bool {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	return agenda.cfg.SenhaApp != "" && agenda.cfg.HomeURL != "" && agenda.cfg.Estado != agendaEstadoSenha
}

func agendaSalvarLocked() error {
	b, err := json.MarshalIndent(agenda.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(agendaConfigPath, b, 0o600)
}

func agendaSalvarCriadosLocked() {
	lista := make([]string, 0, len(agenda.criados))
	for u := range agenda.criados {
		lista = append(lista, u)
	}
	sort.Strings(lista)
	if b, err := json.Marshal(lista); err == nil {
		_ = os.WriteFile(agendaCriadosPath, b, 0o600)
	}
}

// agendaRegistrarResultado atualiza estado/ultimo erro depois de cada ida a
// Apple. Erros de uso (parametro errado) nao mudam o estado.
func agendaRegistrarResultado(err error) {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	if agenda.cfg.SenhaApp == "" {
		return
	}
	if err == nil {
		agenda.cfg.Estado = agendaEstadoOK
		agenda.cfg.UltimaLeitura = time.Now()
		agenda.cfg.UltimoErro = ""
		_ = agendaSalvarLocked()
		return
	}
	var ae *agendaErro
	if errors.As(err, &ae) && ae.Estado != "" {
		agenda.cfg.Estado = ae.Estado
		agenda.cfg.UltimoErro = agendaErroSeguro(err)
		_ = agendaSalvarLocked()
		if panel.logger != nil {
			panel.logger.Warnf("Agenda da Apple: %s (%s)", ae.Estado, agenda.cfg.UltimoErro)
		}
	}
}

// agendaErroSeguro: a mensagem de erro nunca pode carregar a senha (ela
// viaja em Basic auth, nao em URL, mas por garantia).
func agendaErroSeguro(err error) string {
	agenda.mu.Lock()
	senha := agenda.cfg.SenhaApp
	agenda.mu.Unlock()
	s := err.Error()
	if senha != "" {
		s = strings.ReplaceAll(s, senha, "•••")
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func agendaDesconectar() {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	agenda.cfg = agendaConfig{}
	agenda.cache = map[string]agendaCacheItem{}
	agenda.criados = map[string]bool{}
	_ = os.Remove(agendaConfigPath)
	_ = os.Remove(agendaCriadosPath)
	if panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: desconectada pela pessoa")
	}
}

// agendaResumoStatus e o que o painel e o sinal de vida da ponte mostram.
// nil quando nao configurada: o painel nao mostra nada.
func agendaResumoStatus() map[string]interface{} {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	if agenda.cfg.SenhaApp == "" {
		return nil
	}
	lidos := 0
	for _, c := range agenda.cfg.Calendarios {
		if c.Ler {
			lidos++
		}
	}
	r := map[string]interface{}{
		"estado":      agenda.cfg.Estado,
		"calendarios": lidos,
	}
	if !agenda.cfg.UltimaLeitura.IsZero() {
		r["ultima_leitura"] = agenda.cfg.UltimaLeitura.Format(time.RFC3339)
	}
	return r
}

// ------------------------------------------------------------------ HTTP

// agendaHTTP devolve um cliente que NAO segue redirecionamento: o iCloud
// responde a caminhos de calendario em outro host (pNN-caldav.icloud.com), e
// o http.Client do Go trocaria PROPFIND/REPORT por GET num 301/302. Nos
// resolvemos o host na descoberta e falamos direto com ele.
func agendaHTTP() *http.Client {
	return &http.Client{
		Timeout: agendaTimeoutHTTP,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func agendaRequisicao(ctx context.Context, hc *http.Client, id, senha, metodo, u string, cabecalhos map[string]string, corpo string) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, metodo, u, strings.NewReader(corpo))
	if err != nil {
		return 0, nil, nil, err
	}
	req.SetBasicAuth(id, senha)
	req.Header.Set("User-Agent", agendaUserAgent)
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, nil, agendaErroSinal(fmt.Errorf("sem resposta da Apple: %w", err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, resp.Header, b, agendaErroSenha(fmt.Errorf("a Apple recusou a senha de app (HTTP %d)", resp.StatusCode))
	case resp.StatusCode >= 500:
		return resp.StatusCode, resp.Header, b, agendaErroSinal(fmt.Errorf("a Apple respondeu HTTP %d", resp.StatusCode))
	}
	return resp.StatusCode, resp.Header, b, nil
}

// ------------------------------------------------------------- descoberta

// XML do multistatus (so o que usamos). Namespaces: DAV:, caldav, apple.
type agendaMultistatus struct {
	XMLName   xml.Name           `xml:"DAV: multistatus"`
	Responses []agendaMSResponse `xml:"DAV: response"`
}

type agendaMSResponse struct {
	Href      string `xml:"DAV: href"`
	Propstats []struct {
		Status string `xml:"DAV: status"`
		Prop   struct {
			CurrentUserPrincipal struct {
				Href string `xml:"DAV: href"`
			} `xml:"DAV: current-user-principal"`
			CalendarHomeSet struct {
				Href string `xml:"DAV: href"`
			} `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
			DisplayName  string `xml:"DAV: displayname"`
			ResourceType struct {
				Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
			} `xml:"DAV: resourcetype"`
			CompSet struct {
				Comps []struct {
					Name string `xml:"name,attr"`
				} `xml:"urn:ietf:params:xml:ns:caldav comp"`
			} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
			Color      string `xml:"http://apple.com/ns/ical/ calendar-color"`
			Privileges struct {
				Privs []struct {
					Write        *struct{} `xml:"DAV: write"`
					WriteContent *struct{} `xml:"DAV: write-content"`
					Bind         *struct{} `xml:"DAV: bind"`
					All          *struct{} `xml:"DAV: all"`
				} `xml:"DAV: privilege"`
			} `xml:"DAV: current-user-privilege-set"`
			CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
			ETag         string `xml:"DAV: getetag"`
		} `xml:"DAV: prop"`
	} `xml:"DAV: propstat"`
}

func agendaPropfind(ctx context.Context, hc *http.Client, id, senha, u, profundidade, corpo string) (*agendaMultistatus, error) {
	st, _, b, err := agendaRequisicao(ctx, hc, id, senha, "PROPFIND", u,
		map[string]string{"Depth": profundidade, "Content-Type": "application/xml; charset=utf-8"}, corpo)
	if err != nil {
		return nil, err
	}
	if st != http.StatusMultiStatus {
		return nil, agendaErroSinal(fmt.Errorf("PROPFIND %s: HTTP %d", agendaCaminho(u), st))
	}
	var ms agendaMultistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, agendaErroSinal(fmt.Errorf("resposta da Apple ilegível: %w", err))
	}
	return &ms, nil
}

// agendaResolver: href absoluto ou caminho, resolvido contra a URL pedida.
func agendaResolver(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	bu, err := url.Parse(base)
	if err != nil {
		return href
	}
	hu, err := url.Parse(href)
	if err != nil {
		return href
	}
	return bu.ResolveReference(hu).String()
}

func agendaCaminho(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Path
	}
	return u
}

// agendaDescobrir faz os tres PROPFINDs: principal, calendar-home-set (que
// no iCloud vem com o host pNN) e a lista de calendarios do home.
func agendaDescobrir(ctx context.Context, id, senha string) (string, []agendaCalendario, error) {
	hc := agendaHTTP()

	ms, err := agendaPropfind(ctx, hc, id, senha, agendaDiscoverURL, "0",
		`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`)
	if err != nil {
		return "", nil, err
	}
	principal := ""
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if h := ps.Prop.CurrentUserPrincipal.Href; h != "" {
				principal = agendaResolver(agendaDiscoverURL, h)
			}
		}
	}
	if principal == "" {
		return "", nil, agendaErroSinal(errors.New("a Apple não devolveu o principal da conta"))
	}

	ms, err = agendaPropfind(ctx, hc, id, senha, principal, "0",
		`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/></d:prop></d:propfind>`)
	if err != nil {
		return "", nil, err
	}
	home := ""
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if h := ps.Prop.CalendarHomeSet.Href; h != "" {
				home = agendaResolver(principal, h)
			}
		}
	}
	if home == "" {
		return "", nil, agendaErroSinal(errors.New("a Apple não devolveu a pasta de calendários"))
	}
	if !strings.HasSuffix(home, "/") {
		home += "/"
	}

	cals, err := agendaListarCalendarios(ctx, hc, id, senha, home)
	if err != nil {
		return "", nil, err
	}
	return home, cals, nil
}

func agendaListarCalendarios(ctx context.Context, hc *http.Client, id, senha, home string) ([]agendaCalendario, error) {
	ms, err := agendaPropfind(ctx, hc, id, senha, home, "1",
		`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:a="http://apple.com/ns/ical/"><d:prop><d:displayname/><d:resourcetype/><c:supported-calendar-component-set/><a:calendar-color/><d:current-user-privilege-set/></d:prop></d:propfind>`)
	if err != nil {
		return nil, err
	}
	return agendaCalendariosDoMultistatus(home, ms), nil
}

// agendaCalendariosDoMultistatus filtra o que e calendario de EVENTOS. Os de
// VTODO (Lembretes) ficam fora na v1. Calendario sem direito de escrita
// (compartilhado por outra pessoa) vem marcado so_leitura.
func agendaCalendariosDoMultistatus(home string, ms *agendaMultistatus) []agendaCalendario {
	var out []agendaCalendario
	for _, r := range ms.Responses {
		u := agendaResolver(home, r.Href)
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		if u == home {
			continue
		}
		var c agendaCalendario
		ehCalendario, temEvento, temComp, escreve := false, false, false, false
		for _, ps := range r.Propstats {
			if !strings.Contains(ps.Status, " 200") {
				continue
			}
			p := ps.Prop
			if p.ResourceType.Calendar != nil {
				ehCalendario = true
			}
			if p.DisplayName != "" {
				c.Nome = p.DisplayName
			}
			for _, comp := range p.CompSet.Comps {
				temComp = true
				if strings.EqualFold(comp.Name, "VEVENT") {
					temEvento = true
				}
			}
			if p.Color != "" {
				c.Cor = strings.TrimSpace(p.Color)
			}
			for _, pr := range p.Privileges.Privs {
				if pr.Write != nil || pr.WriteContent != nil || pr.Bind != nil || pr.All != nil {
					escreve = true
				}
			}
		}
		if !ehCalendario || (temComp && !temEvento) {
			continue
		}
		// Pastas internas do iCloud (inbox, outbox, notification) nao sao
		// calendarios; a resourcetype ja as exclui, mas por garantia:
		nome := strings.ToLower(strings.Trim(agendaCaminho(u), "/"))
		for _, interno := range []string{"inbox", "outbox", "notification", "freebusy"} {
			if strings.HasSuffix(nome, "/"+interno) {
				ehCalendario = false
			}
		}
		if !ehCalendario {
			continue
		}
		c.URL = u
		if c.Nome == "" {
			c.Nome = "Calendário"
		}
		c.SoLeitura = !escreve
		c.Ler = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Nome < out[j].Nome })
	return out
}

// ------------------------------------------------------------------ leitura

func agendaCalDAVData(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// agendaReport faz o calendar-query com expand (o iCloud expande as
// recorrencias em instancias com RECURRENCE-ID e datas em UTC).
func agendaReport(ctx context.Context, hc *http.Client, id, senha, calURL string, de, ate time.Time) ([]string, error) {
	corpo := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data><c:expand start="%[1]s" end="%[2]s"/></c:calendar-data></d:prop>
  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="%[1]s" end="%[2]s"/></c:comp-filter></c:comp-filter></c:filter>
</c:calendar-query>`, agendaCalDAVData(de), agendaCalDAVData(ate))
	st, _, b, err := agendaRequisicao(ctx, hc, id, senha, "REPORT", calURL,
		map[string]string{"Depth": "1", "Content-Type": "application/xml; charset=utf-8"}, corpo)
	if err != nil {
		return nil, err
	}
	if st != http.StatusMultiStatus {
		return nil, agendaErroSinal(fmt.Errorf("REPORT %s: HTTP %d", agendaCaminho(calURL), st))
	}
	var ms agendaMultistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, agendaErroSinal(fmt.Errorf("resposta da Apple ilegível: %w", err))
	}
	var ics []string
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if strings.TrimSpace(ps.Prop.CalendarData) != "" {
				ics = append(ics, ps.Prop.CalendarData)
			}
		}
	}
	return ics, nil
}

// agendaEventosDoICS converte um VCALENDAR (ja expandido ou nao) em eventos
// no fuso local. Cada objeto e decodificado sozinho: um .ics estranho nao
// derruba a leitura inteira.
func agendaEventosDoICS(texto, calendario string, de, ate time.Time, loc *time.Location) []agendaEvento {
	cal, err := ical.NewDecoder(strings.NewReader(texto)).Decode()
	if err != nil {
		if panel.logger != nil {
			panel.logger.Debugf("Agenda da Apple: .ics ignorado: %v", err)
		}
		return nil
	}
	var out []agendaEvento
	for _, comp := range cal.Children {
		if comp.Name != ical.CompEvent {
			continue
		}
		ev := agendaEventoDoComponente(comp, calendario, loc)
		if ev == nil {
			continue
		}
		if !de.IsZero() && !ev.fim.After(de) {
			continue
		}
		if !ate.IsZero() && !ev.inicio.Before(ate) {
			continue
		}
		out = append(out, *ev)
	}
	return out
}

func agendaDataProp(p *ical.Prop, loc *time.Location) (time.Time, bool, error) {
	if p == nil {
		return time.Time{}, false, errors.New("sem data")
	}
	diaInteiro := p.ValueType() == ical.ValueDate || (p.ValueType() == ical.ValueDefault && len(p.Value) == 8)
	t, err := p.DateTime(loc)
	if err != nil {
		// TZID desconhecido (nome nao-IANA): le como hora local.
		v := p.Value
		if diaInteiro {
			t, err = time.ParseInLocation("20060102", v, loc)
		} else if strings.HasSuffix(v, "Z") {
			t, err = time.ParseInLocation("20060102T150405Z", v, time.UTC)
		} else {
			t, err = time.ParseInLocation("20060102T150405", v, loc)
		}
		if err != nil {
			return time.Time{}, false, err
		}
	}
	return t.In(loc), diaInteiro, nil
}

func agendaEventoDoComponente(comp *ical.Component, calendario string, loc *time.Location) *agendaEvento {
	if s, _ := comp.Props.Text(ical.PropStatus); strings.EqualFold(s, "CANCELLED") {
		return nil
	}
	inicio, diaInteiro, err := agendaDataProp(comp.Props.Get(ical.PropDateTimeStart), loc)
	if err != nil {
		return nil
	}
	var fim time.Time
	if p := comp.Props.Get(ical.PropDateTimeEnd); p != nil {
		fim, _, err = agendaDataProp(p, loc)
		if err != nil {
			fim = time.Time{}
		}
	} else if p := comp.Props.Get(ical.PropDuration); p != nil {
		if d, err := p.Duration(); err == nil {
			fim = inicio.Add(d)
		}
	}
	if fim.IsZero() {
		if diaInteiro {
			fim = inicio.Add(24 * time.Hour)
		} else {
			fim = inicio
		}
	}
	if diaInteiro {
		// DATE vem a meia-noite do fuso dado; normaliza para a meia-noite local
		inicio = time.Date(inicio.Year(), inicio.Month(), inicio.Day(), 0, 0, 0, 0, loc)
		fim = time.Date(fim.Year(), fim.Month(), fim.Day(), 0, 0, 0, 0, loc)
		if !fim.After(inicio) {
			fim = inicio.Add(24 * time.Hour)
		}
	}
	uid, _ := comp.Props.Text(ical.PropUID)
	titulo, _ := comp.Props.Text(ical.PropSummary)
	local, _ := comp.Props.Text(ical.PropLocation)
	notas, _ := comp.Props.Text(ical.PropDescription)
	transp, _ := comp.Props.Text(ical.PropTransparency)
	ev := &agendaEvento{
		UID:        uid,
		Titulo:     strings.TrimSpace(titulo),
		DiaInteiro: diaInteiro,
		Calendario: calendario,
		Local:      strings.TrimSpace(local),
		Notas:      beatitudeCurto(strings.TrimSpace(notas), 160),
		Livre:      strings.EqualFold(transp, "TRANSPARENT"),
		inicio:     inicio,
		fim:        fim,
	}
	if ev.Titulo == "" {
		ev.Titulo = "(sem título)"
	}
	for _, a := range comp.Props.Values(ical.PropAttendee) {
		nome := strings.TrimSpace(a.Params.Get(ical.ParamCommonName))
		if nome == "" {
			nome = strings.TrimPrefix(strings.ToLower(a.Value), "mailto:")
		}
		if nome != "" {
			ev.Convidados = append(ev.Convidados, nome)
		}
	}
	agenda.mu.Lock()
	ev.Beatitude = strings.HasPrefix(uid, agendaUIDPrefixo) || agenda.criados[uid]
	agenda.mu.Unlock()
	ev.Inicio = inicio.Format(time.RFC3339)
	ev.Fim = fim.Format(time.RFC3339)
	ev.Quando = agendaQuando(inicio, fim, diaInteiro)
	return ev
}

var agendaDiasSemana = []string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

func agendaQuando(inicio, fim time.Time, diaInteiro bool) string {
	dia := fmt.Sprintf("%s %02d/%02d", agendaDiasSemana[inicio.Weekday()], inicio.Day(), int(inicio.Month()))
	if diaInteiro {
		if fim.Sub(inicio) > 24*time.Hour {
			ult := fim.Add(-time.Second)
			return fmt.Sprintf("%s a %s %02d/%02d, dia inteiro", dia, agendaDiasSemana[ult.Weekday()], ult.Day(), int(ult.Month()))
		}
		return dia + ", dia inteiro"
	}
	if fim.Equal(inicio) {
		return fmt.Sprintf("%s %s", dia, inicio.Format("15:04"))
	}
	if inicio.YearDay() == fim.YearDay() && inicio.Year() == fim.Year() {
		return fmt.Sprintf("%s %s–%s", dia, inicio.Format("15:04"), fim.Format("15:04"))
	}
	return fmt.Sprintf("%s %s – %s %02d/%02d %s", dia, inicio.Format("15:04"),
		agendaDiasSemana[fim.Weekday()], fim.Day(), int(fim.Month()), fim.Format("15:04"))
}

// agendaLerData aceita "2026-10-16", "2026-10-16T14:00", "2026-10-16 14:00"
// e RFC 3339. Sem fuso, vale o da maquina. fimDoDia: data solta vira 23:59:59.
func agendaLerData(s string, fimDoDia bool, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("data vazia")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), nil
	}
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		if fimDoDia {
			return t.Add(24*time.Hour - time.Second), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("data em formato desconhecido: %q (use 2026-10-16 ou 2026-10-16T14:00)", s)
}

// agendaCredenciais devolve uma copia do que a leitura precisa.
func agendaCredenciais() (id, senha, home string, cals []agendaCalendario, principal string, ok bool) {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	if agenda.cfg.SenhaApp == "" || agenda.cfg.HomeURL == "" || agenda.cfg.Estado == agendaEstadoSenha {
		return "", "", "", nil, "", false
	}
	cals = append(cals, agenda.cfg.Calendarios...)
	return agenda.cfg.AppleID, agenda.cfg.SenhaApp, agenda.cfg.HomeURL, cals, agenda.cfg.Principal, true
}

// agendaAcharCalendario: por nome (sem acento, sem caixa) ou URL.
func agendaAcharCalendario(cals []agendaCalendario, ref string) *agendaCalendario {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	chave := strings.ToLower(ponteSemAcento(ref))
	for i := range cals {
		if cals[i].URL == ref || strings.ToLower(ponteSemAcento(cals[i].Nome)) == chave {
			return &cals[i]
		}
	}
	for i := range cals {
		if strings.Contains(strings.ToLower(ponteSemAcento(cals[i].Nome)), chave) {
			return &cals[i]
		}
	}
	return nil
}

// agendaListar le os calendarios marcados "ler" no intervalo, com cache de
// 10 minutos por (intervalo, calendario). Resultado ordenado por inicio.
func agendaListar(ctx context.Context, de, ate time.Time, calendario string) ([]agendaEvento, error) {
	id, senha, _, cals, _, ok := agendaCredenciais()
	if !ok {
		return nil, agendaErroSenha(errors.New("Agenda da Apple não conectada"))
	}
	if !ate.After(de) {
		return nil, errors.New("o fim precisa ser depois do início")
	}
	if ate.Sub(de) > agendaJanelaMax {
		return nil, fmt.Errorf("janela acima de %d dias; peça um intervalo menor", int(agendaJanelaMax.Hours()/24))
	}
	var alvo []agendaCalendario
	if calendario != "" {
		c := agendaAcharCalendario(cals, calendario)
		if c == nil {
			return nil, fmt.Errorf("calendário %q não encontrado", calendario)
		}
		alvo = []agendaCalendario{*c}
	} else {
		for _, c := range cals {
			if c.Ler {
				alvo = append(alvo, c)
			}
		}
	}
	chave := fmt.Sprintf("%s|%s|%s", de.UTC().Format(time.RFC3339), ate.UTC().Format(time.RFC3339), calendario)
	agenda.mu.Lock()
	if it, ok := agenda.cache[chave]; ok && time.Since(it.em) < agendaCacheTTL {
		agenda.mu.Unlock()
		return it.eventos, nil
	}
	agenda.mu.Unlock()

	hc := agendaHTTP()
	loc := time.Local
	var todos []agendaEvento
	inicio := time.Now()
	for _, c := range alvo {
		ics, err := agendaReport(ctx, hc, id, senha, c.URL, de, ate)
		if err != nil {
			agendaRegistrarResultado(err)
			return nil, err
		}
		for _, texto := range ics {
			todos = append(todos, agendaEventosDoICS(texto, c.Nome, de, ate, loc)...)
		}
	}
	sort.SliceStable(todos, func(i, j int) bool {
		if todos[i].inicio.Equal(todos[j].inicio) {
			return todos[i].Titulo < todos[j].Titulo
		}
		return todos[i].inicio.Before(todos[j].inicio)
	})
	if len(todos) > agendaMaxEventos {
		todos = todos[:agendaMaxEventos]
	}
	agendaRegistrarResultado(nil)
	if panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: %d evento(s) lido(s) em %d calendário(s), %.1f s",
			len(todos), len(alvo), time.Since(inicio).Seconds())
	}
	agenda.mu.Lock()
	agenda.cache[chave] = agendaCacheItem{em: time.Now(), eventos: todos}
	agenda.mu.Unlock()
	return todos, nil
}

func agendaBuscar(ctx context.Context, texto string, de, ate time.Time) ([]agendaEvento, error) {
	chave := strings.ToLower(ponteSemAcento(strings.TrimSpace(texto)))
	if chave == "" {
		return nil, errors.New("diga o que procurar")
	}
	todos, err := agendaListar(ctx, de, ate, "")
	if err != nil {
		return nil, err
	}
	var out []agendaEvento
	for _, ev := range todos {
		alvo := strings.ToLower(ponteSemAcento(ev.Titulo + " " + ev.Local + " " + ev.Notas + " " + strings.Join(ev.Convidados, " ")))
		if strings.Contains(alvo, chave) {
			out = append(out, ev)
			if len(out) >= agendaMaxBusca {
				break
			}
		}
	}
	return out, nil
}

// agendaLivre: janelas livres de pelo menos duracao, entre janela[0] e
// janela[1] de cada dia pedido. Eventos de dia inteiro e TRANSPARENT nao
// ocupam. diasSemana: 0=dom … 6=sab; vazio = todos.
func agendaLivre(eventos []agendaEvento, de, ate time.Time, duracao time.Duration, janelaIni, janelaFim string, diasSemana map[int]bool, loc *time.Location) []agendaJanelaLivre {
	hi, mi, ok1 := parseHM(janelaIni)
	hf, mf, ok2 := parseHM(janelaFim)
	if !ok1 || !ok2 {
		hi, mi, hf, mf = 8, 0, 20, 0
	}
	var ocupados [][2]time.Time
	for _, ev := range eventos {
		if ev.DiaInteiro || ev.Livre || !ev.fim.After(ev.inicio) {
			continue
		}
		ocupados = append(ocupados, [2]time.Time{ev.inicio, ev.fim})
	}
	sort.Slice(ocupados, func(i, j int) bool { return ocupados[i][0].Before(ocupados[j][0]) })

	var out []agendaJanelaLivre
	agora := time.Now().In(loc)
	for dia := time.Date(de.Year(), de.Month(), de.Day(), 0, 0, 0, 0, loc); !dia.After(ate); dia = dia.AddDate(0, 0, 1) {
		if len(diasSemana) > 0 && !diasSemana[int(dia.Weekday())] {
			continue
		}
		cursor := time.Date(dia.Year(), dia.Month(), dia.Day(), hi, mi, 0, 0, loc)
		fimDia := time.Date(dia.Year(), dia.Month(), dia.Day(), hf, mf, 0, 0, loc)
		if cursor.Before(de) {
			cursor = de
		}
		if fimDia.After(ate) {
			fimDia = ate
		}
		if cursor.Before(agora) {
			// nunca sugerir o passado; arredonda para o proximo quarto de hora
			cursor = agora.Truncate(15 * time.Minute).Add(15 * time.Minute)
		}
		for _, oc := range ocupados {
			if !oc[1].After(cursor) {
				continue
			}
			if !oc[0].Before(fimDia) {
				break
			}
			if oc[0].Sub(cursor) >= duracao {
				out = append(out, agendaJanela(cursor, oc[0]))
			}
			if oc[1].After(cursor) {
				cursor = oc[1]
			}
		}
		if fimDia.Sub(cursor) >= duracao {
			out = append(out, agendaJanela(cursor, fimDia))
		}
		if len(out) >= agendaMaxLivres {
			break
		}
	}
	return out
}

func agendaJanela(a, b time.Time) agendaJanelaLivre {
	return agendaJanelaLivre{
		Inicio: a.Format(time.RFC3339),
		Fim:    b.Format(time.RFC3339),
		Quando: agendaQuando(a, b, false),
		Min:    int(b.Sub(a).Minutes()),
	}
}

// ------------------------------------------------------------------ escrita

func agendaNovoUID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return agendaUIDPrefixo + hex.EncodeToString(b)
}

// agendaMontarICS monta o VCALENDAR de um evento novo. Sem ATTENDEE, sempre.
func agendaMontarICS(uid string, p agendaPedidoCriar, inicio, fim time.Time) (string, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//Lumnia//Lumnia Zap//PT")
	cal.Props.SetText(ical.PropVersion, "2.0")
	ev := ical.NewEvent()
	ev.Props.SetText(ical.PropUID, uid)
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	ev.Props.SetText(ical.PropSummary, strings.TrimSpace(p.Titulo))
	if p.DiaInteiro {
		ev.Props.SetDate(ical.PropDateTimeStart, inicio)
		ev.Props.SetDate(ical.PropDateTimeEnd, fim)
	} else {
		// UTC com Z: o iPhone mostra no fuso em que a pessoa estiver, e nao
		// dependemos de o iCloud conhecer o nome do fuso da maquina.
		ev.Props.SetDateTime(ical.PropDateTimeStart, inicio.UTC())
		ev.Props.SetDateTime(ical.PropDateTimeEnd, fim.UTC())
	}
	if s := strings.TrimSpace(p.Local); s != "" {
		ev.Props.SetText(ical.PropLocation, s)
	}
	if s := strings.TrimSpace(p.Notas); s != "" {
		ev.Props.SetText(ical.PropDescription, s)
	}
	if p.AlertaMin > 0 {
		al := ical.NewComponent(ical.CompAlarm)
		al.Props.SetText(ical.PropAction, "DISPLAY")
		al.Props.SetText(ical.PropDescription, strings.TrimSpace(p.Titulo))
		tr := ical.NewProp(ical.PropTrigger)
		tr.Value = fmt.Sprintf("-PT%dM", p.AlertaMin) // legivel no .ics; o iCloud aceita
		al.Props.Set(tr)
		ev.Children = append(ev.Children, al)
	}
	cal.Children = append(cal.Children, ev.Component)
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// agendaCriar grava um evento novo. Convidados: recusado (degrau 2 fica
// para a v2; no iCloud gravar com ATTENDEE ja envia o convite).
func agendaCriar(ctx context.Context, p agendaPedidoCriar) (*agendaEvento, error) {
	id, senha, _, cals, principal, ok := agendaCredenciais()
	if !ok {
		return nil, agendaErroSenha(errors.New("Agenda da Apple não conectada"))
	}
	if len(p.Convidados) > 0 {
		return nil, errors.New("convidados ainda não: crie o compromisso sem convidados e a pessoa convida pelo iPhone, ou deixe o texto do convite pronto para ela")
	}
	if strings.TrimSpace(p.Titulo) == "" {
		return nil, errors.New("o compromisso precisa de título")
	}
	loc := time.Local
	inicio, err := agendaLerData(p.Inicio, false, loc)
	if err != nil {
		return nil, fmt.Errorf("início: %w", err)
	}
	var fim time.Time
	if strings.TrimSpace(p.Fim) == "" {
		if p.DiaInteiro {
			fim = inicio.AddDate(0, 0, 1)
		} else {
			fim = inicio.Add(time.Hour)
		}
	} else {
		fim, err = agendaLerData(p.Fim, false, loc)
		if err != nil {
			return nil, fmt.Errorf("fim: %w", err)
		}
	}
	if p.DiaInteiro {
		inicio = time.Date(inicio.Year(), inicio.Month(), inicio.Day(), 0, 0, 0, 0, loc)
		fim = time.Date(fim.Year(), fim.Month(), fim.Day(), 0, 0, 0, 0, loc)
		if !fim.After(inicio) {
			fim = inicio.AddDate(0, 0, 1)
		}
	}
	if !fim.After(inicio) {
		return nil, errors.New("o fim precisa ser depois do início")
	}
	if fim.Sub(inicio) > 14*24*time.Hour {
		return nil, errors.New("compromisso de mais de 14 dias; confira as datas")
	}

	var alvo *agendaCalendario
	if strings.TrimSpace(p.Calendario) != "" {
		alvo = agendaAcharCalendario(cals, p.Calendario)
		if alvo == nil {
			return nil, fmt.Errorf("calendário %q não encontrado", p.Calendario)
		}
		if !alvo.Ler {
			return nil, fmt.Errorf("o calendário %q está desligado para o Beatitude", alvo.Nome)
		}
	} else {
		for i := range cals {
			if cals[i].URL == principal {
				alvo = &cals[i]
			}
		}
		if alvo == nil {
			for i := range cals {
				if cals[i].Ler && !cals[i].SoLeitura {
					alvo = &cals[i]
					break
				}
			}
		}
		if alvo == nil {
			return nil, errors.New("nenhum calendário principal escolhido na página Agenda da Apple")
		}
	}
	if alvo.SoLeitura {
		return nil, fmt.Errorf("o calendário %q é somente leitura (compartilhado por outra pessoa)", alvo.Nome)
	}

	uid := agendaNovoUID()
	ics, err := agendaMontarICS(uid, p, inicio, fim)
	if err != nil {
		return nil, err
	}
	hc := agendaHTTP()
	st, _, b, err := agendaRequisicao(ctx, hc, id, senha, http.MethodPut, alvo.URL+uid+".ics",
		map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"}, ics)
	if err != nil {
		agendaRegistrarResultado(err)
		return nil, err
	}
	if st != http.StatusCreated && st != http.StatusNoContent && st != http.StatusOK {
		err := fmt.Errorf("a Apple não aceitou o compromisso (HTTP %d: %s)", st, beatitudeCurto(strings.TrimSpace(string(b)), 160))
		return nil, err
	}
	agenda.mu.Lock()
	agenda.criados[uid] = true
	agendaSalvarCriadosLocked()
	agenda.cache = map[string]agendaCacheItem{} // o proximo listar ve o novo
	agenda.mu.Unlock()
	agendaRegistrarResultado(nil)
	if panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: compromisso criado em %q (%s)", alvo.Nome, agendaQuando(inicio, fim, p.DiaInteiro))
	}
	ponteAgendaDepois() // o brief ve o compromisso novo no proximo ciclo da ponte
	ev := &agendaEvento{
		UID: uid, Titulo: strings.TrimSpace(p.Titulo), DiaInteiro: p.DiaInteiro, Calendario: alvo.Nome,
		Local: strings.TrimSpace(p.Local), Notas: beatitudeCurto(strings.TrimSpace(p.Notas), 160), Beatitude: true,
		Inicio: inicio.Format(time.RFC3339), Fim: fim.Format(time.RFC3339), Quando: agendaQuando(inicio, fim, p.DiaInteiro),
		inicio: inicio, fim: fim,
	}
	return ev, nil
}

// ------------------------------------------------------------ configuracao

// agendaTestar valida a senha e devolve os calendarios, sem gravar nada.
func agendaTestar(ctx context.Context, id, senha string) (string, []agendaCalendario, error) {
	id = strings.TrimSpace(id)
	senha = agendaNormalizarSenha(senha)
	if id == "" || senha == "" {
		return "", nil, errors.New("informe o e-mail da conta Apple e a senha de app")
	}
	return agendaDescobrir(ctx, id, senha)
}

// agendaNormalizarSenha: a Apple mostra "xxxx-xxxx-xxxx-xxxx"; aceita com ou
// sem hifens, com espacos colados.
func agendaNormalizarSenha(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	if len(strings.ReplaceAll(s, "-", "")) == 16 && !strings.Contains(s, "-") {
		s = s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
	}
	return s
}

// agendaConfigurar grava (ou atualiza) a conexao. Senha vazia mantem a atual.
func agendaConfigurar(ctx context.Context, id, senha string, ler map[string]bool, principal string) error {
	id = strings.TrimSpace(id)
	senha = agendaNormalizarSenha(senha)
	agenda.mu.Lock()
	atual := agenda.cfg
	agenda.mu.Unlock()
	if senha == "" {
		if atual.SenhaApp == "" || !strings.EqualFold(atual.AppleID, id) {
			return errors.New("informe a senha de app")
		}
		senha = atual.SenhaApp
	}
	home, cals, err := agendaDescobrir(ctx, id, senha)
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		return errors.New("a conta não tem calendário de compromissos no iCloud")
	}
	for i := range cals {
		if v, ok := ler[cals[i].URL]; ok {
			cals[i].Ler = v
		}
	}
	if principal == "" {
		principal = atual.Principal
	}
	achou := false
	for _, c := range cals {
		if c.URL == principal && c.Ler && !c.SoLeitura {
			achou = true
		}
	}
	if !achou {
		principal = ""
		for _, c := range cals {
			if c.Ler && !c.SoLeitura {
				principal = c.URL
				break
			}
		}
	}
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	agenda.cfg = agendaConfig{
		AppleID: id, SenhaApp: senha, HomeURL: home, Calendarios: cals, Principal: principal,
		VerificadoEm: time.Now(), Estado: agendaEstadoOK,
	}
	agenda.cache = map[string]agendaCacheItem{}
	if err := agendaSalvarLocked(); err != nil {
		return fmt.Errorf("não consegui gravar store/apple.json: %w", err)
	}
	if panel.logger != nil {
		panel.logger.Infof("Agenda da Apple: conectada (%s), %d calendário(s)", id, len(cals))
	}
	return nil
}

// agendaConfigPublica: a configuracao sem a senha, para a pagina e o MCP.
func agendaConfigPublica() map[string]interface{} {
	agenda.mu.Lock()
	defer agenda.mu.Unlock()
	estado := agenda.cfg.Estado
	if agenda.cfg.SenhaApp == "" {
		estado = agendaEstadoNao
	}
	r := map[string]interface{}{
		"estado":      estado,
		"apple_id":    agenda.cfg.AppleID,
		"calendarios": agenda.cfg.Calendarios,
		"principal":   agenda.cfg.Principal,
		"fuso":        time.Local.String(),
	}
	if agenda.cfg.Calendarios == nil {
		r["calendarios"] = []agendaCalendario{}
	}
	if !agenda.cfg.VerificadoEm.IsZero() {
		r["verificado_em"] = agenda.cfg.VerificadoEm.Format(time.RFC3339)
	}
	if !agenda.cfg.UltimaLeitura.IsZero() {
		r["ultima_leitura"] = agenda.cfg.UltimaLeitura.Format(time.RFC3339)
	}
	if agenda.cfg.UltimoErro != "" {
		r["ultimo_erro"] = agenda.cfg.UltimoErro
	}
	return r
}
