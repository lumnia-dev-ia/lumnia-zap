// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

//go:build !windows

package main

import "os/exec"

func semJanela(*exec.Cmd) {}
