#!/bin/bash
# mcpb.sh — monta a extensão do Claude Desktop (lumnia-zap.mcpb) para o Lumnia
# Zap instalado como pacote MSIX (Microsoft Store ou pacote de teste).
#
#   bash scripts/mcpb.sh <versao a.b.c> <PackageFamilyName> <saida.mcpb>
#
# A extensão não traz o servidor: ela aponta o Claude para o alias de execução
# que o pacote MSIX cria (%LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe)
# e declara o PFN, para o Claude saber de qual pacote ela depende.
# Usado por build-msix.sh (teste e --store) e por release-windows.sh (anexa à
# release do GitHub, que é de onde lumnia.dev/zap manda baixar).

set -euo pipefail

VSEM="${1:-}"; PFN="${2:-}"; SAIDA="${3:-}"
[[ "$VSEM" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] && [ -n "$PFN" ] && [ -n "$SAIDA" ] \
  || { echo "uso: bash scripts/mcpb.sh <a.b.c> <PackageFamilyName> <saida.mcpb>" >&2; exit 1; }
command -v zip >/dev/null || { echo "ERRO: zip não instalado" >&2; exit 1; }

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
M="$(mktemp -d)"
trap 'rm -rf "$M"' EXIT
mkdir -p "$M/server"
cp "$RAIZ/msix/mcpb-icon.png" "$M/icon.png"
cat > "$M/server/LEIA-ME.txt" <<TXT
Esta extensão não traz o servidor dentro dela. Ela chama o Lumnia Zap
instalado pelo pacote MSIX, pelo alias de execução:
  %LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe
Instale o Lumnia Zap pela Microsoft Store antes (pacote $PFN).
TXT
cat > "$M/manifest.json" <<JSON
{
  "manifest_version": "0.3",
  "name": "lumnia-zap",
  "display_name": "Lumnia Zap",
  "version": "$VSEM",
  "description": "Seu WhatsApp no Claude, pela ponte local do Lumnia Zap (requer o Lumnia Zap da Microsoft Store instalado).",
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
JSON
python3 -m json.tool "$M/manifest.json" >/dev/null || { echo "ERRO: manifest.json inválido" >&2; exit 1; }
mkdir -p "$(dirname "$SAIDA")"
SAIDA="$(cd "$(dirname "$SAIDA")" && pwd)/$(basename "$SAIDA")"
rm -f "$SAIDA"
(cd "$M" && zip -q -r -X "$SAIDA" manifest.json icon.png server)
echo "     $(basename "$SAIDA")  versão $VSEM  PFN $PFN"
