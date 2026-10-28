#!/usr/bin/env bash
# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
#
# suporte.sh — o botão "Liberar suporte" da tela do computador na nuvem.
#
# A senha da tela é só da pessoa. Quando ela quer ajuda, toca no ícone
# "Liberar suporte": nasce um usuário de tela `suporte` com senha nova, que
# vale por algumas horas e vai para o suporte da Lumnia pelo WhatsApp dela.
# Quem entra como `suporte` vê e mexe na MESMA tela que ela, ao vivo.
#
#   suporte.sh liberar [horas]   cria o acesso (padrão 2 h) e manda ao suporte
#   suporte.sh encerrar          apaga o acesso agora
#   suporte.sh limpar            apaga se já venceu (roda ao abrir a tela e no fim do prazo)
#   suporte.sh estado            mostra se há acesso liberado e até quando
#
# Configuração (em ~/.config/lumnia/, gravada pelo instalar.sh):
#   pessoa           "o Douglas", "a Lisiane"…
#   suporte_para     WhatsApp do suporte, só dígitos com DDI (ex.: 5511999999999)
#   endereco         opcional: endereço da tela (sem isso, usa o IP público)
#
# Limite: apagar o usuário barra entradas novas; quem já está conectado só cai
# ao fechar a aba ou quando a tela reinicia.

set -uo pipefail

CASA=${HOME:-/home/beatitude}
CFG=$CASA/.config/lumnia
SENHAS=$CASA/.kasmpasswd
ATE=$CFG/suporte_ate
LOG=$CASA/rotinas/logs/suporte.log
PORTA=8444
EU=$(readlink -f "$0")
mkdir -p "$CFG" "$(dirname "$LOG")"
export DISPLAY=${DISPLAY:-:1}
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}

PESSOA=$(cat "$CFG/pessoa" 2>/dev/null || echo "a pessoa")
PARA=$(tr -dc 0-9 2>/dev/null < "$CFG/suporte_para" || true)

registrar() { echo "$(date '+%F %T') $*" >> "$LOG"; }

janela() { # tipo (info|warning) título texto
  [ -n "${SILENCIOSO:-}" ] && { printf '%s\n%s\n' "$2" "$3"; return; }
  if command -v zenity >/dev/null; then
    zenity --"$1" --title="$2" --text="$3" --no-markup --width=520 2>/dev/null || true
  else
    printf '%s\n%s\n' "$2" "$3"
  fi
}

endereco() {
  local e
  e=$(cat "$CFG/endereco" 2>/dev/null || true)
  [ -n "$e" ] && { echo "$e"; return; }
  e=$(curl -fsS -m 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
  echo "https://$e:$PORTA"
}

tem_suporte() { grep -q '^suporte:' "$SENHAS" 2>/dev/null; }

encerrar() { # motivo
  tem_suporte && kasmvncpasswd -u suporte -d "$SENHAS" >/dev/null 2>&1
  rm -f "$ATE"
  systemctl --user stop lumnia-suporte-fim.timer lumnia-suporte-fim.service >/dev/null 2>&1 || true
  registrar "suporte encerrado ($1)"
}

case ${1:-} in
  liberar)
    horas=${2:-2}
    case $horas in ''|*[!0-9]*) horas=2 ;; esac
    # 12 letras e números sem os que confundem (0/o, 1/l/i), em blocos de 4
    senha=$(tr -dc 'abcdefghjkmnpqrstuvwxyz23456789' < /dev/urandom | head -c 12 | sed 's/.\{4\}/&-/g; s/-$//')
    if ! printf '%s\n%s\n' "$senha" "$senha" | kasmvncpasswd -u suporte -w "$SENHAS" >/dev/null 2>&1; then
      janela warning "Suporte" "Não consegui criar o acesso de suporte. Avise a Lumnia por mensagem."
      registrar "falha ao criar o acesso"; exit 1
    fi
    fim=$(( $(date +%s) + horas * 3600 ))
    echo "$fim" > "$ATE"
    ate=$(date -d "@$fim" '+%Hh%M')
    systemctl --user stop lumnia-suporte-fim.timer lumnia-suporte-fim.service >/dev/null 2>&1 || true
    systemd-run --user --unit=lumnia-suporte-fim --on-active="${horas}h" "$EU" limpar >/dev/null 2>&1 || true

    texto="${PESSOA^} liberou o suporte no computador do Beatitude até as $ate.
Endereço: $(endereco)
Usuário: suporte
Senha: $senha
O acesso some sozinho às $ate, ou antes se $PESSOA tocar em Encerrar suporte."

    enviado=nao
    if [ -n "$PARA" ]; then
      corpo=$(python3 -c 'import json,sys;print(json.dumps({"recipient":sys.argv[1],"message":sys.argv[2]}))' "$PARA" "$texto")
      curl -fsS -m 25 -X POST -H 'Content-Type: application/json' -d "$corpo" \
        http://127.0.0.1:8080/api/send 2>/dev/null | grep -q '"success":true' && enviado=sim
    fi
    command -v xclip >/dev/null && printf '%s' "$texto" | xclip -selection clipboard >/dev/null 2>&1
    registrar "suporte liberado até $ate (enviado pelo WhatsApp: $enviado)"

    if [ "$enviado" = sim ]; then
      janela info "Suporte liberado" "Pronto. A Lumnia recebeu pelo seu WhatsApp o acesso de suporte, que vale até as $ate.

Quem entrar vai ver e mexer nesta mesma tela, ao vivo, junto com você.
Para cortar antes, toque em Encerrar suporte."
    else
      janela warning "Suporte liberado" "O acesso de suporte vale até as $ate, mas não consegui mandar pelo WhatsApp.
A mensagem abaixo já está copiada: cole numa conversa com a Lumnia.

$texto"
    fi
    ;;

  encerrar)
    if tem_suporte; then
      encerrar "pela pessoa"
      janela info "Suporte encerrado" "Pronto. O acesso de suporte foi apagado. Só a sua senha abre esta tela agora."
    else
      rm -f "$ATE"
      janela info "Suporte" "Não há acesso de suporte liberado. Só a sua senha abre esta tela."
    fi
    ;;

  limpar)
    if tem_suporte; then
      fim=$(cat "$ATE" 2>/dev/null || echo 0)
      [ "$(date +%s)" -ge "${fim:-0}" ] && encerrar "venceu"
    else
      rm -f "$ATE"
    fi
    ;;

  estado)
    if tem_suporte; then
      fim=$(cat "$ATE" 2>/dev/null || echo 0)
      echo "suporte liberado até $(date -d "@$fim" '+%F %H:%M')"
    else
      echo "sem suporte liberado"
    fi
    ;;

  *)
    echo "uso: suporte.sh liberar [horas] | encerrar | limpar | estado"; exit 2 ;;
esac
