#!/usr/bin/env bash
# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
#
# atualizar.sh — o computador do Beatitude na nuvem se atualiza sozinho.
#
# Roda como root, de madrugada, pelo lumnia-atualizar.timer (instalar.sh).
# A máquina da nuvem compila o Zap do código (não usa a release assinada do
# Mac e do Windows), então atualizar é: buscar a branch, compilar, trocar o
# binário e reiniciar. Sem Apple, sem Microsoft, sem ninguém tocar.
#
# Seguro por construção:
#   - compila num arquivo à parte; se a compilação falhar, nada muda;
#   - guarda o binário anterior e o commit anterior;
#   - depois de reiniciar, confere se o serviço subiu e o painel responde;
#     se não, volta ao binário e ao commit anteriores e reinicia de novo;
#   - não reinicia com mensagem agendada para os próximos 10 minutos;
#   - não mexe na sessão do WhatsApp nem nas configurações (store/ não é do git).
#
# Para segurar uma máquina numa versão: touch /home/beatitude/.config/lumnia/sem-atualizacao
# Para rodar agora, na mão:              systemctl start lumnia-atualizar.service

set -euo pipefail

USUARIO=beatitude
CASA=/home/$USUARIO
DESTINO=$CASA/lumnia-zap
PONTE=$DESTINO/whatsapp-bridge
CONF=$CASA/.config/lumnia
LOG=$CONF/atualizacao.log

mkdir -p "$CONF"
log() { printf '%s %s\n' "$(date '+%F %T')" "$*" | tee -a "$LOG"; }
como() { sudo -u "$USUARIO" -H bash -lc "$1"; }

[ "$(id -u)" = 0 ] || { echo "Rode como root."; exit 1; }
[ -f "$CONF/sem-atualizacao" ] && { log "segurada por $CONF/sem-atualizacao; nada feito"; exit 0; }
[ -d "$DESTINO/.git" ] || { log "sem $DESTINO; rode o instalar.sh"; exit 1; }

BRANCH=$(cat "$CONF/branch" 2>/dev/null || como "git -C '$DESTINO' rev-parse --abbrev-ref HEAD")
ANTES=$(como "git -C '$DESTINO' rev-parse HEAD")
como "git -C '$DESTINO' fetch -q --depth 1 origin '$BRANCH'"
DEPOIS=$(como "git -C '$DESTINO' rev-parse FETCH_HEAD")
if [ "$ANTES" = "$DEPOIS" ]; then
  log "em dia ($BRANCH ${ANTES:0:7})"
  exit 0
fi

# Mensagem agendada para os próximos 10 minutos: deixa para a próxima rodada.
BANCO=$PONTE/store/messages.db
if [ -f "$BANCO" ]; then
  PROXIMAS=$(sqlite3 "$BANCO" "SELECT COUNT(*) FROM panel_schedules WHERE enabled = 1 AND mode = 'once'
    AND on_date || ' ' || at_time BETWEEN strftime('%Y-%m-%d %H:%M', 'now', 'localtime')
    AND strftime('%Y-%m-%d %H:%M', 'now', 'localtime', '+10 minutes');" 2>/dev/null || echo 0)
  if [ "${PROXIMAS:-0}" -gt 0 ]; then
    log "mensagem agendada nos próximos 10 min; atualização adiada"
    exit 0
  fi
fi

voltar() {
  log "voltando para ${ANTES:0:7}: $1"
  como "git -C '$DESTINO' reset -q --hard '$ANTES'"
  if [ -f "$PONTE/whatsapp-bridge.anterior" ]; then
    como "cp -p '$PONTE/whatsapp-bridge.anterior' '$PONTE/whatsapp-bridge'"
  fi
  systemctl restart lumnia-zap.service || true
  exit 1
}

log "atualizando $BRANCH ${ANTES:0:7} → ${DEPOIS:0:7}"
como "git -C '$DESTINO' reset -q --hard FETCH_HEAD"
VERSAO=${DEPOIS:0:7}

# Go: o código novo pode pedir um Go mais novo (1.1.4: whatsmeow de nov/2026 →
# Go 1.26). Instala o "toolchain" do go.mod antes de compilar. Se a troca
# falhar, o Go antigo fica e o próprio go build tenta baixar o que falta.
GO_QUER=$(sed -n 's/^toolchain go\([0-9.]*\)$/\1/p' "$PONTE/go.mod")
if [ -n "$GO_QUER" ] && ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_QUER "; then
  log "instalando Go $GO_QUER"
  TMPGO=$(mktemp -d)
  if curl -fsSL "https://go.dev/dl/go$GO_QUER.linux-amd64.tar.gz" | tar -C "$TMPGO" -xz &&
     "$TMPGO/go/bin/go" version >/dev/null 2>&1; then
    rm -rf /usr/local/go.anterior
    mv /usr/local/go /usr/local/go.anterior 2>/dev/null || true
    mv "$TMPGO/go" /usr/local/go
  else
    log "aviso: não consegui baixar o Go $GO_QUER; sigo com $(/usr/local/go/bin/go version 2>/dev/null)"
  fi
  rm -rf "$TMPGO"
fi
if ! como "cd '$PONTE' && CGO_ENABLED=1 /usr/local/go/bin/go build -trimpath \
     -ldflags='-X main.versao=nuvem-$VERSAO' -o whatsapp-bridge.novo ." >>"$LOG" 2>&1; then
  como "rm -f '$PONTE/whatsapp-bridge.novo'"
  voltar "a compilação falhou (detalhes acima no log)"
fi
como "cd '$DESTINO/whatsapp-mcp-server' && ~/.local/bin/uv sync -q" >>"$LOG" 2>&1 || log "aviso: uv sync falhou; segue com o ambiente anterior"

# troca por rename (o binário em uso não é sobrescrito no lugar)
como "cp -p '$PONTE/whatsapp-bridge' '$PONTE/whatsapp-bridge.anterior' 2>/dev/null || true"
como "mv -f '$PONTE/whatsapp-bridge.novo' '$PONTE/whatsapp-bridge'"
chmod +x "$DESTINO"/scripts/nuvem/*.sh 2>/dev/null || true
systemctl restart lumnia-zap.service

# confere: serviço ativo e painel respondendo, em até 60 s
for _ in $(seq 1 12); do
  sleep 5
  if systemctl is-active --quiet lumnia-zap.service &&
     curl -fsS --max-time 4 http://127.0.0.1:8080/api/panel/status >/dev/null 2>&1; then
    log "ok: nuvem-$VERSAO no ar"
    exit 0
  fi
done
voltar "o Zap não respondeu depois de reiniciar"
