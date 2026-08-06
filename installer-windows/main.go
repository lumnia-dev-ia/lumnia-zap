// Instalador Lumnia Zap (Windows) — instala a ponte WhatsApp<->Claude no PC
// Windows do usuario.
//
// Roda como o proprio usuario (sem exigir Administrador), como um app de
// console: mostra o progresso na janela que abre ao dar duplo clique.
//
// Equivalente ao instalador de macOS (../installer/main.go), mas usando
// mecanismos nativos do Windows no lugar de launchd/osascript/.app bundle:
//   - autostart + keepalive: uma Tarefa Agendada do Windows que roda no logon
//     e um laco em cmd que sobe a ponte de novo se ela cair, no lugar do
//     RunAtLoad/KeepAlive do launchd
//   - dialogos: prompts no console + uma caixa de mensagem final via
//     PowerShell (best-effort — se falhar, so ignora e segue)
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	produto     = "Lumnia Zap"
	pacoteURL   = "https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/lumnia-zap-windows-amd64.zip"
	painelURL   = "http://localhost:8080"
	repoURL     = "https://github.com/lumnia-dev-ia/lumnia-zap"
	claudeBaixa = "https://claude.ai/download"
	nomeTarefa  = "Lumnia Zap"
)

func pastaLocal(home string) string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return local
	}
	return filepath.Join(home, "AppData", "Local")
}

// pastasMSIX devolve as pastas de pacote do Claude instalado pela Microsoft
// Store. O MSIX roda num sistema de arquivos virtualizado: o que o app enxerga
// como %APPDATA%\Claude fica de verdade em
//
//	%LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming\Claude
//
// O <hash> e a identidade do pacote. Hoje e "pzs8sxrjxfjjc", mas nao vale
// fixar: procuramos por padrao.
func pastasMSIX(home string) []string {
	encontradas, _ := filepath.Glob(filepath.Join(pastaLocal(home), "Packages", "Claude_*"))
	var out []string
	for _, p := range encontradas {
		if st, err := os.Stat(filepath.Join(p, "LocalCache", "Roaming")); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// claudeInstalado diz se achamos o Claude Desktop nesta maquina, cobrindo as
// duas formas de distribuicao (instalador classico e Microsoft Store). E so
// uma checagem informativa — nunca bloqueia a instalacao, porque a lista de
// caminhos envelhece.
func claudeInstalado(home string) bool {
	if len(pastasMSIX(home)) > 0 {
		return true
	}
	local := pastaLocal(home)
	for _, c := range []string{
		filepath.Join(local, "AnthropicClaude", "claude.exe"),
		filepath.Join(local, "Programs", "Claude", "Claude.exe"),
		filepath.Join(local, "Programs", "claude", "Claude.exe"),
	} {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		if st, err := os.Stat(filepath.Join(appdata, "Claude")); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func main() {
	if err := instalar(); err != nil {
		fmt.Println()
		fmt.Println("A instalação não foi concluída:", err)
		fmt.Println()
		fmt.Println("Nada foi deixado pela metade — pode tentar de novo.")
		pausar()
		os.Exit(1)
	}
	pausar()
}

func pausar() {
	fmt.Println()
	fmt.Println("Pressione Enter para fechar...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func instalar() error {
	fmt.Println(produto + " — instalador para Windows")
	fmt.Println(strings.Repeat("-", 40))

	if runtime.GOOS != "windows" {
		return fmt.Errorf("este instalador é só para Windows")
	}
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("este instalador é só para PCs Windows 64-bit (amd64).\n" +
			"Se o seu PC é ARM (Windows on ARM), fale com o Diego")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	if !claudeInstalado(home) {
		fmt.Println()
		fmt.Println("Não encontramos automaticamente o Claude Desktop instalado neste PC.")
		fmt.Println("Isso pode ser normal (a instalação do Claude varia de PC pra PC) — mas")
		fmt.Println("o " + produto + " só funciona dentro do Claude Desktop.")
		if !confirmar("Baixar o Claude Desktop agora antes de continuar?", false) {
			// segue sem abrir nada
		} else {
			abrirNavegador(claudeBaixa)
			fmt.Println()
			fmt.Println("Instale o Claude Desktop e depois rode este instalador de novo.")
			return nil
		}
	}

	// Tela de consentimento: a pessoa merece saber o que esta aceitando.
	fmt.Println()
	fmt.Println("Isto conecta o seu WhatsApp ao Claude neste PC.")
	fmt.Println()
	fmt.Println("O Claude passa a poder LER e RESPONDER suas mensagens quando você pedir.")
	fmt.Println("Suas conversas ficam guardadas só aqui, neste computador.")
	fmt.Println()
	fmt.Println("Você conecta escaneando um QR code, igual ao WhatsApp Web, e pode")
	fmt.Println("desconectar quando quiser.")
	fmt.Println()
	if !confirmar("Instalar o "+produto+"?", true) {
		return fmt.Errorf("instalação cancelada por você")
	}

	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	destino := filepath.Join(local, "LumniaZap")

	zipPath, limpar, err := obterPacote()
	if err != nil {
		return fmt.Errorf("não consegui baixar o pacote (verifique sua internet): %v", err)
	}
	if limpar {
		defer os.Remove(zipPath)
	}

	etapa("Instalando…")
	// Numa reinstalacao a ponte pode estar rodando — e no Windows nao se
	// sobrescreve um .exe em execucao. Derruba antes de extrair.
	pararServico()
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return err
	}
	if err := extrairZip(zipPath, destino); err != nil {
		return fmt.Errorf("falha ao extrair o pacote: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destino, "logs"), 0o755); err != nil {
		return err
	}

	etapa("Preparando o ambiente…")
	uv, err := garantirUV(home)
	if err != nil {
		return fmt.Errorf("não consegui preparar o ambiente Python: %v", err)
	}

	etapa("Preparando o Python (pode levar um minuto)…")
	if err := prepararPython(uv, destino); err != nil {
		return fmt.Errorf("não consegui preparar o Python do servidor: %v", err)
	}

	etapa("Conectando ao Claude Desktop…")
	if err := registrarNoClaude(home, destino, uv); err != nil {
		return fmt.Errorf("falha ao configurar o Claude Desktop: %v", err)
	}

	etapa("Configurando o serviço…")
	launcher, err := escreverLauncher(destino)
	if err != nil {
		return fmt.Errorf("falha ao configurar o serviço: %v", err)
	}
	limparInstalacaoAntiga(destino)
	usouTarefa := true
	if err := instalarTarefa(destino, launcher); err != nil {
		// PCs com politica que bloqueia schtasks: cai para a pasta Inicializar,
		// que so exige escrever um atalho.
		fmt.Println("     (a Tarefa Agendada foi bloqueada neste PC; usando a pasta Inicializar)")
		usouTarefa = false
		if err2 := instalarStartupFallback(destino, launcher); err2 != nil {
			return fmt.Errorf("falha ao registrar a inicialização automática: %v / %v", err, err2)
		}
	}
	if caminho, err := criarAtalhoDesktop(home, destino); err != nil {
		// atalho da Área de Trabalho é conveniência, não impede a instalação
		fmt.Println("     (aviso: não consegui criar o atalho na Área de Trabalho:", err, ")")
	} else {
		fmt.Println("     atalho criado em:", caminho)
	}
	// tambem no Menu Iniciar: imune as esquisitices de OneDrive corporativo
	// com a Area de Trabalho, e faz o app aparecer na busca do Windows.
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		_ = criarAtalho(
			filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", produto+".lnk"),
			filepath.Join(destino, "abrir.cmd"), "", filepath.Join(destino, "painel.ico"), true)
	}

	etapa("Iniciando…")
	logPath := filepath.Join(destino, "logs", "bridge.log")
	if !usouTarefa {
		subirDireto(launcher)
	} else if saida, err := exec.Command("schtasks", "/run", "/tn", nomeTarefa).CombinedOutput(); err != nil {
		fmt.Println("     (a tarefa não iniciou:", strings.TrimSpace(string(saida)), "— subindo direto)")
		subirDireto(launcher)
	}
	if !esperarPainel(60 * time.Second) {
		return fmt.Errorf("o serviço não respondeu a tempo.%s\n\nO log completo está em:\n%s",
			finalDoLog(logPath), logPath)
	}

	abrirNavegador(painelURL)
	fmt.Println()
	fmt.Println(produto + " está instalado.")
	fmt.Println()
	fmt.Println("A página que abriu mostra um QR code. Escaneie com o WhatsApp do seu")
	fmt.Println("celular em Configurações → Aparelhos conectados.")
	fmt.Println()
	fmt.Println("Depois, feche o Claude Desktop e abra de novo.")
	fmt.Println()
	fmt.Println("O atalho ficou na sua Área de Trabalho e no Menu Iniciar (procure")
	fmt.Println("por Lumnia Zap), e a ponte inicia sozinha quando você liga o PC.")
	msgBox("Pronto!", produto+" foi instalado. Escaneie o QR code que abriu no navegador.")
	return nil
}

// ------------------------------------------------------------------ download

// obterPacote usa um lumnia-zap-windows-amd64.zip que esteja na mesma pasta
// do instalador, se existir (útil pra testar sem precisar publicar uma
// release no GitHub primeiro). Senão, baixa da URL da release. O segundo
// valor de retorno indica se o chamador deve apagar o arquivo depois de usar
// (só o baixado; o local, que é do usuário, fica intacto).
func obterPacote() (caminho string, apagarDepois bool, err error) {
	if exe, e := os.Executable(); e == nil {
		local := filepath.Join(filepath.Dir(exe), "lumnia-zap-windows-amd64.zip")
		if st, e := os.Stat(local); e == nil && !st.IsDir() {
			etapa("Usando pacote local (" + local + ")…")
			return local, false, nil
		}
	}
	etapa("Baixando o " + produto + "…")
	caminho, err = baixar(pacoteURL)
	return caminho, true, err
}

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
	f, err := os.CreateTemp("", "lumnia-zap-*.zip")
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

// extrairZip descompacta o zip removendo o diretorio raiz do arquivo (se
// houver um unico diretorio-raiz, como no tar.gz da versao Mac).
func extrairZip(zipPath, destino string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	prefixo := ""
	if len(r.File) > 0 {
		partes := strings.SplitN(filepath.ToSlash(r.File[0].Name), "/", 2)
		if len(partes) == 2 {
			prefixo = partes[0] + "/"
		}
	}

	for _, f := range r.File {
		nome := filepath.ToSlash(f.Name)
		rel := strings.TrimPrefix(nome, prefixo)
		if rel == "" {
			continue
		}
		alvo := filepath.Join(destino, filepath.FromSlash(rel))
		// defesa contra caminhos maliciosos no zip
		if !strings.HasPrefix(alvo, filepath.Clean(destino)+string(os.PathSeparator)) {
			return fmt.Errorf("caminho suspeito no pacote: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(alvo, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		// NAO use f.Mode() aqui. Um arquivo que veio do zip com modo 0444
		// (somente-leitura no Unix) vira FILE_ATTRIBUTE_READONLY no Windows,
		// e a reinstalacao seguinte falha com "Acesso negado" ao tentar
		// sobrescrever. Limpamos o atributo e removemos antes de gravar.
		_ = os.Chmod(alvo, 0o666)
		_ = os.Remove(alvo)
		modo := os.FileMode(0o644)
		if strings.HasSuffix(strings.ToLower(rel), ".exe") {
			modo = 0o755
		}
		// Ate 3 tentativas: logo apos derrubar a ponte, o Windows (ou o
		// antivirus escaneando o processo morto) pode segurar o handle do
		// .exe por alguns segundos.
		var out *os.File
		for tent := 1; ; tent++ {
			out, err = os.OpenFile(alvo, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modo)
			if err == nil {
				break
			}
			if tent >= 3 {
				rc.Close()
				return err
			}
			time.Sleep(1500 * time.Millisecond)
			_ = os.Chmod(alvo, 0o666)
			_ = os.Remove(alvo)
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------------------ ambiente

// garantirUV devolve o caminho do uv, instalando-o se necessario. O uv
// resolve o Python sozinho, o que evita exigir uma instalacao Python separada
// de quem instala.
func garantirUV(home string) (string, error) {
	// A copia propria vem ANTES do PATH: no PC de teste, o PATH devolvia o uv
	// embutido do Langflow Desktop — funciona, mas desinstalar o Langflow
	// quebraria o Zap sem nenhuma pista. Preferimos um caminho que e nosso.
	proprio := filepath.Join(home, ".local", "bin", "uv.exe")
	if st, err := os.Stat(proprio); err == nil && !st.IsDir() {
		return proprio, nil
	}
	if p, err := exec.LookPath("uv.exe"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("uv"); err == nil {
		return p, nil
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "ByPass",
		"-Command", "irm https://astral.sh/uv/install.ps1 | iex")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	uv := filepath.Join(home, ".local", "bin", "uv.exe")
	if _, err := os.Stat(uv); err != nil {
		return "", fmt.Errorf("uv não apareceu em %s", uv)
	}
	return uv, nil
}

// prepararPython baixa o Python e as dependencias do servidor MCP AGORA, no
// instalador — com console visivel e direito a nova tentativa. Antes isso
// acontecia escondido na primeira subida do MCP dentro do Claude, e um bug
// conhecido do uv ("Missing expected target directory for Python minor
// version link") deixava %APPDATA%\uv\python corrompido: o MCP falhava para
// sempre com "Server disconnected" e nenhuma pista. Se o erro aparecer,
// limpamos o estado corrompido e tentamos mais uma vez.
func prepararPython(uv, destino string) error {
	dir := filepath.Join(destino, "whatsapp-mcp-server")
	tentar := func() ([]byte, error) {
		return exec.Command(uv, "--directory", dir, "sync").CombinedOutput()
	}
	out, err := tentar()
	if err != nil && strings.Contains(string(out), "Missing expected target directory") {
		fmt.Println("     (cache de Python do uv corrompido; limpando e tentando de novo…)")
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			_ = os.RemoveAll(filepath.Join(appdata, "uv", "python"))
		}
		out, err = tentar()
	}
	if err != nil {
		return fmt.Errorf("%v: %s", err, ultimasLinhas(out, 5))
	}
	return nil
}

func ultimasLinhas(b []byte, n int) string {
	linhas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(linhas) > n {
		linhas = linhas[len(linhas)-n:]
	}
	return strings.TrimSpace(strings.Join(linhas, " / "))
}

// ------------------------------------------------------------------ Claude

// registrarNoClaude adiciona o servidor MCP preservando o resto do arquivo,
// que guarda tambem as preferencias pessoais do Claude Desktop.
func registrarNoClaude(home, destino, uv string) error {
	alvos := pastasConfigClaude(home)
	if len(alvos) == 0 {
		return fmt.Errorf("não encontrei onde o Claude Desktop guarda a configuração")
	}
	var ultimoErro error
	gravou := 0
	for _, dir := range alvos {
		if err := gravarConfig(dir, destino, uv); err != nil {
			ultimoErro = err
			continue
		}
		gravou++
	}
	if gravou == 0 {
		return ultimoErro
	}
	return nil
}

// pastasConfigClaude lista TODOS os lugares onde o Claude Desktop pode ler a
// configuracao nesta maquina. Sao dois mundos:
//
//   - instalador classico: %APPDATA%\Claude
//   - Microsoft Store (MSIX): %LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming\Claude
//
// A versao da Store ignora completamente o caminho classico — foi assim que a
// primeira instalacao no PC do Douglas configurou o arquivo errado. Gravamos
// em todos os que existirem: escrever a mais e inofensivo, escrever no lugar
// errado faz o Claude nao enxergar o WhatsApp e ninguem descobrir por que.
func pastasConfigClaude(home string) []string {
	var dirs []string
	for _, pkg := range pastasMSIX(home) {
		dirs = append(dirs, filepath.Join(pkg, "LocalCache", "Roaming", "Claude"))
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		classico := filepath.Join(appdata, "Claude")
		if st, err := os.Stat(classico); err == nil && st.IsDir() {
			dirs = append(dirs, classico)
		} else if len(dirs) == 0 {
			// nenhum dos dois existe: cria o classico como palpite
			dirs = append(dirs, classico)
		}
	}
	return dirs
}

func gravarConfig(cfgDir, destino, uv string) error {
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

// escreverLauncher grava um iniciar.cmd que sobe a ponte redirecionando toda
// a saida para logs\bridge.log. Sem isto a saida se perde: no macOS o launchd
// captura stdout/stderr, no Windows nao existe equivalente — foi por isso que
// a primeira falha no PC do Douglas nao deixou rastro nenhum.
func escreverLauncher(destino string) (string, error) {
	cmd := `@echo off
REM Sobe a ponte e a mantem de pe: se ela cair, espera 10s e sobe de novo.
REM - "ping" como pausa, porque "timeout" falha sem stdin interativo (caso
REM   exato de uma Tarefa Agendada rodando escondida).
REM - instancia unica via loop.lock: o cmd segura o handle 9 aberto enquanto
REM   o laco vive; uma segunda copia falha ao abrir e desiste na hora. Sem
REM   isso, dois lacos subiriam duas pontes disputando o mesmo SQLite.
setlocal
set "RAIZ=%~dp0"
set "LOG=%RAIZ%logs\bridge.log"
if not exist "%RAIZ%logs" mkdir "%RAIZ%logs"
cd /d "%RAIZ%whatsapp-bridge"
2>nul (9>"%RAIZ%loop.lock" call :principal) || exit /b 0
exit /b 0

:principal
:laco
if exist "%LOG%" for %%A in ("%LOG%") do if %%~zA GTR 5242880 move /y "%LOG%" "%LOG%.old" >nul 2>&1
echo. >> "%LOG%"
echo ===== inicio %DATE% %TIME% ===== >> "%LOG%"
"%RAIZ%whatsapp-bridge\whatsapp-bridge.exe" >> "%LOG%" 2>&1
echo ===== saiu com codigo %ERRORLEVEL% em %DATE% %TIME% ===== >> "%LOG%"
ping -n 11 127.0.0.1 >nul
goto laco
`
	caminho := filepath.Join(destino, "iniciar.cmd")
	if err := os.WriteFile(caminho, crlf(cmd), 0o755); err != nil {
		return "", err
	}
	return caminho, nil
}

// crlf converte fins de linha para CRLF. O parser de rotulos (goto/call) do
// cmd.exe tem bugs conhecidos com arquivos so-LF; CRLF e a forma canonica.
func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n"))
}

// argumentosPS monta a linha que roda o launcher escondido via PowerShell.
// E o unico jeito sem-admin de uma Tarefa Agendada rodar um .cmd SEM abrir
// uma janela de console visivel: Hidden na tarefa esconde a tarefa, nao a
// janela. Aspas simples do caminho sao dobradas (escape do PowerShell).
func argumentosPS(launcher string) string {
	return `-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -Command "& '` +
		strings.ReplaceAll(launcher, "'", "''") + `'"`
}

// instalarTarefa registra o launcher como Tarefa Agendada do Windows, que e o
// mecanismo nativo para "rodar no logon e ficar de pe": nao exige
// Administrador, sobrevive a reinicializacao, roda escondido e o proprio
// Windows garante uma instancia so (MultipleInstancesPolicy). Substituiu o
// watchdog em VBScript, que dependia de um interpretador em vias de
// desativacao e falhava em silencio.
func instalarTarefa(destino, launcher string) error {
	usuario := os.Getenv("USERNAME")
	if dominio := os.Getenv("USERDOMAIN"); dominio != "" && usuario != "" {
		usuario = dominio + `\` + usuario
	}
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Mantem o Lumnia Zap conectado ao WhatsApp para o Claude.</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>powershell.exe</Command>
      <Arguments>%s</Arguments>
      <WorkingDirectory>%s</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`, escaparXML(usuario), escaparXML(usuario), escaparXML(argumentosPS(launcher)), escaparXML(destino))

	caminho := filepath.Join(destino, "tarefa.xml")
	if err := os.WriteFile(caminho, paraUTF16(xml), 0o644); err != nil {
		return err
	}
	// /f sobrescreve uma tarefa anterior — reinstalar tem que ser idempotente
	saida, err := exec.Command("schtasks", "/create", "/tn", nomeTarefa, "/xml", caminho, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(saida)))
	}
	return nil
}

func escaparXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// paraUTF16 converte para UTF-16LE com BOM, que e o unico formato que o
// schtasks /xml aceita sem reclamar.
func paraUTF16(s string) []byte {
	pontos := utf16.Encode([]rune(s))
	b := make([]byte, 0, len(pontos)*2+2)
	b = append(b, 0xFF, 0xFE)
	for _, p := range pontos {
		b = append(b, byte(p), byte(p>>8))
	}
	return b
}

// finalDoLog devolve as ultimas linhas do log para ir junto da mensagem de
// erro. Quem instala nao vai abrir arquivo em AppData — o erro precisa chegar
// na tela.
func finalDoLog(caminho string) string {
	b, err := os.ReadFile(caminho)
	if err != nil || len(b) == 0 {
		return "\n\nO log está vazio — a ponte não chegou a escrever nada."
	}
	linhas := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(linhas) > 15 {
		linhas = linhas[len(linhas)-15:]
	}
	return "\n\nÚltimas linhas do log:\n" + strings.Join(linhas, "\n")
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

// ------------------------------------------------------------------ atalhos

// criarAtalho cria um .lnk via COM do PowerShell (WScript.Shell), o
// mecanismo padrão do Windows — não há como criar .lnk só com a biblioteca
// padrão do Go.
func criarAtalho(caminhoLnk, alvo, args, icone string, oculto bool) error {
	janela := "1"
	if oculto {
		janela = "7" // minimizada; wscript //B já roda sem janela própria
	}
	script := fmt.Sprintf(`
$s = New-Object -ComObject WScript.Shell
$l = $s.CreateShortcut("%s")
$l.TargetPath = "%s"
$l.Arguments = '%s'
$l.WindowStyle = %s
if (Test-Path "%s") { $l.IconLocation = "%s" }
$l.Save()
`, psEscape(caminhoLnk), psEscape(alvo), strings.ReplaceAll(args, "'", "''"), janela, psEscape(icone), psEscape(icone))
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "ByPass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func criarAtalhoDesktop(home, destino string) (string, error) {
	// launcher do atalho da Area de Trabalho: garante que a ponte esta de pe
	// (mandando a Tarefa Agendada rodar, que e idempotente) e abre o painel.
	abrir := fmt.Sprintf(`@echo off
curl -s -m 2 -o nul %s/api/panel/status
if errorlevel 1 (
  schtasks /run /tn "%s" >nul 2>&1
  if errorlevel 1 start "" /min cmd /c "%%~dp0iniciar.cmd"
  for /l %%%%i in (1,1,40) do (
    ping -n 2 127.0.0.1 >nul
    curl -s -m 2 -o nul %s/api/panel/status && goto abre
  )
  echo O Lumnia Zap nao respondeu. Espere um minuto e tente de novo.
  echo Se continuar assim, reinicie o PC.
  pause
  exit /b 1
)
:abre
start "" %s
`, painelURL, nomeTarefa, painelURL, painelURL)

	abrirPath := filepath.Join(destino, "abrir.cmd")
	if err := os.WriteFile(abrirPath, crlf(abrir), 0o755); err != nil {
		return "", err
	}

	desktop := pastaDesktop(home)
	icone := filepath.Join(destino, "painel.ico")
	lnk := filepath.Join(desktop, produto+".lnk")
	if err := criarAtalho(lnk, abrirPath, "", icone, true); err != nil {
		return "", err
	}
	return lnk, nil
}

// pararServico derruba uma instalacao anterior que esteja de pe, para que a
// extracao consiga sobrescrever os arquivos.
func pararServico() {
	_ = exec.Command("schtasks", "/end", "/tn", nomeTarefa).Run()
	// mata qualquer laco do iniciar.cmd (inclusive os subidos fora da tarefa)
	// ANTES da ponte — na ordem inversa, o laco ressuscitaria a ponte em 10s,
	// no meio da extracao, e a sobrescrita do .exe falharia com Acesso negado.
	_ = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		`Get-CimInstance Win32_Process -Filter "Name='cmd.exe' or Name='powershell.exe'" | `+
			`Where-Object { $_.CommandLine -like '*iniciar.cmd*' } | `+
			`ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`).Run()
	_ = exec.Command("taskkill", "/f", "/im", "whatsapp-bridge.exe").Run()
	time.Sleep(2 * time.Second)
}

// instalarStartupFallback registra o launcher (escondido, via PowerShell) na
// pasta Inicializar — plano B para quando o schtasks e bloqueado por politica.
func instalarStartupFallback(destino, launcher string) error {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return fmt.Errorf("variável de ambiente %%APPDATA%% não encontrada")
	}
	startup := filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	if err := os.MkdirAll(startup, 0o755); err != nil {
		return err
	}
	return criarAtalho(filepath.Join(startup, produto+".lnk"), "powershell.exe",
		argumentosPS(launcher), filepath.Join(destino, "painel.ico"), true)
}

// limparInstalacaoAntiga remove os restos da versao que usava VBScript, para
// nao ficarem dois mecanismos brigando pela mesma porta.
func limparInstalacaoAntiga(destino string) {
	for _, f := range []string{"watchdog.vbs", "abrir.vbs"} {
		_ = os.Remove(filepath.Join(destino, f))
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		_ = os.Remove(filepath.Join(appdata, "Microsoft", "Windows",
			"Start Menu", "Programs", "Startup", produto+".lnk"))
	}
}

// pastaDesktop devolve a Área de Trabalho real. Em PC com OneDrive ligado a
// pasta e redirecionada — e num Windows em portugues ela se chama literalmente
// "Área de Trabalho" no disco — entao perguntamos ao proprio Windows em vez de
// chutar o caminho.
func pastaDesktop(home string) string {
	out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "ByPass",
		"-Command", "[Environment]::GetFolderPath('Desktop')").Output()
	if err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p
			}
		}
	}
	for _, c := range []string{
		filepath.Join(home, "OneDrive", "Área de Trabalho"),
		filepath.Join(home, "OneDrive", "Desktop"),
		filepath.Join(home, "Área de Trabalho"),
		filepath.Join(home, "Desktop"),
	} {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return filepath.Join(home, "Desktop")
}

// psEscape escapa aspas duplas para uso dentro de uma string entre aspas
// duplas no PowerShell (o escape ali é crase + aspas).
func psEscape(s string) string {
	return strings.ReplaceAll(s, `"`, "`\"")
}

// ------------------------------------------------------------------ dialogos

func etapa(msg string) {
	fmt.Println()
	fmt.Println("==> " + msg)
}

func abrirNavegador(url string) {
	exec.Command("cmd", "/c", "start", "", url).Run()
}

// msgBox mostra uma caixa de mensagem nativa; é best-effort — se o
// PowerShell não estiver disponível por algum motivo, apenas ignora, porque
// a mensagem já foi impressa no console.
func msgBox(titulo, msg string) {
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.MessageBox]::Show('%s', '%s', 'OK', 'Information') | Out-Null`,
		strings.ReplaceAll(msg, "'", "''"), strings.ReplaceAll(titulo, "'", "''"))
	exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "ByPass", "-Command", script).Run()
}

// confirmar pergunta sim/não no console. padrao indica o que acontece se a
// pessoa só apertar Enter.
func confirmar(pergunta string, padrao bool) bool {
	sufixo := "[s/N]"
	if padrao {
		sufixo = "[S/n]"
	}
	fmt.Printf("%s %s: ", pergunta, sufixo)
	linha, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	linha = strings.ToLower(strings.TrimSpace(linha))
	if linha == "" {
		return padrao
	}
	return linha == "s" || linha == "sim" || linha == "y" || linha == "yes"
}
