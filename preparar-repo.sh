#!/bin/bash
# preparar-repo.sh — transforma o clone do whatsapp-mcp no repositorio lumnia-zap.
#
# O que faz:
#   - remove arquivos obsoletos (o .webloc antigo, o criar-atalho.sh e um alias solto)
#   - renomeia a pasta para ~/Developer/lumnia-zap
#   - renomeia o remote 'origin' (repo do Luke) para 'upstream', preservando o
#     historico de origem no git — provenencia honesta e permite puxar correcoes dele
#   - faz o commit das nossas mudancas
#
# NAO cria o repositorio no GitHub nem faz push. Isso e o passo seguinte.
#
# Uso:  bash preparar-repo.sh

set -euo pipefail

OLD="$HOME/Developer/whatsapp-mcp"
NEW="$HOME/Developer/lumnia-zap"

say() { printf '\n==> %s\n' "$1"; }

[ -d "$OLD/.git" ] || { echo "ERRO: $OLD nao e um repositorio git"; exit 1; }
[ -e "$NEW" ] && { echo "ERRO: $NEW ja existe"; exit 1; }

cd "$OLD"

say "1/5  Conferindo que nenhum banco esta rastreado"
if git ls-files | grep -Ei '\.db$|store/' ; then
  echo "PARE: ha arquivo de banco rastreado no git. Nao prossiga."
  exit 1
fi
echo "     ok — nenhuma conversa no controle de versao"

say "2/5  Removendo arquivos obsoletos"
rm -f "WhatsApp Painel.webloc" \
      "criar-atalho.sh" \
      "whatsapp-bridge/panel.html alias"
echo "     removidos (o instalador e o migrar.sh substituem o criar-atalho)"

say "3/5  Renomeando a pasta"
cd "$HOME/Developer"
mv "$OLD" "$NEW"
cd "$NEW"
echo "     $NEW"

say "4/5  Reorganizando os remotes"
if git remote | grep -qx origin; then
  git remote rename origin upstream
fi
git remote -v | sed 's/^/     /'
echo "     (o remote 'origin' sera adicionado quando o repo do GitHub existir)"

say "5/5  Registrando as mudancas"
git add -A
git -c user.name="${GIT_AUTHOR_NAME:-Diego Penna Moreira}" \
    -c user.email="${GIT_AUTHOR_EMAIL:-diego@lumnia.dev}" \
    commit -q -F - <<'MSG'
Lumnia Zap: painel de controle, servico macOS e quatro correcoes

Camada nova sobre o whatsapp-mcp de Luke Harries (MIT), preservando o
copyright original e acrescentando o da Lumnia.

Novo:
- panel.go: painel web local servido pela propria ponte, com status,
  reconexao, QR code no navegador, contadores diarios, busca de contatos
  e mensagens programadas (uma vez, diaria, semanal e anual)
- panel.html: interface do painel, com identidade Lumnia
- agendador com tolerancia de atraso: ocorrencia vencida e registrada como
  perdida, nunca enviada fora de hora
- teto de 30 envios por hora nos envios do painel e do agendador
- endpoints do painel restritos a loopback
- servico launchd, app com icone e script de migracao

Correcoes no projeto original:
- whatsmeow desatualizado causava "Client outdated (405)", visivel como
  websocket close 1006; o QR code nunca aparecia
- cinco chamadas passaram a exigir context.Context
- download de midia falhava com HTTP 403 porque extractDirectPathFromURL
  removia a query string assinada, que o whatsmeow espera presente ao
  concatenar seus proprios parametros com "&"
- busca por contato nao encontrava conversas identificadas por @lid; agora
  o LID e resolvido para o JID de telefone antes da consulta ao cadastro
- conexao bloqueante matava o processo sob launchd; agora e assincrona,
  de modo que o painel sempre sobe e consegue exibir o QR code
MSG

echo
echo "======================================================================"
git log --oneline -1 | sed 's/^/  /'
git show --stat --oneline HEAD | tail -n +2 | sed 's/^/  /'
echo "======================================================================"
echo
echo " Pasta:    $NEW"
echo " Upstream: $(git remote get-url upstream 2>/dev/null || echo '-')"
echo
echo " Proximo passo: criar o repositorio no GitHub e adicionar o origin."
