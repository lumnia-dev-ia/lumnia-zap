#!/bin/bash
# publicar-github.sh — cria o repositorio lumnia-zap no GitHub e faz o primeiro push.
#
# Pre-requisitos:
#   1. gh auth login --hostname github.com --git-protocol https --web
#   2. bash preparar-repo.sh   (renomeia a pasta e faz o commit)
#
# Uso:  bash publicar-github.sh

set -euo pipefail

REPO_DIR="$HOME/Developer/lumnia-zap"
REPO_NAME="lumnia-zap"
DESC="Seu WhatsApp pessoal dentro do Claude — painel local, mensagens programadas e instalacao em dois cliques no macOS"

say() { printf '\n==> %s\n' "$1"; }

command -v gh >/dev/null || { echo "ERRO: gh nao instalado. brew install gh"; exit 1; }
[ -d "$REPO_DIR/.git" ] || { echo "ERRO: $REPO_DIR nao existe. Rode preparar-repo.sh antes."; exit 1; }
cd "$REPO_DIR"

say "Conferindo autenticacao"
if ! gh auth status >/dev/null 2>&1; then
  echo "ERRO: nao autenticado. Rode:"
  echo "      gh auth login --hostname github.com --git-protocol https --web"
  exit 1
fi
gh auth status 2>&1 | sed 's/^/     /' || true

say "Trava de seguranca: nada de banco ou sessao no commit"
if git ls-files | grep -Ei '\.db$|store/|\.enc$' ; then
  echo "PARE: ha arquivo de banco rastreado. Nao publique."
  exit 1
fi
echo "     ok — nenhuma conversa sera publicada"

say "O que vai para o GitHub"
TOTAL="$(git ls-files | wc -l | tr -d ' ')"
git ls-files | head -25 | sed 's/^/     /' || true
if [ "$TOTAL" -gt 25 ]; then echo "     … e mais $(( TOTAL - 25 ))"; fi

echo
echo "----------------------------------------------------------------------"
echo " Isto vai criar um repositorio PUBLICO chamado '$REPO_NAME'."
echo " Uma vez publicado, o conteudo pode ser copiado por qualquer pessoa."
echo "----------------------------------------------------------------------"
printf "\nConfirma? (digite: sim) "
read -r RESP
[ "$RESP" = "sim" ] || { echo "Cancelado."; exit 0; }

say "Criando e enviando"
gh repo create "$REPO_NAME" \
  --public \
  --source=. \
  --remote=origin \
  --description "$DESC" \
  --push

say "Marcando o repositorio"
gh repo edit \
  --homepage "https://lumnia.dev" \
  --add-topic whatsapp \
  --add-topic mcp \
  --add-topic claude \
  --add-topic macos \
  --add-topic golang \
  --add-topic model-context-protocol \
  >/dev/null

URL="$(gh repo view --json url -q .url)"
echo
echo "======================================================================"
echo " Publicado: $URL"
echo "======================================================================"
echo
echo " Remotes:"
git remote -v | sed 's/^/   /'
echo
echo " Proximo: abrir o pull request com as 4 correcoes no repo do Luke."
