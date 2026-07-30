// Instalador Lumnia Zap (Windows) — instala a ponte WhatsApp<->Claude no PC
// Windows do usuario.
//
// Roda como o proprio usuario (sem exigir Administrador), como um app de
// console: mostra o progresso na janela que abre ao dar duplo clique.
//
// Equivalente ao instalador de macOS (../installer/main.go), mas usando
// mecanismos nativos do Windows no lugar de launchd/osascript/.app bundle:
//   - autostart: atalho .lnk na pasta Startup do usuario (nao exige admin)
//   - keepalive: um watchdog.vbs que reinicia a ponte se ela cair, no lugar
//     do KeepAlive do launchd
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
)

const (
	produto     = "Lumnia Zap"
	pacoteURL   = "https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/lumnia-zap-windows-amd64.zip"
	painelURL   = "http://localhost:8080"
	repoURL     = "https://github.com/lumnia-dev-ia/lumnia-zap"
	claudeBaixa = "https://claude.ai/download"
)

// candidatos onde o Claude Desktop costuma se instalar no Windows. A
// distribuicao varia (instalador Squirrel classico ou pacote MSIX), entao
// isto e so uma checagem informativa — nunca bloqueia a instalacao.
func claudeCandidatos(home string) []string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	return []string{
		filepath.Join(local, "AnthropicClaude", "claude.exe"),
		filepath.Join(local, "Programs", "Claude", "Claude.exe"),
		filepath.Join(local, "Programs", "claude", "Claude.exe"),
	}
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

	achou := false
	for _, c := range claudeCandidatos(home) {
		if _, err := os.Stat(c); err == nil {
			achou = true
			break
		}
	}
	if !achou {
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

	etapa("Conectando ao Claude Desktop…")
	if err := registrarNoClaude(destino, uv); err != nil {
		return fmt.Errorf("falha ao configurar o Claude Desktop: %v", err)
	}

	etapa("Configurando o serviço…")
	vbs, err := escreverWatchdog(destino)
	if err != nil {
		return fmt.Errorf("falha ao configurar o serviço: %v", err)
	}
	startup, err := pastaStartup()
	if err != nil {
		return fmt.Errorf("não achei a pasta Inicializar do Windows: %v", err)
	}
	if err := criarAtalho(filepath.Join(startup, produto+".lnk"), "wscript.exe",
		fmt.Sprintf(`//B "%s"`, vbs), filepath.Join(destino, "painel.ico"), true); err != nil {
		return fmt.Errorf("falha ao registrar a inicialização automática: %v", err)
	}
	if err := criarAtalhoDesktop(home, destino, vbs); err != nil {
		// atalho da Área de Trabalho é conveniência, não impede a instalação
		fmt.Println("     (aviso: não consegui criar o atalho na Área de Trabalho:", err, ")")
	}

	etapa("Iniciando…")
	exec.Command("wscript.exe", "//B", vbs).Start()
	if !esperarPainel(40 * time.Second) {
		return fmt.Errorf("o serviço não respondeu a tempo.\n\nO log está em:\n%s",
			filepath.Join(destino, "logs", "bridge.log"))
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
	fmt.Println("O atalho ficou na sua Área de Trabalho, e a ponte inicia sozinha")
	fmt.Println("quando você liga o PC.")
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
		out, err := os.OpenFile(alvo, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
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
	if p, err := exec.LookPath("uv.exe"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("uv"); err == nil {
		return p, nil
	}
	candidatos := []string{
		filepath.Join(home, ".local", "bin", "uv.exe"),
	}
	for _, c := range candidatos {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
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

// ------------------------------------------------------------------ Claude

// registrarNoClaude adiciona o servidor MCP preservando o resto do arquivo,
// que guarda tambem as preferencias pessoais do Claude Desktop.
func registrarNoClaude(destino, uv string) error {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return fmt.Errorf("variável de ambiente %%APPDATA%% não encontrada")
	}
	cfgDir := filepath.Join(appdata, "Claude")
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

// escreverWatchdog grava um script VBScript que fica em loop verificando se
// a ponte está respondendo em localhost:8080 e reinicia se ela cair — o
// equivalente ao KeepAlive do launchd, sem exigir instalar um servico do
// Windows (que pediria Administrador).
func escreverWatchdog(destino string) (string, error) {
	bin := filepath.Join(destino, "whatsapp-bridge", "whatsapp-bridge.exe")
	vbs := fmt.Sprintf(`Set WshShell = CreateObject("WScript.Shell")
bin = "%s"
workdir = "%s"
url = "%s/api/panel/status"

' Só um watchdog por vez. Sem isto, o atalho da Área de Trabalho poderia
' subir um segundo loop e dois processos disputariam o mesmo banco SQLite.
If ContarWatchdogs() > 1 Then WScript.Quit

travados = 0

Do
    If EstaViva() Then
        travados = 0
    ElseIf Not PonteRodando() Then
        WshShell.CurrentDirectory = workdir
        WshShell.Run """" & bin & """", 0, False
        travados = 0
        ' a ponte demora pra abrir o banco e conectar: não conte esse
        ' tempo como falha, senão o loop sobe várias cópias dela.
        WScript.Sleep 45000
    Else
        ' processo de pé mas sem responder — depois de ~2 min, derruba
        ' pra que a próxima volta do loop suba uma cópia limpa.
        travados = travados + 1
        If travados >= 8 Then
            MatarPonte
            travados = 0
        End If
    End If
    WScript.Sleep 15000
Loop

Function EstaViva()
    On Error Resume Next
    EstaViva = False
    Set http = CreateObject("MSXML2.ServerXMLHTTP.6.0")
    http.setTimeouts 2000, 2000, 2000, 2000
    http.Open "GET", url, False
    http.Send
    If Err.Number = 0 And http.Status = 200 Then
        EstaViva = True
    End If
    On Error Goto 0
End Function

Function PonteRodando()
    On Error Resume Next
    PonteRodando = False
    Set wmi = GetObject("winmgmts:\\.\root\cimv2")
    Set procs = wmi.ExecQuery("SELECT ProcessId FROM Win32_Process WHERE Name = 'whatsapp-bridge.exe'")
    If Err.Number = 0 Then
        If procs.Count > 0 Then PonteRodando = True
    End If
    On Error Goto 0
End Function

Sub MatarPonte()
    On Error Resume Next
    Set wmi = GetObject("winmgmts:\\.\root\cimv2")
    Set procs = wmi.ExecQuery("SELECT * FROM Win32_Process WHERE Name = 'whatsapp-bridge.exe'")
    For Each p In procs
        p.Terminate()
    Next
    On Error Goto 0
End Sub

Function ContarWatchdogs()
    On Error Resume Next
    ContarWatchdogs = 1
    Set wmi = GetObject("winmgmts:\\.\root\cimv2")
    Set procs = wmi.ExecQuery("SELECT CommandLine FROM Win32_Process WHERE Name = 'wscript.exe'")
    If Err.Number <> 0 Then Exit Function
    n = 0
    For Each p In procs
        If Not IsNull(p.CommandLine) Then
            If InStr(LCase(p.CommandLine), "watchdog.vbs") > 0 Then n = n + 1
        End If
    Next
    If n > 0 Then ContarWatchdogs = n
    On Error Goto 0
End Function
`, escaparVBS(bin), escaparVBS(filepath.Join(destino, "whatsapp-bridge")), painelURL)

	caminho := filepath.Join(destino, "watchdog.vbs")
	if err := os.WriteFile(caminho, []byte(vbs), 0o644); err != nil {
		return "", err
	}
	return caminho, nil
}

func escaparVBS(s string) string {
	return strings.ReplaceAll(s, `"`, `""`)
}

func pastaStartup() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", fmt.Errorf("variável de ambiente %%APPDATA%% não encontrada")
	}
	p := filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
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

func criarAtalhoDesktop(home, destino, vbs string) error {
	// launcher proprio para o atalho da Área de Trabalho: garante que a
	// ponte esta de pé e abre o navegador — mesma lógica do "zap" do
	// instalador de Mac.
	abrir := fmt.Sprintf(`Set WshShell = CreateObject("WScript.Shell")
url = "%s"
statusURL = url & "/api/panel/status"

If Not EstaViva() Then
    ' o watchdog tem guarda de instancia unica: se ja estiver rodando, este
    ' comando nao faz nada e quem vai levantar a ponte e o que ja esta de pe.
    WshShell.Run "wscript.exe //B ""%s""", 0, False
    i = 0
    Do While i < 120
        WScript.Sleep 500
        If EstaViva() Then Exit Do
        i = i + 1
    Loop
End If

If EstaViva() Then
    WshShell.Run url
Else
    MsgBox "O Lumnia Zap ainda não respondeu." & vbCrLf & vbCrLf & _
           "Espere um minuto e clique de novo. Se continuar assim, reinicie o PC — " & _
           "e se ainda não voltar, fale com o Diego.", vbExclamation, "Lumnia Zap"
End If

Function EstaViva()
    On Error Resume Next
    EstaViva = False
    Set http = CreateObject("MSXML2.ServerXMLHTTP.6.0")
    http.setTimeouts 2000, 2000, 2000, 2000
    http.Open "GET", statusURL, False
    http.Send
    If Err.Number = 0 And http.Status = 200 Then
        EstaViva = True
    End If
    On Error Goto 0
End Function
`, painelURL, escaparVBS(vbs))

	abrirPath := filepath.Join(destino, "abrir.vbs")
	if err := os.WriteFile(abrirPath, []byte(abrir), 0o644); err != nil {
		return err
	}

	desktop := pastaDesktop(home)
	icone := filepath.Join(destino, "painel.ico")
	return criarAtalho(filepath.Join(desktop, produto+".lnk"), "wscript.exe",
		fmt.Sprintf(`//B "%s"`, abrirPath), icone, false)
}

// pastaDesktop devolve a Área de Trabalho real. Em PC com OneDrive ligado, a
// pasta e redirecionada para %USERPROFILE%\OneDrive\Desktop e o caminho fixo
// nao existe — por isso perguntamos ao proprio Windows antes de chutar.
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
		filepath.Join(home, "OneDrive", "Desktop"),
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
