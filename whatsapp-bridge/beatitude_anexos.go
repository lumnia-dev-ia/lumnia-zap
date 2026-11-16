// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_anexos.go — arquivo junto da mensagem (16-11, 1.1.3).
//
// Em 16-11 a Lisiane pediu ao mordomo uma mensagem para o Rodrigo às 20h com
// um PDF, e ele só conseguiu o texto: o rascunho guardava destino, texto e
// horário, e o agendador mandava só texto. A ponte em si sempre soube mandar
// arquivo (o /api/send que o send_file do MCP usa).
//
// Como fica:
//   - o grupo Beatitude é a "pasta de upload": o arquivo que a pessoa manda
//     ou encaminha para o grupo (foto, PDF, Word, planilha) fica disponível
//     por 7 dias. Do e-mail ou do OneDrive, ela usa Compartilhar → WhatsApp.
//   - o mordomo recebe a lista desses arquivos com códigos (A1 = o mais novo)
//     e pode pôr "anexo":"A1" no rascunho. Ele só escolhe o código; quem acha
//     o arquivo é a ponte, e só arquivos que chegaram no próprio grupo.
//   - no "sim" a ponte manda o texto e, na sequência, o arquivo. Com horário,
//     o agendador do painel faz o mesmo na hora marcada.
//   - o modelo continua sem nenhuma ferramenta de envio.

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	beatitudeAnexoJanela = 7 * 24 * time.Hour // o que o grupo recebeu nesse prazo pode ir junto
	beatitudeAnexoLista  = 8                  // quantos entram na lista do mordomo
)

type beatitudeAnexo struct {
	Codigo  string // A1, A2… (A1 = o mais novo)
	MsgID   string // id da mensagem no grupo
	Nome    string
	Tipo    string // image | document | video
	Hora    time.Time
	Legenda string
}

// beatitudeAnexosDoGrupo: os arquivos que chegaram no grupo nos últimos 7
// dias, do mais novo para o mais antigo. Áudio fica de fora (áudio é pedido).
func beatitudeAnexosDoGrupo(grupo string, agora time.Time) []beatitudeAnexo {
	if panel.store == nil || grupo == "" {
		return nil
	}
	rows, err := panel.store.db.Query(`SELECT id, media_type, COALESCE(filename,''), COALESCE(content,''), timestamp
		FROM messages WHERE chat_jid = ? AND media_type IN ('image','document','video') AND timestamp >= ?
		ORDER BY timestamp DESC LIMIT ?`, grupo, agora.Add(-beatitudeAnexoJanela), beatitudeAnexoLista)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []beatitudeAnexo
	for rows.Next() {
		var a beatitudeAnexo
		if rows.Scan(&a.MsgID, &a.Tipo, &a.Nome, &a.Legenda, &a.Hora) != nil {
			continue
		}
		a.Codigo = fmt.Sprintf("A%d", len(out)+1)
		if a.Nome == "" {
			a.Nome = map[string]string{"image": "foto", "video": "vídeo"}[a.Tipo]
			if a.Nome == "" {
				a.Nome = "arquivo"
			}
		}
		out = append(out, a)
	}
	return out
}

func beatitudeRotuloAnexo(a beatitudeAnexo) string {
	switch {
	case a.Tipo == "image":
		return "foto"
	case a.Tipo == "video":
		return "vídeo"
	case strings.HasSuffix(strings.ToLower(a.Nome), ".pdf"):
		return "PDF"
	}
	return "documento"
}

// beatitudeBlocoAnexos: como a lista entra no pedido do mordomo.
func beatitudeBlocoAnexos(lista []beatitudeAnexo) string {
	if len(lista) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Arquivos que ela mandou no grupo (últimos 7 dias, mais novo primeiro; use o código em \"anexo\"):\n")
	for _, a := range lista {
		fmt.Fprintf(&b, "%s · %s · %s · %s", a.Codigo, a.Nome, beatitudeRotuloAnexo(a), a.Hora.In(panelLoc).Format("02/01 15:04"))
		if l := strings.TrimSpace(a.Legenda); l != "" {
			b.WriteString(" · \"" + beatitudeCurto(l, 120) + "\"")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func beatitudeAcharAnexo(lista []beatitudeAnexo, codigo string) (beatitudeAnexo, bool) {
	codigo = strings.ToUpper(strings.TrimSpace(codigo))
	for _, a := range lista {
		if a.Codigo == codigo {
			return a, true
		}
	}
	return beatitudeAnexo{}, false
}

// beatitudeArquivoDoAnexo devolve o caminho local do arquivo de uma mensagem
// do grupo, baixando se ainda não estiver no disco. Variável para os testes.
var beatitudeArquivoDoAnexo = func(msgID, grupo string) (string, error) {
	if panel.client == nil || panel.store == nil {
		return "", fmt.Errorf("ponte sem conexão")
	}
	ok, _, _, caminho, err := downloadMedia(panel.client, panel.store, msgID, grupo)
	if err != nil {
		return "", err
	}
	if !ok || caminho == "" {
		return "", fmt.Errorf("arquivo indisponível")
	}
	info, err := os.Stat(caminho)
	if err != nil {
		return "", fmt.Errorf("arquivo indisponível")
	}
	if info.Size() > beatitudeMidiaMaxBytes {
		return "", fmt.Errorf("arquivo maior que 20 MB")
	}
	return caminho, nil
}

const beatitudeRegraAnexo = "Anexo: se ela pedir que a mensagem leve um arquivo que mandou no grupo (\"manda o PDF pro Rodrigo\", \"envia junto o comparativo\", \"encaminha essa foto para a Marina\"), ponha no rascunho \"anexo\":\"A1\" com o código da lista \"Arquivos que ela mandou no grupo\". A ponte envia o texto e, logo depois, o arquivo, também no horário marcado. Só vale arquivo dessa lista. Se o arquivo não estiver nela, não escreva o rascunho: diga em uma linha que ela mande ou encaminhe o arquivo para este grupo (do e-mail ou do OneDrive, pelo Compartilhar do celular) e repita o pedido. Se houver dois arquivos possíveis, pergunte qual. Mensagem com arquivo vai sempre por rascunho, nunca por criar_acao. Nunca diga que não consegue mandar arquivo."

// beatitudeNomeDoArquivo: o nome com que o arquivo chegou no grupo.
func beatitudeNomeDoArquivo(grupo, msgID string) string {
	nome := ""
	if panel.store != nil {
		_ = panel.store.db.QueryRow(`SELECT COALESCE(filename,'') FROM messages WHERE id = ? AND chat_jid = ?`, msgID, grupo).Scan(&nome)
	}
	if strings.TrimSpace(nome) == "" {
		return "que você mandou"
	}
	return nome
}
