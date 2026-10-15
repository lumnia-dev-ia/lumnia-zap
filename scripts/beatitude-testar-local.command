#!/bin/zsh
# Compila a ponte com o modulo Beatitude e troca o binario da instalacao em uso.
# Guarda o binario anterior como whatsapp-bridge.anterior (para voltar atras).
# Duplo clique no Finder ou: zsh scripts/beatitude-testar-local.command
set -e
cd "$(dirname "$0")/../whatsapp-bridge"

DEST="$HOME/Library/Application Support/LumniaZap/whatsapp-bridge"
GO="$(command -v go || echo /opt/homebrew/bin/go)"

echo "1/4  Compilando a ponte…"
CGO_ENABLED=1 "$GO" build -trimpath -o "$TMPDIR/whatsapp-bridge-novo" .

echo "2/4  Guardando o binário atual como whatsapp-bridge.anterior…"
cp -p "$DEST/whatsapp-bridge" "$DEST/whatsapp-bridge.anterior"

echo "3/4  Instalando o novo e reiniciando o serviço…"
# copia ao lado e troca por rename (inode novo): sobrescrever um binario no
# lugar faz o macOS matar o processo por assinatura em cache divergente
cp "$TMPDIR/whatsapp-bridge-novo" "$DEST/whatsapp-bridge.novo"
mv -f "$DEST/whatsapp-bridge.novo" "$DEST/whatsapp-bridge"
launchctl kickstart -k "gui/$(id -u)/dev.lumnia.zap"
sleep 3

echo "3b/4 Atualizando o servidor MCP instalado (agenda.py, main.py…)…"
MCP_DEST="$HOME/Library/Application Support/LumniaZap/whatsapp-mcp-server"
if [ -d "$MCP_DEST" ]; then
  for f in main.py whatsapp.py audio.py agenda.py; do
    cp "$(dirname "$0")/../whatsapp-mcp-server/$f" "$MCP_DEST/$f"
  done
  echo "     copiado para $MCP_DEST (vale na próxima conversa do Claude)"
else
  echo "     pasta do MCP não encontrada em $MCP_DEST — pulei"
fi

echo "4/4  Conferindo o Claude Code…"
CLAUDE_BIN="$(zsh -lic 'command -v claude' 2>/dev/null | tail -1)"
[ -z "$CLAUDE_BIN" ] && [ -x "$HOME/.local/bin/claude" ] && CLAUDE_BIN="$HOME/.local/bin/claude"
if [ -n "$CLAUDE_BIN" ]; then
  echo "     Claude Code encontrado em $CLAUDE_BIN"
else
  echo "     Claude Code NÃO encontrado. Instale com:"
  echo "       curl -fsSL https://claude.ai/install.sh | bash"
  echo "     e depois rode: claude   (para entrar com a sua conta)"
fi

echo
echo "Opcional, para o pedido \"temas\" usar o Beatitude:"
echo "  claude mcp add --transport http --scope user beatitude https://beatitude.lumnia.dev/mcp"
echo "  depois abra: claude   →  digite /mcp  →  autentique o beatitude"
echo
echo "Para voltar atrás: cp \"$DEST/whatsapp-bridge.anterior\" \"$DEST/whatsapp-bridge.novo\" && mv -f \"$DEST/whatsapp-bridge.novo\" \"$DEST/whatsapp-bridge\" && launchctl kickstart -k gui/\$(id -u)/dev.lumnia.zap"
echo
open "http://localhost:8080/#beatitude"
echo "Agenda da Apple (iCloud): http://localhost:8080/apple"
