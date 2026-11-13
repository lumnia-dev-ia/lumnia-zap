// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// beatitude_contatos.go — achar no WhatsApp deste computador o telefone de
// quem o app pede pelo nome.
//
// Contrato (app >= 01/11/2026):
//   - POST /api/ponte/contatos-buscar → { buscas: [{busca_id, nome, apelidos}] }
//     O servidor so pergunta por nomes que a propria pessoa citou: alguem de
//     Pessoas sem telefone, ou o destino de um pedido ("manda um WhatsApp
//     para o Diego").
//   - POST /api/ponte/contatos-achados ← { resultados: [{busca_id, candidatos}] }
//     Ate 3 candidatos por nome: telefone, nome como esta no WhatsApp e a data
//     da ultima conversa. Nunca texto de mensagem.
//
// Travas:
//   - so sai telefone de quem casa com um nome que o servidor perguntou;
//     a agenda inteira nunca deixa este computador.
//   - nada e gravado no app sem a pessoa tocar "E este" na tela.
//   - app antigo (404) fica em silencio.

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

const (
	ponteContatosMaxCandidatos = 3
	ponteContatosMaxResultados = 50
)

// pontePausaContatos: depois de um 404 (app sem a rota) espera antes de tentar de novo.
var pontePausaContatos time.Time

type ponteBusca struct {
	BuscaID  string   `json:"busca_id"`
	Nome     string   `json:"nome"`
	Apelidos []string `json:"apelidos"`
}

type ponteCandidato struct {
	Telefone     string  `json:"telefone"`
	NomeWhatsApp string  `json:"nome_whatsapp"`
	UltimaTroca  *string `json:"ultima_troca"`
}

type ponteResultadoBusca struct {
	BuscaID    string           `json:"busca_id"`
	Candidatos []ponteCandidato `json:"candidatos"`
}

// contatoLocal: um contato individual conhecido por este WhatsApp.
type contatoLocal struct {
	Telefone string // so digitos, com DDI
	Nomes    []string
	Ultima   time.Time
}

// ponteBuscarContatos roda a cada ciclo da ponte.
func ponteBuscarContatos() {
	if time.Now().Before(pontePausaContatos) {
		return
	}
	var r struct {
		Buscas []ponteBusca `json:"buscas"`
	}
	status, err := ponteChamar("/api/ponte/contatos-buscar", map[string]interface{}{}, &r)
	if err != nil {
		if status == http.StatusNotFound {
			pontePausaContatos = time.Now().Add(time.Hour)
			return
		}
		ponteErro(err)
		return
	}
	if len(r.Buscas) == 0 {
		return
	}
	locais := ponteContatosLocais()
	resultados := []ponteResultadoBusca{}
	for _, b := range r.Buscas {
		if b.BuscaID == "" {
			continue
		}
		termos := append([]string{b.Nome}, b.Apelidos...)
		achados := casarContatos(locais, termos, ponteContatosMaxCandidatos)
		cands := []ponteCandidato{}
		for _, c := range achados {
			cand := ponteCandidato{Telefone: c.Telefone, NomeWhatsApp: cortarUTF16(melhorNome(c, termos), 80)}
			if !c.Ultima.IsZero() {
				s := c.Ultima.UTC().Format(time.RFC3339)
				cand.UltimaTroca = &s
			}
			cands = append(cands, cand)
		}
		resultados = append(resultados, ponteResultadoBusca{BuscaID: b.BuscaID, Candidatos: cands})
		if len(resultados) >= ponteContatosMaxResultados {
			break
		}
	}
	if _, err := ponteChamar("/api/ponte/contatos-achados", map[string]interface{}{"resultados": resultados}, nil); err != nil {
		ponteErro(err)
		return
	}
	if panel.logger != nil {
		panel.logger.Infof("Beatitude: %d busca(s) de contato respondida(s)", len(resultados))
	}
}

// ponteContatosLocais junta as conversas individuais (nome e data) e a agenda
// de contatos do WhatsApp (nome completo, primeiro nome, nome de perfil).
func ponteContatosLocais() []contatoLocal {
	porTel := map[string]*contatoLocal{}
	pegar := func(tel string) *contatoLocal {
		tel = strings.TrimLeft(tel, "+")
		if tel == "" {
			return nil
		}
		c := porTel[tel]
		if c == nil {
			c = &contatoLocal{Telefone: tel}
			porTel[tel] = c
		}
		return c
	}
	proprio := ""
	if panel.client != nil && panel.client.Store != nil && panel.client.Store.ID != nil {
		proprio = panel.client.Store.ID.User
	}

	if panel.store != nil {
		rows, err := panel.store.db.Query(`
			SELECT jid, COALESCE(name,''), last_message_time FROM chats
			WHERE jid NOT LIKE '%@g.us' AND jid NOT LIKE '%@broadcast' AND jid NOT LIKE '%@newsletter'`)
		if err == nil {
			for rows.Next() {
				var jid, nome string
				var bruto interface{}
				if rows.Scan(&jid, &nome, &bruto) != nil {
					continue
				}
				c := pegar(ponteTelefoneDoChat(jid))
				if c == nil {
					continue
				}
				if nome != "" {
					c.Nomes = append(c.Nomes, nome)
				}
				if em, ok := ponteTempoSQL(bruto); ok && em.After(c.Ultima) {
					c.Ultima = em
				}
			}
			rows.Close()
		} else {
			ponteErro(err)
		}
	}

	if panel.client != nil && panel.client.Store != nil && panel.client.Store.Contacts != nil {
		ctx, cancel := context.WithTimeout(context.Background(), ponteTimeoutHTTP)
		todos, err := panel.client.Store.Contacts.GetAllContacts(ctx)
		cancel()
		if err == nil {
			for jid, info := range todos {
				tel := ""
				if jid.Server == types.DefaultUserServer {
					tel = jid.User
				} else if jid.Server == types.HiddenUserServer {
					tel = strings.TrimLeft(ponteTelefoneDoChat(jid.String()), "+")
				}
				c := pegar(tel)
				if c == nil {
					continue
				}
				for _, n := range []string{info.FullName, info.FirstName, info.PushName, info.BusinessName} {
					if strings.TrimSpace(n) != "" {
						c.Nomes = append(c.Nomes, n)
					}
				}
			}
		}
	}

	out := make([]contatoLocal, 0, len(porTel))
	for tel, c := range porTel {
		if tel == proprio || len(tel) < 10 || len(tel) > 15 || len(c.Nomes) == 0 {
			continue
		}
		out = append(out, *c)
	}
	return out
}

// ---------------------------------------------------------------- casamento (puro)

func normalizarNome(s string) string {
	s = strings.ToLower(ponteSemAcento(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// notaNome: 3 nome igual; 2 todas as palavras do termo estao no nome;
// 1 termo de uma palavra igual ao primeiro nome. 0 nao casa.
func notaNome(nome, termo string) int {
	n, t := normalizarNome(nome), normalizarNome(termo)
	if n == "" || t == "" {
		return 0
	}
	if n == t {
		return 3
	}
	palavras := strings.Fields(n)
	tem := map[string]bool{}
	for _, p := range palavras {
		tem[p] = true
	}
	todas := true
	for _, p := range strings.Fields(t) {
		if !tem[p] {
			todas = false
			break
		}
	}
	if todas && len(strings.Fields(t)) > 1 {
		return 2
	}
	if len(strings.Fields(t)) == 1 && palavras[0] == t {
		return 1
	}
	return 0
}

func notaContato(c contatoLocal, termos []string) int {
	melhor := 0
	for _, n := range c.Nomes {
		for _, t := range termos {
			if v := notaNome(n, t); v > melhor {
				melhor = v
			}
		}
	}
	return melhor
}

// casarContatos devolve ate max contatos que casam com algum termo, os de
// nota maior primeiro e, na mesma nota, os de conversa mais recente.
func casarContatos(locais []contatoLocal, termos []string, max int) []contatoLocal {
	type par struct {
		c    contatoLocal
		nota int
	}
	var ps []par
	for _, c := range locais {
		if v := notaContato(c, termos); v > 0 {
			ps = append(ps, par{c, v})
		}
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].nota != ps[j].nota {
			return ps[i].nota > ps[j].nota
		}
		if !ps[i].c.Ultima.Equal(ps[j].c.Ultima) {
			return ps[i].c.Ultima.After(ps[j].c.Ultima)
		}
		return ps[i].c.Telefone < ps[j].c.Telefone
	})
	out := []contatoLocal{}
	for _, p := range ps {
		out = append(out, p.c)
		if len(out) >= max {
			break
		}
	}
	return out
}

// cortarUTF16 corta em ate max unidades UTF-16 (o servidor mede assim; um
// emoji conta 2), com reticencias quando corta.
func cortarUTF16(s string, max int) string {
	s = strings.TrimSpace(s)
	unidades := func(r rune) int {
		if r > 0xFFFF {
			return 2
		}
		return 1
	}
	total := 0
	for _, r := range s {
		total += unidades(r)
	}
	if total <= max {
		return s
	}
	usado := 0
	for i, r := range s {
		if usado+unidades(r) > max-1 { // 1 para as reticencias
			return s[:i] + "…"
		}
		usado += unidades(r)
	}
	return s
}

// melhorNome: o nome do contato que melhor casou (para a pessoa reconhecer).
func melhorNome(c contatoLocal, termos []string) string {
	melhor, nota := "", -1
	for _, n := range c.Nomes {
		for _, t := range termos {
			if v := notaNome(n, t); v > nota || (v == nota && len(n) > len(melhor)) {
				melhor, nota = n, v
			}
		}
	}
	return strings.TrimSpace(melhor)
}
