// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// Ligação com o Claude (1.1.1; doc 11 fases 0–2 e doc 12).
//
// O servidor MCP lê o SQLite direto do disco: a ponte não sabia quando o
// Claude lia, nem QUAL banco ele lia. Foi assim que o Douglas (05/11) passou
// dias com o Claude lendo o banco de uma instalação antiga, e que o Mac mini
// (12/11) ficou com a ponte no ar e o Claude sem o lumnia-zap na config.
//
//   - identidade do banco: panel_meta.db_id, um UUID gravado na primeira
//     subida. Copiar o store/ leva o id junto; bancos diferentes, ids
//     diferentes.
//   - sinal do Claude: a cada ferramenta, o servidor MCP manda db_id +
//     ferramenta para /api/panel/claude-sinal (sem conteúdo nenhum).
//   - config do Claude (Mac e Windows clássico): a ponte confere se a entrada
//     lumnia-zap existe e aponta para ESTA instalação; "Reparar" regrava, só
//     com o Claude fechado.
//   - "Testar com o Claude": um código de 4 dígitos para o próprio número; o
//     servidor MCP avisa quando uma ferramenta devolveu o código.
//   - "Primeiros passos": estado do onboarding em panel_meta.

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	testeClaudeValidade = 10 * time.Minute
	testeClaudePrefixo  = "Teste do Lumnia Zap · código "
)

// ---------------------------------------------------------------- panel_meta

func panelMetaCriar(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS panel_meta (chave TEXT PRIMARY KEY, valor TEXT)`)
	if err != nil {
		return err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // UUID v4
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	id := h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	_, err = db.Exec(`INSERT OR IGNORE INTO panel_meta (chave, valor) VALUES ('db_id', ?)`, id)
	return err
}

func panelMetaGet(chave string) string {
	if panel.store == nil {
		return ""
	}
	var v sql.NullString
	_ = panel.store.db.QueryRow(`SELECT valor FROM panel_meta WHERE chave = ?`, chave).Scan(&v)
	return v.String
}

func panelMetaSet(chave, valor string) {
	if panel.store == nil {
		return
	}
	_, _ = panel.store.db.Exec(`INSERT INTO panel_meta (chave, valor) VALUES (?, ?)
		ON CONFLICT(chave) DO UPDATE SET valor = excluded.valor`, chave, valor)
}

// ---------------------------------------------------------------- instalação

// instalacaoTipo: mac | linux | windows-store | windows-classica. O
// servico.exe do MSIX passa LUMNIA_ZAP_INSTALACAO=windows-store.
func instalacaoTipo() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "windows":
		if os.Getenv("LUMNIA_ZAP_INSTALACAO") == "windows-store" {
			return "windows-store"
		}
		return "windows-classica"
	}
	return "linux"
}

func nomeDoComputadorPonte() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
			if n := strings.TrimSpace(string(out)); n != "" {
				return n
			}
		}
	}
	if n := os.Getenv("COMPUTERNAME"); n != "" {
		return n
	}
	h, _ := os.Hostname()
	return h
}

var computadorCache struct {
	once sync.Once
	nome string
}

func computadorPonte() string {
	computadorCache.once.Do(func() { computadorCache.nome = nomeDoComputadorPonte() })
	return computadorCache.nome
}

// ---------------------------------------------------------------- sinal do Claude

type sinalClaude struct {
	Em         time.Time
	DBID       string
	Ferramenta string
	VersaoMCP  string
	Cliente    string // nome do cliente MCP (claude-ai, codex…), quando o servidor sabe
}

var ligacao struct {
	mu    sync.Mutex
	sinal sinalClaude
	teste *testeClaude
}

type testeClaude struct {
	Codigo    string
	Inicio    time.Time
	Enviada   bool
	Erro      string
	Achou     time.Time
	OutroDBID bool
}

// claudeSinalHandler: POST do servidor MCP, só de localhost. Nunca devolve
// erro que atrapalhe a ferramenta: o MCP ignora a resposta.
func claudeSinalHandler(w http.ResponseWriter, r *http.Request) {
	// application/json obrigatório: um site aberto no navegador não consegue
	// mandar esse tipo para o localhost sem preflight (que a ponte não aceita).
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "use POST com JSON", http.StatusMethodNotAllowed)
		return
	}
	var c struct {
		DBID        string `json:"db_id"`
		Ferramenta  string `json:"ferramenta"`
		VersaoMCP   string `json:"versao_mcp"`
		TesteCodigo string `json:"teste_codigo"`
		Cliente     string `json:"cliente"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&c); err != nil {
		http.Error(w, "json inválido", http.StatusBadRequest)
		return
	}
	// O mordomo do Beatitude usa o mesmo servidor MCP pelo Claude Code: a
	// leitura dele não prova nada sobre o Claude Desktop e fica fora do cartão.
	if strings.EqualFold(strings.TrimSpace(c.Cliente), "beatitude") {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	s := sinalClaude{Em: time.Now(), DBID: limitar(c.DBID, 64), Ferramenta: limitar(c.Ferramenta, 64), VersaoMCP: limitar(c.VersaoMCP, 32), Cliente: limitar(c.Cliente, 40)}
	meu := panelMetaGet("db_id")
	ligacao.mu.Lock()
	ligacao.sinal = s
	if t := ligacao.teste; t != nil && c.TesteCodigo != "" && c.TesteCodigo == t.Codigo {
		t.Achou = time.Now()
		t.OutroDBID = s.DBID != "" && meu != "" && s.DBID != meu
	}
	ligacao.mu.Unlock()
	// sobrevive a reinício da ponte
	panelMetaSet("claude_sinal", s.Em.UTC().Format(time.RFC3339)+"|"+s.DBID+"|"+s.Ferramenta+"|"+s.VersaoMCP+"|"+s.Cliente)
	writeJSON(w, map[string]bool{"ok": true})
}

func limitar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func ultimoSinal() sinalClaude {
	ligacao.mu.Lock()
	s := ligacao.sinal
	ligacao.mu.Unlock()
	if !s.Em.IsZero() {
		return s
	}
	p := strings.SplitN(panelMetaGet("claude_sinal"), "|", 5)
	if len(p) >= 4 {
		if t, err := time.Parse(time.RFC3339, p[0]); err == nil {
			s := sinalClaude{Em: t, DBID: p[1], Ferramenta: p[2], VersaoMCP: p[3]}
			if len(p) == 5 {
				s.Cliente = p[4]
			}
			return s
		}
	}
	return sinalClaude{}
}

// ---------------------------------------------------------------- config do Claude

// pastasConfigClaudePonte: onde o Claude Desktop pode ler a config nesta
// máquina. Só Mac e Windows clássico; na Store a ligação é pela extensão
// (.mcpb) e no Linux não há Claude Desktop.
func pastasConfigClaudePonte() []string {
	home, _ := os.UserHomeDir()
	var pastas []string
	switch instalacaoTipo() {
	case "mac":
		suporte := filepath.Join(home, "Library", "Application Support")
		pastas = append(pastas, filepath.Join(suporte, "Claude"))
		if outras, _ := filepath.Glob(filepath.Join(suporte, "Claude?*")); len(outras) > 0 {
			pastas = append(pastas, outras...)
		}
		// ~/Library/Containers fica de fora de propósito: a ponte roda em
		// segundo plano e varrer as pastas de outros apps a cada consulta pode
		// disparar o aviso de privacidade do macOS. O instalador, que roda com
		// a pessoa olhando, continua cobrindo esse caso.
	case "windows-classica":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		if d, _ := filepath.Glob(filepath.Join(local, "Claude-Data", "*")); len(d) > 0 {
			pastas = append(pastas, d...)
		}
		if p, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude")); len(p) > 0 {
			pastas = append(pastas, p...)
		}
		if a := os.Getenv("APPDATA"); a != "" {
			pastas = append(pastas, filepath.Join(a, "Claude"))
		}
	}
	return pastas
}

// servidorDestaInstalacao: a pasta whatsapp-mcp-server ao lado da ponte.
func servidorDestaInstalacao() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(cwd), "whatsapp-mcp-server")
}

func mesmoCaminho(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// estadoConfigClaude: "ok" | "sem_entrada" | "outra_instalacao" |
// "sem_claude" | "nao_se_aplica", e as pastas com config encontradas.
func estadoConfigClaude(pastas []string, servidor string) (string, []string) {
	if len(pastas) == 0 {
		return "nao_se_aplica", nil
	}
	var achadas []string
	outra := false
	for _, d := range pastas {
		raw, err := os.ReadFile(filepath.Join(d, "claude_desktop_config.json"))
		if err != nil {
			continue
		}
		achadas = append(achadas, d)
		raw = []byte(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"))
		var cfg struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			continue
		}
		e, ok := cfg.MCPServers["lumnia-zap"]
		if !ok {
			continue
		}
		for _, a := range e.Args {
			if mesmoCaminho(a, servidor) {
				return "ok", achadas
			}
		}
		outra = true
	}
	switch {
	case outra:
		return "outra_instalacao", achadas
	case len(achadas) == 0:
		return "sem_claude", nil
	}
	return "sem_entrada", achadas
}

var claudeAbertoPonte = func() bool {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("/usr/bin/pgrep", "-x", "Claude").Run() == nil
	case "windows":
		cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq Claude.exe", "/NH")
		semJanela(cmd)
		out, err := cmd.Output()
		return err == nil && strings.Contains(strings.ToLower(string(out)), "claude.exe")
	}
	return false
}

func acharUVPonte() string {
	home, _ := os.UserHomeDir()
	var cand []string
	if runtime.GOOS == "windows" {
		cand = []string{filepath.Join(home, ".local", "bin", "uv.exe"), filepath.Join(home, ".cargo", "bin", "uv.exe")}
	} else {
		cand = []string{filepath.Join(home, ".local", "bin", "uv"), "/opt/homebrew/bin/uv", "/usr/local/bin/uv"}
	}
	for _, c := range cand {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("uv"); err == nil {
		return p
	}
	return ""
}

// repararConfigClaude grava a entrada lumnia-zap apontando para esta
// instalação nas pastas dadas (as que já têm config; ou a primeira, se
// nenhuma tem). Atômico, preservando o resto do arquivo.
func repararConfigClaude(pastas []string, servidor, uv string) ([]string, error) {
	var alvo []string
	for _, d := range pastas {
		if _, err := os.Stat(filepath.Join(d, "claude_desktop_config.json")); err == nil {
			alvo = append(alvo, d)
		}
	}
	if len(alvo) == 0 && len(pastas) > 0 {
		alvo = pastas[:1]
	}
	var feitas []string
	var ultimo error
	for _, d := range alvo {
		if err := os.MkdirAll(d, 0o755); err != nil {
			ultimo = err
			continue
		}
		caminho := filepath.Join(d, "claude_desktop_config.json")
		cfg := map[string]interface{}{}
		if raw, err := os.ReadFile(caminho); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			raw = []byte(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"))
			if err := json.Unmarshal(raw, &cfg); err != nil {
				ultimo = fmt.Errorf("%s parece corrompido", caminho)
				continue
			}
			if _, err := os.Stat(caminho + ".bak-lumnia-zap"); os.IsNotExist(err) {
				_ = os.WriteFile(caminho+".bak-lumnia-zap", raw, 0o600)
			}
		}
		srv, _ := cfg["mcpServers"].(map[string]interface{})
		if srv == nil {
			srv = map[string]interface{}{}
		}
		srv["lumnia-zap"] = map[string]interface{}{
			"command": uv,
			"args":    []string{"--directory", servidor, "run", "main.py"},
		}
		cfg["mcpServers"] = srv
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			ultimo = err
			continue
		}
		tmp := caminho + ".lumnia-novo"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			ultimo = err
			continue
		}
		if err := os.Rename(tmp, caminho); err != nil {
			os.Remove(tmp)
			ultimo = err
			continue
		}
		feitas = append(feitas, d)
	}
	if len(feitas) == 0 {
		if ultimo == nil {
			ultimo = fmt.Errorf("não achei onde o Claude guarda a configuração")
		}
		return nil, ultimo
	}
	return feitas, nil
}

func claudeRepararHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	tipo := instalacaoTipo()
	if tipo != "mac" && tipo != "windows-classica" {
		http.Error(w, "nesta instalação o Claude se liga pela extensão, não pela configuração", http.StatusBadRequest)
		return
	}
	if claudeAbertoPonte() {
		sair := "Feche o Claude com Cmd+Q"
		if runtime.GOOS == "windows" {
			sair = "Saia do Claude pela bandeja (botão direito no ícone perto do relógio → Sair)"
		}
		http.Error(w, sair+" e clique em Reparar de novo. Com o Claude aberto a configuração pode se perder.", http.StatusConflict)
		return
	}
	uv := acharUVPonte()
	if uv == "" {
		http.Error(w, "não achei o uv neste computador; rode o instalador de novo", http.StatusServiceUnavailable)
		return
	}
	feitas, err := repararConfigClaude(pastasConfigClaudePonte(), servidorDestaInstalacao(), uv)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ligacaoCacheZerar()
	writeJSON(w, map[string]interface{}{"ok": true, "pastas": feitas})
}

// ---------------------------------------------------------------- teste com o Claude

var codigoTesteRe = regexp.MustCompile(`^\d{4}$`)

func novoCodigoTeste() string {
	n, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return fmt.Sprintf("%04d", time.Now().UnixNano()%9000+1000)
	}
	return fmt.Sprintf("%04d", n.Int64()+1000)
}

func testeClaudeHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	client := panel.client
	if client == nil || !client.IsConnected() || !client.IsLoggedIn() || client.Store.ID == nil {
		http.Error(w, "conecte o WhatsApp antes de testar", http.StatusConflict)
		return
	}
	t := &testeClaude{Codigo: novoCodigoTeste(), Inicio: time.Now()}
	ok, msg := panelSend(client.Store.ID.User, testeClaudePrefixo+t.Codigo, "teste")
	t.Enviada = ok
	if !ok {
		t.Erro = msg
	}
	ligacao.mu.Lock()
	ligacao.teste = t
	ligacao.mu.Unlock()
	writeJSON(w, testeResumo())
}

// testeGravado: a mensagem do teste está no banco que a ponte escreve?
func testeGravado(codigo string) bool {
	if panel.store == nil || !codigoTesteRe.MatchString(codigo) {
		return false
	}
	var n int
	_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE content = ?`, testeClaudePrefixo+codigo).Scan(&n)
	return n > 0
}

func testeResumo() map[string]interface{} {
	ligacao.mu.Lock()
	t := ligacao.teste
	if t != nil && time.Since(t.Inicio) > testeClaudeValidade {
		ligacao.teste, t = nil, nil
	}
	var c testeClaude
	if t != nil {
		c = *t
	}
	ligacao.mu.Unlock()
	if t == nil {
		return nil
	}
	return map[string]interface{}{
		"codigo":      c.Codigo,
		"pedido":      "Procure no meu WhatsApp a mensagem com o código " + c.Codigo + " e me diga o que ela diz.",
		"enviada":     c.Enviada,
		"erro":        c.Erro,
		"gravada":     c.Enviada && testeGravado(c.Codigo),
		"achou":       !c.Achou.IsZero() && !c.OutroDBID,
		"outro_banco": c.OutroDBID,
		"segundos":    int(time.Since(c.Inicio).Seconds()),
	}
}

// ---------------------------------------------------------------- primeiros passos

func onboardingResumo() map[string]interface{} {
	concluido := panelMetaGet("onboarding_concluido") == "1"
	out := map[string]interface{}{"concluido": concluido}
	if concluido {
		return out // sem contar o banco inteiro para um cartão que nem aparece
	}
	if panel.store != nil {
		var msgs, chats int
		var antiga, nova sql.NullString
		_ = panel.store.db.QueryRow(`SELECT COUNT(*), MIN(timestamp), MAX(timestamp) FROM messages`).Scan(&msgs, &antiga, &nova)
		_ = panel.store.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chats)
		out["mensagens"], out["conversas"] = msgs, chats
		if antiga.Valid {
			out["mais_antiga"] = antiga.String
		}
		if nova.Valid {
			out["mais_nova"] = nova.String
		}
	}
	return out
}

func onboardingHandler(w http.ResponseWriter, r *http.Request) {
	if !beatitudeExigePainel(w, r) {
		return
	}
	var c struct {
		Concluido bool `json:"concluido"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&c)
	v := "0"
	if c.Concluido {
		v = "1"
	}
	panelMetaSet("onboarding_concluido", v)
	ligacaoCacheZerar()
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- status

// ligacaoStatus entra no /api/panel/status como "instalacao", "claude",
// "teste" e "onboarding".
func ligacaoStatus(resp map[string]interface{}) {
	meu := panelMetaGet("db_id")
	inst := map[string]interface{}{
		"tipo":       instalacaoTipo(),
		"computador": computadorPonte(),
		"db_id":      meu,
	}
	// Clássica com a da Store também instalada: os dois disputam a 8080 e o
	// Claude acaba lendo o banco errado (caso do Douglas, 05/11).
	if instalacaoTipo() == "windows-classica" {
		local := os.Getenv("LOCALAPPDATA")
		if achados, _ := filepath.Glob(filepath.Join(local, "Packages", "Lumnia.LumniaZap_*")); local != "" && len(achados) > 0 {
			inst["store_tambem"] = true
		}
	}
	resp["instalacao"] = inst
	s := ultimoSinal()
	claude := map[string]interface{}{}
	if !s.Em.IsZero() {
		claude["ultimo_sinal_secs"] = int(time.Since(s.Em).Seconds())
		claude["ferramenta"] = s.Ferramenta
		claude["versao_mcp"] = s.VersaoMCP
		claude["cliente"] = s.Cliente
		claude["mesmo_banco"] = s.DBID == "" || meu == "" || s.DBID == meu
	}
	claude["config"] = estadoConfigEmCache()
	resp["claude"] = claude
	if t := testeResumo(); t != nil {
		resp["teste"] = t
	}
	resp["onboarding"] = onboardingEmCache()
}

// O painel consulta o status a cada 5 s. Ler os arquivos de config do Claude
// e contar as mensagens do banco a cada consulta é desperdício: valem por
// 30 s (Reparar e a conclusão dos primeiros passos zeram o cache).
var ligacaoCache struct {
	mu         sync.Mutex
	configEm   time.Time
	config     string
	onboardEm  time.Time
	onboarding map[string]interface{}
}

const ligacaoCacheValidade = 30 * time.Second

func estadoConfigEmCache() string {
	ligacaoCache.mu.Lock()
	defer ligacaoCache.mu.Unlock()
	if ligacaoCache.config == "" || time.Since(ligacaoCache.configEm) > ligacaoCacheValidade {
		ligacaoCache.config, _ = estadoConfigClaude(pastasConfigClaudePonte(), servidorDestaInstalacao())
		ligacaoCache.configEm = time.Now()
	}
	return ligacaoCache.config
}

func onboardingEmCache() map[string]interface{} {
	ligacaoCache.mu.Lock()
	defer ligacaoCache.mu.Unlock()
	if ligacaoCache.onboarding == nil || time.Since(ligacaoCache.onboardEm) > ligacaoCacheValidade {
		ligacaoCache.onboarding = onboardingResumo()
		ligacaoCache.onboardEm = time.Now()
	}
	return ligacaoCache.onboarding
}

func ligacaoCacheZerar() {
	ligacaoCache.mu.Lock()
	ligacaoCache.config, ligacaoCache.onboarding = "", nil
	ligacaoCache.mu.Unlock()
}

func registrarRotasLigacao(h func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	http.HandleFunc("/api/panel/claude-sinal", h(claudeSinalHandler))
	http.HandleFunc("/api/panel/claude-reparar", h(claudeRepararHandler))
	http.HandleFunc("/api/panel/teste-claude", h(testeClaudeHandler))
	http.HandleFunc("/api/panel/onboarding", h(onboardingHandler))
}
