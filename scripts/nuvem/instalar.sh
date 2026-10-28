#!/usr/bin/env bash
# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
#
# instalar.sh — monta o computador do Beatitude na nuvem (Ubuntu 24.04).
#
# Roda como root, uma vez, no terminal do painel da Hostinger:
#   bash <(curl -fsSL https://raw.githubusercontent.com/lumnia-dev-ia/lumnia-zap/nuvem-linux/scripts/nuvem/instalar.sh)
#
# O que fica na máquina:
#   - usuário `beatitude` (sem sudo), que roda tudo;
#   - área de trabalho Xfce servida no navegador pelo KasmVNC (porta 8444);
#   - Google Chrome, aberto sozinho na área de trabalho;
#   - Claude Code, uv e o servidor MCP do Zap;
#   - Lumnia Zap compilado desta branch, como serviço do sistema (painel em 127.0.0.1:8080,
#     só visível de dentro da área de trabalho).
#
# É idempotente: rodar de novo atualiza o Zap e confere o resto.
# Os logins (Google/Microsoft, Claude, WhatsApp) são da pessoa e ficam fora do script.

set -euo pipefail

USUARIO=beatitude
CASA=/home/$USUARIO
BRANCH=${LUMNIA_BRANCH:-nuvem-linux}
REPO=https://github.com/lumnia-dev-ia/lumnia-zap
DESTINO=$CASA/lumnia-zap
GO_VERSAO=1.25.0
PORTA_TELA=8444

passo() { printf '\n\033[1;32m==> %s\033[0m\n' "$*"; }
aviso() { printf '\033[1;33m!! %s\033[0m\n' "$*"; }

[ "$(id -u)" = 0 ] || { echo "Rode como root."; exit 1; }
. /etc/os-release
[ "${VERSION_CODENAME:-}" = noble ] || aviso "Feito para Ubuntu 24.04 (noble); este é ${PRETTY_NAME:-?}."

# ------------------------------------------------------------------ senha da tela
if [ ! -f "$CASA/.kasmpasswd" ]; then
  echo
  echo "Escolha a senha da tela (área de trabalho no navegador)."
  echo "Usuário: $USUARIO. Guarde no seu gerenciador de senhas."
  while :; do
    read -rsp "Senha (mínimo 8 caracteres): " SENHA_TELA; echo
    read -rsp "Repita a senha: " SENHA_TELA2; echo
    [ "${#SENHA_TELA}" -ge 8 ] && [ "$SENHA_TELA" = "$SENHA_TELA2" ] && break
    echo "As senhas não conferem ou são curtas. De novo."
  done
fi

# ------------------------------------------------------------------ base
passo "Fuso, swap e pacotes do sistema"
timedatectl set-timezone America/Sao_Paulo || true
if ! swapon --show | grep -q /swapfile; then
  fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
  grep -q /swapfile /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y --no-install-recommends \
  xfce4 xfce4-terminal xfce4-goodies dbus-x11 xdg-utils fonts-noto fonts-noto-color-emoji \
  build-essential git curl ca-certificates wget gnupg jq sqlite3 ffmpeg ssl-cert \
  unattended-upgrades libdatetime-perl libyaml-tiny-perl libhash-merge-simple-perl \
  liblist-moreutils-perl libswitch-perl libtry-tiny-perl

# ------------------------------------------------------------------ usuário
passo "Usuário $USUARIO"
id "$USUARIO" >/dev/null 2>&1 || useradd -m -s /bin/bash "$USUARIO"
usermod -aG ssl-cert "$USUARIO"
loginctl enable-linger "$USUARIO" || true
como() { sudo -u "$USUARIO" -H bash -lc "$*"; }

# ------------------------------------------------------------------ Chrome
passo "Google Chrome"
if ! command -v google-chrome >/dev/null; then
  install -d -m 0755 /etc/apt/keyrings
  curl -fsSL https://dl.google.com/linux/linux_signing_key.pub | gpg --dearmor -o /etc/apt/keyrings/google-chrome.gpg
  echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/google-chrome.gpg] http://dl.google.com/linux/chrome/deb/ stable main" \
    > /etc/apt/sources.list.d/google-chrome.list
  apt-get update -y
  apt-get install -y google-chrome-stable
fi

# ------------------------------------------------------------------ KasmVNC
passo "KasmVNC (a tela no navegador)"
if ! dpkg -s kasmvncserver >/dev/null 2>&1; then
  URL=$(curl -fsSL https://api.github.com/repos/kasmtech/KasmVNC/releases/latest \
    | jq -r '.assets[].browser_download_url' | grep -E 'kasmvncserver_noble_.*_amd64\.deb$' | head -1)
  [ -n "$URL" ] || { echo "Não achei o pacote do KasmVNC para noble."; exit 1; }
  wget -qO /tmp/kasmvnc.deb "$URL"
  apt-get install -y /tmp/kasmvnc.deb
  rm -f /tmp/kasmvnc.deb
fi
install -d -o "$USUARIO" -g "$USUARIO" "$CASA/.vnc"
cat > "$CASA/.vnc/kasmvnc.yaml" <<YAML
network:
  protocol: http
  interface: 0.0.0.0
  websocket_port: $PORTA_TELA
  ssl:
    require_ssl: true
desktop:
  resolution:
    width: 1600
    height: 900
  allow_resize: true
YAML
cat > "$CASA/.vnc/xstartup" <<'XS'
#!/bin/sh
unset SESSION_MANAGER
unset DBUS_SESSION_BUS_ADDRESS
exec dbus-launch --exit-with-session startxfce4
XS
chmod +x "$CASA/.vnc/xstartup"
touch "$CASA/.vnc/.de-was-selected"
chown -R "$USUARIO:$USUARIO" "$CASA/.vnc"
if [ -n "${SENHA_TELA:-}" ]; then
  printf '%s\n%s\n' "$SENHA_TELA" "$SENHA_TELA" | sudo -u "$USUARIO" -H kasmvncpasswd -u "$USUARIO" -w "$CASA/.kasmpasswd"
  unset SENHA_TELA SENHA_TELA2
fi

# Chrome abre sozinho com a área de trabalho
install -d -o "$USUARIO" -g "$USUARIO" "$CASA/.config/autostart"
cat > "$CASA/.config/autostart/google-chrome.desktop" <<'DT'
[Desktop Entry]
Type=Application
Name=Google Chrome
Exec=google-chrome --no-first-run --no-default-browser-check --password-store=basic
X-GNOME-Autostart-enabled=true
DT
chown -R "$USUARIO:$USUARIO" "$CASA/.config"

# ------------------------------------------------------------------ Go, uv, Claude Code
passo "Go $GO_VERSAO"
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_VERSAO"; then
  rm -rf /usr/local/go
  curl -fsSL "https://go.dev/dl/go$GO_VERSAO.linux-amd64.tar.gz" | tar -C /usr/local -xz
fi
echo 'export PATH=/usr/local/go/bin:$HOME/.local/bin:$PATH' > /etc/profile.d/lumnia.sh

passo "uv e Claude Code (como $USUARIO)"
como 'command -v uv >/dev/null || curl -LsSf https://astral.sh/uv/install.sh | sh'
como 'test -x ~/.local/bin/claude || curl -fsSL https://claude.ai/install.sh | bash'

# ------------------------------------------------------------------ Lumnia Zap
passo "Lumnia Zap (branch $BRANCH)"
if [ -d "$DESTINO/.git" ]; then
  como "git -C '$DESTINO' fetch --depth 1 origin '$BRANCH' && git -C '$DESTINO' reset --hard FETCH_HEAD"
else
  como "git clone --depth 1 --branch '$BRANCH' '$REPO' '$DESTINO'"
fi
VERSAO=$(como "git -C '$DESTINO' log -1 --format=%h")
como "cd '$DESTINO/whatsapp-bridge' && CGO_ENABLED=1 /usr/local/go/bin/go build -trimpath \
  -ldflags='-X main.versao=nuvem-$VERSAO' -o whatsapp-bridge ."
como "cd '$DESTINO/whatsapp-mcp-server' && ~/.local/bin/uv sync -q"

# ------------------------------------------------------------------ serviços
passo "Serviços do sistema"
cat > /etc/systemd/system/lumnia-tela.service <<UNIT
[Unit]
Description=Tela do Beatitude (KasmVNC + Xfce)
After=network-online.target
Wants=network-online.target

[Service]
User=$USUARIO
Environment=HOME=$CASA
ExecStartPre=-/usr/bin/vncserver -kill :1
ExecStart=/usr/bin/vncserver :1 -fg -select-de xfce
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

cat > /etc/systemd/system/lumnia-zap.service <<UNIT
[Unit]
Description=Lumnia Zap (ponte do WhatsApp e mordomo do Beatitude)
After=network-online.target lumnia-tela.service
Wants=network-online.target

[Service]
User=$USUARIO
WorkingDirectory=$DESTINO/whatsapp-bridge
Environment=HOME=$CASA
Environment=DISPLAY=:1
Environment=PATH=$CASA/.local/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=$DESTINO/whatsapp-bridge/whatsapp-bridge
Restart=always
RestartSec=5
StandardOutput=append:$DESTINO/whatsapp-bridge/store/bridge.log
StandardError=append:$DESTINO/whatsapp-bridge/store/bridge.log

[Install]
WantedBy=multi-user.target
UNIT
como "mkdir -p '$DESTINO/whatsapp-bridge/store'"

systemctl daemon-reload
systemctl enable lumnia-tela.service lumnia-zap.service
systemctl restart lumnia-tela.service
sleep 3
systemctl restart lumnia-zap.service

# ------------------------------------------------------------------ conferência
passo "Conferindo"
sleep 5
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31m✗\033[0m %s\n' "$*"; }
systemctl is-active --quiet lumnia-tela && ok "tela no ar" || falha "tela parada (journalctl -u lumnia-tela)"
systemctl is-active --quiet lumnia-zap && ok "Zap no ar" || falha "Zap parado (tail $DESTINO/whatsapp-bridge/store/bridge.log)"
curl -fsS -o /dev/null http://127.0.0.1:8080/ && ok "painel do Zap responde" || falha "painel do Zap não respondeu ainda"
como 'test -x ~/.local/bin/claude' && ok "Claude Code instalado" || falha "Claude Code ausente"
IP=$(curl -fsS https://api.ipify.org || hostname -I | awk '{print $1}')
echo
echo "Pronto. A tela abre em https://$IP:$PORTA_TELA (usuário $USUARIO e a senha escolhida)."
echo "O certificado é próprio: o navegador vai avisar na primeira vez."
echo "Dentro da tela: Chrome já aberto; painel do Zap em http://localhost:8080."
