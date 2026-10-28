// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

import (
	"runtime"
	"testing"
)

func TestPlataformaNomes(t *testing.T) {
	m, s, n := maquina(), shellDeLogin(), sistemaDoNavegador()
	if runtime.GOOS == "darwin" {
		if m != "Mac" || s != "/bin/zsh" || n != "macOS" {
			t.Fatalf("darwin: %q %q %q", m, s, n)
		}
		return
	}
	if runtime.GOOS == "linux" && (m != "computador" || s != "/bin/bash" || n != "Linux") {
		t.Fatalf("linux: %q %q %q", m, s, n)
	}
}
