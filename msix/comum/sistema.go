// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

//go:build windows

package comum

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"lumnia-zap/msix/migrar"
)

// SistemaWindows implementa migrar.Sistema.
type SistemaWindows struct{}

func escondido(nome string, args ...string) *exec.Cmd {
	c := exec.Command(nome, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

func (SistemaWindows) TarefaClassicaExiste() bool {
	return escondido("schtasks", "/Query", "/TN", "Lumnia Zap").Run() == nil
}

func (SistemaWindows) ApagarTarefaClassica() error {
	return escondido("schtasks", "/Delete", "/TN", "Lumnia Zap", "/F").Run()
}

// EncerrarProcessosEm: tudo cujo executável ou linha de comando esteja na
// pasta (whatsapp-bridge.exe, o cmd do iniciar.cmd, o python do servidor MCP).
func (SistemaWindows) EncerrarProcessosEm(pasta string) error {
	ps := `$p = '` + strings.ReplaceAll(pasta, "'", "''") + `\*'; ` +
		`Get-CimInstance Win32_Process | Where-Object { $_.ProcessId -ne $PID -and ($_.ExecutablePath -like $p -or $_.CommandLine -like ('*' + $p)) } | ` +
		`ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`
	return escondido("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps).Run()
}

func (SistemaWindows) EncerrarPonteDaStore() error {
	ponte := filepath.Join(PastaInstalacao(), "ponte")
	return SistemaWindows{}.EncerrarProcessosEm(ponte)
}

func (SistemaWindows) PortaLivre(espera time.Duration) bool {
	limite := time.Now().Add(espera)
	for {
		c, err := net.DialTimeout("tcp", "127.0.0.1:8080", 500*time.Millisecond)
		if err != nil {
			return true
		}
		c.Close()
		if time.Now().After(limite) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (SistemaWindows) ClaudeAberto() bool {
	out, err := escondido("tasklist", "/FI", "IMAGENAME eq Claude.exe", "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "claude.exe")
}

// FecharClaude encerra pelo processo: o WM_CLOSE só manda para a bandeja.
func (SistemaWindows) FecharClaude() error {
	return escondido("taskkill", "/IM", "Claude.exe", "/T", "/F").Run()
}

// PastasMigracao: onde está cada coisa neste usuário.
func PastasMigracao() migrar.Pastas {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		home, _ := os.UserHomeDir()
		local = filepath.Join(home, "AppData", "Local")
	}
	p := migrar.Pastas{
		Classica:   filepath.Join(local, "LumniaZap"),
		LocalState: PastaDados(),
		AliasMCP:   filepath.Join(local, "Microsoft", "WindowsApps", "lumnia-zap-mcp.exe"),
	}
	if d, _ := filepath.Glob(filepath.Join(local, "Claude-Data", "*")); len(d) > 0 {
		p.Configs = append(p.Configs, d...)
	}
	if d, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude")); len(d) > 0 {
		p.Configs = append(p.Configs, d...)
	}
	if a := os.Getenv("APPDATA"); a != "" {
		p.Configs = append(p.Configs, filepath.Join(a, "Claude"))
		p.Startup = filepath.Join(a, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	}
	return p
}

// StatusTipo: o "instalacao.tipo" de quem responde na 8080 ("" se nada ou
// ponte antiga, sem o campo).
func StatusTipo() string {
	return statusCampoTipo()
}
