// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_pessoas.go — o que a ponte conta ao Beatitude sobre as pessoas
// do circulo, alem dos pendentes. Duas coisas, e so elas:
//
//   - a data da ultima troca com cada conversa individual (enviada junto com
//     o retrato dos pendentes, no campo "ultimas"). So telefone e data, nunca
//     texto, e so de quem o servidor ja disse que esta em Pessoas: a lista
//     vem de /api/ponte/pessoas e e guardada aqui por algumas horas. Enquanto
//     ela nao chega, nada sai. Telefone de quem nao esta no circulo (um
//     cliente, um paciente, um fornecedor) nunca deixa este computador. O
//     servidor usa a data para o sinal de silencio, que so dispara pela
//     cadencia que a pessoa declarou.
//   - a foto de perfil do WhatsApp de quem esta em Pessoas e ainda nao tem
//     foto no app, uma vez por dia. A lista de quem buscar vem do servidor
//     (/api/ponte/pessoas); o Zap nao decide quem entra.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const (
	ponteJanelaUltimas  = 180 * 24 * time.Hour
	ponteMaxUltimas     = 1000
	ponteFotosCada      = 24 * time.Hour
	ponteFotoTentarCada = 7 * 24 * time.Hour
	ponteMaxFotosRodada = 10
	ponteFotoMaxBytes   = 2_000_000
	ponteCirculoCada    = 6 * time.Hour
)

// ponteCirculo: quem o servidor diz que esta em Pessoas (telefone so digitos).
// Vazio ate a primeira resposta do app; sem ele, "ultimas" sai vazio.
var ponteCirculo struct {
	em        time.Time
	telefones map[string]bool
	pessoas   []pontePessoa
}

var ponteFotos struct {
	ultima    time.Time
	tentativa map[string]time.Time // person_id -> ultima tentativa
}

type ponteUltima struct {
	Telefone string `json:"telefone"`
	Em       string `json:"em"`
}

// ponteUltimas: para cada pessoa do circulo com conversa individual nos
// ultimos 180 dias, o telefone e a data da ultima mensagem (de qualquer lado).
func ponteUltimas(grupo string) []ponteUltima {
	if panel.store == nil {
		return nil
	}
	circulo := ponteTelefonesDoCirculo()
	if len(circulo) == 0 {
		return nil
	}
	limite := time.Now().Add(-ponteJanelaUltimas)
	rows, err := panel.store.db.Query(`
		SELECT chat_jid, MAX(timestamp) FROM messages
		WHERE timestamp >= ?
		  AND chat_jid NOT LIKE '%@g.us' AND chat_jid NOT LIKE '%@broadcast'
		  AND chat_jid NOT LIKE '%@newsletter' AND chat_jid != ?
		GROUP BY chat_jid`, limite, grupo)
	if err != nil {
		ponteErro(err)
		return nil
	}
	defer rows.Close()
	type par struct {
		chat string
		em   time.Time
	}
	var pares []par
	for rows.Next() {
		var c string
		var bruto interface{} // MAX(timestamp) volta como texto; ver ponteTempoSQL
		if rows.Scan(&c, &bruto) == nil {
			if em, ok := ponteTempoSQL(bruto); ok {
				pares = append(pares, par{c, em})
			}
		}
	}
	vistos := map[string]bool{}
	out := []ponteUltima{}
	for _, p := range pares {
		tel := ponteTelefoneDoChat(p.chat)
		if tel == "" || vistos[tel] || !circulo[strings.TrimLeft(tel, "+")] {
			continue
		}
		vistos[tel] = true
		out = append(out, ponteUltima{Telefone: tel, Em: p.em.Format(time.RFC3339)})
		if len(out) >= ponteMaxUltimas {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------- HTTP generico

// ponteRequisitar e o irmao de ponteChamar para GET e multipart.
func ponteRequisitar(metodo, rota, tipo string, corpo io.Reader, saida interface{}) (int, error) {
	ponte.mu.Lock()
	token := ponte.cfg.Token
	base := ponte.cfg.URL
	ponte.mu.Unlock()
	if token == "" {
		return 0, fmt.Errorf("Mac não pareado com o app")
	}
	if base == "" {
		base = ponteURLPadrao
	}
	ctx, cancel := context.WithTimeout(context.Background(), ponteTimeoutHTTP)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, metodo, strings.TrimRight(base, "/")+rota, corpo)
	if err != nil {
		return 0, err
	}
	if tipo != "" {
		req.Header.Set("Content-Type", tipo)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	dados, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Erro string `json:"erro"`
		}
		_ = json.Unmarshal(dados, &e)
		if e.Erro == "" {
			e.Erro = beatitudeCurto(string(dados), 80)
		}
		return resp.StatusCode, &ponteErroHTTP{resp.StatusCode, e.Erro}
	}
	if saida != nil {
		if err := json.Unmarshal(dados, saida); err != nil {
			return resp.StatusCode, fmt.Errorf("resposta inesperada de %s: %v", rota, err)
		}
	}
	return resp.StatusCode, nil
}

// ---------------------------------------------------------------- circulo

type pontePessoa struct {
	ID       string `json:"id"`
	Telefone string `json:"telefone"`
	TemFoto  bool   `json:"tem_foto"`
}

// pontePessoasDoApp devolve a lista de Pessoas do servidor, renovada a cada
// ponteCirculoCada. Em erro, devolve a ultima lista boa (ou nada).
func pontePessoasDoApp(forcar bool) []pontePessoa {
	ponte.mu.Lock()
	fresca := !forcar && !ponteCirculo.em.IsZero() && time.Since(ponteCirculo.em) < ponteCirculoCada
	cache := ponteCirculo.pessoas
	ponte.mu.Unlock()
	if fresca {
		return cache
	}
	var r struct {
		Pessoas []pontePessoa `json:"pessoas"`
	}
	status, err := ponteRequisitar(http.MethodGet, "/api/ponte/pessoas", "", nil, &r)
	if err != nil {
		if status != http.StatusNotFound { // 404: app ainda sem a rota, fica quieto
			ponteErro(err)
		}
		ponte.mu.Lock()
		ponteCirculo.em = time.Now() // nao martela o servidor; tenta de novo no proximo ciclo
		ponte.mu.Unlock()
		return cache
	}
	tels := map[string]bool{}
	for _, p := range r.Pessoas {
		if t := strings.TrimLeft(p.Telefone, "+"); t != "" {
			tels[t] = true
		}
	}
	ponte.mu.Lock()
	ponteCirculo.em = time.Now()
	ponteCirculo.pessoas = r.Pessoas
	ponteCirculo.telefones = tels
	ponte.mu.Unlock()
	return r.Pessoas
}

// ponteTelefonesDoCirculo: o conjunto de telefones em Pessoas, do cache.
func ponteTelefonesDoCirculo() map[string]bool {
	pontePessoasDoApp(false)
	ponte.mu.Lock()
	defer ponte.mu.Unlock()
	return ponteCirculo.telefones
}

// ---------------------------------------------------------------- fotos

func ponteSincronizarFotos() {
	ponte.mu.Lock()
	if time.Since(ponteFotos.ultima) < ponteFotosCada {
		ponte.mu.Unlock()
		return
	}
	ponteFotos.ultima = time.Now()
	if ponteFotos.tentativa == nil {
		ponteFotos.tentativa = map[string]time.Time{}
	}
	ponte.mu.Unlock()

	pessoas := pontePessoasDoApp(true)
	feitas := 0
	for _, p := range pessoas {
		if p.TemFoto || p.ID == "" || p.Telefone == "" {
			continue
		}
		ponte.mu.Lock()
		ultima := ponteFotos.tentativa[p.ID]
		ponte.mu.Unlock()
		if time.Since(ultima) < ponteFotoTentarCada {
			continue
		}
		ponte.mu.Lock()
		ponteFotos.tentativa[p.ID] = time.Now()
		ponte.mu.Unlock()
		if err := ponteEnviarFoto(p); err != nil && panel.logger != nil {
			panel.logger.Infof("Beatitude: foto de uma pessoa não foi enviada (%v)", err)
		}
		feitas++
		if feitas >= ponteMaxFotosRodada {
			break
		}
	}
}

func ponteEnviarFoto(p pontePessoa) error {
	if panel.client == nil || !panel.client.IsConnected() {
		return fmt.Errorf("WhatsApp desconectado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ponteTimeoutHTTP)
	defer cancel()
	tel := strings.TrimLeft(p.Telefone, "+")
	res, err := panel.client.IsOnWhatsApp(ctx, []string{"+" + tel})
	if err != nil || len(res) == 0 || !res[0].IsIn {
		return fmt.Errorf("telefone sem WhatsApp")
	}
	info, err := panel.client.GetProfilePictureInfo(ctx, res[0].JID, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil || info == nil || info.URL == "" {
		return fmt.Errorf("sem foto visível")
	}
	img, tipo, err := ponteBaixarFoto(ctx, info.URL)
	if err != nil {
		return err
	}
	var corpo bytes.Buffer
	w := multipart.NewWriter(&corpo)
	_ = w.WriteField("person_id", p.ID)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="imagem"; filename="foto"`)
	h.Set("Content-Type", tipo)
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(img); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_, err = ponteRequisitar(http.MethodPost, "/api/ponte/foto-pessoa", w.FormDataContentType(), &corpo, nil)
	return err
}

func ponteBaixarFoto(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("foto respondeu %d", resp.StatusCode)
	}
	img, err := io.ReadAll(io.LimitReader(resp.Body, ponteFotoMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(img) > ponteFotoMaxBytes {
		return nil, "", fmt.Errorf("foto grande demais")
	}
	tipo := http.DetectContentType(img)
	switch tipo {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, "", fmt.Errorf("formato de foto inesperado (%s)", tipo)
	}
	return img, tipo, nil
}

var _ = types.JID{}
