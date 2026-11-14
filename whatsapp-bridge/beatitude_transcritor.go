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
// Motor (14-11): no Mac com chip Apple, o residente usa o mlx-whisper com o
// large-v3-turbo, que roda na GPU do chip e erra bem menos em portugues que o
// small (caso de 14-11: "Elevren Labs"). Fora disso (Linux da nuvem, Mac
// Intel, Windows) segue o faster-whisper `small` int8 em CPU. LUMNIA_ZAP_VOZ=
// small forca o motor antigo. Se o residente falhar (uv ausente, modelo nao
// baixado, processo morto, prazo estourado), beatitudeTranscrever cai no
// caminho avulso de sempre (faster-whisper small), em beatitude_extras.go:
// o grupo nunca fica sem transcricao.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	beatitudeWhisperModelo    = "small"
	beatitudeTranscritorSubir = 12 * time.Minute // primeira vez baixa o modelo (~500 MB no small, ~1,6 GB no turbo)
	beatitudeTranscritorPrazo = 3 * time.Minute
	beatitudeMLXRepo          = "mlx-community/whisper-large-v3-turbo"
)

// beatitudeMotorVoz descreve um motor do residente: o script, os pacotes que
// o uv instala, o modelo, o prazo por audio e quanto um pedido espera um
// residente que ainda esta subindo antes de cair no avulso.
type beatitudeMotorVoz struct {
	Nome         string // "mlx" | "faster-whisper"
	Rotulo       string // o que o painel e o mordomo mostram
	Modelo       string
	Pacotes      []string
	Script       string
	Prazo        time.Duration
	EsperaSubida time.Duration
}

// beatitudeMotorTranscricao escolhe o motor desta maquina. Variaveis para o
// teste trocar o sistema.
var beatitudeVozGOOS, beatitudeVozGOARCH = runtime.GOOS, runtime.GOARCH

func beatitudeMotorTranscricao() beatitudeMotorVoz {
	forcar := strings.ToLower(strings.TrimSpace(os.Getenv("LUMNIA_ZAP_VOZ")))
	if beatitudeVozGOOS == "darwin" && beatitudeVozGOARCH == "arm64" && forcar != "small" {
		return beatitudeMotorVoz{
			Nome:    "mlx",
			Rotulo:  "large-v3-turbo no chip do Mac",
			Modelo:  beatitudeMLXRepo,
			Pacotes: []string{"mlx-whisper", "av", "numpy"},
			Script:  beatitudeTranscritorResidenteMLX,
			// o turbo no MLX faz um audio de 1 min em poucos segundos; se
			// passar disso, algo travou e o avulso (small) assume
			Prazo: 45 * time.Second,
			// enquanto o turbo baixa (1,6 GB na primeira vez) o audio nao
			// espera: vai pelo avulso
			EsperaSubida: 3 * time.Second,
		}
	}
	return beatitudeMotorVoz{
		Nome:         "faster-whisper",
		Rotulo:       beatitudeWhisperModelo,
		Modelo:       beatitudeWhisperModelo,
		Pacotes:      []string{"faster-whisper"},
		Script:       beatitudeTranscritorResidente,
		Prazo:        beatitudeTranscritorPrazo,
		EsperaSubida: 6 * time.Minute,
	}
}

// Residente MLX: mesmo protocolo do faster-whisper (um JSON por linha). O
// audio do WhatsApp (ogg/opus) e decodificado pelo PyAV para 16 kHz mono, para
// nao depender do ffmpeg instalado no Mac. O modelo carrega uma vez (o
// mlx_whisper guarda o modelo carregado entre chamadas) e fica no HF_HOME da
// pasta do Zap.
const beatitudeTranscritorResidenteMLX = `import sys, json
import numpy as np
import av
import mlx_whisper
REPO = sys.argv[2]
def carregar(arquivo):
    c = av.open(arquivo)
    r = av.AudioResampler(format="s16", layout="mono", rate=16000)
    partes = []
    for f in c.decode(audio=0):
        for g in r.resample(f):
            partes.append(g.to_ndarray().reshape(-1))
    for g in r.resample(None):
        partes.append(g.to_ndarray().reshape(-1))
    c.close()
    if not partes:
        return np.zeros(0, dtype=np.float32)
    return np.concatenate(partes).astype(np.float32) / 32768.0
mlx_whisper.transcribe(np.zeros(16000, dtype=np.float32), path_or_hf_repo=REPO, language="pt")
print(json.dumps({"pronto": True}), flush=True)
for linha in sys.stdin:
    linha = linha.strip()
    if not linha:
        continue
    try:
        req = json.loads(linha)
        audio = carregar(req["arquivo"])
        if audio.size < 1600:
            print(json.dumps({"texto": ""}), flush=True)
            continue
        out = mlx_whisper.transcribe(audio, path_or_hf_repo=REPO, language="pt",
            initial_prompt=req.get("dica") or None, condition_on_previous_text=False)
        print(json.dumps({"texto": (out.get("text") or "").strip()}), flush=True)
    except Exception as e:
        print(json.dumps({"erro": str(e)[:300]}), flush=True)
`

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
	motor := beatitudeMotorTranscricao()
	args := []string{"run", "--no-project", "--python", "3.11"}
	for _, p := range motor.Pacotes {
		args = append(args, "--with", p)
	}
	args = append(args, "python", script, modelos, motor.Modelo)
	return exec.Command(uv, args...)
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
	motor     string // rotulo do motor em uso pelo residente
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
	motor := beatitudeMotorTranscricao()
	script := filepath.Join(dir, "transcritor-residente.py")
	if err := os.WriteFile(script, []byte(motor.Script), 0o600); err != nil {
		return err
	}
	cmd := beatitudeTranscritorComando(uv, script, filepath.Join(dir, "modelos"))
	cmd.Env = append(beatitudeAmbiente(), "HF_HOME="+filepath.Join(dir, "modelos", "hf"))
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
	beatitudeTranscritor.motor = motor.Rotulo
	beatitudeTranscritor.mu.Unlock()
	if panel.logger != nil {
		panel.logger.Infof("Beatitude: transcritor residente pronto (%s) em %.1fs", motor.Rotulo, time.Since(inicio).Seconds())
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
	// se o aquecimento ainda esta subindo, espera por ele em vez de subir
	// outro — no MLX so uns segundos: o turbo pode estar baixando, e o audio
	// vai pelo avulso (small) enquanto isso
	motor := beatitudeMotorTranscricao()
	prazo := time.Now().Add(motor.EsperaSubida)
	for {
		beatitudeTranscritor.mu.Lock()
		subindo := beatitudeTranscritor.subindo
		beatitudeTranscritor.mu.Unlock()
		if !subindo {
			break
		}
		if time.Now().After(prazo) {
			return "", fmt.Errorf("o modelo de voz ainda está carregando")
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
	linha, err := beatitudeLerLinha(beatitudeTranscritor.saida, motor.Prazo)
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
		"modelo":     beatitudeVozRotulo(beatitudeTranscritor.motor),
		"desde":      beatitudeTranscritor.desde,
		"pedidos":    beatitudeTranscritor.pedidos,
		"ultimo_err": beatitudeTranscritor.ultimoErr,
	}
}

// beatitudeVozRotulo: o motor em uso (o do residente, se ja subiu; senao o
// que esta maquina vai usar).
func beatitudeVozRotulo(emUso string) string {
	if emUso != "" {
		return emUso
	}
	return beatitudeMotorTranscricao().Rotulo
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
