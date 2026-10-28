#!/usr/bin/env bash
# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
#
# rotina-manha.sh — a rodada da manhã do Beatitude no computador da nuvem.
#
# Roda como o usuário `beatitude`, pelo timer lumnia-manha.timer (todo dia, 6h45).
# Substitui as tarefas agendadas que dependiam do Mac: aqui o Claude da pessoa
# tem ao mesmo tempo o WhatsApp (MCP do Zap, local), a agenda e o e-mail
# (conectores da conta Claude dela) e o MCP do Beatitude.
#
# Ordem dos roteiros oficiais: pessoas -> brief_diario -> acoes;
# aos domingos, depois, retrato_semanal e espelho_semanal.
#
# Só leitura no WhatsApp e no e-mail: as ferramentas de envio ficam negadas.
# Quem envia qualquer coisa a terceiros é a ponte, depois do "sim N".

set -uo pipefail

CASA=${HOME:-/home/beatitude}
ZAP=$CASA/lumnia-zap
DIR=$CASA/rotinas
LOGS=$DIR/logs
mkdir -p "$LOGS"
export PATH="$CASA/.local/bin:/usr/local/bin:/usr/bin:/bin"
export DISPLAY=${DISPLAY:-:1}

UV=$(command -v uv || echo "$CASA/.local/bin/uv")
cat > "$DIR/mcp.json" <<JSON
{
  "mcpServers": {
    "lumnia-zap": {
      "command": "$UV",
      "args": ["--directory", "$ZAP/whatsapp-mcp-server", "run", "main.py"]
    }
  }
}
JSON

HOJE=$(date +%F)
DOMINGO=$([ "$(date +%u)" = 7 ] && echo sim || echo nao)
LOG=$LOGS/manha-$HOJE.json

EXTRA_DOMINGO=""
if [ "$DOMINGO" = sim ]; then
  EXTRA_DOMINGO="
4. Hoje é domingo. Depois das ações, chame ler_roteiro('retrato_semanal') e siga; depois ler_roteiro('espelho_semanal') e siga."
fi

PEDIDO="Rodada da manhã do Beatitude, $(date '+%A, %d/%m/%Y') (horário de Brasília). Você roda sozinha, sem ninguém olhando; não faça perguntas.
Faça, nesta ordem, seguindo cada roteiro à risca (os roteiros são o produto, não sugestão):
1. Chame ler_roteiro('pessoas') no MCP beatitude e siga. As mensagens do WhatsApp vêm do MCP lumnia-zap (só leitura).
2. Chame ler_roteiro('brief_diario') e siga: leia a agenda e o e-mail pelos conectores da conta (Microsoft 365 e/ou Google, os que existirem) e as mensagens pelo lumnia-zap; entregue o brief inteiro com gravar_brief_completo.
3. Chame ler_roteiro('acoes') e siga: prepare o que ela delegou e entregue com entregar_acao. Você nunca aprova e nunca envia.${EXTRA_DOMINGO}
Ao terminar, responda em uma linha por etapa: feito, ou o que faltou e por quê."

LIBERADAS="mcp__beatitude,mcp__claude_ai_Beatitude,\
mcp__lumnia-zap__list_messages,mcp__lumnia-zap__list_chats,mcp__lumnia-zap__get_chat,\
mcp__lumnia-zap__search_contacts,mcp__lumnia-zap__get_contact_chats,mcp__lumnia-zap__get_direct_chat_by_contact,\
mcp__lumnia-zap__get_last_interaction,mcp__lumnia-zap__get_message_context,\
mcp__lumnia-zap__agenda_listar,mcp__lumnia-zap__agenda_buscar,mcp__lumnia-zap__agenda_livre,\
mcp__claude_ai_Microsoft_365__outlook_email_search,mcp__claude_ai_Microsoft_365__outlook_calendar_search,\
mcp__claude_ai_Microsoft_365__read_resource,mcp__claude_ai_Microsoft_365__get_me,\
mcp__claude_ai_Microsoft_365__search_people,mcp__claude_ai_Microsoft_365__chat_message_search,\
mcp__claude_ai_Gmail__search_threads,mcp__claude_ai_Gmail__get_thread,mcp__claude_ai_Gmail__get_message,\
mcp__claude_ai_Google_Calendar__list_events,mcp__claude_ai_Google_Calendar__search_events,mcp__claude_ai_Google_Calendar__get_event,\
mcp__claude_ai_Google_Drive__search_files,mcp__claude_ai_Google_Drive__read_file_content"

NEGADAS="Bash,Edit,Write,WebFetch,\
mcp__lumnia-zap__send_message,mcp__lumnia-zap__send_file,mcp__lumnia-zap__send_audio_message,mcp__lumnia-zap__agenda_criar,\
mcp__claude_ai_Microsoft_365__outlook_send_mail,mcp__claude_ai_Microsoft_365__outlook_send_draft,\
mcp__claude_ai_Microsoft_365__outlook_forward_mail,mcp__claude_ai_Microsoft_365__outlook_trash_thread,\
mcp__claude_ai_Gmail__send_message,mcp__claude_ai_Gmail__reply,mcp__claude_ai_Gmail__forward,mcp__claude_ai_Gmail__trash_thread"

cd "$DIR"
inicio=$(date +%s)
timeout 1500 claude -p "$PEDIDO" \
  --mcp-config "$DIR/mcp.json" \
  --output-format json \
  --permission-mode dontAsk \
  --allowedTools "$LIBERADAS" \
  --disallowedTools "$NEGADAS" \
  > "$LOG" 2>> "$LOGS/manha-erros.log"
codigo=$?
echo "$(date '+%F %T') manha codigo=$codigo segundos=$(( $(date +%s) - inicio ))" >> "$LOGS/historico.log"

# guarda só as últimas 30 rodadas
ls -1t "$LOGS"/manha-*.json 2>/dev/null | tail -n +31 | xargs -r rm -f
exit $codigo
