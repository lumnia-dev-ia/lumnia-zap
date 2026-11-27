package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// executorDeTeste liga o executor numa pasta temporária, com sandbox "ok",
// e intercepta o que iria ao grupo. O claude -p é trocado por "rodar".
type executorTeste struct {
	mu       sync.Mutex
	textos   []string
	arquivos []string
	raiz     string
}

func (e *executorTeste) grupo() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.textos, "\n---\n")
}

func (e *executorTeste) anexados() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.arquivos...)
}

type claudeFalso func(ctx context.Context, pasta, prompt, sistema string, extra, liberadas, negadas []string) (string, beatitudeMedida, error)

func executorDeTeste(t *testing.T, rodar claudeFalso) *executorTeste {
	t.Helper()
	e := &executorTeste{raiz: t.TempDir()}
	antStore, antHook, antArq := panel.store, beatitudeSendHook, beatitudeSendArquivoHook
	antRaiz, antSb, antClaude, antAchar, antDrive := beatitudeExecutorRaiz, beatitudeExecutorSandboxOK, beatitudeExecutorClaude, beatitudeExecutorAcharClaude, beatitudeExecutorDrives
	antAnexo := beatitudeArquivoDoAnexo
	panel.store = storeDeTeste(t)
	_, _ = panel.store.db.Exec(`CREATE TABLE beatitude_runs (id INTEGER PRIMARY KEY AUTOINCREMENT,
		iniciado_em TIMESTAMP, terminado_em TIMESTAMP, pedido TEXT, resposta TEXT, status TEXT, erro TEXT)`)
	initBeatitudeBastao()
	beatitude.mu.Lock()
	antCfg := beatitude.cfg
	beatitude.cfg.Executor, beatitude.cfg.ExecutorWhatsApp, beatitude.cfg.ExecutorDominios = true, false, nil
	beatitude.mu.Unlock()
	ponte.mu.Lock()
	token := ponte.cfg.Token
	ponte.cfg.Token = ""
	ponte.mu.Unlock()
	beatitudeSendHook = func(s string) { e.mu.Lock(); e.textos = append(e.textos, s); e.mu.Unlock() }
	beatitudeSendArquivoHook = func(p string) (bool, string) {
		e.mu.Lock()
		e.arquivos = append(e.arquivos, filepath.Base(p))
		e.mu.Unlock()
		return true, ""
	}
	beatitudeExecutorRaiz = func() string { return e.raiz }
	beatitudeExecutorSandboxOK = func() (bool, string) { return true, "" }
	beatitudeExecutorAcharClaude = func() (string, error) { return "/falso/claude", nil }
	beatitudeExecutorDrives = func() []beatitudeDrive { return nil }
	beatitudeExecutorClaude = func(ctx context.Context, pasta, claude, prompt, sistema string, extra, liberadas, negadas []string) (string, beatitudeMedida, error) {
		return rodar(ctx, pasta, prompt, sistema, extra, liberadas, negadas)
	}
	t.Cleanup(func() {
		executorEsperar(t)
		panel.store, beatitudeSendHook, beatitudeSendArquivoHook = antStore, antHook, antArq
		beatitudeExecutorRaiz, beatitudeExecutorSandboxOK, beatitudeExecutorClaude, beatitudeExecutorAcharClaude, beatitudeExecutorDrives = antRaiz, antSb, antClaude, antAchar, antDrive
		beatitudeArquivoDoAnexo = antAnexo
		beatitude.mu.Lock()
		beatitude.cfg = antCfg
		beatitude.mu.Unlock()
		ponte.mu.Lock()
		ponte.cfg.Token = token
		ponte.mu.Unlock()
	})
	return e
}

// executorEsperar: até a fila do executor esvaziar.
func executorEsperar(t *testing.T) {
	t.Helper()
	for i := 0; i < 500; i++ {
		beatitudeExec.mu.Lock()
		parado := !beatitudeExec.trabalhando
		beatitudeExec.mu.Unlock()
		if parado {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("o executor não terminou")
}

func estadoDoBastao(t *testing.T, id int64) string {
	t.Helper()
	var s string
	_ = panel.store.db.QueryRow(`SELECT COALESCE(executor,'') FROM beatitude_bastoes WHERE id = ?`, id).Scan(&s)
	return s
}

func bastaoDeTeste(t *testing.T, bloco string) *beatitudeBastao {
	t.Helper()
	_, b := beatitudeTirarBastao("ok\n<bastao>" + bloco + "</bastao>")
	if b == nil {
		t.Fatalf("bloco não virou bastão: %s", bloco)
	}
	return b
}

// O caso de 27-11: planilha no grupo, pausa para as datas, planilha de volta.
func TestExecutorDoComecoAoFim(t *testing.T) {
	rodadas := 0
	var args1, args2 []string
	var liberadas, negadas []string
	var prompt1, prompt2 string
	e := executorDeTeste(t, func(ctx context.Context, pasta, prompt, sistema string, extra, lib, neg []string) (string, beatitudeMedida, error) {
		rodadas++
		if rodadas == 1 {
			args1, liberadas, negadas, prompt1 = extra, lib, neg, prompt
			return `Li a planilha. <preciso>{"o_que":"as datas de aniversário de vocês e as férias escolares de 2027"}</preciso>`, beatitudeMedida{Sessao: "sess-1"}, nil
		}
		args2, prompt2 = extra, prompt
		_ = os.WriteFile(filepath.Join(pasta, "saida", "Guarda_2027.xlsx"), []byte("xlsx"), 0o600)
		return "Calendário 2027 pronto: Diego 182 dias, Renata 180.", beatitudeMedida{Sessao: "sess-1"}, nil
	})
	origem := filepath.Join(t.TempDir(), "baixado.xlsx")
	_ = os.WriteFile(origem, []byte("planilha"), 0o600)
	beatitudeArquivoDoAnexo = func(msgID, grupo string) (string, error) { return origem, nil }
	anexos := []beatitudeAnexo{{Codigo: "A1", MsgID: "m1", Nome: "Calendario_2026.xlsx", Hora: time.Now().Add(-2 * time.Hour)}}

	b := bastaoDeTeste(t, `{"pedido":"Monte a guarda de 2027 a partir do calendário de 2026.","porque":"arquivo e código","anexos":["a1"]}`)
	aviso, ok := beatitudeExecutorAceitar(*b, anexos, "1203@g.us")
	if !ok || !strings.HasPrefix(aviso, beatitudeMarca) || !strings.Contains(aviso, "*B1*") || !strings.Contains(aviso, "Calendario_2026.xlsx") || !strings.Contains(aviso, "*para B1*") {
		t.Fatalf("aviso: %q ok=%v", aviso, ok)
	}
	executorEsperar(t)

	if got, _ := os.ReadFile(filepath.Join(e.raiz, "B1", "entrada", "Calendario_2026.xlsx")); string(got) != "planilha" {
		t.Fatal("o arquivo do grupo deveria estar em entrada/")
	}
	if !strings.Contains(prompt1, "Monte a guarda de 2027") || !strings.Contains(prompt1, "Arquivos em entrada/: Calendario_2026.xlsx") {
		t.Fatalf("prompt da 1ª rodada: %q", prompt1)
	}
	if estadoDoBastao(t, b.ID) != beatitudeExecPausado {
		t.Fatalf("deveria estar pausado: %q", estadoDoBastao(t, b.ID))
	}
	if g := e.grupo(); !strings.Contains(g, "◆ Beatitude · B1: Li a planilha.") || !strings.Contains(g, "Responda *continua B1* e a resposta") {
		t.Fatalf("pergunta no grupo: %q", g)
	}
	if l := strings.Join(beatitudeListarBastoes(time.Now()), "\n"); !strings.Contains(l, "*B1* (esperando você") {
		t.Fatalf("bastões deveria mostrar a pausa: %q", l)
	}

	// as travas: sandbox, Read/Edit só na pasta, nenhuma ferramenta de envio, sem WhatsApp
	a := strings.Join(args1, " ")
	pasta := filepath.Join(e.raiz, "B1")
	for _, quer := range []string{"--chrome", "--model opus", "--settings", `"failIfUnavailable":true`, `"allowUnsandboxedCommands":false`, `"denyRead":["~/"`} {
		if !strings.Contains(a, quer) {
			t.Errorf("argumentos sem %s: %s", quer, a)
		}
	}
	lib := strings.Join(liberadas, ",")
	if !strings.Contains(lib, "Read(/"+filepath.ToSlash(pasta)+"/**)") || !strings.Contains(lib, "Edit(/"+filepath.ToSlash(pasta)+"/**)") || !strings.Contains(lib, "Bash") {
		t.Errorf("liberadas: %s", lib)
	}
	for _, l := range liberadas {
		if l == "Read" || l == "Edit" || l == "Write" || l == "Glob" || l == "Grep" || strings.HasPrefix(l, "mcp__lumnia-zap") {
			t.Errorf("não pode estar liberada sem restrição: %s", l)
		}
	}
	neg := "," + strings.Join(negadas, ",") + ","
	for _, quer := range []string{"mcp__lumnia-zap__send_message", "mcp__lumnia-zap__send_file", "mcp__lumnia-zap__send_audio_message", "mcp__beatitude__devolver_ao_grupo", "mcp__claude_ai_Beatitude__devolver_ao_grupo", "Task", "Agent", "mcp__lumnia-zap"} {
		if !strings.Contains(neg, ","+quer+",") {
			t.Errorf("negadas sem %s", quer)
		}
	}
	if strings.Contains(neg, ",Bash,") || strings.Contains(neg, ",WebFetch,") {
		t.Errorf("Bash e WebFetch são do executor: %s", neg)
	}

	// continua B1 <resposta>: mesma sessão, com o texto dela
	id, resposta, ok := beatitudeEhContinuaBastao("Continua B1 eu 14/03, Renata 02/07; férias 01/07 a 31/07")
	if !ok || id != b.ID || resposta != "eu 14/03, Renata 02/07; férias 01/07 a 31/07" {
		t.Fatalf("continua B1: %d %q %v", id, resposta, ok)
	}
	beatitudeExecutorContinuar(id, resposta)
	executorEsperar(t)
	if rodadas != 2 || !strings.Contains(strings.Join(args2, " "), "--resume sess-1") || !strings.Contains(prompt2, "Ela respondeu: eu 14/03") {
		t.Fatalf("2ª rodada: %d %v %q", rodadas, args2, prompt2)
	}
	g := e.grupo()
	if !strings.Contains(g, "◆ Beatitude · B1: continuando.") || !strings.Contains(g, "◆ Beatitude · do seu Claude (B1)\n\nCalendário 2027 pronto") || !strings.Contains(g, "Salvo neste") {
		t.Fatalf("entrega: %q", g)
	}
	if arq := e.anexados(); len(arq) != 1 || arq[0] != "Guarda_2027.xlsx" {
		t.Fatalf("anexados: %v", arq)
	}
	if estadoDoBastao(t, b.ID) != beatitudeExecFeito {
		t.Fatalf("estado final: %q", estadoDoBastao(t, b.ID))
	}
	var devolvido *time.Time
	_ = panel.store.db.QueryRow(`SELECT devolvido_em FROM beatitude_bastoes WHERE id = ?`, b.ID).Scan(&devolvido)
	if devolvido == nil {
		t.Fatal("devolvido_em deveria estar marcado")
	}
	if l := strings.Join(beatitudeListarBastoes(time.Now()), "\n"); strings.Contains(l, "B1") {
		t.Fatalf("feito não aparece mais em bastões: %q", l)
	}
}

// Desligado (o padrão), sem sandbox, projeto do claude.ai ou teto do dia: colar.
func TestExecutorCaiNoPlanoB(t *testing.T) {
	rodou := false
	executorDeTeste(t, func(context.Context, string, string, string, []string, []string, []string) (string, beatitudeMedida, error) {
		rodou = true
		return "ok", beatitudeMedida{}, nil
	})
	b := bastaoDeTeste(t, `{"pedido":"Refaça a planilha.","porque":"arquivo e código"}`)

	beatitude.mu.Lock()
	beatitude.cfg.Executor = false
	beatitude.mu.Unlock()
	if _, ok := beatitudeExecutorAceitar(*b, nil, "g"); ok {
		t.Fatal("desligado não aceita")
	}
	if beatitudeRegraBastaoAtual() != beatitudeRegraBastao {
		t.Fatal("desligado, o mordomo segue com a regra de colar")
	}
	beatitude.mu.Lock()
	beatitude.cfg.Executor = true
	beatitude.mu.Unlock()
	if beatitudeRegraBastaoAtual() != beatitudeRegraBastaoExecutor || !strings.Contains(beatitudeRegraBastaoExecutor, "Nunca diga que passou") {
		t.Fatal("ligado, o mordomo não narra a passagem")
	}

	beatitudeExecutorSandboxOK = func() (bool, string) { return false, "sem sandbox" }
	if _, ok := beatitudeExecutorAceitar(*b, nil, "g"); ok {
		t.Fatal("sem sandbox não aceita")
	}
	beatitudeExecutorSandboxOK = func() (bool, string) { return true, "" }

	proj := bastaoDeTeste(t, `{"pedido":"Analise o projeto Timão.","porque":"os seus projetos do claude.ai"}`)
	if _, ok := beatitudeExecutorAceitar(*proj, nil, "g"); ok {
		t.Fatal("projeto do claude.ai vai para colar")
	}
	if aviso, copiar := proj.ParaColar(); !strings.Contains(aviso, "Copie a próxima mensagem") || !strings.HasPrefix(copiar, beatitudeBastaoPrefixo) {
		t.Fatalf("plano B: %q / %q", aviso, copiar)
	}

	for i := 0; i < beatitudeExecutorTetoDia; i++ {
		_, _ = panel.store.db.Exec(`INSERT INTO beatitude_bastoes (pedido, criado_em, executor) VALUES ('x', ?, ?)`, time.Now(), beatitudeExecFeito)
	}
	if _, ok := beatitudeExecutorAceitar(*b, nil, "g"); ok {
		t.Fatal("no teto do dia volta a colar")
	}
	executorEsperar(t)
	if rodou {
		t.Fatal("nada deveria ter rodado")
	}
}

// Um por vez; o segundo espera e o grupo sabe atrás de quem. "para Bn"
// mata o que roda e tira da fila o que espera.
func TestExecutorFilaEPara(t *testing.T) {
	comecou := make(chan string, 4)
	var cancelado sync.WaitGroup
	cancelado.Add(1)
	e := executorDeTeste(t, func(ctx context.Context, pasta, prompt, sistema string, extra, lib, neg []string) (string, beatitudeMedida, error) {
		comecou <- filepath.Base(pasta)
		<-ctx.Done() // só termina quando alguém para
		cancelado.Done()
		return "", beatitudeMedida{}, errBeatitudeInterrompido
	})
	b1 := bastaoDeTeste(t, `{"pedido":"primeiro"}`)
	b2 := bastaoDeTeste(t, `{"pedido":"segundo"}`)
	if _, ok := beatitudeExecutorAceitar(*b1, nil, "g"); !ok {
		t.Fatal("B1 deveria entrar")
	}
	if got := <-comecou; got != "B1" {
		t.Fatalf("rodando %s", got)
	}
	aviso, ok := beatitudeExecutorAceitar(*b2, nil, "g")
	if !ok || !strings.Contains(aviso, "Está na fila, depois do B1") {
		t.Fatalf("B2: %q", aviso)
	}
	if beatitudeExecutorAtual() != "B1" {
		t.Fatalf("atual = %q", beatitudeExecutorAtual())
	}
	if aviso, ocupado := beatitudeChromeOcupado("Responda *abre* de novo"); !ocupado || !strings.Contains(aviso, "*B1*") {
		t.Fatalf("a tarefa de Chrome deveria esperar o B1: %q", aviso)
	}
	if l := strings.Join(beatitudeListarBastoes(time.Now()), "\n"); !strings.Contains(l, "*B1* (rodando") || !strings.Contains(l, "*B2* (na fila") {
		t.Fatalf("bastões: %q", l)
	}

	// para B2 (na fila) e depois para B1 (rodando)
	for texto, quer := range map[string]int64{"para B2": b2.ID, "Cancela o b1.": b1.ID} {
		if id, colar, ok := beatitudeEhParaBastao(texto, time.Now()); !ok || colar || id != quer {
			t.Fatalf("%q: %d %v %v", texto, id, colar, ok)
		}
	}
	if _, _, ok := beatitudeEhParaBastao("para B1 e me diz o que achou até agora", time.Now()); ok {
		t.Fatal("frase livre depois de 'para B1' segue para o mordomo")
	}
	if _, _, ok := beatitudeEhParaBastao("para B9", time.Now()); ok {
		t.Fatal("bastão que não existe não é comando")
	}
	beatitudeExecutorParar(b2.ID, false)
	beatitudeExecutorParar(b1.ID, false)
	cancelado.Wait()
	executorEsperar(t)
	if estadoDoBastao(t, b1.ID) != beatitudeExecCancelado || estadoDoBastao(t, b2.ID) != beatitudeExecCancelado {
		t.Fatalf("estados: %q %q", estadoDoBastao(t, b1.ID), estadoDoBastao(t, b2.ID))
	}
	select {
	case got := <-comecou:
		t.Fatalf("%s não deveria ter rodado depois de cancelado", got)
	default:
	}
	g := e.grupo()
	if !strings.Contains(g, "B1: interrompido. O que já estava pronto ficou em") || !strings.Contains(g, "B2: cancelado.") {
		t.Fatalf("grupo: %q", g)
	}

	// "para B1 manda para eu colar": o texto do plano B
	if id, colar, ok := beatitudeEhParaBastao("para B1, manda para eu colar", time.Now()); !ok || !colar || id != b1.ID {
		t.Fatal("deveria reconhecer o pedido de colar")
	}
	beatitudeExecutorParar(b1.ID, true)
	if g := e.grupo(); !strings.Contains(g, beatitudeBastaoPrefixo+"B1]\nprimeiro") {
		t.Fatalf("texto para colar: %q", g)
	}
}

func TestExecutorEntregaCortaArquivosETexto(t *testing.T) {
	longo := strings.Repeat("a", beatitudeBastaoMaxResp+900)
	drive := filepath.Join(t.TempDir(), "Beatitude")
	onedrive := filepath.Join(t.TempDir(), "Beatitude")
	_ = os.MkdirAll(drive, 0o700)
	_ = os.MkdirAll(onedrive, 0o700)
	e := executorDeTeste(t, func(ctx context.Context, pasta, prompt, sistema string, extra, lib, neg []string) (string, beatitudeMedida, error) {
		for _, n := range []string{"1.txt", "2.txt", "3.txt", "4.txt", "5.txt", "6.txt", ".oculto"} {
			_ = os.WriteFile(filepath.Join(pasta, "saida", n), []byte("x"), 0o600)
		}
		f, _ := os.Create(filepath.Join(pasta, "saida", "0-grande.bin"))
		_ = f.Truncate(beatitudeMidiaMaxBytes + 1)
		f.Close()
		return longo, beatitudeMedida{}, nil
	})
	// quem tem as duas nuvens recebe a cópia nas duas
	beatitudeExecutorDrives = func() []beatitudeDrive {
		return []beatitudeDrive{{Nome: "Drive", Pasta: drive}, {Nome: "OneDrive", Pasta: onedrive}}
	}
	b := bastaoDeTeste(t, `{"pedido":"gera os arquivos"}`)
	if _, ok := beatitudeExecutorAceitar(*b, nil, "g"); !ok {
		t.Fatal("deveria aceitar")
	}
	executorEsperar(t)
	if arq := e.anexados(); strings.Join(arq, ",") != "1.txt,2.txt,3.txt,4.txt,5.txt" {
		t.Fatalf("anexados (no máximo 5, sem o de 20 MB): %v", arq)
	}
	g := e.grupo()
	if !strings.Contains(g, "Salvo no Drive e no OneDrive em Beatitude/Pedidos/B1.") || !strings.Contains(g, "Não anexei") || !strings.Contains(g, "0-grande.bin") || !strings.Contains(g, "6.txt") || strings.Contains(g, ".oculto") {
		t.Fatalf("rodapé: %q", g[len(g)-400:])
	}
	if strings.Contains(g, strings.Repeat("a", beatitudeBastaoMaxResp+300)) || !strings.Contains(g, "…") {
		t.Fatal("texto acima do limite deveria ser cortado")
	}
	if _, err := os.Stat(filepath.Join(drive, "Pedidos", "B1", "6.txt")); err != nil {
		t.Fatal("o Drive recebe todos os arquivos de saida/")
	}
	if _, err := os.Stat(filepath.Join(onedrive, "Pedidos", "B1", "1.txt")); err != nil {
		t.Fatal("o OneDrive também recebe a cópia")
	}
	if _, err := os.Stat(filepath.Join(drive, "Pedidos", "B1", ".oculto")); err == nil {
		t.Fatal("arquivo oculto não vai ao Drive")
	}
}

func TestExecutorAnexoMaisRecenteEFalta(t *testing.T) {
	executorDeTeste(t, func(context.Context, string, string, string, []string, []string, []string) (string, beatitudeMedida, error) {
		return "ok", beatitudeMedida{}, nil
	})
	dir := t.TempDir()
	origem := filepath.Join(dir, "x.pdf")
	_ = os.WriteFile(origem, []byte("pdf"), 0o600)
	beatitudeArquivoDoAnexo = func(msgID, grupo string) (string, error) {
		if msgID == "ruim" {
			return "", errors.New("arquivo indisponível")
		}
		return origem, nil
	}
	agora := time.Now()
	recente := []beatitudeAnexo{{Codigo: "A1", MsgID: "m1", Nome: "../../fora/Contrato", Hora: agora.Add(-5 * time.Minute)}, {Codigo: "A2", MsgID: "m2", Nome: "velho.pdf", Hora: agora.Add(-3 * time.Hour)}}

	// sem "anexos": o mais novo, se chegou nos últimos 15 min; o nome não escapa da pasta
	p1 := filepath.Join(dir, "p1")
	_ = os.MkdirAll(filepath.Join(p1, "entrada"), 0o700)
	cop, falt := beatitudeExecutorCopiarEntrada(p1, nil, recente, "g", agora)
	if len(cop) != 1 || cop[0] != "Contrato.pdf" || len(falt) != 0 {
		t.Fatalf("mais recente: %v %v", cop, falt)
	}
	if _, err := os.Stat(filepath.Join(p1, "entrada", "Contrato.pdf")); err != nil {
		t.Fatal("arquivo deveria estar dentro de entrada/")
	}
	// mais de 15 min: nada vai sozinho
	velhos := []beatitudeAnexo{{Codigo: "A1", MsgID: "m2", Nome: "velho.pdf", Hora: agora.Add(-40 * time.Minute)}}
	if cop, _ := beatitudeExecutorCopiarEntrada(p1, nil, velhos, "g", agora); len(cop) != 0 {
		t.Fatalf("arquivo de 40 min atrás não entra sozinho: %v", cop)
	}
	// código que não existe e arquivo que não baixa: seguem, e o aviso diz
	ruim := []beatitudeAnexo{{Codigo: "A1", MsgID: "ruim", Nome: "sumiu.xlsx", Hora: agora}}
	cop, falt = beatitudeExecutorCopiarEntrada(p1, []string{"A1", "A7"}, ruim, "g", agora)
	if len(cop) != 0 || strings.Join(falt, ",") != "A7,sumiu.xlsx" {
		t.Fatalf("faltaram: %v %v", cop, falt)
	}
}

func TestExecutorPausaVenceEFalhaOfereceColar(t *testing.T) {
	e := executorDeTeste(t, func(ctx context.Context, pasta, prompt, sistema string, extra, lib, neg []string) (string, beatitudeMedida, error) {
		if strings.Contains(prompt, "quebra") {
			return "", beatitudeMedida{}, errors.New("Claude devolveu erro (error_during_execution): sandbox indisponível")
		}
		if strings.Contains(prompt, "demora") {
			_ = os.WriteFile(filepath.Join(pasta, "saida", "parcial.csv"), []byte("x"), 0o600)
			return "", beatitudeMedida{}, errors.New("passou de 45 minutos")
		}
		return `<preciso>{"site":"gov.br","o_que":"entrar com a sua conta"}</preciso>`, beatitudeMedida{Sessao: "s9"}, nil
	})
	pausa := bastaoDeTeste(t, `{"pedido":"baixa a certidão"}`)
	_, _ = beatitudeExecutorAceitar(*pausa, nil, "g")
	executorEsperar(t)
	if g := e.grupo(); !strings.Contains(g, "preciso de você em gov.br") || !strings.Contains(g, "Depois responda *continua B1*") {
		t.Fatalf("pausa em site: %q", g)
	}
	var em time.Time
	_ = panel.store.db.QueryRow(`SELECT pausado_em FROM beatitude_bastoes WHERE id = ?`, pausa.ID).Scan(&em)
	beatitudeExecutorPausaVencida(pausa.ID, em.Add(-time.Hour)) // relógio de uma pausa antiga: não cancela a atual
	if estadoDoBastao(t, pausa.ID) != beatitudeExecPausado {
		t.Fatal("a pausa nova tem o seu próprio relógio")
	}
	beatitudeExecutorPausaVencida(pausa.ID, em)
	beatitudeExecutorPausaVencida(pausa.ID, em) // repetido: um aviso só
	if estadoDoBastao(t, pausa.ID) != beatitudeExecCancelado || strings.Count(e.grupo(), "ficou 24 horas esperando") != 1 {
		t.Fatalf("pausa vencida: %q", e.grupo())
	}
	beatitudeExecutorContinuar(pausa.ID, "pronto")
	if g := e.grupo(); !strings.Contains(g, "B1: esse pedido não está esperando você") {
		t.Fatalf("continuar depois de vencida: %q", g)
	}

	falha := bastaoDeTeste(t, `{"pedido":"quebra"}`)
	_, _ = beatitudeExecutorAceitar(*falha, nil, "g")
	executorEsperar(t)
	if g := e.grupo(); estadoDoBastao(t, falha.ID) != beatitudeExecFalhou || !strings.Contains(g, "B2: não consegui fazer neste") || !strings.Contains(g, "*para B2 manda para eu colar*") {
		t.Fatalf("falha: %q", g)
	}

	lento := bastaoDeTeste(t, `{"pedido":"demora"}`)
	_, _ = beatitudeExecutorAceitar(*lento, nil, "g")
	executorEsperar(t)
	if g := e.grupo(); !strings.Contains(g, "do seu Claude (B3)\n\nParei aos 45 minutos") || len(e.anexados()) != 1 {
		t.Fatalf("tempo esgotado entrega o que há em saida/: %q %v", g, e.anexados())
	}
}

func TestExecutorReinicioMarcaFalhouEAvisa(t *testing.T) {
	e := executorDeTeste(t, func(context.Context, string, string, string, []string, []string, []string) (string, beatitudeMedida, error) {
		return "ok", beatitudeMedida{}, nil
	})
	_, _ = panel.store.db.Exec(`INSERT INTO beatitude_bastoes (pedido, criado_em, executor) VALUES ('a', ?, ?), ('b', ?, ?), ('c', ?, ?)`,
		time.Now(), beatitudeExecRodando, time.Now(), beatitudeExecFila, time.Now(), beatitudeExecFeito)
	initBeatitudeBastao() // a ponte subiu de novo
	for i := 0; i < 200 && !strings.Contains(e.grupo(), "reiniciou"); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if g := e.grupo(); !strings.Contains(g, "reiniciou no meio de *B1*, *B2*") {
		t.Fatalf("aviso do reinício: %q", g)
	}
	if estadoDoBastao(t, 1) != beatitudeExecFalhou || estadoDoBastao(t, 2) != beatitudeExecFalhou || estadoDoBastao(t, 3) != beatitudeExecFeito {
		t.Fatal("rodando e fila viram falhou; feito fica")
	}
}

func TestExecutorComandosNaoQuebramOChrome(t *testing.T) {
	executorDeTeste(t, func(context.Context, string, string, string, []string, []string, []string) (string, beatitudeMedida, error) {
		return "ok", beatitudeMedida{}, nil
	})
	// "continua 3" e "continua" seguem sendo da tarefa de Chrome
	for _, s := range []string{"continua 3", "continua", "pode continuar 2"} {
		if beatitudeContinuaBastaoRe.MatchString(s) {
			t.Errorf("%q é da tarefa de Chrome", s)
		}
		if !beatitudeContinuaRe.MatchString(s) {
			t.Errorf("%q deveria continuar valendo para o Chrome", s)
		}
	}
	// bastão que nunca passou pelo executor (plano B): "continua B1" é conversa
	b := bastaoDeTeste(t, `{"pedido":"colar"}`)
	if _, _, ok := beatitudeEhContinuaBastao("continua B1 com isso"); ok {
		t.Fatal("bastão de colar não tem o que continuar")
	}
	_, _ = panel.store.db.Exec(`UPDATE beatitude_bastoes SET executor = ? WHERE id = ?`, beatitudeExecPausado, b.ID)
	if id, r, ok := beatitudeEhContinuaBastao("continua b1"); !ok || id != b.ID || r != "" {
		t.Fatal("continua B1 sem texto vale (login feito no Chrome)")
	}
	if falaEhConversa("para B1") || falaEhConversa("continua B1 eu 14/03") {
		t.Fatal("comandos do executor não viram fala no app")
	}
}

func TestExecutorAjustesDoSandbox(t *testing.T) {
	s := beatitudeExecutorAjustes("/Users/d/Beatitude/Pedidos/B1", "/Users/d", "/Users/d/Library/Application Support/LumniaZap", nil)
	var a struct {
		Sandbox struct {
			Enabled           bool `json:"enabled"`
			FailIfUnavailable bool `json:"failIfUnavailable"`
			Unsandboxed       bool `json:"allowUnsandboxedCommands"`
			Filesystem        struct {
				DenyRead  []string `json:"denyRead"`
				AllowRead []string `json:"allowRead"`
			} `json:"filesystem"`
			Network struct {
				Dominios []string `json:"allowedDomains"`
			} `json:"network"`
		} `json:"sandbox"`
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		t.Fatal(err)
	}
	if !a.Sandbox.Enabled || !a.Sandbox.FailIfUnavailable || a.Sandbox.Unsandboxed {
		t.Fatalf("sandbox obrigatório: %s", s)
	}
	if strings.Join(a.Sandbox.Filesystem.DenyRead, ",") != "~/" || strings.Join(a.Sandbox.Filesystem.AllowRead, ",") != "/Users/d/Beatitude/Pedidos/B1" {
		t.Fatalf("leitura do terminal: %s", s)
	}
	nega := strings.Join(a.Permissions.Deny, "|")
	for _, quer := range []string{"Read(//Users/d/Library/Application Support/LumniaZap/**)", "Read(//Users/d/.ssh/**)", "Read(//Users/d/.lumnia/**)", "Edit(//Users/d/.aws/**)"} {
		if !strings.Contains(nega, quer) {
			t.Errorf("deny sem %s", quer)
		}
	}
	for _, d := range a.Sandbox.Network.Dominios {
		if d == "*" {
			t.Fatal("rede do terminal não é aberta por padrão")
		}
	}
	// na nuvem o Zap mora fora da pasta pessoal? então entra no denyRead também
	s = beatitudeExecutorAjustes("/home/b/Beatitude/Pedidos/B1", "/home/b", "/opt/lumnia-zap", []string{"*"})
	if !strings.Contains(s, `"denyRead":["~/","/opt/lumnia-zap"]`) || !strings.Contains(s, `"allowedDomains":["*"]`) {
		t.Fatalf("ajustes: %s", s)
	}
}
