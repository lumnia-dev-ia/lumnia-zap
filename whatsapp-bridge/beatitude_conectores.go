// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

package main

// Catálogo dos conectores da conta Claude que o mordomo pode alcançar (1.1.1).
//
// Antes, cada conector era uma lista solta e uma linha fixa no prompt, para
// todo mundo. Agora cada um é uma entrada aqui: o que fica liberado, o que é
// negado por garantia e a regra que entra no prompt. Conector novo = uma
// entrada + release.
//
// Por máquina, beatitudeConfig.Conectores diz quais valem (marcados no painel).
// Vazio (nil) = todos, para quem já usava não perder nada na atualização. Só os
// marcados entram nas listas e no prompt.
//
// Liberar não dá acesso a ninguém: cada um só funciona para quem tem aquele
// conector ligado na própria conta Claude (claude.ai → Configurações →
// Conectores).

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type beatitudeConector struct {
	ID         string   // chave estável, gravada no beatitude.json
	Nome       string   // como aparece no painel e no grupo
	Servidores []string // nome do servidor no Claude Code: mcp__claude_ai_<Servidor>__ferramenta
	Liberadas  []string
	Negadas    []string
	Regra      string // linha do prompt do mordomo
}

var beatitudeConectores = []beatitudeConector{
	{
		ID:         "google",
		Nome:       "Google (Agenda, Gmail, Drive)",
		Servidores: []string{"Google_Calendar", "Gmail", "Google_Drive"},
		// Agenda: ler e criar/ajustar eventos dela (degrau 1, reversivel).
		// Gmail: ler e deixar RASCUNHO. Drive: so leitura. As perigosas ficam
		// nas negadas, que vencem as liberadas.
		Liberadas: []string{
			"mcp__claude_ai_Google_Calendar",
			"mcp__claude_ai_Gmail",
			"mcp__claude_ai_Google_Drive",
		},
		Negadas: []string{
			// Gmail: nada sai, nada some
			"mcp__claude_ai_Gmail__send_message", "mcp__claude_ai_Gmail__reply", "mcp__claude_ai_Gmail__forward",
			"mcp__claude_ai_Gmail__trash_message", "mcp__claude_ai_Gmail__trash_thread",
			"mcp__claude_ai_Gmail__delete_draft", "mcp__claude_ai_Gmail__delete_label",
			"mcp__claude_ai_Gmail__mark_message_spam", "mcp__claude_ai_Gmail__mark_thread_spam",
			// Agenda: nao apaga e nao responde convite em nome da pessoa
			"mcp__claude_ai_Google_Calendar__delete_event", "mcp__claude_ai_Google_Calendar__respond_to_event",
			// Drive: so leitura
			"mcp__claude_ai_Google_Drive__share_file", "mcp__claude_ai_Google_Drive__trash_file",
			"mcp__claude_ai_Google_Drive__update_file", "mcp__claude_ai_Google_Drive__create_file",
			"mcp__claude_ai_Google_Drive__copy_file",
		},
		Regra: "Agenda Google: você pode ler, criar e ajustar eventos da pessoa, SEMPRE sem convidados: convidar alguém é mandar algo a outra pessoa e precisa do sim dela. Evento \"com\" alguém (\"almoço com a Lisiane sábado 14h\") sai só na agenda dela; se ela quiser convidar, crie uma ação com criar_acao do Beatitude (canal convite, destino a pessoa) e diga em uma linha que o convite sai quando ela responder sim com o número que o Beatitude der. Confira conflito antes; havendo, crie mesmo assim só se ela tiver sido explícita, e avise do conflito. Duração padrão: almoço e jantar 1h30, reunião 1h, café 30 min. Não apague eventos.\n" +
			"Gmail: você pode ler e criar RASCUNHOS (nunca enviar). Quando ela pedir um e-mail, deixe o rascunho no Gmail e diga para quem ficou e o assunto.\n" +
			"Drive: só leitura, para achar e resumir arquivos.",
	},
	{
		ID:         "microsoft",
		Nome:       "Microsoft 365 (Outlook)",
		Servidores: []string{"Microsoft_365"},
		// Lista fechada, ferramenta por ferramenta: enviar, encaminhar, apagar,
		// responder convite, regras e arquivos ficam de fora.
		Liberadas: []string{
			"mcp__claude_ai_Microsoft_365__outlook_email_search",
			"mcp__claude_ai_Microsoft_365__outlook_calendar_search",
			"mcp__claude_ai_Microsoft_365__outlook_find_available_time",
			"mcp__claude_ai_Microsoft_365__find_meeting_availability",
			"mcp__claude_ai_Microsoft_365__read_resource",
			"mcp__claude_ai_Microsoft_365__get_me",
			"mcp__claude_ai_Microsoft_365__search_people",
			"mcp__claude_ai_Microsoft_365__chat_message_search",
			"mcp__claude_ai_Microsoft_365__outlook_create_draft",
			"mcp__claude_ai_Microsoft_365__outlook_create_reply_draft",
			"mcp__claude_ai_Microsoft_365__outlook_create_event",
			"mcp__claude_ai_Microsoft_365__outlook_update_event",
		},
		Negadas: []string{ // redundante com a lista fechada, por garantia
			"mcp__claude_ai_Microsoft_365__outlook_send_mail", "mcp__claude_ai_Microsoft_365__outlook_send_draft",
			"mcp__claude_ai_Microsoft_365__outlook_forward_mail", "mcp__claude_ai_Microsoft_365__outlook_create_reply_all_draft",
			"mcp__claude_ai_Microsoft_365__outlook_delete_event", "mcp__claude_ai_Microsoft_365__outlook_respond_to_event",
			"mcp__claude_ai_Microsoft_365__outlook_trash_thread", "mcp__claude_ai_Microsoft_365__outlook_batch_delete_messages",
			"mcp__claude_ai_Microsoft_365__outlook_delete_draft", "mcp__claude_ai_Microsoft_365__outlook_set_vacation",
			"mcp__claude_ai_Microsoft_365__outlook_create_filter", "mcp__claude_ai_Microsoft_365__sharepoint_delete_item",
		},
		Regra: "Microsoft (quem usa Outlook): as mesmas regras do Google pelo conector Microsoft 365. Ler e-mail e agenda; e-mail só como RASCUNHO (outlook_create_draft ou outlook_create_reply_draft), nunca enviar; evento só na agenda dela e sem convidados (outlook_create_event, outlook_update_event). Use o Google ou o Microsoft conforme o que estiver conectado; se os dois estiverem, a agenda e o e-mail de trabalho costumam ser os do Microsoft.",
	},
	{
		ID:   "timao",
		Nome: "Timão",
		// "Timão" no claude.ai vira Tim_o no Claude Code (o "ã" vira "_").
		Servidores: []string{"Tim_o"},
		// 13-11: ler fragatas, missões e CRM; criar e mover missões e leads,
		// registrar atividade e diário. O que chega a outra pessoa e o que
		// apaga fica de fora.
		Liberadas: []string{
			"mcp__claude_ai_Tim_o__list_boards",
			"mcp__claude_ai_Tim_o__portfolio_overview",
			"mcp__claude_ai_Tim_o__project_status",
			"mcp__claude_ai_Tim_o__list_my_tasks",
			"mcp__claude_ai_Tim_o__weekly_agenda",
			"mcp__claude_ai_Tim_o__pending_by_user",
			"mcp__claude_ai_Tim_o__create_task",
			"mcp__claude_ai_Tim_o__move_task",
			"mcp__claude_ai_Tim_o__complete_task",
			"mcp__claude_ai_Tim_o__append_board_log",
			"mcp__claude_ai_Tim_o__create_meeting_summary",
			"mcp__claude_ai_Tim_o__list_channels",
			"mcp__claude_ai_Tim_o__list_dm_peers",
			"mcp__claude_ai_Tim_o__read_channel",
			"mcp__claude_ai_Tim_o__search_messages",
			"mcp__claude_ai_Tim_o__crm_pipeline_summary",
			"mcp__claude_ai_Tim_o__crm_list_leads",
			"mcp__claude_ai_Tim_o__crm_lead_detail",
			"mcp__claude_ai_Tim_o__crm_list_proposals",
			"mcp__claude_ai_Tim_o__crm_proposal_detail",
			"mcp__claude_ai_Tim_o__crm_create_lead",
			"mcp__claude_ai_Tim_o__crm_update_lead",
			"mcp__claude_ai_Tim_o__crm_move_lead_stage",
			"mcp__claude_ai_Tim_o__crm_create_activity",
		},
		Negadas: []string{ // o que fala com terceiros, apaga ou reconfigura o funil
			"mcp__claude_ai_Tim_o__send_message", "mcp__claude_ai_Tim_o__reply_in_thread",
			"mcp__claude_ai_Tim_o__start_dm", "mcp__claude_ai_Tim_o__delete_message",
			"mcp__claude_ai_Tim_o__delete_task", "mcp__claude_ai_Tim_o__crm_delete_lead",
			"mcp__claude_ai_Tim_o__crm_delete_activity", "mcp__claude_ai_Tim_o__crm_create_pipeline",
			"mcp__claude_ai_Tim_o__crm_create_stage",
		},
		Regra: "Timão (quem tem o conector ligado no Claude): você pode ler fragatas, missões, canais e o CRM, criar, mover e concluir missões, registrar no diário de bordo, criar e atualizar leads, mover lead de fase e registrar atividade. Pedido claro é para fazer: ache a fragata ou o lead pelo nome, faça e conte em uma linha o que fez (quantas missões, em qual fragata). Mandar mensagem, abrir DM ou responder em canal do Timão chega a outra pessoa: não faça; diga em uma linha que isso ela manda pelo próprio Timão. Não apague nada no Timão.",
	},

	{
		ID:   "lovable",
		Nome: "Lovable (só leitura)",
		// 14-11: o mordomo nao sabia responder "qual transcritor o app usa"
		// porque nao alcancava o codigo. So leitura: ler projetos, arquivos,
		// buscar no codigo e ver o historico. Nada que publique, edite, gaste
		// credito ou mexa no banco (query_database tambem escreve).
		Servidores: []string{"Lovable"},
		Liberadas: []string{
			"mcp__claude_ai_Lovable__list_projects",
			"mcp__claude_ai_Lovable__get_project",
			"mcp__claude_ai_Lovable__list_files",
			"mcp__claude_ai_Lovable__read_file",
			"mcp__claude_ai_Lovable__grep_files",
			"mcp__claude_ai_Lovable__get_diff",
			"mcp__claude_ai_Lovable__list_edits",
			"mcp__claude_ai_Lovable__get_project_knowledge",
		},
		Negadas: []string{
			"mcp__claude_ai_Lovable__send_message", "mcp__claude_ai_Lovable__deploy_project",
			"mcp__claude_ai_Lovable__create_project", "mcp__claude_ai_Lovable__initiate_project",
			"mcp__claude_ai_Lovable__remix_project", "mcp__claude_ai_Lovable__query_database",
			"mcp__claude_ai_Lovable__enable_database", "mcp__claude_ai_Lovable__add_connector",
			"mcp__claude_ai_Lovable__set_project_knowledge", "mcp__claude_ai_Lovable__set_workspace_knowledge",
			"mcp__claude_ai_Lovable__respond_to_approval", "mcp__claude_ai_Lovable__set_project_visibility",
		},
		Regra: "Lovable (quem tem o conector ligado no Claude): só leitura. Você pode listar os projetos, ler arquivos, buscar no código (grep_files) e ver o histórico de alterações para responder como um app funciona. Nunca peça ao agente do Lovable para mudar nada, nunca publique e não consulte o banco; se a pessoa quiser mudar algo, passe o bastão.",
	},
	{
		ID:   "readai",
		Nome: "Read AI (só leitura)",
		// Reunioes gravadas: listar, ler resumo e transcricao. Compartilhar e
		// reorganizar pastas fica de fora.
		Servidores: []string{"Read_AI"},
		Liberadas: []string{
			"mcp__claude_ai_Read_AI__list_meetings",
			"mcp__claude_ai_Read_AI__get_meeting_by_id",
			"mcp__claude_ai_Read_AI__search_meeting_agent",
			"mcp__claude_ai_Read_AI__list_folders",
			"mcp__claude_ai_Read_AI__get_folder",
			"mcp__claude_ai_Read_AI__list_folder_items",
		},
		Negadas: []string{
			"mcp__claude_ai_Read_AI__share_meeting_report", "mcp__claude_ai_Read_AI__share_folder",
			"mcp__claude_ai_Read_AI__create_folder", "mcp__claude_ai_Read_AI__update_folder",
			"mcp__claude_ai_Read_AI__delete_folder", "mcp__claude_ai_Read_AI__add_folder_items",
			"mcp__claude_ai_Read_AI__remove_folder_items", "mcp__claude_ai_Read_AI__create_meeting_agent",
		},
		Regra: "Read AI (quem tem o conector ligado no Claude): só leitura das reuniões dela. Ache a reunião pelo título ou pela data e resuma o que foi pedido (decisões, pendências, quem ficou de quê). Não cite trechos longos da transcrição e não compartilhe nada.",
	},
}

// Base do mordomo: o que vale com ou sem conector.
var beatitudeFerramentasBase = []string{
	"mcp__lumnia-zap__list_chats",
	"mcp__lumnia-zap__list_messages",
	"mcp__lumnia-zap__get_chat",
	"mcp__lumnia-zap__get_direct_chat_by_contact",
	"mcp__lumnia-zap__get_contact_chats",
	"mcp__lumnia-zap__get_last_interaction",
	"mcp__lumnia-zap__get_message_context",
	"mcp__lumnia-zap__search_contacts",
	// Agenda da Apple (agenda_apple.go): o MCP so registra estas quando a
	// pessoa configurou a senha de app; liberar aqui nao cria nada sozinho.
	"mcp__lumnia-zap__agenda_listar",
	"mcp__lumnia-zap__agenda_buscar",
	"mcp__lumnia-zap__agenda_livre",
	"mcp__lumnia-zap__agenda_criar",
	"mcp__beatitude",           // registrado com `claude mcp add ... beatitude`
	"mcp__claude_ai_Beatitude", // ou o conector do claude.ai, quando sincronizado
	"WebSearch",                // pesquisa simples (horario, endereco, telefone)
}

var beatitudeNegadasBase = []string{
	"mcp__lumnia-zap__send_message",
	"mcp__lumnia-zap__send_file",
	"mcp__lumnia-zap__send_audio_message",
	"mcp__lumnia-zap__download_media",
	"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "Read", "Glob", "Grep",
	"WebFetch", "Task", "Agent",
	// o bastão volta pela conversa do Claude dela, nunca pelo mordomo (1.1.2)
	"mcp__beatitude__devolver_ao_grupo", "mcp__claude_ai_Beatitude__devolver_ao_grupo",
}

// beatitudeFerramentasLiberadas / Negadas: as listas com TODOS os conectores
// do catálogo. Servem ao teste do modo rápido e de teto para os outros passos
// (navegador, preparo do app), que usam as negadas como trava extra. O
// mordomo usa beatitudeListas, que respeita os conectores da máquina.
var beatitudeFerramentasLiberadas, beatitudeFerramentasNegadas = beatitudeMontarListas(nil)

// beatitudeConectoresAtivos: os do catálogo que valem nesta máquina, na ordem
// do catálogo. ids nil = todos; lista vazia (não nil) = nenhum.
func beatitudeConectoresAtivos(ids []string) []beatitudeConector {
	if ids == nil {
		return beatitudeConectores
	}
	quer := map[string]bool{}
	for _, id := range ids {
		quer[id] = true
	}
	var out []beatitudeConector
	for _, c := range beatitudeConectores {
		if quer[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func beatitudeMontarListas(ids []string) (liberadas, negadas []string) {
	liberadas = append(liberadas, beatitudeFerramentasBase...)
	negadas = append(negadas, beatitudeNegadasBase...)
	for _, c := range beatitudeConectoresAtivos(ids) {
		liberadas = append(liberadas, c.Liberadas...)
		negadas = append(negadas, c.Negadas...)
	}
	return liberadas, negadas
}

// beatitudeRegrasConectores: as linhas do prompt dos conectores ativos.
func beatitudeRegrasConectores(ids []string) []string {
	var out []string
	for _, c := range beatitudeConectoresAtivos(ids) {
		out = append(out, strings.Split(beatitudeRegraDoConector(c), "\n")...)
	}
	return out
}

// beatitudeOQueAlcanco: a linha do prompt que diz ao mordomo o que ele
// alcança NESTA máquina, gerada do catálogo ativo. Sem ela, em 14-11 ele
// respondeu "qual transcritor o Beatitude usa?" com "olhe a configuração da
// ponte": não sabia o próprio motor de voz nem o que estava fora do alcance.
func beatitudeOQueAlcanco(ids []string) string {
	var nomes []string
	for _, c := range beatitudeConectoresAtivos(ids) {
		nomes = append(nomes, c.Nome)
	}
	conectores := "nenhum conector extra"
	if len(nomes) > 0 {
		conectores = strings.Join(nomes, ", ")
	}
	return "O que você alcança neste " + maquina() + " (use isto, sem inventar, quando ela perguntar o que você consegue fazer, qual ferramenta ou modelo você usa, ou por que algo não deu): " +
		"o WhatsApp dela (ler conversas e contatos; mensagem a alguém só como rascunho que ela aprova); o Beatitude; pesquisa simples na web; " +
		"os conectores " + conectores + " (cada um só funciona se estiver ligado na conta Claude dela); " +
		"o Chrome deste " + maquina() + " em tarefa que ela aprova; imagens e PDFs do grupo, lidos neste " + maquina() + "; " +
		"os áudios do grupo, transcritos neste " + maquina() + " pelo Whisper (" + beatitudeVozRotulo("") + "), sem sair da máquina. " +
		"Você NÃO alcança: os projetos do claude.ai, os arquivos deste " + maquina() + ", o código dos apps (fora o que o Lovable mostra) nem conectores fora desta lista. Para isso existe o bastão (regra abaixo). " +
		"Você é o Claude da Anthropic rodando pelo Claude Code; o modelo de cada resposta é o que a ponte escolheu (o padrão, ou o mais forte quando ela pede \"pensa melhor\")."
}

// beatitudeIDsValidos filtra o que veio do painel: só ids do catálogo, sem
// repetir, na ordem do catálogo. Nunca devolve nil (nil quer dizer "todos").
func beatitudeIDsValidos(ids []string) []string {
	quer := map[string]bool{}
	for _, id := range ids {
		quer[strings.TrimSpace(id)] = true
	}
	out := []string{}
	for _, c := range beatitudeConectores {
		if quer[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out
}

// beatitudeConectoresPainel: o catálogo com a marca de cada um nesta máquina.
func beatitudeConectoresPainel(ids []string) []map[string]interface{} {
	ativo := map[string]bool{}
	for _, c := range beatitudeConectoresAtivos(ids) {
		ativo[c.ID] = true
	}
	var out []map[string]interface{}
	for _, c := range beatitudeConectores {
		out = append(out, map[string]interface{}{"id": c.ID, "nome": c.Nome, "ligado": ativo[c.ID]})
	}
	return out
}

// ---------------------------------------------------------------- negações (13-11, parte 3)

// beatitudeServidorDaFerramenta: "mcp__claude_ai_Tim_o__list_boards" → "Tim_o".
func beatitudeServidorDaFerramenta(ferramenta string) (string, bool) {
	const pre = "mcp__claude_ai_"
	if !strings.HasPrefix(ferramenta, pre) {
		return "", false
	}
	resto := strings.TrimPrefix(ferramenta, pre)
	if i := strings.Index(resto, "__"); i > 0 {
		return resto[:i], true
	}
	return resto, resto != ""
}

// beatitudeNomeDoServidor: o nome do catálogo, ou o próprio servidor com
// espaços ("Tim_o" desconhecido vira "Tim o"; melhor que nada).
func beatitudeNomeDoServidor(servidor string) string {
	for _, c := range beatitudeConectores {
		for _, s := range c.Servidores {
			if s == servidor {
				return c.Nome
			}
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(servidor, "_", " "))
}

// beatitudeConectoresNegados: dos nomes que o `claude -p` negou, os que são de
// conector da conta e NÃO estão na lista de negadas de propósito (enviar,
// apagar: essas o próprio prompt explica). Devolve nomes amigáveis, sem
// repetir, em ordem. O Beatitude nunca entra: ele é base.
func beatitudeConectoresNegados(negadas, negadasDeProposito []string) []string {
	proposito := map[string]bool{}
	for _, n := range negadasDeProposito {
		proposito[n] = true
	}
	vistos := map[string]bool{}
	var out []string
	for _, f := range negadas {
		if proposito[f] {
			continue
		}
		srv, ok := beatitudeServidorDaFerramenta(f)
		if !ok || srv == "Beatitude" {
			continue
		}
		nome := beatitudeNomeDoServidor(srv)
		if !vistos[nome] {
			vistos[nome] = true
			out = append(out, nome)
		}
	}
	sort.Strings(out)
	return out
}

var beatitudeAlertas struct {
	mu sync.Mutex
	em map[string]time.Time
}

// beatitudeAvisarNegacao: uma linha fixa no grupo e um aviso ao suporte pelo
// servidor do Beatitude (o mesmo /api/ponte/alerta do verificador da nuvem),
// no máximo um por conector a cada 24 h. A pessoa não ganha tarefa técnica.
func beatitudeAvisarNegacao(nomes []string, ferramentas []string) {
	if len(nomes) == 0 {
		return
	}
	beatitudeAlertas.mu.Lock()
	if beatitudeAlertas.em == nil {
		beatitudeAlertas.em = map[string]time.Time{}
	}
	var novos []string
	for _, n := range nomes {
		if time.Since(beatitudeAlertas.em[n]) > 24*time.Hour {
			novos = append(novos, n)
		}
	}
	beatitudeAlertas.mu.Unlock()

	avisado := false
	if len(novos) > 0 && pontePareado() {
		host, _ := os.Hostname()
		var itens []map[string]string
		for _, n := range novos {
			itens = append(itens, map[string]string{
				"peca":   beatitudeCurto("Conector "+n, 40),
				"estado": "negado ao mordomo",
				"fazer": beatitudeCurto("Ferramentas negadas: "+strings.Join(ferramentas, ", ")+
					". Ligar o conector no painel do Zap (Beatitude → Conectores) ou incluir no catálogo (beatitude_conectores.go).", 300),
			})
		}
		corpo := map[string]interface{}{
			"maquina": fmt.Sprintf("Mordomo no %s (%s)", maquina(), host),
			"versao":  versao,
			"itens":   itens,
		}
		if st, err := ponteChamar("/api/ponte/alerta", corpo, nil); err == nil && st < 300 {
			avisado = true
			beatitudeAlertas.mu.Lock()
			for _, n := range novos {
				beatitudeAlertas.em[n] = time.Now()
			}
			beatitudeAlertas.mu.Unlock()
		} else if panel.logger != nil {
			panel.logger.Warnf("Beatitude: aviso de conector negado não saiu (%d, %v)", st, err)
		}
	}
	linha := strings.Join(nomes, " e ") + " não está liberado para o Beatitude neste " + maquina() + "."
	if len(nomes) > 1 {
		linha = strings.Join(nomes, " e ") + " não estão liberados para o Beatitude neste " + maquina() + "."
	}
	switch {
	case avisado:
		linha += " Já avisei o suporte."
	case len(novos) == 0:
		linha += " O suporte já foi avisado."
	default:
		linha += " Avise quem instalou o Beatitude para você."
	}
	beatitudeSend(beatitudeMarca + ": " + linha)
}
