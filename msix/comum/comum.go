// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// Package comum guarda o que os tres executaveis do pacote MSIX dividem:
// onde ficam os dados, onde fica o log e como saber se a ponte esta viva.
//
// Por que existe: dentro de um MSIX a pasta de instalacao (WindowsApps) e
// somente leitura e as escritas em %LOCALAPPDATA% sao virtualizadas. A ponte
// cria store/ relativo a pasta em que roda, entao ela precisa rodar numa pasta
// gravavel e estavel. Usamos o LocalState do proprio pacote:
//
//	%LOCALAPPDATA%\Packages\<PackageFamilyName>\LocalState
//
// que e um caminho real (nao virtualizado), igual para a ponte e para o
// servidor MCP, e visivel de fora para diagnostico. Atencao: desinstalar o
// MSIX apaga essa pasta, inclusive a sessao do WhatsApp.
package comum

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const (
	Painel   = "http://localhost:8080"
	StatusEP = "http://127.0.0.1:8080/api/panel/status"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentPackageFamily = kernel32.NewProc("GetCurrentPackageFamilyName")
	procGetCurrentPackagePath   = kernel32.NewProc("GetCurrentPackagePath")
)

// FamiliaDoPacote devolve o PackageFamilyName do processo atual, ou "" se o
// processo nao roda com identidade de pacote (ex.: exe executado a mao).
func FamiliaDoPacote() string {
	if procGetCurrentPackageFamily.Find() != nil {
		return ""
	}
	n := uint32(256)
	buf := make([]uint16, n)
	r, _, _ := procGetCurrentPackageFamily.Call(uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 { // 0 = ERROR_SUCCESS; 15700 = APPMODEL_ERROR_NO_PACKAGE
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// PastaDados e a raiz gravavel: LocalState do pacote, ou, fora do pacote,
// %LOCALAPPDATA%\LumniaZap-msix (so para teste a mao — nunca a pasta da
// instalacao classica, para os dois nao disputarem o mesmo store/).
func PastaDados() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		home, _ := os.UserHomeDir()
		local = filepath.Join(home, "AppData", "Local")
	}
	if pfn := FamiliaDoPacote(); pfn != "" {
		return filepath.Join(local, "Packages", pfn, "LocalState")
	}
	return filepath.Join(local, "LumniaZap-msix")
}

func PastaPonte() string { return filepath.Join(PastaDados(), "whatsapp-bridge") }
func PastaStore() string { return filepath.Join(PastaPonte(), "store") }
func PastaLogs() string  { return filepath.Join(PastaDados(), "logs") }

// PastaInstalacao e a raiz do pacote instalado (dentro de WindowsApps). Pela
// API do pacote, e nao por os.Executable, porque um processo aberto pelo
// alias de execucao pode reportar o caminho do alias. Fora do pacote, cai
// para a pasta do executavel.
func PastaInstalacao() string {
	if procGetCurrentPackagePath.Find() == nil {
		n := uint32(1024)
		buf := make([]uint16, n)
		r, _, _ := procGetCurrentPackagePath.Call(uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
		if r == 0 {
			return syscall.UTF16ToString(buf)
		}
	}
	return PastaDoExe()
}

// PastaDoExe e a pasta do executavel em uso.
func PastaDoExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// Vivo diz se a ponte responde no painel.
func Vivo() bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(StatusEP)
	if err != nil {
		return false
	}
	r.Body.Close()
	return true
}

// AbrirLog abre (anexando) um log em PastaLogs, girando-o acima de 5 MB.
func AbrirLog(nome string) (*os.File, error) {
	if err := os.MkdirAll(PastaLogs(), 0o755); err != nil {
		return nil, err
	}
	caminho := filepath.Join(PastaLogs(), nome)
	if st, err := os.Stat(caminho); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(caminho, caminho+".old")
	}
	return os.OpenFile(caminho, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// Linha escreve uma linha com data no log.
func Linha(f *os.File, formato string, args ...any) {
	if f == nil {
		return
	}
	fmt.Fprintf(f, "%s  %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(formato, args...))
}
