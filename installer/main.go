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
		alerta("A instalação não foi concluída", err.Error()+"\n\nNada foi deixado pela metade — pode tentar de novo.", true)
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
	if _, err := os.Stat(claudeApp); os.IsNotExist(err) {
		if perguntar("Claude Desktop não encontrado",
			"O "+produto+" funciona dentro do Claude Desktop, que não está instalado neste Mac.\n\n"+
				"Quer abrir a página de download do Claude agora?") {
			exec.Command("/usr/bin/open", claudeBaixa).Run()
		}
		return fmt.Errorf("instale o Claude Desktop e rode este instalador de novo")
	}

	// Tela de consentimento: a pessoa merece saber o que esta aceitando.
	if !perguntar("Instalar o "+produto+"?",
		"Isto conecta o seu WhatsApp ao Claude neste Mac.\n\n"+
			"O Claude passa a poder LER e RESPONDER suas mensagens quando você pedir. "+
			"Suas conversas ficam guardadas só aqui, neste computador.\n\n"+
			"Você conecta escaneando um QR code, igual ao WhatsApp Web, e pode "+
			"desconectar quando quiser.") {
		return fmt.Errorf("instalação cancelada por você")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	destino := filepath.Join(home, "Library", "Application Support", "LumniaZap")

	etapa("Baixando o " + produto + "…")
	tgz, err := baixar(pacoteURL)
	if err != nil {
		return fmt.Errorf("não consegui baixar o pacote (verifique sua internet): %v", err)
	}
	defer os.Remove(tgz)

	etapa("Instalando…")
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return err
	}
	if err := extrair(tgz, destino); err != nil {
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
	if err := registrarNoClaude(home, destino, uv); err != nil {
		return fmt.Errorf("falha ao configurar o Claude Desktop: %v", err)
	}

	etapa("Configurando o serviço…")
	if err := instalarServico(home, destino); err != nil {
		return fmt.Errorf("falha ao configurar o serviço: %v", err)
	}
	if err := criarAtalho(home, destino); err != nil {
		return fmt.Errorf("falha ao criar o atalho: %v", err)
	}

	etapa("Iniciando…")
	subirServico(home)
	if !esperarPainel(40 * time.Second) {
		return fmt.Errorf("o serviço não respondeu a tempo.\n\nO log está em:\n%s",
			filepath.Join(destino, "logs", "bridge.log"))
	}

	exec.Command("/usr/bin/open", painelURL).Run()
	alerta("Pronto!",
		"O "+produto+" está instalado.\n\n"+
			"A página que abriu mostra um QR code. Escaneie com o WhatsApp do seu "+
			"celular em Configurações → Aparelhos conectados.\n\n"+
			"Depois, feche o Claude Desktop e abra de novo.\n\n"+
			"O atalho ficou na sua Mesa.", false)
	return nil
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
			if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(alvo, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
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

func subirServico(home string) {
	uid := fmt.Sprint(os.Getuid())
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	// se ja estava carregado, descarrega antes para pegar a versao nova
	exec.Command("/bin/launchctl", "bootout", "gui/"+uid+"/"+label).Run()
	time.Sleep(500 * time.Millisecond)
	exec.Command("/bin/launchctl", "bootstrap", "gui/"+uid, plist).Run()
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

func etapa(msg string) {
	// notificacao discreta: nao bloqueia o fluxo
	osa(fmt.Sprintf(`display notification "%s" with title "%s"`, escapar(msg), produto))
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
