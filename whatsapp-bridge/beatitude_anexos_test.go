package main

import (
	"os"

	"go.mau.fi/whatsmeow"
	waLog "go.mau.fi/whatsmeow/util/log"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type envioDeTeste struct{ para, texto, arquivo string }

// storeAnexos: o banco do storeAgendado, um PDF no grupo, o arquivo no disco
// e o envio de fato trocado por um registro.
func storeAnexos(t *testing.T) (*[]envioDeTeste, string, func()) {
	t.Helper()
	fim := storeAgendado(t)
	grupo := "1203@g.us"
	antigoCfg := beatitude.cfg
	beatitude.cfg.GrupoJID = grupo
	agora := time.Now()
	_, _ = panel.store.db.Exec(`INSERT INTO chats VALUES (?, 'Beatitude', ?)`, grupo, agora)
	_, _ = panel.store.db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename)
		VALUES ('PDF1', ?, 'eu', '', ?, 1, 'document', 'comparativo-formacao.pdf')`, grupo, agora.Add(-10*time.Minute))
	_, _ = panel.store.db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename)
		VALUES ('VELHO', ?, 'eu', '', ?, 1, 'document', 'antigo.pdf')`, grupo, agora.Add(-8*24*time.Hour))
	_, _ = panel.store.db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename)
		VALUES ('AUD', ?, 'eu', '', ?, 1, 'audio', 'a.ogg')`, grupo, agora.Add(-5*time.Minute))

	caminho := filepath.Join(t.TempDir(), "comparativo-formacao.pdf")
	if err := os.WriteFile(caminho, []byte("%PDF-1.4"), 0o600); err != nil {
		t.Fatal(err)
	}
	antigoArq, antigoEnviar := beatitudeArquivoDoAnexo, panelEnviar
	beatitudeArquivoDoAnexo = func(msgID, g string) (string, error) {
		if msgID == "PDF1" && g == grupo {
			return caminho, nil
		}
		return "", os.ErrNotExist
	}
	var envios []envioDeTeste
	panelEnviar = func(para, texto, arquivo string) (bool, string) {
		envios = append(envios, envioDeTeste{para, texto, arquivo})
		return true, "ok"
	}
	return &envios, caminho, func() {
		beatitudeArquivoDoAnexo, panelEnviar = antigoArq, antigoEnviar
		beatitude.cfg = antigoCfg
		fim()
	}
}

func TestAnexosDoGrupoListaSoArquivosRecentes(t *testing.T) {
	_, _, fim := storeAnexos(t)
	defer fim()
	lista := beatitudeAnexosDoGrupo("1203@g.us", time.Now())
	if len(lista) != 1 || lista[0].Codigo != "A1" || lista[0].MsgID != "PDF1" {
		t.Fatalf("só o PDF recente, sem áudio e sem o de 8 dias: %+v", lista)
	}
	bloco := beatitudeBlocoAnexos(lista)
	if !strings.Contains(bloco, "A1 · comparativo-formacao.pdf · PDF") {
		t.Fatalf("bloco: %q", bloco)
	}
	if beatitudeBlocoAnexos(nil) != "" {
		t.Fatal("sem arquivos, sem bloco")
	}
}

// O caso de 16-11: mensagem ao Rodrigo às 20h com o PDF.
func TestRascunhoComAnexoAgendadoSaiComArquivo(t *testing.T) {
	envios, caminho, fim := storeAnexos(t)
	defer fim()
	grupo := "1203@g.us"
	quando := time.Now().In(panelLoc).Add(3 * time.Hour).Truncate(time.Minute)
	anexos := beatitudeAnexosDoGrupo(grupo, time.Now())
	resp := `Vai para a Cris, com o comparativo. <rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi, segue o comparativo","enviar_em":"` +
		quando.Format("2006-01-02 15:04") + `","anexo":"A1"}]</rascunhos>`
	_, ponte := beatitudeSepararRascunhos(resp, grupo, "às "+quando.Format("15:04")+" manda o PDF", anexos...)
	if !strings.Contains(ponte, "com o arquivo *comparativo-formacao.pdf*") || !strings.Contains(ponte, "programada para *") {
		t.Fatalf("o cartão deveria mostrar horário e arquivo: %q", ponte)
	}
	if len(*envios) != 0 {
		t.Fatal("nada sai antes da hora")
	}
	var midia string
	var id int64
	if err := panel.store.db.QueryRow(`SELECT id, COALESCE(media_path,'') FROM panel_schedules`).Scan(&id, &midia); err != nil {
		t.Fatal(err)
	}
	if midia != caminho {
		t.Fatalf("o agendamento deveria levar o arquivo: %q", midia)
	}

	// a hora chega: o agendador do painel manda o texto e, em seguida, o arquivo
	_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET on_date = ?, at_time = ? WHERE id = ?`,
		time.Now().In(panelLoc).Format("2006-01-02"), time.Now().In(panelLoc).Format("15:04"), id)
	antigoCli, antigoLog := panel.client, panel.logger
	panel.client, panel.logger = &whatsmeow.Client{}, waLog.Noop
	defer func() { panel.client, panel.logger = antigoCli, antigoLog }()
	runDueSchedules()
	if len(*envios) != 2 || (*envios)[0].texto != "Oi, segue o comparativo" || (*envios)[0].arquivo != "" ||
		(*envios)[1].arquivo != caminho || (*envios)[1].texto != "" {
		t.Fatalf("texto e depois o arquivo, sem legenda: %+v", *envios)
	}
	var enviado *time.Time
	_ = panel.store.db.QueryRow(`SELECT enviado_em FROM beatitude_rascunhos`).Scan(&enviado)
	if enviado == nil {
		t.Fatal("o aviso do agendador deveria marcar enviado")
	}
}

func TestRascunhoComAnexoSimEnviaTextoEArquivo(t *testing.T) {
	envios, caminho, fim := storeAnexos(t)
	defer fim()
	grupo := "1203@g.us"
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi, segue","anexo":"a1"}]</rascunhos>`
	beatitudeSepararRascunhos(resp, grupo, "manda o PDF pra Cris", beatitudeAnexosDoGrupo(grupo, time.Now())...)
	linhas := strings.Join(beatitudeComando("sim", time.Now()), "\n")
	if !strings.Contains(linhas, "enviado, com o arquivo comparativo-formacao.pdf") {
		t.Fatalf("linhas: %q", linhas)
	}
	if len(*envios) != 2 || (*envios)[0].texto != "Oi, segue" || (*envios)[1].arquivo != caminho {
		t.Fatalf("envios: %+v", *envios)
	}
}

func TestAnexoInventadoDerrubaORascunho(t *testing.T) {
	envios, _, fim := storeAnexos(t)
	defer fim()
	grupo := "1203@g.us"
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi, segue","anexo":"A7"}]</rascunhos>`
	_, ponte := beatitudeSepararRascunhos(resp, grupo, "manda o PDF", beatitudeAnexosDoGrupo(grupo, time.Now())...)
	if !strings.Contains(ponte, "Não achei o arquivo para Cris Meirelles") {
		t.Fatalf("deveria avisar e não criar o rascunho: %q", ponte)
	}
	if beatitudePendentes(beatitudeJanelaResposta) != 0 {
		t.Fatal("rascunho com arquivo inexistente não pode ficar pendente")
	}
	if len(*envios) != 0 {
		t.Fatal("nada sai")
	}
}

func TestArquivoIndisponivelNaoMandaNemOTexto(t *testing.T) {
	envios, _, fim := storeAnexos(t)
	defer fim()
	grupo := "1203@g.us"
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi, segue","anexo":"A1"}]</rascunhos>`
	beatitudeSepararRascunhos(resp, grupo, "manda o PDF", beatitudeAnexosDoGrupo(grupo, time.Now())...)
	beatitudeArquivoDoAnexo = func(string, string) (string, error) { return "", os.ErrNotExist }
	linhas := strings.Join(beatitudeComando("sim", time.Now()), "\n")
	if !strings.Contains(linhas, "não enviei, o arquivo comparativo-formacao.pdf não está disponível") {
		t.Fatalf("linhas: %q", linhas)
	}
	if len(*envios) != 0 {
		t.Fatalf("o texto não pode sair sem o arquivo prometido: %+v", *envios)
	}
}

func TestAgendadoSemOArquivoAvisaNoGrupo(t *testing.T) {
	_, _, fim := storeAnexos(t)
	defer fim()
	grupo := "1203@g.us"
	quando := time.Now().In(panelLoc).Add(2 * time.Hour)
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi","enviar_em":"` + quando.Format("2006-01-02 15:04") + `","anexo":"A1"}]</rascunhos>`
	beatitudeSepararRascunhos(resp, grupo, "às 20h manda o PDF", beatitudeAnexosDoGrupo(grupo, time.Now())...)
	beatitudeComando("1", time.Now())
	var id int64
	_ = panel.store.db.QueryRow(`SELECT id FROM panel_schedules`).Scan(&id)
	panelAoRodarAgendamento(id, "enviada sem o arquivo: arquivo não encontrado", quando)
	var enviado, cancelado *time.Time
	_ = panel.store.db.QueryRow(`SELECT enviado_em, cancelado_em FROM beatitude_rascunhos`).Scan(&enviado, &cancelado)
	if enviado == nil || cancelado != nil {
		t.Fatal("o texto saiu: marca enviado, não cancelado")
	}
}

func TestRascunhoSemAnexoContinuaIgual(t *testing.T) {
	envios, _, fim := storeAnexos(t)
	defer fim()
	resp := `<rascunhos>[{"n":1,"chat_jid":"5511888880000@s.whatsapp.net","texto":"Oi Cris"}]</rascunhos>`
	_, ponte := beatitudeSepararRascunhos(resp, "1203@g.us", "responde a Cris")
	if strings.Contains(ponte, "arquivo") {
		t.Fatalf("sem anexo não fala de arquivo: %q", ponte)
	}
	beatitudeComando("sim", time.Now())
	if len(*envios) != 1 || (*envios)[0].arquivo != "" {
		t.Fatalf("só o texto: %+v", *envios)
	}
}
