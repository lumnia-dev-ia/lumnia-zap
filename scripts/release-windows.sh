#!/bin/bash
# release-windows.sh — compila e publica a versao Windows do Lumnia Zap.
#
# RODA EM LINUX (ou WSL), a partir da raiz do repositorio — nao precisa de um
# PC Windows para compilar, so para testar o instalador de verdade.
#
#   bash scripts/release-windows.sh 1.0.0            # continua de onde parou
#   bash scripts/release-windows.sh 1.0.0 --fresh    # recomeca do zero
#
# RETOMAVEL, como o release.sh do Mac: cada etapa concluida deixa uma marca em
# dist-windows/.state.
#
# Pre-requisitos (Ubuntu/Debian):
#   sudo apt-get install -y gcc-mingw-w64-x86-64 zip
#   go (mesma versao do go.mod)
#   gh autenticado
#
# IMPORTANTE — sem assinatura de codigo: ao contrario do instalador de Mac
# (assinado e notarizado pela Apple), o instalador Windows NAO e assinado —
# assinatura Authenticode exige comprar um certificado de um emissor
# reconhecido (DigiCert, Sectigo, SignPath etc., na faixa de US$100-400/ano).
# Sem isso, o Windows SmartScreen vai mostrar um aviso ("O Windows protegeu
# seu PC") na primeira execucao. E esperado; documente isso para quem for
# instalar (Mais informacoes -> Executar assim mesmo).

set -euo pipefail

VERSAO="${1:-}"
FRESH="${2:-}"
[ -n "$VERSAO" ] || { echo "uso: bash scripts/release-windows.sh <versao> [--fresh]"; exit 1; }

REPO="lumnia-dev-ia/lumnia-zap"
RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$RAIZ/dist-windows"
STATE="$DIST/.state"
PACOTE="lumnia-zap-windows-amd64.zip"
INSTALADOR="Instalador-Lumnia-Zap-Windows.exe"

say()    { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
pulou()  { printf '     \033[2m(já feito, pulando)\033[0m\n'; }
falhar() { echo "ERRO: $1" >&2; exit 1; }
feito()  { [ -f "$STATE/$1" ]; }
marcar() { touch "$STATE/$1"; }

# As ferramentas de compilacao so sao exigidas nas etapas que ainda faltam:
# se dist-windows/ ja veio pronto (compilado em outra maquina Linux), este
# script roda em qualquer lugar e faz so a publicacao.
precisa() { command -v "$1" >/dev/null || falhar "$2"; }
command -v gh >/dev/null || falhar "gh não instalado"
gh auth status >/dev/null 2>&1 || falhar "gh não autenticado"

[ "$FRESH" = "--fresh" ] && rm -rf "$DIST"
mkdir -p "$DIST" "$STATE"

# ---------------------------------------------------------------- 1. a ponte
say "1/4  Compilando a ponte (cgo + mingw-w64, mesmo driver sqlite do Mac)"
if feito bridge.built; then pulou; else
  precisa x86_64-w64-mingw32-gcc "mingw-w64 não instalado — compile num Linux/WSL (sudo apt-get install gcc-mingw-w64-x86-64)"
  cd "$RAIZ/whatsapp-bridge"
  CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -trimpath -o "$DIST/whatsapp-bridge.exe" .
  echo "     $(du -h "$DIST/whatsapp-bridge.exe" | cut -f1)"
  marcar bridge.built
fi

# ------------------------------------------------------------ 2. o instalador
say "2/4  Compilando o instalador"
if feito installer.built; then pulou; else
  cd "$RAIZ/installer-windows"
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o "$DIST/$INSTALADOR" .
  echo "     $(du -h "$DIST/$INSTALADOR" | cut -f1)"
  marcar installer.built
fi

# ----------------------------------------------------------------- 3. pacote
say "3/4  Empacotando"
if feito pacote.built; then pulou; else
  precisa zip "zip não instalado (sudo apt-get install zip)"
  STAGE="$(mktemp -d)/lumnia-zap"
  mkdir -p "$STAGE/whatsapp-bridge" "$STAGE/whatsapp-mcp-server"
  cp "$DIST/whatsapp-bridge.exe" "$STAGE/whatsapp-bridge/whatsapp-bridge.exe"
  for f in main.py whatsapp.py audio.py pyproject.toml uv.lock .python-version; do
    cp "$RAIZ/whatsapp-mcp-server/$f" "$STAGE/whatsapp-mcp-server/$f"
  done
  printf '# lumnia-zap mcp server\n' > "$STAGE/whatsapp-mcp-server/README.md"
  # painel.ico eh gerado a partir do painel.icns (nao versionado — gere uma vez com
  # Pillow: Image.open("painel.icns").convert("RGBA").save("painel.ico", sizes=[...]))
  [ -f "$RAIZ/painel.ico" ] || falhar "painel.ico não encontrado na raiz do repo — gere a partir do painel.icns"
  cp "$RAIZ/painel.ico" "$STAGE/painel.ico"
  cp "$RAIZ/LICENSE" "$STAGE/LICENSE"
  printf '%s\n' "$VERSAO" > "$STAGE/VERSION"
  rm -f "$DIST/$PACOTE"
  (cd "$(dirname "$STAGE")" && zip -r -X "$DIST/$PACOTE" lumnia-zap >/dev/null)
  echo "     $PACOTE  $(du -h "$DIST/$PACOTE" | cut -f1)"
  marcar pacote.built
fi

# --------------------------------------------------------------- 4. publicar
say "4/4  Publicando na release v$VERSAO"
cd "$RAIZ"
if gh release view "v$VERSAO" --repo "$REPO" >/dev/null 2>&1; then
  echo "     release v$VERSAO já existe — adicionando/substituindo os arquivos Windows"
  gh release upload "v$VERSAO" --repo "$REPO" --clobber "$DIST/$INSTALADOR" "$DIST/$PACOTE"
else
  gh release create "v$VERSAO" --repo "$REPO" \
    --title "Lumnia Zap v$VERSAO" \
    --notes "Windows: baixe **$INSTALADOR** e dê duplo clique (o Windows vai avisar que o app não é reconhecido — clique em 'Mais informações' → 'Executar assim mesmo'; o instalador ainda não é assinado digitalmente)." \
    "$DIST/$INSTALADOR" "$DIST/$PACOTE"
fi

echo
echo "======================================================================"
echo " Publicado: https://github.com/$REPO/releases/tag/v$VERSAO"
echo
echo " Link para quem for instalar no Windows:"
echo "   https://github.com/$REPO/releases/latest/download/$INSTALADOR"
echo "======================================================================"
