#!/bin/bash
# build-msix.sh — pacote MSIX do Lumnia Zap para Windows, em dois modos.
#
# RODA EM LINUX, a partir da raiz do repositorio:
#
#   bash scripts/build-msix.sh 1.0.9.3            # modo TESTE (padrao)
#   bash scripts/build-msix.sh 1.1.0.0 --store    # modo STORE
#
# Modo teste — fora da Store, sem custo:
#   Lumnia-Zap-<versao>.msix   assinado com o certificado de TESTE
#   Lumnia-Zap-Teste.cer       certificado publico, para confiar no Windows
#   lumnia-zap.mcpb            extensao do Claude Desktop (aponta para o alias)
#   Suba o 4o numero a cada build, senao o Windows recusa "a mesma versao".
#
# Modo store — para enviar no Partner Center:
#   Lumnia-Zap-<versao>-store.msix   SEM assinatura (a Microsoft assina)
#   lumnia-zap.mcpb                  com o PFN da identidade da Store
#   A identidade vem de msix/identidade-store.env (nao versionado), criado a
#   partir de "Apps e jogos > Lumnia Zap > Identidade do produto":
#     STORE_NAME="12345Lumnia.LumniaZap"
#     STORE_PUBLISHER="CN=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
#     STORE_PUBLISHER_DISPLAY="Lumnia"
#     STORE_PFN="12345Lumnia.LumniaZap_abcdefghijklm"
#   A versao precisa terminar em .0.
#
# Pre-requisitos (Ubuntu 24.04):
#   apt-get install gcc-mingw-w64-x86-64 osslsigncode zip unzip python3-pil
#   go (versao do go.mod), uv (para o Python 3.12 do compileall), curl, pip
#   makemsix compilado de github.com/microsoft/msix-packaging
#     (./makelinux.sh --pack --skip-samples --skip-tests; precisa cmake >= 3.29,
#      libicu-dev) — caminho em MAKEMSIX=/.../.vs/bin/makemsix
#
# O certificado de teste fica em dist-msix/cert/ (chave privada inclusa) e e
# reaproveitado entre builds: trocar de certificado obriga a confiar de novo.
# NUNCA versionar dist-msix/.

set -euo pipefail

VERSAO="${1:-}"
MODO="${2:-}"
[[ "$VERSAO" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "uso: bash scripts/build-msix.sh <a.b.c.d> [--store]"; exit 1; }
case "$MODO" in ""|--store) ;; *) echo "uso: bash scripts/build-msix.sh <a.b.c.d> [--store]"; exit 1;; esac
STORE=0; [ "$MODO" = "--store" ] && STORE=1

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$RAIZ/dist-msix"
STAGE="$DIST/stage"
CERT="$DIST/cert"
PUBLISHER="CN=Lumnia Zap Teste"
IDENT_NAME="Lumnia.LumniaZap"
PUBLISHER_DISPLAY="Lumnia"
PY_VERSAO="3.12.10"
MAKEMSIX="${MAKEMSIX:-makemsix}"

say()    { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
falhar() { echo "ERRO: $1" >&2; exit 1; }
precisa() { command -v "$1" >/dev/null || falhar "$2"; }

precisa x86_64-w64-mingw32-gcc "mingw-w64 não instalado"
precisa osslsigncode "osslsigncode não instalado"
precisa go "go não instalado"
precisa uv "uv não instalado"
[ -x "$MAKEMSIX" ] || precisa "$MAKEMSIX" "makemsix não encontrado (defina MAKEMSIX=...)"

if [ "$STORE" = 1 ]; then
  ENVF="$RAIZ/msix/identidade-store.env"
  [ -f "$ENVF" ] || falhar "modo --store exige $ENVF (ver cabeçalho deste script)"
  # shellcheck disable=SC1090
  . "$ENVF"
  for v in STORE_NAME STORE_PUBLISHER STORE_PUBLISHER_DISPLAY STORE_PFN; do
    [ -n "${!v:-}" ] || falhar "$v não definido em $ENVF"
  done
  [[ "$VERSAO" =~ \.0$ ]] || falhar "a Store exige versão terminada em .0 (recebi $VERSAO)"
  [[ "$STORE_PFN" == "${STORE_NAME}_"* ]] || falhar "STORE_PFN não começa com STORE_NAME_"
  PUBLISHER="$STORE_PUBLISHER"; IDENT_NAME="$STORE_NAME"; PUBLISHER_DISPLAY="$STORE_PUBLISHER_DISPLAY"
  echo "     modo STORE: $IDENT_NAME / $PUBLISHER / $PUBLISHER_DISPLAY"
fi

rm -rf "$STAGE"
mkdir -p "$STAGE/ponte" "$STAGE/mcp/servidor" "$STAGE/mcp/python" "$STAGE/mcp/site-packages" "$STAGE/assets" "$DIST/cache" "$CERT"

# ------------------------------------------------------------ 1. binarios
say "1/6  Ponte (cgo + mingw-w64)"
(cd "$RAIZ/whatsapp-bridge" && CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -trimpath -ldflags="-s -w" -o "$STAGE/ponte/whatsapp-bridge.exe" .)

say "2/6  servico.exe, LumniaZap.exe, lumnia-zap-mcp.exe"
(cd "$RAIZ/msix" && export CGO_ENABLED=0 GOOS=windows GOARCH=amd64 &&
  go vet ./... &&
  go build -trimpath -ldflags="-s -w -H windowsgui" -o "$STAGE/servico.exe" ./cmd/servico &&
  go build -trimpath -ldflags="-s -w -H windowsgui" -o "$STAGE/LumniaZap.exe" ./cmd/abrir &&
  go build -trimpath -ldflags="-s -w" -o "$STAGE/mcp/lumnia-zap-mcp.exe" ./cmd/mcp)

# ------------------------------------------------- 2. Python embutido + deps
say "3/6  Python $PY_VERSAO embutido e dependências (versões do uv.lock)"
EMB="$DIST/cache/python-$PY_VERSAO-embed-amd64.zip"
[ -f "$EMB" ] || curl -fsSL -o "$EMB" "https://www.python.org/ftp/python/$PY_VERSAO/python-$PY_VERSAO-embed-amd64.zip"
unzip -q "$EMB" -d "$STAGE/mcp/python"
PTH="$(ls "$STAGE/mcp/python"/python3*._pth)"
ZIPSTD="$(basename "$(ls "$STAGE/mcp/python"/python3*.zip)")"
# caminhos relativos ao proprio ._pth (mcp\python\)
printf '%s\r\n.\r\n..\\servidor\r\n..\\site-packages\r\nimport site\r\n' "$ZIPSTD" > "$PTH"

(cd "$RAIZ/whatsapp-mcp-server" && uv export --frozen --no-hashes --no-dev --no-emit-project -q -o "$DIST/cache/req.txt")
# o marcador sys_platform=='win32' seria avaliado contra o Linux; tirar os marcadores
sed -E 's/ *;.*$//' "$DIST/cache/req.txt" | grep -E '^[A-Za-z0-9]' > "$DIST/cache/req-win.txt"
pip install -q --disable-pip-version-check --no-compile --target "$STAGE/mcp/site-packages" \
  --platform win_amd64 --python-version 3.12 --implementation cp --abi cp312 \
  --only-binary=:all: --no-deps -r "$DIST/cache/req-win.txt"
rm -rf "$STAGE/mcp/site-packages/bin"

for f in main.py whatsapp.py audio.py agenda.py; do
  cp "$RAIZ/whatsapp-mcp-server/$f" "$STAGE/mcp/servidor/$f"
done
grep -q LUMNIA_ZAP_STORE "$STAGE/mcp/servidor/whatsapp.py" || falhar "whatsapp.py sem LUMNIA_ZAP_STORE"

# .pyc pre-compilados (a pasta do pacote e somente leitura; sem isto o
# Python recompila tudo a cada vez que o Claude abre o servidor)
PY312="$(uv python find 3.12 2>/dev/null || true)"
if [ -n "$PY312" ]; then
  # unchecked-hash: o .pyc vale pelo conteudo, nao pela data do .py — a data
  # muda ao desempacotar o MSIX e invalidaria todos os .pyc por timestamp
  "$PY312" -m compileall -q -f -j 0 --invalidation-mode unchecked-hash \
    "$STAGE/mcp/site-packages" "$STAGE/mcp/servidor" >/dev/null || true
  [ -d "$STAGE/mcp/servidor/__pycache__" ] || falhar "compileall não gerou .pyc"
else
  echo "     (sem Python 3.12 local: pacote vai sem .pyc — funciona, só inicia mais devagar)"
fi

# ----------------------------------------------------- 3. manifesto e icones
say "4/6  Manifesto e ícones"
sed -e "s/@VERSAO@/$VERSAO/" -e "s/@PUBLISHER@/$PUBLISHER/" -e "s/@NAME@/$IDENT_NAME/" \
    -e "s/@PUBLISHER_DISPLAY@/$PUBLISHER_DISPLAY/" "$RAIZ/msix/AppxManifest.xml.in" > "$STAGE/AppxManifest.xml"
grep -q '@' "$STAGE/AppxManifest.xml" && falhar "sobrou marcador @...@ no AppxManifest.xml"
python3 - "$RAIZ/painel.ico" "$STAGE/assets" "$DIST/cache/icone.png" <<'PY'
import sys
from PIL import Image
ico, out, mcpb_icon = sys.argv[1:]
im = Image.open(ico)
im.size = max(im.info.get("sizes", [im.size]))
im = im.convert("RGBA")
for nome, lado in [("Square150x150Logo", 150), ("Square44x44Logo", 44), ("StoreLogo", 50)]:
    im.resize((lado, lado), Image.LANCZOS).save(f"{out}/{nome}.png")
im.resize((256, 256), Image.LANCZOS).save(mcpb_icon)
PY
cp "$RAIZ/LICENSE" "$STAGE/LICENSE.txt"
# panel.go le VERSION em <pasta-do-exe>\..\VERSION = raiz do pacote
printf '%s\n' "$(echo "$VERSAO" | cut -d. -f1-3)" > "$STAGE/VERSION"

# ------------------------------------------------------------- 4. empacotar
say "5/6  Empacotando e assinando"
SEM="$DIST/cache/sem-assinatura.msix"
rm -f "$SEM"
"$MAKEMSIX" pack -d "$STAGE" -p "$SEM"

if [ "$STORE" = 1 ]; then
  # A Store rejeita pacote assinado por terceiros: vai sem assinatura.
  MSIX="$DIST/Lumnia-Zap-$VERSAO-store.msix"
  rm -f "$MSIX" && cp "$SEM" "$MSIX"
  unzip -l "$MSIX" | grep -q AppxSignature.p7x && falhar "pacote da Store saiu assinado"
  echo "     sem assinatura (a Microsoft assina na publicação)"
else
  if [ ! -f "$CERT/teste.pfx" ]; then
    openssl req -x509 -newkey rsa:3072 -sha256 -days 365 -nodes \
      -keyout "$CERT/teste.key" -out "$CERT/teste.crt" -subj "/CN=Lumnia Zap Teste" \
      -addext "basicConstraints=critical,CA:FALSE" \
      -addext "keyUsage=critical,digitalSignature" \
      -addext "extendedKeyUsage=codeSigning" 2>/dev/null
    openssl pkcs12 -export -out "$CERT/teste.pfx" -inkey "$CERT/teste.key" -in "$CERT/teste.crt" -passout pass:teste
  fi
  openssl x509 -in "$CERT/teste.crt" -outform DER -out "$DIST/Lumnia-Zap-Teste.cer"
  MSIX="$DIST/Lumnia-Zap-$VERSAO.msix"
  rm -f "$MSIX"
  # carimbo de tempo: sem ele, o pacote deixa de instalar quando o certificado
  # de teste vencer (365 dias). Se o TSA nao responder, assina sem carimbo.
  osslsigncode sign -pkcs12 "$CERT/teste.pfx" -pass teste -h sha256 \
    -ts http://timestamp.digicert.com -in "$SEM" -out "$MSIX" \
    || { echo "     (carimbo de tempo indisponível; assinando sem)"; rm -f "$MSIX";
         osslsigncode sign -pkcs12 "$CERT/teste.pfx" -pass teste -h sha256 -in "$SEM" -out "$MSIX"; }
  osslsigncode verify -CAfile "$CERT/teste.crt" -in "$MSIX" | grep -q "Signature verification: ok" \
    || falhar "a assinatura do MSIX não confere"
fi

# --------------------------------------------------------------- 5. .mcpb
say "6/6  Extensão do Claude Desktop (.mcpb)"
PFN="$(python3 - "$PUBLISHER" "$IDENT_NAME" <<'PY'
import hashlib, sys
# PublisherId = 13 caracteres base32 (alfabeto do Windows) dos 8 primeiros
# bytes do SHA-256 do Publisher em UTF-16LE.
h = hashlib.sha256(sys.argv[1].encode("utf-16-le")).digest()[:8]
bits = "".join(f"{b:08b}" for b in h) + "0"
alfa = "0123456789abcdefghjkmnpqrstvwxyz"
print(sys.argv[2] + "_" + "".join(alfa[int(bits[i:i+5], 2)] for i in range(0, 65, 5)))
PY
)"
if [ "$STORE" = 1 ] && [ "$PFN" != "$STORE_PFN" ]; then
  falhar "PFN calculado ($PFN) difere do STORE_PFN do Partner Center ($STORE_PFN) — confira o Publisher"
fi
echo "     PackageFamilyName esperado: $PFN"
M="$DIST/cache/mcpb"
rm -rf "$M" && mkdir -p "$M/server"
cp "$DIST/cache/icone.png" "$M/icon.png"
cat > "$M/server/LEIA-ME.txt" <<EOF
Esta extensão não traz o servidor dentro dela. Ela chama o Lumnia Zap
instalado pelo pacote MSIX, pelo alias de execução:
  %LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe
Instale o MSIX antes (pacote $PFN).
EOF
VSEM="$(echo "$VERSAO" | cut -d. -f1-3)"
cat > "$M/manifest.json" <<EOF
{
  "manifest_version": "0.3",
  "name": "lumnia-zap",
  "display_name": "Lumnia Zap",
  "version": "$VSEM",
  "description": "Seu WhatsApp no Claude, pela ponte local do Lumnia Zap (requer o pacote MSIX instalado).",
  "author": { "name": "Lumnia", "url": "https://lumnia.dev/zap" },
  "homepage": "https://lumnia.dev/zap",
  "repository": { "type": "git", "url": "https://github.com/lumnia-dev-ia/lumnia-zap" },
  "license": "MIT",
  "icon": "icon.png",
  "server": {
    "type": "binary",
    "entry_point": "server/LEIA-ME.txt",
    "mcp_config": {
      "command": "\${HOME}\\\\AppData\\\\Local\\\\Microsoft\\\\WindowsApps\\\\lumnia-zap-mcp.exe",
      "args": []
    }
  },
  "compatibility": { "platforms": ["win32"] },
  "_meta": {
    "com.microsoft.windows": { "package_family_name": "$PFN" }
  }
}
EOF
python3 -m json.tool "$M/manifest.json" >/dev/null || falhar "manifest.json inválido"
rm -f "$DIST/lumnia-zap.mcpb"
(cd "$M" && zip -q -r -X "$DIST/lumnia-zap.mcpb" manifest.json icon.png server)

echo
echo "======================================================================"
ls -la "$MSIX" "$DIST/lumnia-zap.mcpb" $( [ "$STORE" = 1 ] || echo "$DIST/Lumnia-Zap-Teste.cer" ) | awk '{print " " $5 "\t" $9}'
echo "======================================================================"
