// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_transcritor.go — o transcritor residente (passo 6 do plano de
// 15-10). Medido em 15-10: um audio curto levava 13 s, e quase tudo era
// subir o Python, importar o ctranslate2 e carregar o modelo do zero a cada
// pedido. Aqui o processo sobe uma vez (quando o modulo liga), carrega o
// modelo e fica esperando pedidos por stdin, um JSON por linha; a resposta
// volta por stdout, um JSON por linha. Sem porta, sem socket: so os pipes do
// processo filho. Quando a ponte morre, o stdin fecha e o Python sai sozinho.
//
// faster-whisper em CPU, modelo `small` int8, como antes: funciona igual no
// Mac e no Linux da nuvem. Se o residente falhar (uv ausente, modelo nao
// baixado, processo morto), beatitudeTranscrever cai no caminho avulso de
// antes, que continua existindo em beatitude_extras.go.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	beatitudeWhisperModelo    = "small"
	beatitudeTranscritorSubir = 6 * time.Minute // primeira vez baixa o modelo (~500 MB)
	beatitudeTranscritorPrazo = 3 * time.Minute
)

const beatitudeTranscritorResidente = `import sys, json
from faster_whisper import WhisperModel
m = WhisperModel(sys.argv[2], device="cpu", compute_type="int8", download_root=sys.argv[1])
print(json.dumps({"pronto": True}), flush=True)
for linha in sys.stdin:
    linha = linha.strip()
    if not linha:
        continue
    try:
        req = json.loads(linha)
        segs, _ = m.transcribe(req["arquivo"], language="pt", vad_filter=True, initial_prompt=req.get("dica") or None)
        texto = " ".join(s.text.strip() for s in segs).strip()
        print(json.dumps({"texto": texto}), flush=True)
    except Exception as e:
        print(json.dumps({"erro": str(e)[:300]}), flush=True)
`

// beatitudeTranscritorComando monta o processo; variavel para o teste trocar
// o faster-whisper por um stub que fala o mesmo protocolo.
var beatitudeTranscritorComando = func(uv, script, modelos string) *exec.Cmd {
	return exec.Command(uv, "run", "--no-project", "--python", "3.11", "--with", "faster-whisper",
		"python", script, modelos, beatitudeWhisperModelo)
}

var beatitudeTranscritor struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	entrada   io.WriteCloser
	saida     *bufio.Reader
	pronto    bool
	subindo   bool
	desde     time.Time
	ultimoErr string
	pedidos   int
}

// beatitudeTranscritorAquecer sobe o residente em segundo plano. Chamado
// quando a ponte inicia com o modulo ligado e quando a pessoa liga o modulo.
func beatitudeTranscritorAquecer() {
	beatitudeTranscritor.mu.Lock()
	if beatitudeTranscritor.pronto || beatitudeTranscritor.subindo {
		beatitudeTranscritor.mu.Unlock()
		return
	}
	beatitudeTranscritor.subindo = true
	beatitudeTranscritor.mu.Unlock()
	go func() {
		err := beatitudeTranscritorSubirAgora()
		beatitudeTranscritor.mu.Lock()
		beatitudeTranscritor.subindo = false
		beatitudeTranscritor.mu.Unlock()
		if err != nil && panel.logger != nil {
			panel.logger.Warnf("Beatitude: transcritor residente não subiu (%v); áudios usam o caminho avulso", err)
		}
	}()
}

// beatitudeTranscritorSubirAgora inicia o processo e espera o {"pronto":true}.
func beatitudeTranscritorSubirAgora() error {
	uv := beatitudeAcharUV()
	if uv == "" {
		return fmt.Errorf("uv não encontrado")
	}
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, beatitudeWorkDir)
	_ = os.MkdirAll(filepath.Join(dir, "modelos"), 0o700)
	script := filepath.Join(dir, "transcritor-residente.py")
	if err := os.WriteFile(script, []byte(beatitudeTranscritorResidente), 0o600); err != nil {
		return err
	}
	cmd := beatitudeTranscritorComando(uv, script, filepath.Join(dir, "modelos"))
	cmd.Env = beatitudeAmbiente()
	cmd.Stderr = nil // o faster-whisper e o uv falam no stderr; nao precisamos
	entrada, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	saidaPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	saida := bufio.NewReaderSize(saidaPipe, 1<<20)
	inicio := time.Now()
	linha, err := beatitudeLerLinha(saida, beatitudeTranscritorSubir)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("esperando o modelo: %v", err)
	}
	var r struct {
		Pronto bool `json:"pronto"`
	}
	if json.Unmarshal([]byte(linha), &r) != nil || !r.Pronto {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("resposta inesperada do transcritor: %s", beatitudeCurto(linha, 200))
	}
	beatitudeTranscritor.mu.Lock()
	beatitudeTranscritor.cmd, beatitudeTranscritor.entrada, beatitudeTranscritor.saida = cmd, entrada, saida
	beatitudeTranscritor.pronto, beatitudeTranscritor.desde, beatitudeTranscritor.ultimoErr = true, time.Now(), ""
	beatitudeTranscritor.mu.Unlock()
	if panel.logger != nil {
		panel.logger.Infof("Beatitude: transcritor residente pronto (modelo %s) em %.1fs", beatitudeWhisperModelo, time.Since(inicio).Seconds())
	}
	go func() { // quando o processo morrer por conta propria, esquece-o
		_ = cmd.Wait()
		beatitudeTranscritor.mu.Lock()
		if beatitudeTranscritor.cmd == cmd {
			beatitudeTranscritor.cmd, beatitudeTranscritor.pronto = nil, false
		}
		beatitudeTranscritor.mu.Unlock()
	}()
	return nil
}

// beatitudeTranscritorPedir manda um audio ao residente e devolve o texto.
// Sobe o processo se precisar; em timeout ou erro de pipe, mata e esquece,
// para o proximo pedido subir de novo.
func beatitudeTranscritorPedir(arquivo, dica string) (string, error) {
	// se o aquecimento ainda esta subindo, espera por ele em vez de subir outro
	prazo := time.Now().Add(beatitudeTranscritorSubir)
	for {
		beatitudeTranscritor.mu.Lock()
		subindo := beatitudeTranscritor.subindo
		beatitudeTranscritor.mu.Unlock()
		if !subindo || time.Now().After(prazo) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	beatitudeTranscritor.mu.Lock()
	pronto := beatitudeTranscritor.pronto
	beatitudeTranscritor.mu.Unlock()
	if !pronto {
		if err := beatitudeTranscritorSubirAgora(); err != nil {
			beatitudeTranscritor.mu.Lock()
			beatitudeTranscritor.ultimoErr = err.Error()
			beatitudeTranscritor.mu.Unlock()
			return "", err
		}
	}
	beatitudeTranscritor.mu.Lock()
	defer beatitudeTranscritor.mu.Unlock()
	if !beatitudeTranscritor.pronto {
		return "", fmt.Errorf("transcritor caiu")
	}
	pedido, _ := json.Marshal(map[string]string{"arquivo": arquivo, "dica": dica})
	if _, err := beatitudeTranscritor.entrada.Write(append(pedido, '\n')); err != nil {
		beatitudeTranscritorMatar()
		return "", fmt.Errorf("escrevendo ao transcritor: %v", err)
	}
	linha, err := beatitudeLerLinha(beatitudeTranscritor.saida, beatitudeTranscritorPrazo)
	if err != nil {
		beatitudeTranscritorMatar()
		return "", fmt.Errorf("lendo do transcritor: %v", err)
	}
	var r struct {
		Texto string `json:"texto"`
		Erro  string `json:"erro"`
	}
	if json.Unmarshal([]byte(linha), &r) != nil {
		beatitudeTranscritorMatar()
		return "", fmt.Errorf("resposta inesperada do transcritor: %s", beatitudeCurto(linha, 200))
	}
	beatitudeTranscritor.pedidos++
	if r.Erro != "" {
		return "", fmt.Errorf("transcrição falhou: %s", r.Erro)
	}
	return strings.TrimSpace(r.Texto), nil
}

// beatitudeTranscritorMatar: chamar com o mutex preso.
func beatitudeTranscritorMatar() {
	if beatitudeTranscritor.cmd != nil && beatitudeTranscritor.cmd.Process != nil {
		_ = beatitudeTranscritor.cmd.Process.Kill()
	}
	beatitudeTranscritor.cmd, beatitudeTranscritor.pronto = nil, false
}

// beatitudeTranscritorEstado para o painel.
func beatitudeTranscritorEstado() map[string]interface{} {
	beatitudeTranscritor.mu.Lock()
	defer beatitudeTranscritor.mu.Unlock()
	return map[string]interface{}{
		"pronto":     beatitudeTranscritor.pronto,
		"subindo":    beatitudeTranscritor.subindo,
		"modelo":     beatitudeWhisperModelo,
		"desde":      beatitudeTranscritor.desde,
		"pedidos":    beatitudeTranscritor.pedidos,
		"ultimo_err": beatitudeTranscritor.ultimoErr,
	}
}

// beatitudeLerLinha le uma linha com limite de tempo. O leitor fica numa
// goroutine; se estourar, quem chamou mata o processo e a goroutine termina
// com o pipe.
func beatitudeLerLinha(r *bufio.Reader, limite time.Duration) (string, error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := r.ReadString('\n')
		ch <- res{s, err}
	}()
	select {
	case x := <-ch:
		if x.err != nil && strings.TrimSpace(x.s) == "" {
			return "", x.err
		}
		return strings.TrimSpace(x.s), nil
	case <-time.After(limite):
		return "", fmt.Errorf("passou de %s", limite)
	}
}
