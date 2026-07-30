// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// panel.go — painel de controle local servido pela propria ponte.
//
// Adiciona ao binario existente: pagina web em http://localhost:8080,
// endpoints de status/reconexao/QR/estatisticas/contatos, mensagens
// programadas (uma vez, diaria, semanal, anual) e um agendador proprio.
//
// Nada aqui depende de biblioteca externa nova: o QR e desenhado com a
// rsc.io/qr que ja vem de carona com a qrterminal.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // embute o banco de fusos, para nao depender do sistema

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"rsc.io/qr"
)

//go:embed panel.html
var panelHTML []byte

// Limite deliberado: uso pessoal. Vale para envios do painel e do agendador.
const panelHourlyLimit = 30

// Tolerancia de atraso. Passado isso, a ocorrencia e marcada como perdida e
// NAO e enviada — melhor nao mandar "bom dia" as tres da tarde.
const panelLateTolerance = 2 * time.Minute

type panelRuntime struct {
	mu sync.RWMutex

	client    *whatsmeow.Client
	store     *MessageStore
	container *sqlstore.Container
	logger    waLog.Logger

	startedAt    time.Time
	lastReceived time.Time

	qrCode   string
	qrSetAt  time.Time
	qrActive bool

	loginBusy bool
}

var panel panelRuntime
var panelLoc = time.FixedZone("-03", -3*60*60) // fallback

// ---------------------------------------------------------------- ciclo de vida

func initPanel(client *whatsmeow.Client, store *MessageStore, container *sqlstore.Container, logger waLog.Logger) {
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		panelLoc = loc
	}
	panel.mu.Lock()
	panel.client = client
	panel.store = store
	panel.container = container
	panel.logger = logger
	panel.startedAt = time.Now()
	panel.mu.Unlock()

	if err := ensurePanelTables(store); err != nil {
		logger.Errorf("Painel: falha ao criar tabelas: %v", err)
	}
}

func ensurePanelTables(store *MessageStore) error {
	_, err := store.db.Exec(`
	CREATE TABLE IF NOT EXISTS panel_api_sends (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		recipient TEXT,
		source    TEXT,
		sent_at   TIMESTAMP NOT NULL
	);
	CREATE TABLE IF NOT EXISTS panel_schedules (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		recipient   TEXT    NOT NULL,
		display     TEXT    NOT NULL,
		message     TEXT    NOT NULL,
		mode        TEXT    NOT NULL,
		at_time     TEXT    NOT NULL,
		on_date     TEXT,
		weekday     INTEGER,
		day_of      INTEGER,
		month_of    INTEGER,
		enabled     INTEGER NOT NULL DEFAULT 1,
		last_key    TEXT,
		last_status TEXT,
		created_at  TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_panel_sends_at ON panel_api_sends(sent_at);`)
	return err
}

// panelConnect conecta sem bloquear. Essencial: se a sessao morreu, o processo
// precisa continuar vivo para servir a pagina e mostrar o QR — o codigo original
// bloqueava aqui e saia por timeout, matando o servico em loop.
func panelConnect() {
	panel.mu.Lock()
	if panel.loginBusy {
		panel.mu.Unlock()
		return
	}
	panel.loginBusy = true
	client := panel.client
	panel.mu.Unlock()

	defer func() {
		panel.mu.Lock()
		panel.loginBusy = false
		panel.mu.Unlock()
	}()

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		if err := client.Connect(); err != nil {
			panel.logger.Errorf("Falha ao conectar: %v", err)
			return
		}
		go func() {
			for evt := range qrChan {
				switch evt.Event {
				case "code":
					panelSetQR(evt.Code)
					fmt.Println("\nEscaneie este QR code com o WhatsApp (ou abra http://localhost:8080):")
					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				case "success":
					panelClearQR()
					fmt.Println("\n✓ Pareado com sucesso!")
				case "timeout":
					panelClearQR()
					panel.logger.Warnf("QR expirou sem leitura; abra o painel para gerar outro")
				}
			}
		}()
		return
	}

	if err := client.Connect(); err != nil {
		panel.logger.Errorf("Falha ao conectar: %v", err)
	}
}

// panelHandleLoggedOut reage ao WhatsApp encerrar a sessao do aparelho.
func panelHandleLoggedOut() {
	panel.logger.Warnf("Sessao encerrada pelo WhatsApp — aguardando novo pareamento")
	time.Sleep(2 * time.Second)
	panelConnect()
}

func panelSetQR(code string) {
	panel.mu.Lock()
	panel.qrCode = code
	panel.qrSetAt = time.Now()
	panel.qrActive = true
	panel.mu.Unlock()
}

func panelClearQR() {
	panel.mu.Lock()
	panel.qrCode = ""
	panel.qrActive = false
	panel.mu.Unlock()
}

func panelNoteReceived() {
	panel.mu.Lock()
	panel.lastReceived = time.Now()
	panel.mu.Unlock()
}

// ---------------------------------------------------------------- QR em imagem

// renderQRPNG desenha o QR com zona de silencio propria. A funcao Image() da
// rsc.io/qr nao adiciona a borda branca que os leitores exigem, entao montamos
// o bitmap a partir de Black(x,y).
func renderQRPNG(text string, scale, quiet int) ([]byte, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	side := (c.Size + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if !c.Black(x, y) {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{Y: 0x00})
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------- contabilidade

func recordAPISend(recipient, source string) {
	panel.mu.RLock()
	store := panel.store
	panel.mu.RUnlock()
	if store == nil {
		return
	}
	if _, err := store.db.Exec(
		`INSERT INTO panel_api_sends (recipient, source, sent_at) VALUES (?, ?, ?)`,
		recipient, source, time.Now()); err != nil {
		panel.logger.Warnf("Painel: falha ao registrar envio: %v", err)
	}
}

func apiSendsSince(t time.Time) int {
	var n int
	if panel.store == nil {
		return 0
	}
	_ = panel.store.db.QueryRow(
		`SELECT COUNT(*) FROM panel_api_sends WHERE sent_at >= ?`, t).Scan(&n)
	return n
}

// panelSend centraliza os envios originados no painel/agendador, aplicando o
// teto por hora. Envios vindos do MCP passam pelo /api/send original e sao
// apenas contabilizados, nunca bloqueados.
func panelSend(recipient, message, source string) (bool, string) {
	if n := apiSendsSince(time.Now().Add(-time.Hour)); n >= panelHourlyLimit {
		return false, fmt.Sprintf("limite de %d envios por hora atingido", panelHourlyLimit)
	}
	ok, msg := sendWhatsAppMessage(panel.client, recipient, message, "")
	if ok {
		recordAPISend(recipient, source)
	}
	return ok, msg
}

// ---------------------------------------------------------------- agendamentos

type schedule struct {
	ID         int64  `json:"id"`
	Recipient  string `json:"recipient"`
	Display    string `json:"display"`
	Message    string `json:"message"`
	Mode       string `json:"mode"` // once|daily|weekly|yearly
	AtTime     string `json:"at_time"`
	OnDate     string `json:"on_date,omitempty"`
	Weekday    *int   `json:"weekday,omitempty"`
	DayOf      *int   `json:"day_of,omitempty"`
	MonthOf    *int   `json:"month_of,omitempty"`
	Enabled    bool   `json:"enabled"`
	LastStatus string `json:"last_status,omitempty"`
	NextRun    string `json:"next_run,omitempty"`
}

func parseHM(s string) (int, int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// occurrenceOn devolve o horario agendado para o dia de `ref`, se houver.
func occurrenceOn(s schedule, ref time.Time) (time.Time, bool) {
	h, m, ok := parseHM(s.AtTime)
	if !ok {
		return time.Time{}, false
	}
	at := func(y int, mo time.Month, d int) time.Time {
		return time.Date(y, mo, d, h, m, 0, 0, panelLoc)
	}
	switch s.Mode {
	case "once":
		d, err := time.ParseInLocation("2006-01-02", s.OnDate, panelLoc)
		if err != nil {
			return time.Time{}, false
		}
		if d.Year() != ref.Year() || d.Month() != ref.Month() || d.Day() != ref.Day() {
			return time.Time{}, false
		}
		return at(d.Year(), d.Month(), d.Day()), true
	case "daily":
		return at(ref.Year(), ref.Month(), ref.Day()), true
	case "weekly":
		if s.Weekday == nil || int(ref.Weekday()) != *s.Weekday {
			return time.Time{}, false
		}
		return at(ref.Year(), ref.Month(), ref.Day()), true
	case "yearly":
		if s.DayOf == nil || s.MonthOf == nil {
			return time.Time{}, false
		}
		if ref.Day() != *s.DayOf || int(ref.Month()) != *s.MonthOf {
			return time.Time{}, false
		}
		return at(ref.Year(), ref.Month(), ref.Day()), true
	}
	return time.Time{}, false
}

// nextRunAfter procura a proxima ocorrencia futura, olhando ate 400 dias.
func nextRunAfter(s schedule, now time.Time) (time.Time, bool) {
	for i := 0; i < 400; i++ {
		day := now.AddDate(0, 0, i)
		if occ, ok := occurrenceOn(s, day); ok && occ.After(now) {
			return occ, true
		}
	}
	return time.Time{}, false
}

func occurrenceKey(t time.Time) string { return t.Format("2006-01-02 15:04") }

func loadSchedules(onlyEnabled bool) ([]schedule, error) {
	q := `SELECT id, recipient, display, message, mode, at_time,
	             COALESCE(on_date,''), weekday, day_of, month_of,
	             enabled, COALESCE(last_status,''), COALESCE(last_key,'')
	      FROM panel_schedules`
	if onlyEnabled {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY id`
	rows, err := panel.store.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []schedule
	for rows.Next() {
		var s schedule
		var enabled int
		var lastKey string
		if err := rows.Scan(&s.ID, &s.Recipient, &s.Display, &s.Message, &s.Mode, &s.AtTime,
			&s.OnDate, &s.Weekday, &s.DayOf, &s.MonthOf, &enabled, &s.LastStatus, &lastKey); err != nil {
			return nil, err
		}
		s.Enabled = enabled == 1
		if occ, ok := nextRunAfter(s, time.Now().In(panelLoc)); ok {
			s.NextRun = occ.Format("02/01 15:04")
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func startPanelScheduler() {
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for range tick.C {
			runDueSchedules()
		}
	}()
	panel.logger.Infof("Painel: agendador ativo (fuso %s)", panelLoc)
}

func runDueSchedules() {
	panel.mu.RLock()
	store, client := panel.store, panel.client
	panel.mu.RUnlock()
	if store == nil || client == nil {
		return
	}

	now := time.Now().In(panelLoc)
	rows, err := store.db.Query(
		`SELECT id, recipient, display, message, mode, at_time, COALESCE(on_date,''),
		        weekday, day_of, month_of, COALESCE(last_key,'')
		 FROM panel_schedules WHERE enabled = 1`)
	if err != nil {
		panel.logger.Warnf("Painel: falha ao ler agendamentos: %v", err)
		return
	}
	type pending struct {
		s      schedule
		occ    time.Time
		key    string
		missed bool
	}
	var todo []pending
	for rows.Next() {
		var s schedule
		var lastKey string
		if err := rows.Scan(&s.ID, &s.Recipient, &s.Display, &s.Message, &s.Mode,
			&s.AtTime, &s.OnDate, &s.Weekday, &s.DayOf, &s.MonthOf, &lastKey); err != nil {
			continue
		}
		occ, ok := occurrenceOn(s, now)
		if !ok || now.Before(occ) {
			continue
		}
		key := occurrenceKey(occ)
		if key == lastKey {
			continue // ja tratada
		}
		todo = append(todo, pending{s: s, occ: occ, key: key,
			missed: now.Sub(occ) > panelLateTolerance})
	}
	rows.Close()

	for _, p := range todo {
		status := "enviada"
		if p.missed {
			status = "perdida (Mac desligado ou fora do ar)"
			panel.logger.Warnf("Painel: agendamento %d perdido (%s)", p.s.ID, p.key)
		} else {
			ok, msg := panelSend(p.s.Recipient, p.s.Message, "agendamento")
			if !ok {
				status = "falhou: " + msg
				panel.logger.Warnf("Painel: agendamento %d falhou: %s", p.s.ID, msg)
			} else {
				panel.logger.Infof("Painel: agendamento %d enviado para %s", p.s.ID, p.s.Display)
			}
		}
		if _, err := store.db.Exec(
			`UPDATE panel_schedules SET last_key = ?, last_status = ? WHERE id = ?`,
			p.key, status, p.s.ID); err != nil {
			panel.logger.Warnf("Painel: falha ao atualizar agendamento %d: %v", p.s.ID, err)
		}
		if p.s.Mode == "once" {
			_, _ = store.db.Exec(`UPDATE panel_schedules SET enabled = 0 WHERE id = ?`, p.s.ID)
		}
	}
}

// ---------------------------------------------------------------- HTTP

func registerPanelRoutes() {
	h := func(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isLocalRequest(r) {
				http.Error(w, "apenas localhost", http.StatusForbidden)
				return
			}
			fn(w, r)
		}
	}

	http.HandleFunc("/", h(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(panelHTML)
	}))

	http.HandleFunc("/api/panel/status", h(panelStatusHandler))
	http.HandleFunc("/api/panel/stats", h(panelStatsHandler))
	http.HandleFunc("/api/panel/qr.png", h(panelQRHandler))
	http.HandleFunc("/api/panel/reconnect", h(panelReconnectHandler))
	http.HandleFunc("/api/panel/unlink", h(panelUnlinkHandler))
	http.HandleFunc("/api/panel/contacts", h(panelContactsHandler))
	http.HandleFunc("/api/panel/send", h(panelSendHandler))
	http.HandleFunc("/api/panel/schedules", h(panelSchedulesHandler))
	http.HandleFunc("/api/panel/schedules/toggle", h(panelToggleHandler))
	http.HandleFunc("/api/panel/schedules/delete", h(panelDeleteHandler))

	panel.logger.Infof("Painel disponivel em http://localhost:8080")
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func panelStatusHandler(w http.ResponseWriter, r *http.Request) {
	panel.mu.RLock()
	client := panel.client
	startedAt := panel.startedAt
	lastRecv := panel.lastReceived
	qrActive := panel.qrActive
	qrSetAt := panel.qrSetAt
	panel.mu.RUnlock()

	connected := client != nil && client.IsConnected() && client.IsLoggedIn()
	number := ""
	if client != nil && client.Store.ID != nil {
		number = client.Store.ID.User
	}

	if lastRecv.IsZero() && panel.store != nil {
		var t time.Time
		if err := panel.store.db.QueryRow(
			`SELECT MAX(timestamp) FROM messages WHERE is_from_me = 0`).Scan(&t); err == nil {
			lastRecv = t
		}
	}

	resp := map[string]interface{}{
		"connected":   connected,
		"needs_pair":  qrActive || (client != nil && client.Store.ID == nil),
		"number":      number,
		"uptime_secs": int(time.Since(startedAt).Seconds()),
	}
	if !lastRecv.IsZero() {
		resp["last_received_secs"] = int(time.Since(lastRecv).Seconds())
	}
	if qrActive {
		// O WhatsApp rotaciona o codigo a cada ~20s.
		remaining := 20 - int(time.Since(qrSetAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		resp["qr_expires_in"] = remaining
	}
	writeJSON(w, resp)
}

func panelStatsHandler(w http.ResponseWriter, r *http.Request) {
	if panel.store == nil {
		http.Error(w, "sem banco", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().In(panelLoc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, panelLoc)

	count := func(q string, args ...interface{}) int {
		var n int
		_ = panel.store.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	received := count(`SELECT COUNT(*) FROM messages WHERE is_from_me = 0 AND timestamp >= ?`, startOfDay)
	sent := count(`SELECT COUNT(*) FROM messages WHERE is_from_me = 1 AND timestamp >= ?`, startOfDay)
	viaAPI := count(`SELECT COUNT(*) FROM panel_api_sends WHERE sent_at >= ?`, startOfDay)

	// media diaria dos 7 dias anteriores, para o comparativo
	weekAgo := startOfDay.AddDate(0, 0, -7)
	prev := count(`SELECT COUNT(*) FROM messages WHERE is_from_me = 0 AND timestamp >= ? AND timestamp < ?`,
		weekAgo, startOfDay)
	var deltaPct *int
	if prev > 0 {
		avg := float64(prev) / 7.0
		d := int((float64(received)/avg - 1) * 100)
		deltaPct = &d
	}

	// serie das ultimas 12 horas, para os sparklines
	spark := func(fromMe int) []int {
		out := make([]int, 12)
		for i := 0; i < 12; i++ {
			from := now.Add(time.Duration(-(12 - i)) * time.Hour).Truncate(time.Hour)
			to := from.Add(time.Hour)
			out[i] = count(`SELECT COUNT(*) FROM messages WHERE is_from_me = ? AND timestamp >= ? AND timestamp < ?`,
				fromMe, from, to)
		}
		return out
	}
	sparkAPI := make([]int, 12)
	for i := 0; i < 12; i++ {
		from := now.Add(time.Duration(-(12 - i)) * time.Hour).Truncate(time.Hour)
		sparkAPI[i] = count(`SELECT COUNT(*) FROM panel_api_sends WHERE sent_at >= ? AND sent_at < ?`,
			from, from.Add(time.Hour))
	}

	writeJSON(w, map[string]interface{}{
		"received_today": received,
		"sent_today":     sent,
		"sent_via_api":   viaAPI,
		"delta_pct":      deltaPct,
		"hourly_limit":   panelHourlyLimit,
		"used_last_hour": apiSendsSince(now.Add(-time.Hour)),
		"spark_received": spark(0),
		"spark_sent":     spark(1),
		"spark_api":      sparkAPI,
	})
}

func panelQRHandler(w http.ResponseWriter, r *http.Request) {
	panel.mu.RLock()
	code, active := panel.qrCode, panel.qrActive
	panel.mu.RUnlock()
	if !active || code == "" {
		http.Error(w, "sem QR ativo", http.StatusConflict)
		return
	}
	img, err := renderQRPNG(code, 6, 3)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(img)
}

func panelReconnectHandler(w http.ResponseWriter, r *http.Request) {
	panel.mu.RLock()
	client := panel.client
	panel.mu.RUnlock()
	if client == nil {
		http.Error(w, "cliente indisponivel", http.StatusServiceUnavailable)
		return
	}
	go func() {
		client.Disconnect()
		time.Sleep(700 * time.Millisecond)
		panelConnect()
	}()
	writeJSON(w, map[string]interface{}{"ok": true})
}

func panelUnlinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	panel.mu.RLock()
	client := panel.client
	panel.mu.RUnlock()
	if client == nil {
		http.Error(w, "cliente indisponivel", http.StatusServiceUnavailable)
		return
	}
	go func() {
		if err := client.Logout(context.Background()); err != nil {
			panel.logger.Warnf("Painel: logout retornou erro (%v) — desconectando de todo modo", err)
			client.Disconnect()
		}
		time.Sleep(1 * time.Second)
		panelConnect()
	}()
	writeJSON(w, map[string]interface{}{"ok": true})
}

func panelContactsHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, []interface{}{})
		return
	}
	rows, err := panel.store.db.Query(
		`SELECT jid, name FROM chats
		 WHERE jid NOT LIKE '%@g.us'
		   AND (LOWER(name) LIKE LOWER(?) OR jid LIKE ?)
		 ORDER BY last_message_time DESC LIMIT 12`, "%"+q+"%", "%"+q+"%")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type contact struct {
		JID  string `json:"jid"`
		Name string `json:"name"`
	}
	out := []contact{}
	for rows.Next() {
		var c contact
		if err := rows.Scan(&c.JID, &c.Name); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, out)
}

func panelSendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Recipient string `json:"recipient"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Recipient) == "" || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "destinatario e mensagem sao obrigatorios", http.StatusBadRequest)
		return
	}
	ok, msg := panelSend(req.Recipient, req.Message, "painel")
	writeJSON(w, map[string]interface{}{"ok": ok, "message": msg})
}

func panelSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list, err := loadSchedules(false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []schedule{}
		}
		writeJSON(w, list)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "use GET ou POST", http.StatusMethodNotAllowed)
		return
	}

	var s schedule
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(s.Recipient) == "" || strings.TrimSpace(s.Message) == "" {
		http.Error(w, "destinatario e mensagem sao obrigatorios", http.StatusBadRequest)
		return
	}
	if _, _, ok := parseHM(s.AtTime); !ok {
		http.Error(w, "hora invalida", http.StatusBadRequest)
		return
	}
	switch s.Mode {
	case "once":
		if _, err := time.ParseInLocation("2006-01-02", s.OnDate, panelLoc); err != nil {
			http.Error(w, "data invalida", http.StatusBadRequest)
			return
		}
	case "daily":
	case "weekly":
		if s.Weekday == nil || *s.Weekday < 0 || *s.Weekday > 6 {
			http.Error(w, "dia da semana invalido", http.StatusBadRequest)
			return
		}
	case "yearly":
		if s.DayOf == nil || s.MonthOf == nil ||
			*s.DayOf < 1 || *s.DayOf > 31 || *s.MonthOf < 1 || *s.MonthOf > 12 {
			http.Error(w, "dia/mes invalido", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "repeticao invalida", http.StatusBadRequest)
		return
	}
	if s.Display == "" {
		s.Display = s.Recipient
	}

	// Se a ocorrencia de hoje ja passou, marca como tratada — evita registrar
	// um "perdida" logo na criacao.
	now := time.Now().In(panelLoc)
	lastKey := ""
	if occ, ok := occurrenceOn(s, now); ok && now.After(occ) {
		lastKey = occurrenceKey(occ)
	}

	res, err := panel.store.db.Exec(
		`INSERT INTO panel_schedules
		 (recipient, display, message, mode, at_time, on_date, weekday, day_of, month_of,
		  enabled, last_key, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,1,?,?)`,
		s.Recipient, s.Display, s.Message, s.Mode, s.AtTime,
		nullIfEmpty(s.OnDate), s.Weekday, s.DayOf, s.MonthOf, nullIfEmpty(lastKey), time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}

func panelToggleHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64 `json:"id"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}
	v := 0
	if req.Enabled {
		v = 1
	}
	if _, err := panel.store.db.Exec(
		`UPDATE panel_schedules SET enabled = ? WHERE id = ?`, v, req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func panelDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}
	if _, err := panel.store.db.Exec(`DELETE FROM panel_schedules WHERE id = ?`, req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
