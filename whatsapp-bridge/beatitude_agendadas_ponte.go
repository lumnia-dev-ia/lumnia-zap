package main

// Espelho das agendadas no app (17-11).
//
// A mensagem aprovada com horário pelo grupo vive só nesta ponte. Para o app
// mostrar o quadro "Programadas" e a pessoa cancelar por lá, a ponte manda ao
// servidor a lista das agendadas e recebe de volta os comandos que a pessoa
// deu no app.
//
// Contrato (docs/ponte-agendados.md no repositório do app):
//
//	POST /api/ponte/agendados
//	  { "itens": [{ref, numero, destino_nome, destino, texto, tem_anexo,
//	               enviar_em, estado, erro, atualizado_em}],
//	    "comandos_feitos": [{id, ok, erro}] }
//	→ { "comandos": [{id, ref, tipo, valor}] }
//
// "itens" é a lista COMPLETA das abertas mais as que mudaram de estado nas
// últimas 48 h. "tipo" é cancelar, agora, texto ou hora.
//
// O texto sai desta máquina para o servidor do app: decisão do Diego em
// 17-11, para o quadro mostrar o que vai sair. Quem usa o Zap sem o
// Beatitude pareado não manda nada.
//
// Servidor que ainda não conhece a rota (404): a ponte fica 30 minutos sem
// tentar e segue normal; o grupo continua funcionando sozinho.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ponteAgendadaItem struct {
	Ref          string  `json:"ref"`
	Numero       int     `json:"numero"`
	DestinoNome  string  `json:"destino_nome"`
	Destino      string  `json:"destino,omitempty"`
	Texto        string  `json:"texto"`
	TemAnexo     bool    `json:"tem_anexo"`
	EnviarEm     string  `json:"enviar_em"`
	Estado       string  `json:"estado"` // agendado | enviado | cancelado | falhou
	Erro         *string `json:"erro"`
	AtualizadoEm string  `json:"atualizado_em"`
}

type ponteComandoFeito struct {
	ID   string  `json:"id"`
	OK   bool    `json:"ok"`
	Erro *string `json:"erro"`
}

type ponteComandoAgendada struct {
	ID    string `json:"id"`
	Ref   string `json:"ref"`
	Tipo  string `json:"tipo"`
	Valor string `json:"valor"`
}

var ponteAgend struct {
	mu         sync.Mutex
	semRotaAte time.Time
	feitos     []ponteComandoFeito
	vistos     map[string]time.Time // comandos já aplicados, para não repetir se o servidor reenviar
}

const (
	ponteAgendJanela  = 48 * time.Hour
	ponteAgendSemRota = 30 * time.Minute
	ponteAgendTextoMx = 4000
)

// ponteAgendadasMudaram: algo mudou numa agendada; adianta a próxima rodada.
func ponteAgendadasMudaram() {
	if ponteAtiva() {
		ponteAcordar()
	}
}

// ponteItensAgendadas monta a lista que vai ao app.
func ponteItensAgendadas(agora time.Time) []ponteAgendadaItem {
	if panel.store == nil {
		return nil
	}
	rows, err := panel.store.db.Query(`SELECT id, COALESCE(falha,''), criado_em, mudou_em FROM beatitude_rascunhos
		WHERE agendamento_id IS NOT NULL AND (
			(enviado_em IS NULL AND cancelado_em IS NULL AND enviar_em >= ?)
			OR enviado_em >= ? OR cancelado_em >= ?)
		ORDER BY enviar_em, id`, agora.Add(-time.Hour), agora.Add(-ponteAgendJanela), agora.Add(-ponteAgendJanela))
	if err != nil {
		return nil
	}
	type base struct {
		id     int64
		falha  string
		criado interface{}
		mudou  interface{}
	}
	var bases []base
	for rows.Next() {
		var b base
		if rows.Scan(&b.id, &b.falha, &b.criado, &b.mudou) == nil {
			bases = append(bases, b)
		}
	}
	rows.Close()
	out := []ponteAgendadaItem{}
	for _, b := range bases {
		r, err := beatitudeLerRascunho(`id = ?`, b.id)
		if err != nil || r.enviarEm == nil {
			continue
		}
		nome, _ := beatitudeNomeDoChat(r.jid)
		it := ponteAgendadaItem{
			Ref:         "r" + strconv.FormatInt(r.id, 10),
			Numero:      r.n,
			DestinoNome: nome,
			Destino:     r.jid,
			Texto:       r.texto,
			TemAnexo:    r.anexoMsgID != "",
			EnviarEm:    r.enviarEm.UTC().Format(time.RFC3339),
			Estado:      "agendado",
		}
		quando := time.Time{}
		if t, ok := ponteTempoSQL(b.mudou); ok {
			quando = t
		} else if t, ok := ponteTempoSQL(b.criado); ok {
			quando = t
		}
		switch {
		case r.enviado != nil:
			it.Estado = "enviado"
			if r.enviado.After(quando) {
				quando = *r.enviado
			}
		case b.falha != "":
			it.Estado = "falhou"
			f := b.falha
			it.Erro = &f
		case r.cancelado != nil:
			it.Estado = "cancelado"
		}
		if r.cancelado != nil && r.cancelado.After(quando) {
			quando = *r.cancelado
		}
		if quando.IsZero() {
			quando = agora
		}
		it.AtualizadoEm = quando.UTC().Format(time.RFC3339)
		out = append(out, it)
	}
	return out
}

// ponteSincronizarAgendadas roda a cada ciclo da ponte.
func ponteSincronizarAgendadas() {
	ponteSincronizarAgendadasN(0)
}

func ponteSincronizarAgendadasN(volta int) {
	agora := time.Now()
	ponteAgend.mu.Lock()
	if agora.Before(ponteAgend.semRotaAte) {
		ponteAgend.mu.Unlock()
		return
	}
	feitos := ponteAgend.feitos
	ponteAgend.mu.Unlock()

	itens := ponteItensAgendadas(agora)
	if len(itens) == 0 && len(feitos) == 0 {
		return // nada agendado nem a confirmar: não há o que o app possa pedir
	}
	corpo := map[string]interface{}{"itens": itens, "comandos_feitos": feitos}
	if feitos == nil {
		corpo["comandos_feitos"] = []ponteComandoFeito{}
	}
	var resp struct {
		Comandos []ponteComandoAgendada `json:"comandos"`
	}
	status, err := ponteChamar("/api/ponte/agendados", corpo, &resp)
	if err != nil {
		if status == 404 || status == 405 {
			ponteAgend.mu.Lock()
			ponteAgend.semRotaAte = agora.Add(ponteAgendSemRota)
			ponteAgend.mu.Unlock()
			return
		}
		ponteErro(err)
		return
	}
	ponteAgend.mu.Lock()
	ponteAgend.feitos = ponteAgend.feitos[len(feitos):] // o servidor recebeu estes
	ponteAgend.mu.Unlock()

	if len(resp.Comandos) == 0 {
		return
	}
	linhas := ponteAplicarComandosAgendadas(resp.Comandos, agora)
	if len(linhas) > 0 {
		beatitudeSend(beatitudeMarca + "\n\nPedido feito no app:\n" + strings.Join(linhas, "\n"))
	}
	if volta == 0 { // confirma já, em vez de esperar o próximo ciclo
		ponteSincronizarAgendadasN(1)
	}
}

// ponteAplicarComandosAgendadas aplica o que a pessoa pediu no quadro do app
// e guarda a confirmação de cada comando. Devolve as linhas do aviso ao grupo.
func ponteAplicarComandosAgendadas(cmds []ponteComandoAgendada, agora time.Time) []string {
	var linhas []string
	for _, c := range cmds {
		ponteAgend.mu.Lock()
		if ponteAgend.vistos == nil {
			ponteAgend.vistos = map[string]time.Time{}
		}
		_, repetido := ponteAgend.vistos[c.ID]
		ponteAgend.vistos[c.ID] = agora
		for id, t := range ponteAgend.vistos {
			if agora.Sub(t) > 24*time.Hour {
				delete(ponteAgend.vistos, id)
			}
		}
		ponteAgend.mu.Unlock()
		if c.ID == "" {
			continue
		}
		if repetido {
			ponteGuardarFeito(c.ID, true, "")
			continue
		}
		linha, err := ponteAplicarComandoAgendada(c, agora)
		if err != nil {
			ponteGuardarFeito(c.ID, false, err.Error())
			linhas = append(linhas, "- não consegui ("+c.Tipo+"): "+err.Error())
			continue
		}
		ponteGuardarFeito(c.ID, true, "")
		linhas = append(linhas, "- "+linha)
	}
	return linhas
}

func ponteGuardarFeito(id string, ok bool, erro string) {
	f := ponteComandoFeito{ID: id, OK: ok}
	if erro != "" {
		e := beatitudeCurto(erro, 200)
		f.Erro = &e
	}
	ponteAgend.mu.Lock()
	ponteAgend.feitos = append(ponteAgend.feitos, f)
	ponteAgend.mu.Unlock()
}

func ponteAplicarComandoAgendada(c ponteComandoAgendada, agora time.Time) (string, error) {
	if !strings.HasPrefix(c.Ref, "r") {
		return "", fmt.Errorf("referência desconhecida")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(c.Ref, "r"), 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("referência desconhecida")
	}
	r, err := beatitudeLerRascunho(`id = ? AND agendamento_id IS NOT NULL`, id)
	if err != nil {
		return "", fmt.Errorf("essa mensagem não está mais neste computador")
	}
	nome, _ := beatitudeNomeDoChat(r.jid)
	rot := fmt.Sprintf("%d · %s", r.n, nome)
	if c.Tipo == "cancelar" { // cancelar o que já foi cancelado é sucesso
		if r.enviado != nil {
			return "", fmt.Errorf("já tinha sido enviada")
		}
		if r.cancelado != nil {
			return rot + ": já estava cancelado", nil
		}
		return beatitudeCancelarRascunho(r, agora, "cancela (app)"), nil
	}
	if r.enviado != nil {
		return "", fmt.Errorf("já tinha sido enviada")
	}
	if r.cancelado != nil {
		return "", fmt.Errorf("já tinha sido cancelada")
	}
	switch c.Tipo {
	case "agora":
		ls := beatitudeAplicar([]beatitudeAlvo{{id: r.id}}, 0, true, false, agora)
		if len(ls) == 0 {
			return "", fmt.Errorf("nada foi enviado")
		}
		if depois, e := beatitudeLerRascunho(`id = ?`, r.id); e != nil || depois.enviado == nil {
			return "", fmt.Errorf("%s", beatitudeCurto(strings.Join(ls, " "), 180))
		}
		return ls[0], nil
	case "texto":
		novo := strings.TrimSpace(c.Valor)
		if novo == "" || len([]rune(novo)) > ponteAgendTextoMx {
			return "", fmt.Errorf("texto vazio ou longo demais")
		}
		_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET message = ? WHERE id = ? AND enabled = 1`, novo, *r.agendamento)
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET texto = ?, mudou_em = ? WHERE id = ?`, novo, agora, r.id)
		beatitudeRegistrar(agora, "texto (app)", "texto trocado para "+nome, "agendado", "")
		return rot + ": texto trocado, continua para " + beatitudeQuando(*r.enviarEm, agora), nil
	case "hora":
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(c.Valor))
		if err != nil {
			return "", fmt.Errorf("horário inválido")
		}
		t = t.In(panelLoc).Truncate(time.Minute)
		if !t.After(agora.Add(time.Minute)) {
			return "", fmt.Errorf("o horário já passou")
		}
		if t.After(agora.Add(beatitudeHorizonteAgenda)) {
			return "", fmt.Errorf("só agendo até 7 dias à frente")
		}
		_, _ = panel.store.db.Exec(`UPDATE panel_schedules SET at_time = ?, on_date = ?, last_key = NULL WHERE id = ? AND enabled = 1`,
			t.Format("15:04"), t.Format("2006-01-02"), *r.agendamento)
		_, _ = panel.store.db.Exec(`UPDATE beatitude_rascunhos SET enviar_em = ?, mudou_em = ? WHERE id = ?`, t, agora, r.id)
		beatitudeRegistrar(agora, "hora (app)", "horário de "+nome+" passou para "+t.Format("02/01 15:04"), "agendado", "")
		return rot + ": horário trocado, sai " + beatitudeQuando(t, agora), nil
	}
	return "", fmt.Errorf("comando desconhecido: %s", beatitudeCurto(c.Tipo, 20))
}
