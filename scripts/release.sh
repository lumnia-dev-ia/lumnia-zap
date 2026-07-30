#!/bin/bash
# release.sh — compila, assina, notariza e publica uma versao do Lumnia Zap.
#
# RODE NO SEU MAC (Apple Silicon), a partir da raiz do repositorio.
#
#   bash scripts/release.sh 1.0.0            # continua de onde parou
#   bash scripts/release.sh 1.0.0 --fresh    # recomeca do zero
#
# RETOMAVEL: cada etapa concluida deixa uma marca em dist/.state. Se a
# notarizacao for interrompida (terminal fechado, queda de conexao), rodar de
# novo NAO recompila nem pede sua senha outra vez — ele reconecta na submissao
# que ja esta na fila da Apple e continua esperando.
#
# Pre-requisitos:
#   - identidade "Developer ID Application: ... (5K8YAHD77Y)" no Keychain
#   - perfil de notarizacao "lumnia-zap"
#   - gh autenticado

set -euo pipefail

VERSAO="${1:-}"
FRESH="${2:-}"
[ -n "$VERSAO" ] || { echo "uso: bash scripts/release.sh <versao> [--fresh]"; exit 1; }

IDENTIDADE="Developer ID Application: DPM ASSESSORIA EMPRESARIAL LTDA (5K8YAHD77Y)"
PERFIL="lumnia-zap"
REPO="lumnia-dev-ia/lumnia-zap"
RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$RAIZ/dist"
STATE="$DIST/.state"
PACOTE="lumnia-zap-arm64.tar.gz"
APP="Instalador Lumnia Zap.app"
APPZIP="Instalador-Lumnia-Zap.zip"
BUNDLE="$DIST/$APP"

say()    { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
pulou()  { printf '     \033[2m(já feito, pulando)\033[0m\n'; }
falhar() { echo "ERRO: $1" >&2; exit 1; }
feito()  { [ -f "$STATE/$1" ]; }
marcar() { touch "$STATE/$1"; }

[ "$(uname -m)" = "arm64" ] || falhar "precisa rodar num Mac Apple Silicon"
security find-identity -v -p codesigning | grep -q "5K8YAHD77Y" \
  || falhar "identidade de assinatura não encontrada no Keychain"
command -v gh >/dev/null || falhar "gh não instalado"
gh auth status >/dev/null 2>&1 || falhar "gh não autenticado"

[ "$FRESH" = "--fresh" ] && rm -rf "$DIST"
mkdir -p "$DIST" "$STATE"

# ------------------------------------------------------- notarizacao assincrona

# enviar <zip> <nome> — submete sem esperar e guarda o id
enviar() {
  local zip="$1" nome="$2" id
  if [ -f "$STATE/$nome.id" ]; then
    echo "     $nome: já enviado ($(cat "$STATE/$nome.id"))"
    return 0
  fi
  id="$(xcrun notarytool submit "$zip" --keychain-profile "$PERFIL" --no-wait 2>&1 \
        | awk '/^[[:space:]]*id:/ {print $2; exit}')"
  [ -n "$id" ] || falhar "não consegui enviar $nome para notarização"
  printf '%s' "$id" > "$STATE/$nome.id"
  echo "     $nome enviado: $id"
}

# aguardar <nome> — espera o veredito, mostrando o log em caso de recusa
aguardar() {
  local nome="$1" id status
  feito "$nome.notarized" && { echo "     $nome: já notarizado"; return 0; }
  id="$(cat "$STATE/$nome.id")"
  printf '     %s: aguardando' "$nome"
  while :; do
    status="$(xcrun notarytool info "$id" --keychain-profile "$PERFIL" 2>&1 \
              | awk '/^[[:space:]]*status:/ {print $2; exit}')"
    case "$status" in
      Accepted)
        printf ' aceito\n'; marcar "$nome.notarized"; return 0 ;;
      Invalid|Rejected)
        printf ' RECUSADO\n\n'
        xcrun notarytool log "$id" --keychain-profile "$PERFIL" || true
        falhar "a Apple recusou $nome (veja o relatório acima)" ;;
      *)
        printf '.'; sleep 15 ;;
    esac
  done
}

# ---------------------------------------------------------------- 1. a ponte
say "1/7  Compilando a ponte"
if feito bridge.built; then pulou; else
  cd "$RAIZ/whatsapp-bridge"
  CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$DIST/whatsapp-bridge" .
  echo "     $(du -h "$DIST/whatsapp-bridge" | cut -f1)"
  marcar bridge.built
fi

say "2/7  Assinando a ponte"
if feito bridge.signed; then pulou; else
  codesign --force --options runtime --timestamp --sign "$IDENTIDADE" "$DIST/whatsapp-bridge"
  codesign --verify --strict "$DIST/whatsapp-bridge" && echo "     assinatura válida"
  marcar bridge.signed
fi

# ------------------------------------------------------------ 2. o instalador
say "3/7  Montando o instalador"
if feito app.built; then pulou; else
  cd "$RAIZ/installer"
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$DIST/instalador" .
  rm -rf "$BUNDLE"
  mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
  mv "$DIST/instalador" "$BUNDLE/Contents/MacOS/instalador"
  chmod +x "$BUNDLE/Contents/MacOS/instalador"
  cp "$RAIZ/painel.icns" "$BUNDLE/Contents/Resources/painel.icns"
  cat > "$BUNDLE/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Instalador Lumnia Zap</string>
	<key>CFBundleDisplayName</key><string>Instalador Lumnia Zap</string>
	<key>CFBundleIdentifier</key><string>dev.lumnia.zap.installer</string>
	<key>CFBundleExecutable</key><string>instalador</string>
	<key>CFBundleIconFile</key><string>painel</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$VERSAO</string>
	<key>CFBundleVersion</key><string>$VERSAO</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>LSMinimumSystemVersion</key><string>12.0</string>
</dict>
</plist>
PLIST
  printf 'APPL????' > "$BUNDLE/Contents/PkgInfo"
  marcar app.built
fi

say "4/7  Assinando o instalador"
if feito app.signed; then pulou; else
  codesign --force --options runtime --timestamp --sign "$IDENTIDADE" "$BUNDLE/Contents/MacOS/instalador"
  codesign --force --options runtime --timestamp --sign "$IDENTIDADE" "$BUNDLE"
  codesign --verify --deep --strict "$BUNDLE" && echo "     assinatura válida"
  marcar app.signed
fi

# ------------------------------------------------------------- 3. notarizacao
say "5/7  Notarizando"
# A ponte NAO e notarizada por padrao, de proposito. O instalador a baixa por
# dentro do proprio programa Go, e arquivo escrito assim nao recebe o atributo
# de quarentena do macOS — entao o Gatekeeper nem consulta a notarizacao dele.
# O que importa para a ponte e estar ASSINADA, o que ela esta. Na pratica,
# submeter binario avulso (fora de .app/.pkg/.dmg) tambem se mostrou o caminho
# lento da esteira da Apple: travou mais de uma hora em In Progress.
# Para notarizar a ponte assim mesmo:  LUMNIA_NOTARIZE_BRIDGE=1 bash scripts/release.sh ...
NOTARIZAR_PONTE="${LUMNIA_NOTARIZE_BRIDGE:-0}"

if [ "$NOTARIZAR_PONTE" = "1" ] && ! feito bridge.notarized; then
  [ -f "$DIST/bridge-notarize.zip" ] || ditto -c -k "$DIST/whatsapp-bridge" "$DIST/bridge-notarize.zip"
  enviar "$DIST/bridge-notarize.zip" bridge
fi
if ! feito app.notarized; then
  [ -f "$DIST/app-notarize.zip" ] || ditto -c -k --keepParent "$BUNDLE" "$DIST/app-notarize.zip"
  enviar "$DIST/app-notarize.zip" app
fi
if [ "$NOTARIZAR_PONTE" = "1" ]; then aguardar bridge; fi
aguardar app

if ! feito app.stapled; then
  xcrun stapler staple "$BUNDLE"
  marcar app.stapled
fi
rm -f "$DIST/bridge-notarize.zip" "$DIST/app-notarize.zip"

echo "     verificação final:"
spctl --assess --type execute --verbose=2 "$BUNDLE" 2>&1 | sed 's/^/       /'
xcrun stapler validate "$BUNDLE" 2>&1 | sed 's/^/       /'

# ----------------------------------------------------------------- 4. pacotes
say "6/7  Empacotando"
rm -f "$DIST/$APPZIP"
ditto -c -k --keepParent "$BUNDLE" "$DIST/$APPZIP"

STAGE="$(mktemp -d)/lumnia-zap"
mkdir -p "$STAGE/whatsapp-bridge" "$STAGE/whatsapp-mcp-server"
cp "$DIST/whatsapp-bridge" "$STAGE/whatsapp-bridge/whatsapp-bridge"
chmod +x "$STAGE/whatsapp-bridge/whatsapp-bridge"
for f in main.py whatsapp.py audio.py pyproject.toml uv.lock .python-version; do
  cp "$RAIZ/whatsapp-mcp-server/$f" "$STAGE/whatsapp-mcp-server/$f"
done
printf '# lumnia-zap mcp server\n' > "$STAGE/whatsapp-mcp-server/README.md"
cp "$RAIZ/painel.icns" "$STAGE/painel.icns"
cp "$RAIZ/LICENSE" "$STAGE/LICENSE"
printf '%s\n' "$VERSAO" > "$STAGE/VERSION"
tar --no-xattrs -czf "$DIST/$PACOTE" -C "$(dirname "$STAGE")" lumnia-zap
echo "     $APPZIP  $(du -h "$DIST/$APPZIP" | cut -f1)"
echo "     $PACOTE  $(du -h "$DIST/$PACOTE" | cut -f1)"

# --------------------------------------------------------------- 5. publicar
say "7/7  Publicando a release v$VERSAO"
cd "$RAIZ"
if gh release view "v$VERSAO" --repo "$REPO" >/dev/null 2>&1; then
  echo "     release v$VERSAO já existe — substituindo os arquivos"
  gh release upload "v$VERSAO" --repo "$REPO" --clobber "$DIST/$APPZIP" "$DIST/$PACOTE"
else
  NOTAS="$(mktemp)"
  cat > "$NOTAS" <<NOTAS_FIM
Instalação em dois cliques no macOS (Apple Silicon).

1. Baixe **$APPZIP**
2. Descompacte e dê duplo clique no **Instalador Lumnia Zap**
3. Escaneie o QR code que aparece na tela

O instalador é assinado e notarizado pela Apple — não aparece aviso de segurança.

---

O arquivo \`$PACOTE\` é baixado automaticamente pelo instalador; você não precisa dele.
NOTAS_FIM
  gh release create "v$VERSAO" --repo "$REPO" \
    --title "Lumnia Zap v$VERSAO" --notes-file "$NOTAS" \
    "$DIST/$APPZIP" "$DIST/$PACOTE"
  rm -f "$NOTAS"
fi

echo
echo "======================================================================"
echo " Publicado: https://github.com/$REPO/releases/tag/v$VERSAO"
echo
echo " Link para a família (sempre a versão mais recente):"
echo "   https://github.com/$REPO/releases/latest/download/$APPZIP"
echo "======================================================================"
