// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func agendaTestLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

const agendaTestPropfindCals = `<?xml version="1.0" encoding="UTF-8"?>
<multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="http://apple.com/ns/ical/">
 <response><href>/123456/calendars/</href><propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>
 <response><href>/123456/calendars/inbox/</href><propstat><prop><resourcetype><collection/><C:schedule-inbox/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>
 <response><href>/123456/calendars/home/</href>
  <propstat><prop>
   <displayname>Pessoal</displayname>
   <resourcetype><collection/><C:calendar/></resourcetype>
   <C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set>
   <A:calendar-color>#FF2D55FF</A:calendar-color>
   <current-user-privilege-set><privilege><read/></privilege><privilege><write/></privilege></current-user-privilege-set>
  </prop><status>HTTP/1.1 200 OK</status></propstat></response>
 <response><href>https://p123-caldav.icloud.com:443/123456/calendars/tarefas/</href>
  <propstat><prop>
   <displayname>Lembretes</displayname>
   <resourcetype><collection/><C:calendar/></resourcetype>
   <C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set>
   <current-user-privilege-set><privilege><read/></privilege><privilege><write/></privilege></current-user-privilege-set>
  </prop><status>HTTP/1.1 200 OK</status></propstat></response>
 <response><href>/123456/calendars/familia/</href>
  <propstat><prop>
   <displayname>Família (Lisiane)</displayname>
   <resourcetype><collection/><C:calendar/></resourcetype>
   <C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set>
   <current-user-privilege-set><privilege><read/></privilege></current-user-privilege-set>
  </prop><status>HTTP/1.1 200 OK</status></propstat>
  <propstat><prop><A:calendar-color/></prop><status>HTTP/1.1 404 Not Found</status></propstat></response>
</multistatus>`

func TestAgendaCalendariosDoMultistatus(t *testing.T) {
	var ms agendaMultistatus
	if err := xml.Unmarshal([]byte(agendaTestPropfindCals), &ms); err != nil {
		t.Fatal(err)
	}
	home := "https://p123-caldav.icloud.com/123456/calendars/"
	cals := agendaCalendariosDoMultistatus(home, &ms)
	if len(cals) != 2 {
		t.Fatalf("esperava 2 calendários de eventos, veio %d: %+v", len(cals), cals)
	}
	// ordenados por nome: "Família (Lisiane)" < "Pessoal"
	fam, pes := cals[0], cals[1]
	if fam.Nome != "Família (Lisiane)" || !fam.SoLeitura || fam.URL != home+"familia/" {
		t.Errorf("família errado: %+v", fam)
	}
	if pes.Nome != "Pessoal" || pes.SoLeitura || pes.Cor != "#FF2D55FF" || pes.URL != home+"home/" || !pes.Ler {
		t.Errorf("pessoal errado: %+v", pes)
	}
}

func TestAgendaResolver(t *testing.T) {
	casos := map[[2]string]string{
		{"https://caldav.icloud.com/", "/123/principal/"}:                                                "https://caldav.icloud.com/123/principal/",
		{"https://caldav.icloud.com/123/principal/", "https://p42-caldav.icloud.com:443/123/calendars/"}: "https://p42-caldav.icloud.com:443/123/calendars/",
		{"https://p42-caldav.icloud.com/123/calendars/", "/123/calendars/home/"}:                         "https://p42-caldav.icloud.com/123/calendars/home/",
	}
	for in, quer := range casos {
		if got := agendaResolver(in[0], in[1]); got != quer {
			t.Errorf("agendaResolver(%q, %q) = %q, queria %q", in[0], in[1], got, quer)
		}
	}
}

const agendaTestICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//iCloud//EN
BEGIN:VEVENT
UID:abc-1
DTSTAMP:20261001T120000Z
DTSTART:20261016T170000Z
DTEND:20261016T180000Z
RECURRENCE-ID:20261016T170000Z
SUMMARY:Reunião semanal
LOCATION:Sala 2
DESCRIPTION:Pauta: números do mês
ATTENDEE;CN=Lisiane;PARTSTAT=ACCEPTED:mailto:lisiane@example.com
ATTENDEE:mailto:fausto@example.com
END:VEVENT
BEGIN:VEVENT
UID:abc-2
DTSTAMP:20261001T120000Z
DTSTART;VALUE=DATE:20261018
DTEND;VALUE=DATE:20261019
SUMMARY:Aniversário da Paula
TRANSP:TRANSPARENT
END:VEVENT
BEGIN:VEVENT
UID:abc-3
DTSTAMP:20261001T120000Z
DTSTART;TZID=America/Sao_Paulo:20261017T090000
DTEND;TZID=America/Sao_Paulo:20261017T093000
SUMMARY:Cancelada
STATUS:CANCELLED
END:VEVENT
BEGIN:VEVENT
UID:beatitude-ff00
DTSTAMP:20261001T120000Z
DTSTART;TZID=America/Sao_Paulo:20261017T120000
DURATION:PT1H30M
SUMMARY:Almoço com o Fausto
END:VEVENT
END:VCALENDAR
`

func TestAgendaEventosDoICS(t *testing.T) {
	loc := agendaTestLoc(t)
	agenda.criados = map[string]bool{}
	de := time.Date(2026, 10, 15, 0, 0, 0, 0, loc)
	ate := time.Date(2026, 10, 22, 0, 0, 0, 0, loc)
	evs := agendaEventosDoICS(agendaTestICS, "Pessoal", de, ate, loc)
	if len(evs) != 3 {
		t.Fatalf("esperava 3 eventos (cancelado fora), veio %d: %+v", len(evs), evs)
	}
	r := evs[0]
	if r.Titulo != "Reunião semanal" || r.Inicio != "2026-10-16T14:00:00-03:00" || r.Fim != "2026-10-16T15:00:00-03:00" {
		t.Errorf("reunião: %+v", r)
	}
	if r.Quando != "sex 16/10 14:00–15:00" {
		t.Errorf("quando: %q", r.Quando)
	}
	if len(r.Convidados) != 2 || r.Convidados[0] != "Lisiane" || r.Convidados[1] != "fausto@example.com" {
		t.Errorf("convidados: %v", r.Convidados)
	}
	if r.Local != "Sala 2" || r.Notas != "Pauta: números do mês" || r.Beatitude {
		t.Errorf("campos: %+v", r)
	}
	a := evs[1]
	if !a.DiaInteiro || a.Inicio != "2026-10-18T00:00:00-03:00" || a.Quando != "dom 18/10, dia inteiro" || !a.Livre {
		t.Errorf("aniversário: %+v", a)
	}
	b := evs[2]
	if !b.Beatitude || b.Fim != "2026-10-17T13:30:00-03:00" || b.Quando != "sáb 17/10 12:00–13:30" {
		t.Errorf("almoço: %+v", b)
	}
}

func TestAgendaLerData(t *testing.T) {
	loc := agendaTestLoc(t)
	casos := []struct {
		in     string
		fimDia bool
		quer   string
	}{
		{"2026-10-16", false, "2026-10-16T00:00:00-03:00"},
		{"2026-10-16", true, "2026-10-16T23:59:59-03:00"},
		{"2026-10-16T14:00", false, "2026-10-16T14:00:00-03:00"},
		{"2026-10-16 14:30", false, "2026-10-16T14:30:00-03:00"},
		{"2026-10-16T14:00:00Z", false, "2026-10-16T11:00:00-03:00"},
		{"2026-10-16T14:00:00-03:00", false, "2026-10-16T14:00:00-03:00"},
	}
	for _, c := range casos {
		got, err := agendaLerData(c.in, c.fimDia, loc)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.Format(time.RFC3339) != c.quer {
			t.Errorf("%q -> %s, queria %s", c.in, got.Format(time.RFC3339), c.quer)
		}
	}
	if _, err := agendaLerData("amanhã", false, loc); err == nil {
		t.Error("devia recusar 'amanhã'")
	}
}

func TestAgendaLivre(t *testing.T) {
	loc := agendaTestLoc(t)
	// um dia no futuro distante, para "nunca sugerir o passado" nao interferir
	dia := time.Date(2099, 3, 10, 0, 0, 0, 0, loc) // terça
	mk := func(h1, m1, h2, m2 int, livre, diaInteiro bool) agendaEvento {
		return agendaEvento{
			inicio: time.Date(2099, 3, 10, h1, m1, 0, 0, loc), fim: time.Date(2099, 3, 10, h2, m2, 0, 0, loc),
			Livre: livre, DiaInteiro: diaInteiro,
		}
	}
	evs := []agendaEvento{
		mk(9, 0, 10, 30, false, false),
		mk(10, 0, 11, 0, false, false), // sobreposto
		mk(14, 0, 15, 0, false, false),
		mk(0, 0, 0, 0, true, true),    // dia inteiro: nao ocupa
		mk(16, 0, 17, 0, true, false), // transparente: nao ocupa
	}
	livres := agendaLivre(evs, dia, dia.Add(24*time.Hour-time.Second), 60*time.Minute, "08:00", "18:00", nil, loc)
	quer := []string{"08:00–09:00", "11:00–14:00", "15:00–18:00"}
	if len(livres) != len(quer) {
		t.Fatalf("janelas: %+v", livres)
	}
	for i, q := range quer {
		if !strings.HasSuffix(livres[i].Quando, q) {
			t.Errorf("janela %d: %q, queria sufixo %q", i, livres[i].Quando, q)
		}
	}
	// 90 minutos: a primeira janela (60) some
	livres = agendaLivre(evs, dia, dia.Add(24*time.Hour-time.Second), 90*time.Minute, "08:00", "18:00", nil, loc)
	if len(livres) != 2 || livres[0].Min != 180 {
		t.Errorf("90 min: %+v", livres)
	}
	// so quarta: nada nesse dia
	livres = agendaLivre(evs, dia, dia.Add(24*time.Hour-time.Second), 30*time.Minute, "08:00", "18:00", map[int]bool{3: true}, loc)
	if len(livres) != 0 {
		t.Errorf("dias_semana: %+v", livres)
	}
}

func TestAgendaMontarICS(t *testing.T) {
	loc := agendaTestLoc(t)
	ini := time.Date(2026, 10, 20, 12, 0, 0, 0, loc)
	ics, err := agendaMontarICS("beatitude-0102", agendaPedidoCriar{
		Titulo: "Almoço com o Fausto", Local: "Figueira", Notas: "levar o contrato", AlertaMin: 30,
		Convidados: []string{"ignorado@example.com"}, // montar nunca grava ATTENDEE
	}, ini, ini.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, linha := range []string{
		"BEGIN:VEVENT", "UID:beatitude-0102", "SUMMARY:Almoço com o Fausto", "DTSTART:20261020T150000Z",
		"DTEND:20261020T163000Z", "LOCATION:Figueira", "DESCRIPTION:levar o contrato", "BEGIN:VALARM", "TRIGGER:-PT30M",
	} {
		if !strings.Contains(ics, linha) {
			t.Errorf("faltou %q em:\n%s", linha, ics)
		}
	}
	if strings.Contains(ics, "ATTENDEE") {
		t.Error("ATTENDEE nunca pode sair da ponte")
	}
	// dia inteiro
	ics, err = agendaMontarICS("beatitude-0103", agendaPedidoCriar{Titulo: "Viagem", DiaInteiro: true}, ini, ini.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ics, "DTSTART;VALUE=DATE:20261020") || !strings.Contains(ics, "DTEND;VALUE=DATE:20261022") {
		t.Errorf("dia inteiro:\n%s", ics)
	}
}

func TestAgendaNormalizarSenha(t *testing.T) {
	for in, quer := range map[string]string{
		"abcd-efgh-ijkl-mnop":   "abcd-efgh-ijkl-mnop",
		" abcdefghijklmnop ":    "abcd-efgh-ijkl-mnop",
		"abcd efgh ijkl mnop":   "abcd-efgh-ijkl-mnop",
		"abcd-efgh-ijkl-mnop\n": "abcd-efgh-ijkl-mnop",
	} {
		if got := agendaNormalizarSenha(in); got != quer {
			t.Errorf("%q -> %q, queria %q", in, got, quer)
		}
	}
}

// agendaServidorFalso imita o bastante do iCloud: descoberta em dois hosts
// (o segundo "pNN" e o mesmo servidor, com prefixo /p42), PROPFIND dos
// calendarios, REPORT e PUT. Senha "boa" passa; qualquer outra, 401.
func agendaServidorFalso(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var puts []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "diego@example.com" || p != "abcd-efgh-ijkl-mnop" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		corpo, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/" && strings.Contains(string(corpo), "current-user-principal"):
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, `<?xml version="1.0"?><multistatus xmlns="DAV:"><response><href>/</href><propstat><prop><current-user-principal><href>/123456/principal/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/123456/principal/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><response><href>/123456/principal/</href><propstat><prop><C:calendar-home-set><href>%s/p42/123456/calendars/</href></C:calendar-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, srv.URL)
		case r.Method == "PROPFIND" && r.URL.Path == "/p42/123456/calendars/":
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, strings.ReplaceAll(agendaTestPropfindCals, "/123456/calendars/", "/p42/123456/calendars/"))
		case r.Method == "REPORT" && strings.HasPrefix(r.URL.Path, "/p42/123456/calendars/"):
			if !strings.Contains(string(corpo), "<c:expand") || !strings.Contains(string(corpo), "time-range") {
				t.Errorf("REPORT sem expand/time-range:\n%s", corpo)
			}
			w.WriteHeader(http.StatusMultiStatus)
			ics := agendaTestICS
			if strings.Contains(r.URL.Path, "familia") {
				ics = "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:x\nBEGIN:VEVENT\nUID:fam-1\nDTSTAMP:20261001T120000Z\nDTSTART:20261016T200000Z\nDTEND:20261016T210000Z\nSUMMARY:Pediatra\nEND:VEVENT\nEND:VCALENDAR\n"
			}
			fmt.Fprintf(w, `<?xml version="1.0"?><multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><response><href>%sx.ics</href><propstat><prop><getetag>"1"</getetag><C:calendar-data><![CDATA[%s]]></C:calendar-data></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, r.URL.Path, ics)
		case r.Method == http.MethodPut:
			puts = append(puts, r.URL.Path+"\n"+string(corpo))
			if r.Header.Get("If-None-Match") != "*" {
				t.Errorf("PUT sem If-None-Match")
			}
			w.Header().Set("ETag", `"2"`)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("pedido inesperado: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &puts
}

func TestAgendaFluxoCompleto(t *testing.T) {
	loc := agendaTestLoc(t)
	antes := time.Local
	time.Local = loc
	defer func() { time.Local = antes }()

	srv, puts := agendaServidorFalso(t)
	defer srv.Close()

	// store/ temporario, para nao tocar no de verdade
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.MkdirAll(filepath.Join(dir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	initAgendaApple()
	if agendaConfigurada() || agendaResumoStatus() != nil {
		t.Fatal("sem apple.json devia estar inerte")
	}

	// a descoberta comeca no host do iCloud; no teste, no servidor falso
	oldURL := agendaDiscoverURL
	agendaDiscoverURL = srv.URL + "/"
	defer func() { agendaDiscoverURL = oldURL }()

	ctx := context.Background()
	if _, _, err := agendaTestar(ctx, "diego@example.com", "senha-errada-xxxx-yyyy"); err == nil {
		t.Fatal("senha errada devia falhar")
	} else if ae, ok := err.(*agendaErro); !ok || ae.Estado != agendaEstadoSenha {
		t.Fatalf("senha errada: erro %T %v", err, err)
	}
	home, cals, err := agendaTestar(ctx, "diego@example.com", "abcdefghijklmnop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(home, "/p42/123456/calendars/") || len(cals) != 2 {
		t.Fatalf("descoberta: %s %+v", home, cals)
	}

	if err := agendaConfigurar(ctx, "diego@example.com", "abcd-efgh-ijkl-mnop", nil, ""); err != nil {
		t.Fatal(err)
	}
	if !agendaConectada() {
		t.Fatal("devia estar conectada")
	}
	if st, err := os.Stat(filepath.Join(dir, "store", "apple.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("apple.json: %v %v", st, err)
	}
	pub := agendaConfigPublica()
	if pub["principal"] != home+"home/" { // o primeiro com escrita, nao o somente-leitura
		t.Errorf("principal: %v", pub["principal"])
	}
	if _, tem := pub["senha_app"]; tem {
		t.Error("a senha nunca sai na configuracao publica")
	}

	de := time.Date(2026, 10, 15, 0, 0, 0, 0, loc)
	ate := time.Date(2026, 10, 22, 0, 0, 0, 0, loc)
	evs, err := agendaListar(ctx, de, ate, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 4 { // 3 do Pessoal + 1 da Familia, ordenados
		t.Fatalf("listar: %d %+v", len(evs), evs)
	}
	if evs[0].Titulo != "Reunião semanal" || evs[1].Titulo != "Pediatra" || evs[1].Calendario != "Família (Lisiane)" {
		t.Errorf("ordem: %q, %q", evs[0].Titulo, evs[1].Titulo)
	}
	achados, err := agendaBuscar(ctx, "fausto", de, ate)
	if err != nil || len(achados) != 2 { // reuniao (convidado) e almoco (titulo)
		t.Errorf("buscar: %v %+v", err, achados)
	}

	// criar: no principal, sem convidados; com convidados recusa antes de ir a rede
	if _, err := agendaCriar(ctx, agendaPedidoCriar{Titulo: "x", Inicio: "2026-10-20T10:00", Convidados: []string{"a@b"}}); err == nil {
		t.Error("convidados deviam ser recusados")
	}
	if _, err := agendaCriar(ctx, agendaPedidoCriar{Titulo: "x", Inicio: "2026-10-20T10:00", Calendario: "família"}); err == nil {
		t.Error("calendário somente leitura devia ser recusado")
	}
	ev, err := agendaCriar(ctx, agendaPedidoCriar{Titulo: "Dentista", Inicio: "2026-10-20T10:00", Fim: "2026-10-20T10:45", AlertaMin: 15})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Beatitude || ev.Calendario != "Pessoal" || ev.Quando != "ter 20/10 10:00–10:45" || !strings.HasPrefix(ev.UID, agendaUIDPrefixo) {
		t.Errorf("criado: %+v", ev)
	}
	if len(*puts) != 1 || !strings.HasPrefix((*puts)[0], "/p42/123456/calendars/home/beatitude-") || !strings.Contains((*puts)[0], "DTSTART:20261020T130000Z") {
		t.Errorf("PUT: %v", *puts)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "store", "apple_criados.json")); !strings.Contains(string(b), ev.UID) {
		t.Errorf("apple_criados.json: %s", b)
	}

	agendaDesconectar()
	if agendaConfigurada() {
		t.Error("devia ter desconectado")
	}
	if _, err := os.Stat(filepath.Join(dir, "store", "apple.json")); !os.IsNotExist(err) {
		t.Error("apple.json devia sumir")
	}
}

func TestPonteAgendaCorpoEDeposito(t *testing.T) {
	loc := agendaTestLoc(t)
	antes := time.Local
	time.Local = loc
	defer func() { time.Local = antes }()

	// servidor do Beatitude falso: primeiro 404 (rota ainda nao existe), depois 200
	var recebidos []map[string]interface{}
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ponteAgendaRota || r.Header.Get("Authorization") != "Bearer bz_teste" {
			t.Errorf("pedido inesperado: %s %s", r.Method, r.URL.Path)
		}
		chamadas++
		if chamadas == 1 {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"ok":false,"erro":"rota_desconhecida"}`)
			return
		}
		var corpo map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&corpo)
		recebidos = append(recebidos, corpo)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	// agenda conectada contra o iCloud falso
	icloud, _ := agendaServidorFalso(t)
	defer icloud.Close()
	dir := t.TempDir()
	wd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer os.Chdir(wd)
	_ = os.MkdirAll(filepath.Join(dir, "store"), 0o755)
	initAgendaApple()
	oldURL := agendaDiscoverURL
	agendaDiscoverURL = icloud.URL + "/"
	defer func() { agendaDiscoverURL = oldURL }()
	if err := agendaConfigurar(context.Background(), "diego@example.com", "abcd-efgh-ijkl-mnop", nil, ""); err != nil {
		t.Fatal(err)
	}
	ponte.mu.Lock()
	ponte.cfg = ponteConfig{Token: "bz_teste", URL: srv.URL}
	ponte.mu.Unlock()
	ponteAgenda.mu.Lock()
	ponteAgenda.ultimoEm, ponteAgenda.proximoEm, ponteAgenda.forcar, ponteAgenda.ultimoErr = time.Time{}, time.Time{}, false, ""
	ponteAgenda.mu.Unlock()

	// 1a rodada: 404 -> recuo, sem erro na ponte
	ponteEnviarAgenda()
	if chamadas != 1 || !ponteAgenda.proximoEm.After(time.Now().Add(ponteAgendaRecuo-time.Minute)) {
		t.Fatalf("esperava recuo depois do 404: chamadas=%d proximo=%v", chamadas, ponteAgenda.proximoEm)
	}
	// 2a rodada no mesmo minuto: nada (recuo)
	ponteEnviarAgenda()
	if chamadas != 1 {
		t.Fatalf("devia respeitar o recuo, chamadas=%d", chamadas)
	}
	// compromisso criado forca o deposito, que agora o servidor aceita
	ponteAgendaDepois()
	ponteEnviarAgenda()
	if chamadas != 2 || len(recebidos) != 1 {
		t.Fatalf("esperava deposito forcado: chamadas=%d recebidos=%d", chamadas, len(recebidos))
	}
	corpo := recebidos[0]
	if corpo["fonte"] != "apple" || corpo["fuso"] != "America/Sao_Paulo" {
		t.Errorf("corpo: %v", corpo)
	}
	evs, _ := corpo["eventos"].([]interface{})
	// o iCloud falso tem eventos em out/2026; a janela e hoje+7, entao a lista depende da data.
	// O que importa aqui e a forma: sem "notas" e sem "uid" em nenhum evento.
	for _, e := range evs {
		m := e.(map[string]interface{})
		if _, tem := m["notas"]; tem {
			t.Error("notas nunca vao ao servidor")
		}
		if _, tem := m["uid"]; tem {
			t.Error("uid nao vai ao servidor")
		}
	}
	// 3a rodada logo em seguida: cadencia de 30 min segura
	ponteEnviarAgenda()
	if chamadas != 2 {
		t.Errorf("devia esperar a cadencia, chamadas=%d", chamadas)
	}
}
