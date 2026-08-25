//go:build !windows

// Stub de subirDireto para outros sistemas. O instalador em si só roda em
// Windows (instalar() recusa qualquer outro GOOS logo na entrada), mas este
// stub deixa o pacote compilar em Linux/macOS — que é onde os testes de
// configuração do Codex (codex_test.go) rodam, inclusive no CI.
//
// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT
package main

func subirDireto(string) {}
