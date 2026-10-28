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
PESSOA=${LUMNIA_PESSOA:-}   # ex.: LUMNIA_PESSOA="o Douglas" — nome usado nos avisos do verificador
SUPORTE=${LUMNIA_SUPORTE_WHATSAPP:-}   # WhatsApp do suporte da Lumnia (só dígitos, com 55) — destino do botão "Liberar suporte"
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

# ------------------------------------------------------------------ pessoa
if [ -z "$PESSOA" ] && [ ! -f "$CASA/.config/lumnia/pessoa" ]; then
  echo
  read -rp "De quem é este computador? (com artigo, ex.: \"a Lisiane\", \"o Douglas\"): " PESSOA
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
  liblist-moreutils-perl libswitch-perl libtry-tiny-perl \
  zenity xclip libnotify-bin libglib2.0-bin gvfs

# ------------------------------------------------------------------ usuário
passo "Usuário $USUARIO"
id "$USUARIO" >/dev/null 2>&1 || useradd -m -s /bin/bash "$USUARIO"
# Se a pasta já existia (criada à mão como root antes deste script), o useradd -m não
# a cria nem troca o dono — e aí a senha da tela e o uv falham com "Permission denied".
# Garante que a casa é da pessoa, sempre.
install -d -o "$USUARIO" -g "$USUARIO" "$CASA"
chown -R "$USUARIO:$USUARIO" "$CASA"
usermod -aG ssl-cert "$USUARIO"
loginctl enable-linger "$USUARIO" || true
como() { sudo -u "$USUARIO" -H bash -lc "$*"; }

if [ -n "$PESSOA" ]; then
  install -d -o "$USUARIO" -g "$USUARIO" "$CASA/.config/lumnia"
  printf '%s\n' "$PESSOA" > "$CASA/.config/lumnia/pessoa"
  chown "$USUARIO:$USUARIO" "$CASA/.config/lumnia/pessoa"
fi
if [ -n "$SUPORTE" ]; then
  install -d -o "$USUARIO" -g "$USUARIO" "$CASA/.config/lumnia"
  printf '%s\n' "$SUPORTE" | tr -dc '0-9\n' > "$CASA/.config/lumnia/suporte_para"
  chown "$USUARIO:$USUARIO" "$CASA/.config/lumnia/suporte_para"
fi

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
    | jq -r '.assets[].browser_download_url' | grep -E 'kasmvncserver_(ubuntu_)?noble_.*_amd64\.deb$' | head -1 || true)
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

# ------------------------------------------------------------------ botão de suporte (da máquina, não do Zap)
passo "Ícones Liberar suporte / Encerrar suporte"
# Programa da máquina, do root: a pessoa executa, não altera.
install -o root -g root -m 0755 "$DESTINO/scripts/nuvem/suporte.sh" /usr/local/bin/lumnia-suporte
MESA=$CASA/Desktop
install -d -o "$USUARIO" -g "$USUARIO" "$MESA" "$CASA/.config/autostart"
icone() { # arquivo nome comando ícone comentário
  printf '[Desktop Entry]\nType=Application\nName=%s\nComment=%s\nExec=/usr/local/bin/lumnia-suporte %s\nIcon=%s\nTerminal=false\n' \
    "$2" "$5" "$3" "$4" > "$MESA/$1"
  chmod 0755 "$MESA/$1"; chown "$USUARIO:$USUARIO" "$MESA/$1"
  # Xfce 4.18 pede confirmação para atalho "não confiável"; marca como confiável (precisa do gvfs).
  sudo -u "$USUARIO" -H dbus-launch gio set -t string "$MESA/$1" metadata::xfce-exe-checksum \
    "$(sha256sum "$MESA/$1" | cut -d' ' -f1)" 2>/dev/null || true
}
icone lumnia-suporte-liberar.desktop "Liberar suporte" liberar help-browser \
  "Cria um acesso temporário (2 h) para o suporte da Lumnia"
icone lumnia-suporte-encerrar.desktop "Encerrar suporte" encerrar process-stop \
  "Apaga o acesso de suporte agora"
# Ao abrir a tela (inclusive depois de reiniciar), apaga acesso de suporte vencido.
printf '[Desktop Entry]\nType=Application\nName=Beatitude: limpar suporte vencido\nExec=/usr/local/bin/lumnia-suporte limpar\nNoDisplay=true\nX-GNOME-Autostart-enabled=true\n' \
  > "$CASA/.config/autostart/lumnia-suporte-limpar.desktop"
chown -R "$USUARIO:$USUARIO" "$CASA/.config/autostart"
# Endereço que vai na mensagem de suporte
IP_TELA=$(curl -fsS -m 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
install -d -o "$USUARIO" -g "$USUARIO" "$CASA/.config/lumnia"
[ -s "$CASA/.config/lumnia/endereco" ] || echo "https://$IP_TELA:$PORTA_TELA" > "$CASA/.config/lumnia/endereco"
chown -R "$USUARIO:$USUARIO" "$CASA/.config/lumnia"

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

# Rodada da manhã do Beatitude (pessoas -> brief -> ações), na própria máquina.
cat > /etc/systemd/system/lumnia-manha.service <<UNIT
[Unit]
Description=Beatitude: rodada da manhã (pessoas, brief, ações)
After=network-online.target lumnia-zap.service

[Service]
Type=oneshot
User=$USUARIO
Environment=HOME=$CASA
Environment=DISPLAY=:1
ExecStart=$DESTINO/scripts/nuvem/rotina-manha.sh
UNIT

cat > /etc/systemd/system/lumnia-manha.timer <<UNIT
[Unit]
Description=Beatitude: rodada da manhã às 6h45

[Timer]
OnCalendar=*-*-* 06:45:00
Persistent=true

[Install]
WantedBy=timers.target
UNIT
chmod +x "$DESTINO/scripts/nuvem/rotina-manha.sh"

# Verificador: a cada 12 h confere as peças e avisa o administrador por e-mail.
cat > /etc/systemd/system/lumnia-verificar.service <<UNIT
[Unit]
Description=Beatitude: verificador do computador (avisa por e-mail se algo cair)
After=network-online.target lumnia-zap.service

[Service]
Type=oneshot
User=$USUARIO
Environment=HOME=$CASA
Environment=DISPLAY=:1
ExecStart=$DESTINO/scripts/nuvem/verificar.sh
UNIT

cat > /etc/systemd/system/lumnia-verificar.timer <<UNIT
[Unit]
Description=Beatitude: verificador às 8h10 e 20h10

[Timer]
OnCalendar=*-*-* 08,20:10:00
Persistent=true

[Install]
WantedBy=timers.target
UNIT
chmod +x "$DESTINO/scripts/nuvem/verificar.sh"

# O terminal da tela não é shell de login: o PATH vai no .bashrc.
grep -q 'lumnia: PATH' "$CASA/.bashrc" 2>/dev/null || \
  echo 'export PATH="$HOME/.local/bin:/usr/local/go/bin:$PATH" # lumnia: PATH' >> "$CASA/.bashrc"

systemctl daemon-reload
systemctl enable lumnia-tela.service lumnia-zap.service lumnia-manha.timer lumnia-verificar.timer
# A tela só reinicia se estiver parada: reiniciar derruba a sessão (e o Chrome) da pessoa.
systemctl is-active --quiet lumnia-tela.service || systemctl restart lumnia-tela.service
sleep 3
systemctl restart lumnia-zap.service
systemctl start lumnia-manha.timer lumnia-verificar.timer

# ------------------------------------------------------------------ conferência
passo "Conferindo"
sleep 5
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31m✗\033[0m %s\n' "$*"; }
systemctl is-active --quiet lumnia-tela && ok "tela no ar" || falha "tela parada (journalctl -u lumnia-tela)"
systemctl is-active --quiet lumnia-zap && ok "Zap no ar" || falha "Zap parado (tail $DESTINO/whatsapp-bridge/store/bridge.log)"
curl -fsS -o /dev/null http://127.0.0.1:8080/ && ok "painel do Zap responde" || falha "painel do Zap não respondeu ainda"
como 'test -x ~/.local/bin/claude' && ok "Claude Code instalado" || falha "Claude Code ausente"
systemctl is-active --quiet lumnia-manha.timer && ok "rodada da manhã agendada ($(systemctl show -p NextElapseUSecRealtime --value lumnia-manha.timer))" || falha "rodada da manhã sem agendamento"
systemctl is-active --quiet lumnia-verificar.timer && ok "verificador agendado ($(systemctl show -p NextElapseUSecRealtime --value lumnia-verificar.timer))" || falha "verificador sem agendamento"
IP=$(curl -fsS https://api.ipify.org || hostname -I | awk '{print $1}')
echo
echo "Pronto. A tela abre em https://$IP:$PORTA_TELA (usuário $USUARIO e a senha escolhida)."
echo "O certificado é próprio: o navegador vai avisar na primeira vez."
echo "Dentro da tela: Chrome já aberto; painel do Zap em http://localhost:8080."
echo "Ícones na área de trabalho: Liberar suporte / Encerrar suporte."
[ -s "$CASA/.config/lumnia/suporte_para" ] || echo "Atenção: sem LUMNIA_SUPORTE_WHATSAPP, o botão só copia a mensagem (não manda pelo WhatsApp)."
