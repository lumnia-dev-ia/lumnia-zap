#!/usr/bin/env bash
# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
#
# verificar.sh — o verificador do computador do Beatitude na nuvem.
#
# Roda como o usuário `beatitude`, pelo timer lumnia-verificar.timer (8h10 e 20h10).
# Confere as peças que caem sozinhas e, se alguma caiu, avisa o administrador
# por e-mail pelo servidor do Beatitude (POST /api/ponte/alerta, token da ponte).
# Só viajam nome da peça, estado e o que fazer: nenhum conteúdo de mensagem.
#
#   verificar.sh            confere e avisa se algo caiu
#   verificar.sh --teste    manda o e-mail de teste (confere a entrega)
#   verificar.sh --seco     confere e mostra, sem avisar ninguém

set -uo pipefail

CASA=${HOME:-/home/beatitude}
ZAP=$CASA/lumnia-zap
STORE=$ZAP/whatsapp-bridge/store
LOGS=$CASA/rotinas/logs
mkdir -p "$LOGS"
export PATH="$CASA/.local/bin:/usr/local/bin:/usr/bin:/bin"
export DISPLAY=${DISPLAY:-:1}
PAINEL=http://127.0.0.1:8080
MODO=${1:-}
# Nome da pessoa dona da máquina (gravado pelo instalar.sh em ~/.config/lumnia/pessoa).
PESSOA=$(cat "$CASA/.config/lumnia/pessoa" 2>/dev/null || echo "a pessoa")

ITENS=$(mktemp)
trap 'rm -f "$ITENS"' EXIT
caiu() { printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$ITENS"; }

TELA="Abrir a tela da máquina no navegador (endereço e senha no relatório de instalação)"

# 1. Serviços
systemctl is-active --quiet lumnia-tela || caiu "tela" "serviço parado" "Como root na máquina: systemctl restart lumnia-tela (o Chrome reabre sozinho)."
if ! systemctl is-active --quiet lumnia-zap; then
  caiu "lumnia zap" "serviço parado" "Como root na máquina: systemctl restart lumnia-zap; se repetir, ver whatsapp-bridge/store/bridge.log."
else
  # 2. WhatsApp
  st=$(curl -fsS -m 10 "$PAINEL/api/panel/status" 2>/dev/null || true)
  if [ -z "$st" ]; then
    caiu "painel do zap" "não respondeu" "Como root: systemctl restart lumnia-zap."
  else
    conectado=$(printf '%s' "$st" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("sim" if d.get("connected") and not d.get("needs_pair") else ("qr" if d.get("needs_pair") else "nao"))' 2>/dev/null || echo nao)
    case $conectado in
      qr)  caiu "whatsapp" "pede novo pareamento" "$TELA; em localhost:8080 clicar Reconectar e $PESSOA lê o QR no celular (Aparelhos conectados)." ;;
      nao) caiu "whatsapp" "desconectado" "$TELA; em localhost:8080 clicar Reconectar. Se pedir QR, $PESSOA lê no celular." ;;
    esac
  fi
  # 3. Beatitude ligado e Claude achado pela ponte
  bt=$(curl -fsS -m 10 "$PAINEL/api/panel/beatitude" 2>/dev/null || true)
  if [ -n "$bt" ]; then
    printf '%s' "$bt" | python3 -c 'import sys,json;d=json.load(sys.stdin);sys.exit(0 if d.get("ligado") else 1)' 2>/dev/null \
      || caiu "beatitude no zap" "desligado" "$TELA; em localhost:8080, aba Beatitude, ligar de novo no grupo Beatitude."
    printf '%s' "$bt" | python3 -c 'import sys,json;d=json.load(sys.stdin);sys.exit(0 if d.get("claude_ok") else 1)' 2>/dev/null \
      || caiu "claude code" "a ponte não acha o claude" "Na tela, terminal: claude --version; se faltar, rodar de novo o instalar.sh como root."
  fi
fi

# 4. Chrome aberto na tela (o mordomo usa para Outlook e sites)
pgrep -u "$(id -u)" -f '/opt/google/chrome/chrome' >/dev/null \
  || caiu "chrome" "fechado" "$TELA e abrir o Chrome (ícone na área de trabalho). Conferir se a extensão Claude segue conectada."

# 5. Claude Code logado
resp=$(cd "$CASA" && timeout 120 claude -p "Responda só OK." --model haiku 2>&1 | tail -c 300)
case $resp in
  *OK*) : ;;
  *) caiu "login do claude" "não respondeu" "Na tela, terminal: claude, depois /login com a conta de $PESSOA." ;;
esac

# 6. MCP do Beatitude
mcp=$(cd "$CASA" && timeout 90 claude mcp list 2>/dev/null | grep -i '^beatitude' || true)
case $mcp in
  *Connected*|*✓*) : ;;
  "") caiu "mcp beatitude" "não está configurado" "Na tela, terminal: claude mcp add --scope user --transport http beatitude https://beatitude.lumnia.dev/mcp e autorizar." ;;
  *) caiu "mcp beatitude" "sem conexão ou sem autorização" "Na tela, terminal: claude, depois /mcp, escolher beatitude e Autenticar com a conta de $PESSOA." ;;
esac

# 7. Rodada da manhã
if [ -s "$LOGS/historico.log" ]; then
  ultima=$(grep ' manha ' "$LOGS/historico.log" | tail -1)
  dia=${ultima%% *}
  cod=$(printf '%s' "$ultima" | sed -n 's/.*codigo=\([0-9]*\).*/\1/p')
  idade=$(( ( $(date +%s) - $(date -d "$dia" +%s 2>/dev/null || echo 0) ) / 86400 ))
  [ -n "$cod" ] && [ "$cod" != 0 ] && caiu "rodada da manhã" "terminou com erro (código $cod em $dia)" "Ver ~/rotinas/logs/manha-$dia.json e manha-erros.log; rodar à mão: systemctl start lumnia-manha (como root)."
  [ "$idade" -ge 2 ] && caiu "rodada da manhã" "não roda desde $dia" "Como root: systemctl status lumnia-manha.timer; systemctl start lumnia-manha.timer."
fi

# 8. Disco
uso=$(df --output=pcent "$CASA" | tail -1 | tr -dc 0-9)
[ "${uso:-0}" -ge 90 ] && caiu "disco" "${uso}% ocupado" "Limpar logs antigos em ~/rotinas/logs e whatsapp-bridge/store; ou aumentar o plano."

# ---------------------------------------------------------------- resultado
n=$(wc -l < "$ITENS" | tr -d ' ')
echo "$(date '+%F %T') verificar falhas=$n $(cut -f1 "$ITENS" | paste -sd, -)" >> "$LOGS/historico.log"
[ -s "$ITENS" ] && column -t -s $'\t' "$ITENS" 2>/dev/null || true

[ "$MODO" = "--seco" ] && exit 0
[ "$MODO" != "--teste" ] && [ "$n" = 0 ] && exit 0

# Aviso pelo servidor do Beatitude
python3 - "$STORE/beatitude-ponte.json" "$ITENS" "$MODO" "$PESSOA" <<'PY'
import json, sys, socket, urllib.request
cfg_path, itens_path, modo, pessoa = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
try:
    cfg = json.load(open(cfg_path))
except Exception:
    print("sem ponte pareada: alerta não enviado"); sys.exit(0)
url = (cfg.get("url") or "https://beatitude.lumnia.dev").rstrip("/") + "/api/ponte/alerta"
versao = "nuvem"
try:
    versao = json.load(urllib.request.urlopen("http://127.0.0.1:8080/api/panel/status", timeout=10)).get("version") or versao
except Exception:
    pass
if modo == "--teste":
    corpo = {"teste": True, "maquina": "Computador na nuvem de " + pessoa + " (" + socket.gethostname() + ")", "versao": versao}
else:
    itens = []
    for linha in open(itens_path, encoding="utf-8"):
        p = linha.rstrip("\n").split("\t")
        if len(p) == 3:
            itens.append({"peca": p[0][:40], "estado": p[1][:80], "fazer": p[2][:300]})
    corpo = {"maquina": "Computador na nuvem de " + pessoa + " (" + socket.gethostname() + ")", "versao": versao, "itens": itens[:10]}
req = urllib.request.Request(url, data=json.dumps(corpo).encode(), method="POST",
    # User-Agent próprio: o firewall do site recusa o "Python-urllib" padrão (403).
    headers={"Content-Type": "application/json", "Authorization": "Bearer " + cfg["token"],
             "User-Agent": "lumnia-zap-verificador/1 (" + versao + ")"})
try:
    print("alerta:", urllib.request.urlopen(req, timeout=30).read().decode())
except Exception as e:
    print("alerta falhou:", e)
PY
exit 0
