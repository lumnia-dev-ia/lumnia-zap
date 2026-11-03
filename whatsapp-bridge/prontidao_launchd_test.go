package main

import "testing"

// Saída real de `launchctl print` (resumida): o primeiro nível diz running e
// um bloco aninhado diz active. A leitura antiga ficava com o "active".
const launchdRodando = "gui/501/dev.lumnia.zap = {\n" +
	"\tactive count = 1\n" +
	"\tpath = /Users/x/Library/LaunchAgents/dev.lumnia.zap.plist\n" +
	"\ttype = LaunchAgent\n" +
	"\tstate = running\n" +
	"\n" +
	"\tprogram = /Users/x/Library/Application Support/LumniaZap/whatsapp-bridge/whatsapp-bridge\n" +
	"\tpid = 4321\n" +
	"\tevent triggers = {\n" +
	"\t\tdev.lumnia.zap.gatilho = {\n" +
	"\t\t\tstate = active\n" +
	"\t\t\tpid = 0\n" +
	"\t\t}\n" +
	"\t}\n" +
	"}\n"

func TestEstadoLaunchdIgnoraBlocosAninhados(t *testing.T) {
	estado, pid := prontidaoEstadoLaunchd(launchdRodando)
	if estado != "running" || pid != 4321 {
		t.Fatalf("estado=%q pid=%d", estado, pid)
	}
}

func TestEstadoLaunchdOutraPalavraComPid(t *testing.T) {
	saida := "gui/501/dev.lumnia.zap = {\n\tstate = active\n\tpid = 99\n}\n"
	estado, pid := prontidaoEstadoLaunchd(saida)
	if estado != "active" || pid != 99 {
		t.Fatalf("estado=%q pid=%d", estado, pid)
	}
}

func TestEstadoLaunchdParado(t *testing.T) {
	saida := "gui/501/dev.lumnia.zap = {\n\tstate = not running\n\tlast exit code = 1\n}\n"
	estado, pid := prontidaoEstadoLaunchd(saida)
	if estado != "not running" || pid != 0 {
		t.Fatalf("estado=%q pid=%d", estado, pid)
	}
}

func TestEstadoLaunchdRecuoComEspacos(t *testing.T) {
	saida := "x = {\n    state = running\n    pid = 7\n        state = active\n}\n"
	estado, pid := prontidaoEstadoLaunchd(saida)
	if estado != "running" || pid != 7 {
		t.Fatalf("estado=%q pid=%d", estado, pid)
	}
}
