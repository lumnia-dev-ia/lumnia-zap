// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// atualizacao.go — a ponte se atualiza sozinha, pelo painel.
//
// Hoje a pessoa precisava baixar e rodar o instalador de novo. Agora o painel
// confere no GitHub (a cada 6 h e quando a pagina abre) se ha versao nova e
// mostra "atualizar para vX.Y.Z" ao lado da versao. Um clique: a ponte baixa
// o MESMO pacote que o instalador usa (releases/download/vX/lumnia-zap-…tar.gz),
// extrai numa pasta temporaria ao lado, troca os arquivos por rename (nunca
// sobrescreve um binario assinado no lugar — ver escreverArquivo no
// instalador), guarda o binario anterior como whatsapp-bridge.anterior e pede
// ao launchd (ou ao systemd) para reinicia-la. A sessao do WhatsApp, as
// configuracoes e o store/ nao sao tocados.
//
// Opcional: "atualizar sozinho de madrugada" (store/atualizacao.json). Com
// isso ligado, a ponte aplica a versao nova entre 3h e 4h, horario em que
// nenhuma rotina do Beatitude roda. Vem desligado.

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	atualizacaoRepo       = "lumnia-dev-ia/lumnia-zap"
	atualizacaoConfigPath = "store/atualizacao.json"
	atualizacaoCada       = 6 * time.Hour
	atualizacaoHoraAuto   = 3 // 3h–4h locais
	atualizacaoServico    = "dev.lumnia.zap"
	atualizacaoTimeout    = 5 * time.Minute
)

// atualizacaoLatestURL e variavel so para o teste apontar a um servidor falso.
var atualizacaoLatestURL = "https://github.com/" + atualizacaoRepo + "/releases/latest"
var atualizacaoDownloadBase = "https://github.com/" + atualizacaoRepo + "/releases/download/"

type atualizacaoConfig struct {
	Sozinho bool `json:"atualizar_sozinho"`
}

var atualizacao struct {
	mu           sync.Mutex
	cfg          atualizacaoConfig
	disponivel   string // "1.0.6" ou vazio
	verificadoEm time.Time
	estado       string // ocioso | verificando | baixando | instalando | reiniciando | erro
	erro         string
	aplicadoDia  string // "2026-10-16": a automatica roda uma vez por dia
}

func initAtualizacao() {
	atualizacao.mu.Lock()
	if b, err := os.ReadFile(atualizacaoConfigPath); err == nil {
		_ = json.Unmarshal(b, &atualizacao.cfg)
	}
	atualizacao.estado = "ocioso"
	atualizacao.mu.Unlock()
	go atualizacaoLaco()
}

func atualizacaoSalvarLocked() error {
	b, err := json.MarshalIndent(atualizacao.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(atualizacaoConfigPath, b, 0o600)
}

// atualizacaoPacote: o nome do tar.gz desta plataforma, como o release.sh gera.
func atualizacaoPacote() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "lumnia-zap-arm64.tar.gz", nil
	case "linux/amd64":
		return "lumnia-zap-linux-amd64.tar.gz", nil
	case "linux/arm64":
		return "lumnia-zap-linux-arm64.tar.gz", nil
	}
	return "", fmt.Errorf("sem pacote de atualização para %s/%s", runtime.GOOS, runtime.GOARCH)
}

// atualizacaoComparar devolve >0 se a e mais nova que b ("1.0.10" > "1.0.6").
func atualizacaoComparar(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	pb := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}

// atualizacaoUltimaVersao pergunta ao GitHub qual e o release mais novo, sem
// usar a API (sem limite de chamadas): o redirecionamento de /releases/latest
// termina em /releases/tag/vX.Y.Z.
func atualizacaoUltimaVersao() (string, error) {
	hc := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodHead, atualizacaoLatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LumniaZap/atualizacao")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/v")
	if resp.StatusCode/100 != 3 || i < 0 {
		return "", fmt.Errorf("resposta inesperada do GitHub (HTTP %d)", resp.StatusCode)
	}
	v := strings.TrimSpace(loc[i+len("/tag/v"):])
	if v == "" || strings.ContainsAny(v, "/ ") {
		return "", fmt.Errorf("versão ilegível: %q", loc)
	}
	return v, nil
}

// atualizacaoVerificar atualiza o que o painel mostra. forcar ignora o cache.
func atualizacaoVerificar(forcar bool) {
	atualizacao.mu.Lock()
	if !forcar && time.Since(atualizacao.verificadoEm) < atualizacaoCada {
		atualizacao.mu.Unlock()
		return
	}
	if atualizacao.estado != "ocioso" && atualizacao.estado != "erro" {
		atualizacao.mu.Unlock()
		return
	}
	atualizacao.mu.Unlock()

	v, err := atualizacaoUltimaVersao()
	atualizacao.mu.Lock()
	defer atualizacao.mu.Unlock()
	atualizacao.verificadoEm = time.Now()
	if err != nil {
		if panel.logger != nil {
			panel.logger.Debugf("Atualização: não consegui conferir a versão: %v", err)
		}
		return
	}
	instalada := versaoPonte()
	if instalada == "" || atualizacaoComparar(v, instalada) > 0 {
		atualizacao.disponivel = v
	} else {
		atualizacao.disponivel = ""
	}
}

// atualizacaoResumo e o que vai no /api/panel/status.
func atualizacaoResumo() map[string]interface{} {
	atualizacao.mu.Lock()
	defer atualizacao.mu.Unlock()
	r := map[string]interface{}{
		"instalada": versaoPonte(),
		"estado":    atualizacao.estado,
		"sozinho":   atualizacao.cfg.Sozinho,
	}
	if atualizacao.disponivel != "" {
		r["disponivel"] = atualizacao.disponivel
	}
	if !atualizacao.verificadoEm.IsZero() {
		r["verificado_em"] = atualizacao.verificadoEm.Format(time.RFC3339)
	}
	if atualizacao.erro != "" {
		r["erro"] = atualizacao.erro
	}
	return r
}

func atualizacaoEstado(estado, erro string) {
	atualizacao.mu.Lock()
	atualizacao.estado = estado
	atualizacao.erro = erro
	atualizacao.mu.Unlock()
	if panel.logger != nil {
		if erro != "" {
			panel.logger.Warnf("Atualização: %s: %s", estado, erro)
		} else {
			panel.logger.Infof("Atualização: %s", estado)
		}
	}
}

// atualizacaoIniciar dispara a atualizacao em segundo plano. Devolve erro so
// se nao houver o que fazer ou ja estiver em andamento.
func atualizacaoIniciar() error {
	atualizacao.mu.Lock()
	if atualizacao.estado != "ocioso" && atualizacao.estado != "erro" {
		atualizacao.mu.Unlock()
		return errors.New("já estou atualizando")
	}
	v := atualizacao.disponivel
	atualizacao.mu.Unlock()
	if v == "" {
		atualizacaoVerificar(true)
		atualizacao.mu.Lock()
		v = atualizacao.disponivel
		atualizacao.mu.Unlock()
		if v == "" {
			return errors.New("já está na versão mais nova")
		}
	}
	go func() {
		if err := atualizacaoAplicar(v); err != nil {
			atualizacaoEstado("erro", beatitudeCurto(err.Error(), 300))
			return
		}
		atualizacaoEstado("reiniciando", "")
		time.Sleep(1500 * time.Millisecond)
		atualizacaoReiniciar()
	}()
	return nil
}

// atualizacaoAplicar baixa, extrai e troca os arquivos. Nao reinicia.
func atualizacaoAplicar(versao string) error {
	pacote, err := atualizacaoPacote()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	destino := filepath.Dir(filepath.Dir(exe)) // …/LumniaZap
	if filepath.Base(filepath.Dir(exe)) != "whatsapp-bridge" {
		return fmt.Errorf("instalação fora do padrão (%s); use o instalador", exe)
	}

	atualizacaoEstado("baixando", "")
	tgz, err := os.CreateTemp(destino, "atualizacao-*.tar.gz")
	if err != nil {
		return err
	}
	tgzNome := tgz.Name()
	defer os.Remove(tgzNome)
	hc := &http.Client{Timeout: atualizacaoTimeout}
	req, _ := http.NewRequest(http.MethodGet, atualizacaoDownloadBase+"v"+versao+"/"+pacote, nil)
	req.Header.Set("User-Agent", "LumniaZap/atualizacao")
	resp, err := hc.Do(req)
	if err != nil {
		tgz.Close()
		return fmt.Errorf("não consegui baixar a versão %s: %w", versao, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		tgz.Close()
		return fmt.Errorf("o GitHub respondeu HTTP %d para o pacote da versão %s", resp.StatusCode, versao)
	}
	n, err := io.Copy(tgz, io.LimitReader(resp.Body, 400<<20))
	resp.Body.Close()
	tgz.Close()
	if err != nil || n < 1<<20 {
		return fmt.Errorf("download incompleto (%d bytes): %v", n, err)
	}

	atualizacaoEstado("instalando", "")
	tmp, err := os.MkdirTemp(destino, "atualizacao-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := atualizacaoExtrair(tgzNome, tmp); err != nil {
		return err
	}
	return atualizacaoTrocar(tmp, destino, versao)
}

// atualizacaoExtrair: mesmo formato do instalador (primeiro componente
// "lumnia-zap/" fora; defesa contra caminhos fora da pasta; "._*" ignorados).
func atualizacaoExtrair(tgz, destino string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("pacote inválido: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		partes := strings.SplitN(filepath.Clean(h.Name), string(os.PathSeparator), 2)
		if len(partes) < 2 {
			continue
		}
		rel := partes[1]
		alvo := filepath.Join(destino, rel)
		if !strings.HasPrefix(alvo, filepath.Clean(destino)+string(os.PathSeparator)) {
			return fmt.Errorf("caminho suspeito no pacote: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(alvo, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if strings.HasPrefix(filepath.Base(rel), "._") {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(alvo, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode).Perm()|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// atualizacaoTrocar leva os arquivos extraidos para a instalacao, por rename.
// Confere antes que o pacote e mesmo da versao pedida e tem a ponte.
func atualizacaoTrocar(tmp, destino, versao string) error {
	bin := filepath.Join(tmp, "whatsapp-bridge", "whatsapp-bridge")
	if st, err := os.Stat(bin); err != nil || st.Size() < 1<<20 {
		return errors.New("o pacote não tem a ponte; nada foi trocado")
	}
	if b, err := os.ReadFile(filepath.Join(tmp, "VERSION")); err != nil || strings.TrimSpace(string(b)) != versao {
		return fmt.Errorf("o pacote diz versão %q, esperava %q; nada foi trocado", strings.TrimSpace(string(b)), versao)
	}
	// guarda o binario atual para voltar atras
	atual := filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge")
	if err := atualizacaoCopiar(atual, atual+".anterior", 0o755); err != nil && panel.logger != nil {
		panel.logger.Warnf("Atualização: não guardei o binário anterior: %v", err)
	}
	return filepath.Walk(tmp, func(caminho string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(tmp, caminho)
		alvo := filepath.Join(destino, rel)
		if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
			return err
		}
		modo := info.Mode().Perm()
		if rel == filepath.Join("whatsapp-bridge", "whatsapp-bridge") {
			modo = 0o755
		}
		// copia ao lado e troca por rename: inode novo, assinatura nova
		if err := atualizacaoCopiar(caminho, alvo+".novo", modo); err != nil {
			return err
		}
		return os.Rename(alvo+".novo", alvo)
	})
}

func atualizacaoCopiar(de, para string, modo os.FileMode) error {
	in, err := os.Open(de)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(para, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, modo)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(para, modo)
}

// atualizacaoReiniciar pede ao supervisor para subir a ponte nova. Se nao
// houver supervisor, sai: o launchd (KeepAlive) ou o systemd sobem de novo.
func atualizacaoReiniciar() {
	switch runtime.GOOS {
	case "darwin":
		uid := fmt.Sprint(os.Getuid())
		_ = exec.Command("/bin/launchctl", "kickstart", "-k", "gui/"+uid+"/"+atualizacaoServico).Start()
	case "linux":
		_ = exec.Command("systemctl", "--user", "restart", "lumnia-zap.service").Start()
	}
	time.Sleep(3 * time.Second)
	os.Exit(0)
}

// atualizacaoLaco: confere a cada 6 h e, com "sozinho" ligado, aplica entre 3h e 4h.
func atualizacaoLaco() {
	time.Sleep(45 * time.Second) // deixa a ponte subir antes de ir ao GitHub
	for {
		atualizacaoVerificar(false)
		atualizacao.mu.Lock()
		sozinho := atualizacao.cfg.Sozinho
		disponivel := atualizacao.disponivel
		hoje := time.Now().Format("2006-01-02")
		ja := atualizacao.aplicadoDia == hoje
		if sozinho && disponivel != "" && !ja && time.Now().Hour() == atualizacaoHoraAuto {
			atualizacao.aplicadoDia = hoje
		} else {
			sozinho = false
		}
		atualizacao.mu.Unlock()
		if sozinho {
			if panel.logger != nil {
				panel.logger.Infof("Atualização: aplicando a versão %s sozinha, de madrugada", disponivel)
			}
			_ = atualizacaoIniciar()
		}
		time.Sleep(30 * time.Minute)
	}
}

// ---------------------------------------------------------------- rotas

func registerAtualizacaoRoutes() {
	h := func(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isLocalRequest(r) {
				http.Error(w, "apenas localhost", http.StatusForbidden)
				return
			}
			fn(w, r)
		}
	}
	http.HandleFunc("/api/panel/atualizacao", h(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("conferir") == "1" {
			atualizacaoVerificar(true)
		}
		writeJSON(w, atualizacaoResumo())
	}))
	http.HandleFunc("/api/panel/atualizar", h(func(w http.ResponseWriter, r *http.Request) {
		if !beatitudeExigePainel(w, r) {
			return
		}
		if err := atualizacaoIniciar(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true})
	}))
	http.HandleFunc("/api/panel/atualizacao/sozinho", h(func(w http.ResponseWriter, r *http.Request) {
		if !beatitudeExigePainel(w, r) {
			return
		}
		var p struct {
			Sozinho bool `json:"sozinho"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "corpo inválido", http.StatusBadRequest)
			return
		}
		atualizacao.mu.Lock()
		atualizacao.cfg.Sozinho = p.Sozinho
		err := atualizacaoSalvarLocked()
		atualizacao.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "sozinho": p.Sozinho})
	}))
}
