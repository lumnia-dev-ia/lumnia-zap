#!/bin/bash
# migrar.sh — migra a instalacao artesanal para o layout definitivo do Lumnia Zap.
#
# Sai:   ~/Developer/whatsapp-mcp  +  rotulo com.diego.whatsapp-bridge
# Entra: ~/Library/Application Support/LumniaZap  +  rotulo dev.lumnia.zap
#
# A sessao do WhatsApp e todo o historico sao PRESERVADOS — nao precisa parear
# de novo. O codigo-fonte continua em ~/Developer/whatsapp-mcp para desenvolver.
#
# Uso:  bash migrar.sh

set -euo pipefail

OLD_LABEL="com.diego.whatsapp-bridge"
NEW_LABEL="dev.lumnia.zap"
APP_NAME="Lumnia Zap"
SRC="$HOME/Developer/whatsapp-mcp"
DEST="$HOME/Library/Application Support/LumniaZap"
AGENTS="$HOME/Library/LaunchAgents"
MYUID="$(id -u)"
CFG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"

say() { printf '\n==> %s\n' "$1"; }

[ -d "$SRC/whatsapp-bridge" ] || { echo "ERRO: nao achei $SRC/whatsapp-bridge"; exit 1; }
if [ -e "$DEST/whatsapp-bridge/store" ]; then
  echo "ERRO: ja existe $DEST/whatsapp-bridge/store"
  echo "      Migracao ja foi feita antes. Remova o destino antes de repetir."
  exit 1
fi

say "1/8  Compilando a partir do codigo-fonte"
cd "$SRC/whatsapp-bridge"
go build -o whatsapp-bridge .
echo "     ok ($(du -h whatsapp-bridge | cut -f1))"

say "2/8  Parando o servico antigo"
launchctl bootout "gui/$MYUID/$OLD_LABEL" 2>/dev/null || true
sleep 1

say "3/8  Criando a estrutura nova"
mkdir -p "$DEST/whatsapp-bridge" "$DEST/whatsapp-mcp-server" "$DEST/logs"

say "4/8  Movendo sessao e historico (preserva o pareamento)"
mv "$SRC/whatsapp-bridge/store" "$DEST/whatsapp-bridge/store"
echo "     $(du -sh "$DEST/whatsapp-bridge/store" | cut -f1) movidos"

say "5/8  Instalando binario, servidor MCP e icone"
cp "$SRC/whatsapp-bridge/whatsapp-bridge" "$DEST/whatsapp-bridge/whatsapp-bridge"
chmod +x "$DEST/whatsapp-bridge/whatsapp-bridge"
for f in main.py whatsapp.py audio.py pyproject.toml uv.lock .python-version; do
  cp "$SRC/whatsapp-mcp-server/$f" "$DEST/whatsapp-mcp-server/$f"
done
printf '# lumnia-zap mcp server\n' > "$DEST/whatsapp-mcp-server/README.md"
cp "$SRC/painel.icns" "$DEST/painel.icns"

say "6/8  Registrando o servico dev.lumnia.zap"
mkdir -p "$AGENTS"
cat > "$AGENTS/$NEW_LABEL.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$NEW_LABEL</string>
	<key>ProgramArguments</key>
	<array><string>$DEST/whatsapp-bridge/whatsapp-bridge</string></array>
	<key>WorkingDirectory</key><string>$DEST/whatsapp-bridge</string>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>$DEST/logs/bridge.log</string>
	<key>StandardErrorPath</key><string>$DEST/logs/bridge-error.log</string>
</dict>
</plist>
PLIST
rm -f "$AGENTS/$OLD_LABEL.plist"

say "7/8  Apontando o Claude Desktop para o caminho novo"
cp "$CFG" "$CFG.bak-migracao"
UV="$(command -v uv || echo "$HOME/.local/bin/uv")"
python3 - "$CFG" "$DEST" "$UV" <<'PY'
import json, sys, pathlib
cfg_path, dest, uv = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
cfg = json.loads(cfg_path.read_text())
servers = cfg.setdefault("mcpServers", {})
servers.pop("whatsapp", None)          # remove a entrada antiga
servers["lumnia-zap"] = {
    "command": uv,
    "args": ["--directory", f"{dest}/whatsapp-mcp-server", "run", "main.py"],
}
cfg_path.write_text(json.dumps(cfg, indent=2, ensure_ascii=False))
print("     servidores MCP:", ", ".join(servers))
print("     preferencias preservadas:", "preferences" in cfg)
PY

say "8/8  Criando o app \"$APP_NAME\" no Desktop"
APP="$HOME/Desktop/$APP_NAME.app"
rm -rf "$APP" "$HOME/Desktop/WhatsApp Painel.app" "$HOME/Desktop/WhatsApp Painel.webloc"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$DEST/painel.icns" "$APP/Contents/Resources/painel.icns"
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Lumnia Zap</string>
	<key>CFBundleDisplayName</key><string>Lumnia Zap</string>
	<key>CFBundleIdentifier</key><string>dev.lumnia.zap.launcher</string>
	<key>CFBundleExecutable</key><string>zap</string>
	<key>CFBundleIconFile</key><string>painel</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleSignature</key><string>????</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
cat > "$APP/Contents/MacOS/zap" <<'LAUNCHER'
#!/bin/sh
LABEL="dev.lumnia.zap"
URL="http://localhost:8080"
MYUID="$(/usr/bin/id -u)"
alive() { /usr/bin/curl -s -o /dev/null --max-time 2 "$URL/api/panel/status"; }
if ! alive; then
	if ! /bin/launchctl kickstart -k "gui/$MYUID/$LABEL" 2>/dev/null; then
		/bin/launchctl bootstrap "gui/$MYUID" "$HOME/Library/LaunchAgents/$LABEL.plist" 2>/dev/null
	fi
	i=0
	while [ "$i" -lt 40 ]; do
		if alive; then break; fi
		/bin/sleep 0.5
		i=$((i + 1))
	done
fi
if alive; then
	/usr/bin/open "$URL"
else
	/usr/bin/osascript -e 'display alert "Lumnia Zap indisponível" message "A ponte não respondeu em 20 segundos.

No Terminal:
tail -30 ~/Library/Application\ Support/LumniaZap/logs/bridge.log" as critical'
fi
LAUNCHER
chmod +x "$APP/Contents/MacOS/zap"
printf 'APPL????' > "$APP/Contents/PkgInfo"
xattr -cr "$APP" 2>/dev/null || true
touch "$APP"

say "Subindo o servico"
launchctl bootstrap "gui/$MYUID" "$AGENTS/$NEW_LABEL.plist"
for i in $(seq 1 30); do
  curl -s -o /dev/null --max-time 1 http://localhost:8080/api/panel/status && break
  sleep 0.5
done

echo
echo "======================================================================"
echo " Migracao concluida."
echo
echo "   Runtime:  $DEST"
echo "   Servico:  $NEW_LABEL"
echo "   Logs:     $DEST/logs/bridge.log"
echo "   App:      $APP"
echo "   Fonte:    $SRC   (continua aqui, para desenvolver)"
echo
echo " Backup da config do Claude: $CFG.bak-migracao"
echo
echo " AGORA: feche o Claude Desktop com Cmd+Q e abra de novo."
echo "======================================================================"
curl -s http://localhost:8080/api/panel/status || echo " (a ponte ainda esta subindo)"
echo
open "http://localhost:8080" 2>/dev/null || true
