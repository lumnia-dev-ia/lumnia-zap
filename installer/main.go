// Instalador Lumnia Zap — instala a ponte WhatsApp↔Claude no Mac do usuario.
//
// Roda como o proprio usuario (nao como root), dentro de um .app assinado e
// notarizado, para que a instalacao seja duplo clique sem aviso do Gatekeeper.
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	produto     = "Lumnia Zap"
	label       = "dev.lumnia.zap"
	pacoteURL   = "https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/lumnia-zap-arm64.tar.gz"
	painelURL   = "http://localhost:8080"
	claudeApp   = "/Applications/Claude.app"
	repoURL     = "https://github.com/lumnia-dev-ia/lumnia-zap"
	claudeBaixa = "https://claude.ai/download"
)

func main() {
	if err := instalar(); err != nil {
		progressoFalhar(err.Error())
		alerta("A instalação não foi concluída",
			err.Error()+"\n\nAbri uma página no navegador com os detalhes.\n\n"+
				"Nada foi deixado pela metade — pode tentar de novo.", true)
		os.Exit(1)
	}
}

func instalar() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("este instalador é só para macOS")
	}
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("este instalador é só para Macs com chip Apple (M1 ou mais novo).\n" +
			"Se o seu Mac é Intel, fale com o Diego")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Quais agentes de IA existem neste Mac. O Codex e aditivo: quem tem os
	// dois configura os dois; quem tem so um segue como antes. So bloqueamos
	// quando nao ha NENHUM cliente MCP para conectar.
	claudeOK := true
	if _, err := os.Stat(claudeApp); os.IsNotExist(err) {
		claudeOK = false
	}
	codexOK := detectarCodex(home)
	if !claudeOK && !codexOK {
		if perguntar("Nenhum agente de IA encontrado",
			"O "+produto+" funciona dentro do Claude Desktop (ou do OpenAI Codex), "+
				"e nenhum dos dois está instalado neste Mac.\n\n"+
				"Quer abrir a página de download do Claude agora?") {
			exec.Command("/usr/bin/open", claudeBaixa).Run()
		}
		return fmt.Errorf("instale o Claude Desktop (ou o Codex) e rode este instalador de novo")
	}

	agentes := "ao Claude"
	switch {
	case claudeOK && codexOK:
		agentes = "ao Claude e ao Codex"
	case codexOK:
		agentes = "ao Codex"
	}

	// Tela de consentimento: a pessoa merece saber o que esta aceitando.
	if !perguntar("Instalar o "+produto+"?",
		"Isto conecta o seu WhatsApp "+agentes+" neste Mac.\n\n"+
			"O assistente passa a poder LER e RESPONDER suas mensagens quando você pedir. "+
			"Suas conversas ficam guardadas só aqui, neste computador.\n\n"+
			"Você conecta escaneando um QR code, igual ao WhatsApp Web, e pode "+
			"desconectar quando quiser.") {
		return fmt.Errorf("instalação cancelada por você")
	}
	destino := filepath.Join(home, "Library", "Application Support", "LumniaZap")

	// A partir daqui a instalacao pode demorar. Abre a tela de progresso para
	// que ninguem fique olhando para uma tela parada sem saber se travou.
	progressoAbrir()

	etapa(0)
	// Se ja existe instalacao, para a ponte antes de trocar os arquivos: nao
	// da para substituir com seguranca um binario que esta rodando.
	pararServico()

	etapa(1)
	tgz, err := baixar(pacoteURL)
	if err != nil {
		return fmt.Errorf("não consegui baixar o pacote (verifique sua internet): %v", err)
	}
	defer os.Remove(tgz)

	etapa(2)
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return err
	}
	if err := extrair(tgz, destino); err != nil {
		return fmt.Errorf("falha ao extrair o pacote: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destino, "logs"), 0o755); err != nil {
		return err
	}

	etapa(3)
	uv, err := garantirUV(home)
	if err != nil {
		return fmt.Errorf("não consegui preparar o ambiente Python: %v", err)
	}

	etapa(4)
	var avisoCodex string
	if claudeOK {
		if err := registrarNoClaude(home, destino, uv); err != nil {
			return fmt.Errorf("falha ao configurar o Claude Desktop: %v", err)
		}
	}
	if codexOK {
		// O Codex e aditivo: um problema aqui nao pode derrubar uma
		// instalacao que ja deixou o Claude funcionando. Guardamos o aviso
		// para mostrar no resumo final em vez de abortar.
		if err := registrarNoCodex(home, destino, uv); err != nil {
			avisoCodex = err.Error()
		}
	}

	etapa(5)
	if err := instalarServico(home, destino); err != nil {
		return fmt.Errorf("falha ao configurar o serviço: %v", err)
	}
	if err := criarAtalho(home, destino); err != nil {
		return fmt.Errorf("falha ao criar o atalho: %v", err)
	}

	etapa(6)
	if err := subirServico(home); err != nil {
		return fmt.Errorf("falha ao iniciar o serviço.\n\n%v", err)
	}
	if !esperarPainel(40 * time.Second) {
		return fmt.Errorf("o serviço não respondeu a tempo.\n\n%s", porQueNaoSubiu(destino))
	}

	progressoConcluir()
	proximo := ""
	if claudeOK {
		proximo += "Depois, feche o Claude Desktop com Cmd+Q e abra de novo.\n\n"
	}
	if codexOK && avisoCodex == "" {
		proximo += "No Codex, o lumnia-zap já entra na próxima sessão.\n\n"
	}
	alerta("Pronto!",
		"O "+produto+" está instalado.\n\n"+
			resumoAgentes(claudeOK, codexOK, avisoCodex)+"\n\n"+
			"A página que abriu mostra um QR code. Escaneie com o WhatsApp do seu "+
			"celular em Configurações → Aparelhos conectados.\n\n"+
			proximo+
			"O atalho ficou na sua Mesa.", false)
	return nil
}

// resumoAgentes monta as linhas do resumo final: quem foi configurado, quem
// nao foi encontrado e o que deu errado — para ninguem sair achando que o
// Codex foi configurado num Mac que nem o tem.
func resumoAgentes(claudeOK, codexOK bool, avisoCodex string) string {
	linhas := []string{"Agentes de IA:"}
	if claudeOK {
		linhas = append(linhas, "✓ Claude configurado")
	} else {
		linhas = append(linhas, "○ Claude não encontrado")
	}
	switch {
	case !codexOK:
		linhas = append(linhas, "○ Codex não encontrado")
	case avisoCodex != "":
		linhas = append(linhas, "✗ Codex: "+avisoCodex)
	default:
		linhas = append(linhas, "✓ Codex configurado")
	}
	return strings.Join(linhas, "\n")
}

// ------------------------------------------------------------------ progresso

// A tela de progresso e uma pagina HTML local que se recarrega sozinha a cada
// segundo. E feia de propósito: nao depende de nenhuma biblioteca grafica,
// funciona em qualquer Mac e usa o mesmo navegador que ja vai abrir o painel
// no final. Sem ela a instalacao parece travada, porque baixar 15 MB e rodar
// o `uv sync` leva um tempo em que nada aparece na tela.

var passos = []string{
	"Verificando o Mac",
	"Baixando o " + produto + " (uns 15 MB)",
	"Instalando os arquivos",
	"Preparando o ambiente Python",
	"Configurando os agentes de IA (Claude/Codex)",
	"Configurando o serviço",
	"Iniciando a ponte",
}

var (
	passoAtual   = -1
	paginaPath   = filepath.Join(os.TempDir(), "lumnia-zap-instalacao.html")
	paginaAberta bool
)

func progressoAbrir() {
	passoAtual = 0
	escreverPagina(cabecalhoPagina(1), corpoPassos(), "")
	if !paginaAberta {
		exec.Command("/usr/bin/open", paginaPath).Run()
		paginaAberta = true
		time.Sleep(700 * time.Millisecond) // deixa o navegador aparecer
	}
}

func etapa(i int) {
	passoAtual = i
	escreverPagina(cabecalhoPagina(1), corpoPassos(), "")
}

func progressoConcluir() {
	passoAtual = len(passos)
	// A propria pagina vira o painel: recarrega apontando para o QR code.
	escreverPagina(
		`<meta http-equiv="refresh" content="3; url=`+painelURL+`">`,
		corpoPassos()+`<p class="ok">Pronto! Abrindo o painel com o QR code…</p>`,
		"")
}

func progressoFalhar(detalhe string) {
	if !paginaAberta {
		return // nem chegou a comecar; o alerta sozinho ja explica
	}
	extra := `<p class="erro"><b>A instalação parou aqui.</b></p><pre>` +
		html.EscapeString(detalhe) + `</pre>` +
		`<p>Mande esta tela para o Diego — ela já tem o que ele precisa.</p>`
	escreverPagina("", corpoPassos(), extra)
}

func cabecalhoPagina(segundos int) string {
	return fmt.Sprintf(`<meta http-equiv="refresh" content="%d">`, segundos)
}

func corpoPassos() string {
	var b strings.Builder
	b.WriteString(`<ol>`)
	for i, p := range passos {
		classe, marca := "futuro", "○"
		switch {
		case i < passoAtual:
			classe, marca = "feito", "✓"
		case i == passoAtual:
			classe, marca = "agora", "●"
		}
		fmt.Fprintf(&b, `<li class="%s"><span class="m">%s</span>%s</li>`,
			classe, marca, html.EscapeString(p))
	}
	b.WriteString(`</ol>`)
	return b.String()
}

func escreverPagina(cabecalho, corpo, extra string) {
	pag := `<!doctype html><html lang="pt-br"><head><meta charset="utf-8">` +
		cabecalho +
		`<title>Instalando o ` + produto + `</title><style>
body{font:16px/1.55 -apple-system,BlinkMacSystemFont,"Helvetica Neue",sans-serif;
 max-width:560px;margin:60px auto;padding:0 24px;color:#1c1c1e;background:#fff}
h1{font-size:22px;margin:0 0 4px}
p.sub{color:#6b6b70;margin:0 0 28px}
ol{list-style:none;padding:0;margin:0}
li{padding:9px 0;display:flex;align-items:baseline;gap:12px;border-bottom:1px solid #f0f0f2}
li:last-child{border-bottom:0}
.m{width:18px;display:inline-block;text-align:center}
.feito{color:#8a8a8e}.feito .m{color:#34a853}
.agora{font-weight:600}.agora .m{color:#1a73e8}
.futuro{color:#b8b8bd}
p.ok{margin-top:28px;font-weight:600;color:#34a853}
p.erro{margin-top:28px;color:#c5221f}
pre{white-space:pre-wrap;word-break:break-word;background:#f6f6f8;border:1px solid #e5e5ea;
 border-radius:8px;padding:14px;font:12px/1.5 ui-monospace,Menlo,monospace;color:#3c3c43}
@media (prefers-color-scheme:dark){
 body{background:#1c1c1e;color:#f2f2f7}li{border-bottom-color:#2c2c2e}
 pre{background:#2c2c2e;border-color:#3a3a3c;color:#d1d1d6}p.sub{color:#98989d}}
</style></head><body>
<h1>Instalando o ` + produto + `</h1>
<p class="sub">Pode deixar esta janela aberta. Leva de um a três minutos.</p>` +
		corpo + extra + `</body></html>`
	os.WriteFile(paginaPath, []byte(pag), 0o644)
}

// ------------------------------------------------------------------ download

func baixar(url string) (string, error) {
	cli := &http.Client{Timeout: 10 * time.Minute}
	resp, err := cli.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("servidor respondeu %s", resp.Status)
	}
	f, err := os.CreateTemp("", "lumnia-zap-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// extrair descompacta o tar.gz removendo o diretorio raiz do arquivo.
func extrair(tgz, destino string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
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
		// tira o primeiro componente ("lumnia-zap/")
		partes := strings.SplitN(filepath.Clean(h.Name), string(os.PathSeparator), 2)
		if len(partes) < 2 {
			continue
		}
		rel := partes[1]
		// defesa contra caminhos maliciosos no tar
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
			// Os "._alguma-coisa" sao resource forks que o tar do macOS gera
			// junto. Nao servem para nada aqui e so poluem a pasta.
			if strings.HasPrefix(filepath.Base(rel), "._") {
				continue
			}
			if err := escreverArquivo(alvo, tr, os.FileMode(h.Mode).Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

// escreverArquivo grava num arquivo temporario e troca por cima com rename,
// em vez de truncar o arquivo que ja existe.
//
// Isto nao e frescura: sobrescrever um binario ASSINADO no lugar mantem o
// mesmo inode, e o kernel do macOS continua com a assinatura antiga em cache
// para aquele arquivo. O binario novo nao bate com ela e todo exec passa a
// morrer com SIGKILL — "load code signature error" no log do sistema e
// `last exit reason = OS_REASON_CODESIGNING` no launchctl. Foi exatamente
// isso que quebrou a reinstalacao no Mac do meu pai em 06/08/2026: a ponte
// era morta 2.380 vezes seguidas e o log ficava vazio, porque o processo
// morria antes de escrever a primeira linha.
//
// O rename e atomico e sempre produz um inode novo, entao o kernel avalia a
// assinatura do zero. De quebra, definir a permissao na mao evita o caso do
// arquivo vir somente-leitura no pacote e a proxima instalacao falhar com
// "permission denied" — a mesma classe de bug que apareceu no Windows.
func escreverArquivo(alvo string, r io.Reader, modo os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
		return err
	}
	tmp := alvo + ".novo"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if modo == 0 {
		modo = 0o644
	}
	// Chmod explicito: o modo do OpenFile passa pelo umask do usuario.
	if err := os.Chmod(tmp, modo|0o200); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, alvo); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ------------------------------------------------------------------ ambiente

// garantirUV devolve o caminho do uv, instalando-o se necessario. O uv resolve
// o Python sozinho, o que evita exigir Homebrew ou Xcode de quem instala.
func garantirUV(home string) (string, error) {
	candidatos := []string{
		filepath.Join(home, ".local", "bin", "uv"),
		"/opt/homebrew/bin/uv",
		"/usr/local/bin/uv",
	}
	for _, c := range candidatos {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	cmd := exec.Command("/bin/sh", "-c",
		"curl -LsSf https://astral.sh/uv/install.sh | sh")
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	uv := filepath.Join(home, ".local", "bin", "uv")
	if _, err := os.Stat(uv); err != nil {
		return "", fmt.Errorf("uv não apareceu em %s", uv)
	}
	return uv, nil
}

// ------------------------------------------------------------------ Claude

// registrarNoClaude adiciona o servidor MCP preservando o resto do arquivo,
// que guarda tambem as preferencias pessoais do Claude Desktop.
func registrarNoClaude(home, destino, uv string) error {
	cfgDir := filepath.Join(home, "Library", "Application Support", "Claude")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return err
	}
	cfgPath := filepath.Join(cfgDir, "claude_desktop_config.json")

	cfg := map[string]any{}
	if raw, err := os.ReadFile(cfgPath); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("o arquivo de configuração do Claude parece corrompido; "+
				"renomeie %s e tente de novo", cfgPath)
		}
		// backup antes de mexer
		_ = os.WriteFile(cfgPath+".bak-lumnia-zap", raw, 0o600)
	}

	servidores, _ := cfg["mcpServers"].(map[string]any)
	if servidores == nil {
		servidores = map[string]any{}
	}
	servidores["lumnia-zap"] = map[string]any{
		"command": uv,
		"args": []string{
			"--directory", filepath.Join(destino, "whatsapp-mcp-server"),
			"run", "main.py",
		},
	}
	cfg["mcpServers"] = servidores

	novo, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, novo, 0o600)
}

// ------------------------------------------------------------------ servico

func instalarServico(home, destino string) error {
	agentes := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentes, 0o755); err != nil {
		return err
	}
	bin := filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge")
	if err := os.Chmod(bin, 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string></array>
	<key>WorkingDirectory</key><string>%s</string>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, label, bin, filepath.Join(destino, "whatsapp-bridge"),
		filepath.Join(destino, "logs", "bridge.log"),
		filepath.Join(destino, "logs", "bridge-error.log"))

	return os.WriteFile(filepath.Join(agentes, label+".plist"), []byte(plist), 0o644)
}

// pararServico derruba uma instancia anterior antes de trocar os arquivos.
// Erro aqui e esperado quando nao havia nada instalado.
func pararServico() {
	uid := fmt.Sprint(os.Getuid())
	exec.Command("/bin/launchctl", "bootout", "gui/"+uid+"/"+label).Run()
	time.Sleep(1500 * time.Millisecond)
}

// subirServico carrega o LaunchAgent e DEVOLVE o erro do launchctl.
//
// A versao anterior chamava bootout e bootstrap com `.Run()` e jogava fora o
// resultado dos dois. Quando o bootstrap falhava — e ele falha com
// "Bootstrap failed: 5: Input/output error" se o job anterior ainda estiver
// morrendo — o instalador seguia em frente, esperava o painel e terminava
// com um "o serviço não respondeu a tempo" que nao dizia nada. Silenciar
// stderr ja custou caro tres vezes neste projeto; aqui nao mais.
func subirServico(home string) error {
	uid := fmt.Sprint(os.Getuid())
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")

	// Um job pode ter ficado marcado como desabilitado por uma tentativa
	// anterior. Esse marcador sobrevive a reinstalacao e a reinicio, e
	// enquanto ele estiver la nem bootstrap nem RunAtLoad sobem coisa alguma.
	exec.Command("/bin/launchctl", "enable", "gui/"+uid+"/"+label).Run()

	saidaBootout, _ := exec.Command("/bin/launchctl",
		"bootout", "gui/"+uid+"/"+label).CombinedOutput()

	// O launchd leva um instante para liberar o nome do job depois do
	// bootout. Meio segundo fixo nao bastava; aqui tentamos seis vezes com
	// espera crescente (1s, 2s, 3s... ate 6s).
	var ultimo string
	for tentativa := 1; tentativa <= 6; tentativa++ {
		time.Sleep(time.Duration(tentativa) * time.Second)
		out, err := exec.Command("/bin/launchctl",
			"bootstrap", "gui/"+uid, plist).CombinedOutput()
		if err == nil {
			return nil
		}
		ultimo = strings.TrimSpace(string(out))
		if ultimo == "" {
			ultimo = err.Error()
		}
	}
	msg := "o launchctl recusou iniciar o serviço depois de 6 tentativas.\n\nÚltimo erro:\n" + ultimo
	if s := strings.TrimSpace(string(saidaBootout)); s != "" {
		msg += "\n\nSaída do bootout anterior:\n" + s
	}
	return fmt.Errorf("%s", msg)
}

func esperarPainel(limite time.Duration) bool {
	cli := &http.Client{Timeout: 2 * time.Second}
	fim := time.Now().Add(limite)
	for time.Now().Before(fim) {
		if resp, err := cli.Get(painelURL + "/api/panel/status"); err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// porQueNaoSubiu monta o relatorio que o instalador antigo nao dava: em vez
// de apontar o caminho de um log que quase sempre esta vazio, ele mesmo le o
// estado do launchd e o fim dos dois logs, e traduz o motivo mais comum.
func porQueNaoSubiu(destino string) string {
	uid := fmt.Sprint(os.Getuid())
	saida, _ := exec.Command("/bin/launchctl", "print", "gui/"+uid+"/"+label).CombinedOutput()

	var estado []string
	for _, l := range strings.Split(string(saida), "\n") {
		t := strings.TrimSpace(l)
		for _, chave := range []string{"state = ", "job state = ", "last exit reason = ",
			"last exit code = ", "runs = "} {
			if strings.HasPrefix(t, chave) {
				estado = append(estado, t)
			}
		}
	}

	msg := "Estado do serviço:\n  " + strings.Join(estado, "\n  ")
	if len(estado) == 0 {
		msg = "O launchd não tem o serviço carregado."
	}

	if strings.Contains(string(saida), "OS_REASON_CODESIGNING") {
		msg += "\n\nO sistema está matando a ponte por assinatura de código.\n" +
			"Rode o instalador mais uma vez: esta versão substitui o arquivo\n" +
			"por um novo em vez de escrever por cima, o que resolve esse caso."
	}

	logs := filepath.Join(destino, "logs")
	msg += "\n\nFim de bridge-error.log:\n" + ultimasLinhas(filepath.Join(logs, "bridge-error.log"), 12)
	msg += "\n\nFim de bridge.log:\n" + ultimasLinhas(filepath.Join(logs, "bridge.log"), 12)
	return msg
}

// ultimasLinhas devolve o fim de um arquivo de log, pulando as linhas de
// conversa (que comecam com a data entre colchetes) para nao expor mensagem
// de ninguem numa tela que provavelmente vai virar print.
func ultimasLinhas(caminho string, n int) string {
	b, err := os.ReadFile(caminho)
	if err != nil {
		return "  (arquivo não existe)"
	}
	if len(b) == 0 {
		return "  (vazio)"
	}
	var uteis []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.HasPrefix(l, "[20") || strings.ContainsAny(l, "█▀▄") {
			continue
		}
		uteis = append(uteis, "  "+l)
	}
	if len(uteis) > n {
		uteis = uteis[len(uteis)-n:]
	}
	if len(uteis) == 0 {
		return "  (nada além de mensagens)"
	}
	return strings.Join(uteis, "\n")
}

// ------------------------------------------------------------------ atalho

func criarAtalho(home, destino string) error {
	app := filepath.Join(home, "Desktop", produto+".app")
	_ = os.RemoveAll(app)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(app, "Contents", "Resources"), 0o755); err != nil {
		return err
	}
	if icone, err := os.ReadFile(filepath.Join(destino, "painel.icns")); err == nil {
		_ = os.WriteFile(filepath.Join(app, "Contents", "Resources", "painel.icns"), icone, 0o644)
	}
	info := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Lumnia Zap</string>
	<key>CFBundleDisplayName</key><string>Lumnia Zap</string>
	<key>CFBundleIdentifier</key><string>dev.lumnia.zap.launcher</string>
	<key>CFBundleExecutable</key><string>zap</string>
	<key>CFBundleIconFile</key><string>painel</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(info), 0o644); err != nil {
		return err
	}
	launcher := fmt.Sprintf(`#!/bin/sh
LABEL="%s"
URL="%s"
MYUID="$(/usr/bin/id -u)"
alive() { /usr/bin/curl -s -o /dev/null --max-time 2 "$URL/api/panel/status"; }
if ! alive; then
	/bin/launchctl enable "gui/$MYUID/$LABEL" 2>/dev/null
	if ! /bin/launchctl kickstart -k "gui/$MYUID/$LABEL" 2>/dev/null; then
		/bin/launchctl bootstrap "gui/$MYUID" "$HOME/Library/LaunchAgents/$LABEL.plist" 2>/dev/null
	fi
	i=0
	while [ "$i" -lt 40 ]; do
		if alive; then break; fi
		/bin/sleep 0.5
		i=$((i + 1))
	done
fi
if alive; then
	/usr/bin/open "$URL"
else
	/usr/bin/osascript -e 'display alert "Lumnia Zap indisponível" message "A ponte não respondeu. Rode o instalador de novo." as critical'
fi
`, label, painelURL)
	if err := os.WriteFile(filepath.Join(app, "Contents", "MacOS", "zap"), []byte(launcher), 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(app, "Contents", "PkgInfo"), []byte("APPL????"), 0o644)
	exec.Command("/usr/bin/xattr", "-cr", app).Run()
	return nil
}

// ------------------------------------------------------------------ dialogos

func osa(script string) (string, error) {
	out, err := exec.Command("/usr/bin/osascript", "-e", script).Output()
	return strings.TrimSpace(string(out)), err
}

func escapar(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
}

func alerta(titulo, msg string, critico bool) {
	tipo := ""
	if critico {
		tipo = " as critical"
	}
	osa(fmt.Sprintf(`display alert "%s" message "%s"%s`, escapar(titulo), escapar(msg), tipo))
}

func perguntar(titulo, msg string) bool {
	_, err := osa(fmt.Sprintf(
		`display alert "%s" message "%s" buttons {"Cancelar","Continuar"} default button "Continuar" cancel button "Cancelar"`,
		escapar(titulo), escapar(msg)))
	return err == nil
}
