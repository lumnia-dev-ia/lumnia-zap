// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// lumnia-zap-mcp.exe — o servidor MCP que o Claude Desktop chama.
//
// Fica exposto pelo alias de execucao do MSIX, num caminho fixo:
//
//	%LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe
//
// Chamado por esse alias, o processo roda com a identidade do pacote, entao
// enxerga o mesmo LocalState da ponte. Ele so prepara o ambiente e roda o
// Python embutido no pacote (sem uv, sem download na maquina) com o
// servidor de sempre, repassando stdin/stdout intactos — o stdout e do
// protocolo JSON-RPC. O stderr vai para o Claude e tambem para
// LocalState\logs\mcp.log.
package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"lumnia-zap/msix/comum"
)

func main() {
	base := comum.PastaDoExe() // fora do pacote (teste a mao): ao lado do exe
	if comum.FamiliaDoPacote() != "" {
		base = filepath.Join(comum.PastaInstalacao(), "mcp")
	}
	py := filepath.Join(base, "python", "python.exe")
	script := filepath.Join(base, "servidor", "main.py")

	log, _ := comum.AbrirLog("mcp.log")
	comum.Linha(log, "===== mcp inicio (pacote %q) =====", comum.FamiliaDoPacote())
	comum.Linha(log, "store: %s", comum.PastaStore())

	cmd := exec.Command(py, "-u", script)
	cmd.Args = append(cmd.Args, os.Args[1:]...)
	cmd.Dir = filepath.Join(base, "servidor")
	cmd.Env = append(os.Environ(),
		"LUMNIA_ZAP_STORE="+comum.PastaStore(),
		"PYTHONUTF8=1",
		"PYTHONIOENCODING=utf-8",
		"PYTHONDONTWRITEBYTECODE=1", // a pasta do pacote e somente leitura
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if log != nil {
		cmd.Stderr = io.MultiWriter(os.Stderr, log)
	} else {
		cmd.Stderr = os.Stderr
	}
	err := cmd.Run()
	codigo := 0
	if cmd.ProcessState != nil {
		codigo = cmd.ProcessState.ExitCode()
	} else if err != nil {
		codigo = 1
	}
	comum.Linha(log, "===== mcp saiu (codigo %d, erro %v) =====", codigo, err)
	if log != nil {
		log.Close()
	}
	os.Exit(codigo)
}
