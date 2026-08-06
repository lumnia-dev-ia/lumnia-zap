#!/bin/bash
# Diagnostico do Lumnia Zap no macOS.
# Roda como o proprio usuario, nao pede senha de administrador.
# NAO instala, NAO apaga e NAO altera nada: so olha, testa e anota.
#
# A unica coisa que ele mexe e o servico: para a ponte por ~30 segundos para
# poder rodar ela na mao e capturar o erro de verdade, e religa no final.
#
# Copyright (c) 2026 Lumnia - Diego Penna Moreira
# SPDX-License-Identifier: MIT

RAIZ="$HOME/Library/Application Support/LumniaZap"
PONTE="$RAIZ/whatsapp-bridge/whatsapp-bridge"
LABEL="dev.lumnia.zap"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
PAINEL="http://localhost:8080"
CFG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"
MYUID="$(/usr/bin/id -u)"

TMPOUT="/tmp/diagnostico-lumnia-zap-mac.txt"
CARIMBO="$(date +%Y%m%d-%H%M)"

# Bandeiras para o resumo do final.
F_PASTA=nao; F_BIN=nao; F_PLIST=nao; F_SERVICO=nao; F_DESABILITADO=nao
F_PROC=0; F_PORTA=nao; F_PAINEL=nao; F_ERRLOG=nao; F_BANCO=ok
F_MANUAL=nao; F_CLAUDE=nao; F_MCP=nao; F_ULTIMALINHA="(sem log)"

# ------------------------------------------------------------------ ajudantes

# Mostra progresso na tela mesmo com a saida redirecionada para o arquivo.
etapa() { printf '  %s\n' "$1" > /dev/tty 2>/dev/null; }

titulo() { printf '\n----- %s -----\n' "$1"; }

# Mascara telefones, JIDs e digitos longos, e tira os codigos de cor do ANSI.
ESC="$(printf '\033')"
mascarar() {
  sed -E -e "s/${ESC}\[[0-9;]*m//g" \
         -e 's/[0-9]{7,}/<numero>/g' \
         -e 's/@(s\.whatsapp\.net|lid|g\.us|broadcast)/@<jid>/g'
}

# Filtra o log da ponte: mantem so o que e diagnostico. Tira o conteudo das
# mensagens, os nomes de contato e de grupo, e o desenho do QR code.
#
# Sao tres camadas de proposito. A lista de permissao pega as linhas de
# diagnostico; a lista de proibicao derruba tudo que carrega texto de
# conversa (o `Message content:` e o `Stored message:` do whatsmeow despejam
# a mensagem inteira numa linha de INFO); e o awk descarta qualquer OUTRO
# INFO que nao esteja na lista curta de INFOs sabidamente inofensivos.
# WARN e ERROR passam porque so carregam identificadores, ja mascarados.
redigir() {
  grep -a -E '\[(Client|Client/Socket|Database|Panel|Painel)[^]]*\]|Starting WhatsApp client|Ponte iniciada|REST (API )?server|Painel disponivel|panic:|fatal error|runtime error|signal (SIGSEGV|SIGABRT)|goroutine [0-9]+|\.go:[0-9]+|Escaneie|Disconnecting' \
  | grep -a -v -E 'Message content:|Stored message:|Using existing chat name|Using contact name|Getting name for contact|Getting name for group|Using group name|Resolved LID|chat name for|\[(document|image|video|audio|sticker|gif|contact|location):' \
  | awk '/\[Client INFO\]/ && !/Starting WhatsApp client|Painel|agendador|Successfully authenticated|Connected to WhatsApp|device removed|LoggedOut|logged out|key request|Disconnect|Stored [0-9]|duplicate contacts|Successfully paired|Logged in/ { next } { print }' \
  | grep -a -v '[█▀▄]' \
  | mascarar
}

# Roda um comando com limite de tempo (macOS nao tem `timeout`).
comlimite() {
  local seg="$1"; shift
  "$@" &
  local pid=$!
  local i=0
  while kill -0 "$pid" 2>/dev/null; do
    i=$((i + 1))
    if [ "$i" -gt $((seg * 4)) ]; then
      kill -9 "$pid" 2>/dev/null
      echo "(interrompido: passou de ${seg}s)"
      return 1
    fi
    sleep 0.25
  done
  wait "$pid" 2>/dev/null
}

# ------------------------------------------------------------------ coleta

coletar() {

echo "===== DIAGNOSTICO LUMNIA ZAP (macOS) ====="
echo "Gerado em: $(date '+%d/%m/%Y %H:%M:%S %Z')"
echo

titulo "0. A maquina"
echo "macOS:     $(sw_vers -productVersion 2>/dev/null) (build $(sw_vers -buildVersion 2>/dev/null))"
echo "Chip:      $(uname -m)  |  $(sysctl -n machdep.cpu.brand_string 2>/dev/null)"
echo "Modelo:    $(sysctl -n hw.model 2>/dev/null)"
echo "Usuario:   $(whoami)  (uid $MYUID)"
echo "Ligado ha: $(uptime | sed 's/^ *//')"
echo "Disco livre:"
df -h / "$HOME" 2>/dev/null | sed 's/^/  /'

titulo "1. A pasta de instalacao existe?"
if [ -d "$RAIZ" ]; then
  F_PASTA=sim
  echo "SIM: $RAIZ"
  [ -f "$RAIZ/VERSION" ] && echo "VERSION instalada: $(cat "$RAIZ/VERSION")" || echo "VERSION: arquivo ausente (instalacao anterior a v1.0.1?)"
  echo "Conteudo:"
  ls -laR "$RAIZ" 2>/dev/null | grep -v '^total' | sed 's/^/  /' | head -60
else
  echo "NAO. A pasta nao existe: $RAIZ"
  echo "Suspeita: o instalador nunca chegou a extrair o pacote."
fi

titulo "2. O executavel da ponte esta la e pode rodar?"
if [ -f "$PONTE" ]; then
  F_BIN=sim
  echo "SIM"
  ls -la "$PONTE" | sed 's/^/  /'
  [ -x "$PONTE" ] && echo "Permissao de execucao: OK" || echo "Permissao de execucao: FALTANDO (o instalador deveria ter feito chmod 755)"
  echo "Arquitetura:"
  file "$PONTE" 2>/dev/null | sed 's/^/  /'
  echo "Assinatura de codigo:"
  codesign -dv --verbose=2 "$PONTE" 2>&1 | sed 's/^/  /'
  echo "A assinatura confere?"
  codesign --verify --strict "$PONTE" 2>&1 | sed 's/^/  /' || echo "  (codesign --verify reclamou; ver acima)"
  echo "Quarentena do Gatekeeper (esperado: nenhuma):"
  xattr -l "$PONTE" 2>/dev/null | sed 's/^/  /' || echo "  nenhum atributo estendido"
  echo "Avaliacao do Gatekeeper (spctl):"
  spctl -a -t exec -vv "$PONTE" 2>&1 | sed 's/^/  /'
  echo "  Obs: 'rejected' aqui e ESPERADO - o pacote nao e notarizado, e como"
  echo "  ele nao chega pelo navegador, nao ganha quarentena e roda assim mesmo."
else
  echo "NAO. Arquivo ausente: $PONTE"
  echo "Suspeita principal: o pacote nao foi extraido (erro de download ou permissao)."
fi

titulo "3. O SERVICO ESTA REGISTRADO NO LAUNCHD?  <== secao mais importante"
if [ -f "$PLIST" ]; then
  F_PLIST=sim
  echo "plist existe: $PLIST"
  echo "Conteudo:"
  sed 's/^/  /' "$PLIST"
else
  echo "plist NAO existe: $PLIST"
  echo "Suspeita: o instalador falhou antes de configurar o servico."
fi
echo
echo "Estado do servico (launchctl print gui/$MYUID/$LABEL):"
SAIDA_PRINT="$(launchctl print "gui/$MYUID/$LABEL" 2>&1)"
if echo "$SAIDA_PRINT" | grep -qi "could not find service\|no such process"; then
  echo "  SERVICO NAO CARREGADO."
  echo "  Isto explica ponte parada + painel fora do ar + log sem linhas novas:"
  echo "  o launchd nao tem o job, entao o KeepAlive nunca reinicia nada."
else
  F_SERVICO=sim
  echo "$SAIDA_PRINT" | sed 's/^/  /' | head -60
  echo "  ..."
  echo "  Resumo:"
  echo "$SAIDA_PRINT" | grep -E "^[[:space:]]*(state|pid|program|last exit (code|status)|runs) " | sed 's/^/    /'
fi
echo
echo "O servico foi DESABILITADO alguma vez? (launchctl print-disabled)"
DISAB="$(launchctl print-disabled "gui/$MYUID" 2>/dev/null | grep -i lumnia)"
if [ -n "$DISAB" ]; then
  echo "$DISAB" | sed 's/^/  /'
  # O formato mudou entre versoes do macOS: umas dizem "=> true", outras
  # "=> disabled". "enabled" nao contem "disabled", entao nao ha falso positivo.
  case "$DISAB" in
    *true*|*disabled*) F_DESABILITADO=sim
            echo "  ATENCAO: marcado como DESABILITADO. Enquanto estiver assim, nem"
            echo "  bootstrap nem RunAtLoad sobem a ponte. Conserto:"
            echo "    launchctl enable gui/$MYUID/$LABEL" ;;
  esac
else
  echo "  nao aparece na lista de desabilitados (bom)"
fi

titulo "4. A ponte esta rodando agora? Quantas copias?"
PROCS="$(pgrep -fl "whatsapp-bridge" 2>/dev/null | grep -v pgrep)"
if [ -n "$PROCS" ]; then
  echo "$PROCS" | sed 's/^/  /'
  F_PROC="$(echo "$PROCS" | wc -l | tr -d ' ')"
  [ "$F_PROC" -gt 1 ] && echo "  ATENCAO: mais de uma copia rodando. Duas instancias na mesma sessao"
  [ "$F_PROC" -gt 1 ] && echo "  brigam pela conexao e o WhatsApp acaba derrubando o aparelho."
else
  echo "  nenhum processo whatsapp-bridge rodando"
fi

titulo "5. A porta 8080 e o painel"
LSOF="$(lsof -nP -iTCP:8080 -sTCP:LISTEN 2>/dev/null)"
if [ -n "$LSOF" ]; then
  F_PORTA=sim
  echo "$LSOF" | sed 's/^/  /'
else
  echo "  ninguem escutando na porta 8080"
fi
CODE="$(curl -s -o /dev/null -m 3 -w '%{http_code}' "$PAINEL/api/panel/status" 2>/dev/null)"
if [ "$CODE" = "200" ]; then
  F_PAINEL=sim
  echo "  painel responde: HTTP $CODE (OK)"
  echo "  status:"
  curl -s -m 3 "$PAINEL/api/panel/status" 2>/dev/null | mascarar | sed 's/^/    /' | head -20
else
  echo "  painel NAO responde (codigo '$CODE')"
fi

titulo "6. Os logs"
for f in "$RAIZ/logs/bridge.log" "$RAIZ/logs/bridge-error.log"; do
  if [ -f "$f" ]; then
    echo "  $f"
    echo "     tamanho: $(du -h "$f" 2>/dev/null | cut -f1)   linhas: $(wc -l < "$f" | tr -d ' ')   modificado: $(stat -f '%Sm' -t '%d/%m/%Y %H:%M:%S' "$f" 2>/dev/null)"
  else
    echo "  $f  -> NAO EXISTE"
  fi
done
echo
echo "  >>> bridge-error.log (stderr: e aqui que aparece panic e erro fatal) <<<"
if [ -s "$RAIZ/logs/bridge-error.log" ]; then
  F_ERRLOG=sim
  tail -120 "$RAIZ/logs/bridge-error.log" | mascarar | sed 's/^/    /'
else
  echo "    vazio ou inexistente"
fi
echo
echo "  >>> bridge.log: ultimas 100 linhas de diagnostico (sem o conteudo das mensagens) <<<"
if [ -f "$RAIZ/logs/bridge.log" ]; then
  F_ULTIMALINHA="$(tail -1 "$RAIZ/logs/bridge.log" | mascarar | cut -c1-90)"
  tail -400 "$RAIZ/logs/bridge.log" | redigir | tail -100 | sed 's/^/    /'
  echo
  echo "  >>> contagem das assinaturas conhecidas no log inteiro <<<"
  for p in "Starting WhatsApp client" "Successfully authenticated" "Keepalive timed out" "invalid use of deleted device" "rate-overlimit" "didn't find app state key" "Device logged out" "panic:"; do
    printf '    %-34s %s\n' "$p" "$(grep -ac "$p" "$RAIZ/logs/bridge.log" 2>/dev/null)"
  done
  echo
  echo "    ultima linha do log: $F_ULTIMALINHA"
else
  echo "    (sem log)"
fi

titulo "7. O banco de dados esta inteiro?"
STORE="$RAIZ/whatsapp-bridge/store"
if [ -d "$STORE" ]; then
  ls -la "$STORE" | grep -v '^total' | sed 's/^/  /'
  RESTOS="$(ls "$STORE" 2>/dev/null | grep -E '\-(wal|shm|journal)$')"
  if [ -n "$RESTOS" ]; then
    echo "  ATENCAO: sobraram arquivos de transacao (wal/shm/journal):"
    echo "$RESTOS" | sed 's/^/    /'
    echo "  Normal se a ponte estiver rodando; suspeito se ela estiver parada -"
    echo "  indica que o processo morreu no meio de uma escrita."
  fi
  if command -v sqlite3 >/dev/null 2>&1; then
    for db in "$STORE/whatsapp.db" "$STORE/messages.db"; do
      if [ -f "$db" ]; then
        R="$(comlimite 20 sqlite3 -cmd ".timeout 3000" "$db" "PRAGMA quick_check(1);" 2>&1 | head -3)"
        echo "  quick_check $(basename "$db"): $R"
        case "$R" in ok) : ;; *) F_BANCO=ruim ;; esac
      else
        echo "  $(basename "$db"): NAO EXISTE"
      fi
    done
    if [ -f "$STORE/whatsapp.db" ]; then
      N="$(comlimite 15 sqlite3 -cmd ".timeout 3000" "$STORE/whatsapp.db" "SELECT COUNT(*) FROM whatsmeow_device;" 2>&1)"
      echo "  aparelhos pareados na base (whatsmeow_device): $N"
      echo "  (0 = sessao apagada, precisa escanear QR de novo; 1 = pareado)"
    fi
  else
    echo "  sqlite3 nao encontrado nesta maquina; pulando a checagem de integridade"
  fi
else
  echo "  pasta store/ nao existe: $STORE"
fi

titulo "8. O Claude Desktop e o MCP"
if [ -d "/Applications/Claude.app" ]; then
  F_CLAUDE=sim
  echo "  Claude.app instalado, versao $(defaults read /Applications/Claude.app/Contents/Info CFBundleShortVersionString 2>/dev/null)"
else
  echo "  Claude.app NAO esta em /Applications"
fi
pgrep -x Claude >/dev/null 2>&1 && echo "  Claude esta ABERTO agora (pid $(pgrep -x Claude | tr '\n' ' '))" || echo "  Claude nao esta aberto"
if [ -f "$CFG" ]; then
  echo "  config: $CFG"
  # plutil le JSON e ja vem no macOS. Nao usamos python3 de proposito: em Mac
  # sem as Command Line Tools o /usr/bin/python3 abre uma janela pedindo para
  # instalar o Xcode, e isso assustaria quem esta rodando o diagnostico.
  echo "  JSON valido?"
  plutil -lint "$CFG" 2>&1 | sed 's/^/    /'
  if grep -q '"lumnia-zap"' "$CFG" 2>/dev/null; then
    F_MCP=sim
    echo "  entrada lumnia-zap: PRESENTE"
    UVCFG="$(plutil -extract mcpServers.lumnia-zap.command raw -o - "$CFG" 2>/dev/null)"
    echo "    command: ${UVCFG:-(nao consegui ler)}"
    echo "    args:    $(plutil -extract mcpServers.lumnia-zap.args json -o - "$CFG" 2>/dev/null)"
    echo "    outros MCPs: $(plutil -extract mcpServers json -o - "$CFG" 2>/dev/null | tr ',' '\n' | grep -o '"[^"]*":{' | tr -d '":{' | grep -v '^lumnia-zap$' | tr '\n' ' ')"
    if [ -n "$UVCFG" ] && [ -x "$UVCFG" ]; then
      echo "    o uv apontado existe e roda: $("$UVCFG" --version 2>&1 | head -1)"
    else
      echo "    ATENCAO: o uv apontado na config NAO existe ou nao e executavel: '$UVCFG'"
    fi
    echo "    a pasta do servidor MCP existe?"
    if [ -d "$RAIZ/whatsapp-mcp-server" ]; then
      echo "      sim ($(ls "$RAIZ/whatsapp-mcp-server" | tr '\n' ' '))"
    else
      echo "      NAO: $RAIZ/whatsapp-mcp-server"
    fi
  else
    echo "  entrada lumnia-zap: AUSENTE (o Claude nao enxerga o WhatsApp)"
  fi
else
  echo "  config do Claude NAO existe: $CFG"
fi
echo "  uv encontrado em:"
for c in "$HOME/.local/bin/uv" /opt/homebrew/bin/uv /usr/local/bin/uv; do
  [ -x "$c" ] && echo "    $c ($("$c" --version 2>&1 | head -1))"
done

titulo "9. O atalho na Mesa"
if [ -d "$HOME/Desktop/Lumnia Zap.app" ]; then
  echo "  existe: $HOME/Desktop/Lumnia Zap.app"
else
  echo "  NAO existe em $HOME/Desktop/Lumnia Zap.app"
  ls -d "$HOME"/Desktop/*.app 2>/dev/null | sed 's/^/    achei em vez disso: /'
fi

titulo "10. Internet"
for u in https://github.com https://objects.githubusercontent.com https://web.whatsapp.com; do
  printf '  %-45s ' "$u"
  curl -s -o /dev/null -m 8 -w 'HTTP %{http_code} em %{time_total}s\n' "$u" 2>/dev/null || echo "FALHOU"
done

titulo "11. A maquina dormiu? (relevante para 'Keepalive timed out')"
comlimite 25 pmset -g log 2>/dev/null | grep -E "Entering Sleep|Wake from|DarkWake|Failure" | tail -15 | sed 's/^/  /' || echo "  (nao consegui ler o log de energia)"

titulo "12. Relatorios de crash do sistema"
CRASH="$(ls -t "$HOME/Library/Logs/DiagnosticReports/" 2>/dev/null | grep -i "whatsapp-bridge" | head -3)"
if [ -n "$CRASH" ]; then
  echo "$CRASH" | sed 's/^/  /'
  P="$HOME/Library/Logs/DiagnosticReports/$(echo "$CRASH" | head -1)"
  echo "  --- inicio do mais recente ---"
  head -30 "$P" 2>/dev/null | mascarar | sed 's/^/    /'
else
  echo "  nenhum crash report de whatsapp-bridge (bom)"
fi
echo "  Mensagens do sistema sobre o processo (ultimas 12h):"
comlimite 45 log show --last 12h --style compact --predicate 'eventMessage CONTAINS "whatsapp-bridge" OR eventMessage CONTAINS "dev.lumnia.zap"' 2>/dev/null | tail -25 | mascarar | sed 's/^/    /' || echo "    (nao consegui consultar)"

titulo "13. RODANDO A PONTE NA MAO (30 segundos)  <== e aqui que o erro aparece"
echo "Esta e a parte que importa: fora do launchd, tudo que ela imprime fica visivel."
echo
if [ ! -x "$PONTE" ]; then
  echo "Pulado: o executavel nao existe ou nao pode ser executado."
else
  echo "Parando o servico para nao disputar a porta 8080..."
  launchctl bootout "gui/$MYUID/$LABEL" 2>&1 | sed 's/^/  /'
  sleep 2
  pkill -f "$PONTE" 2>/dev/null
  sleep 1
  SAIDA_MANUAL="/tmp/lz-ponte-manual.txt"
  rm -f "$SAIDA_MANUAL"
  ( cd "$RAIZ/whatsapp-bridge" && "$PONTE" > "$SAIDA_MANUAL" 2>&1 ) &
  RUNPID=$!
  sleep 30
  echo "--- saida da ponte (filtrada, sem o QR e sem mensagens) ---"
  if [ -s "$SAIDA_MANUAL" ]; then
    F_MANUAL=sim
    redigir < "$SAIDA_MANUAL" | head -60 | sed 's/^/  /'
    echo "--- fim da saida ---"
    echo "  (linhas totais capturadas: $(wc -l < "$SAIDA_MANUAL" | tr -d ' '))"
    if grep -qa "Escaneie\|[█▀▄]" "$SAIDA_MANUAL"; then
      echo "  DESENHOU O QR CODE: a ponte sobe normalmente; o problema esta no"
      echo "  servico/launchd, nao no binario."
    fi
  else
    echo "  NENHUMA SAIDA - o processo morreu antes de escrever qualquer coisa."
    echo "  Suspeitas: binario morto pelo sistema, banco corrompido, ou falta de permissao."
    echo "--- fim da saida ---"
  fi
  echo
  echo "Respondeu na porta 8080 durante o teste?"
  lsof -nP -iTCP:8080 -sTCP:LISTEN 2>/dev/null | sed 's/^/  /' || echo "  nao"
  curl -s -o /dev/null -m 3 -w '  painel: HTTP %{http_code}\n' "$PAINEL/api/panel/status" 2>/dev/null
  echo "Ainda vivo depois de 30s?"
  if kill -0 "$RUNPID" 2>/dev/null; then echo "  SIM (bom sinal)"; else echo "  NAO - morreu sozinho (mau sinal, ver saida acima)"; fi
  kill "$RUNPID" 2>/dev/null; sleep 1; pkill -f "$PONTE" 2>/dev/null
  rm -f "$SAIDA_MANUAL"
  echo
  echo "Religando o servico..."
  launchctl enable "gui/$MYUID/$LABEL" 2>/dev/null
  [ -f "$PLIST" ] && launchctl bootstrap "gui/$MYUID" "$PLIST" 2>&1 | sed 's/^/  /'
  sleep 3
  RECODE="$(curl -s -o /dev/null -m 5 -w '%{http_code}' "$PAINEL/api/panel/status" 2>/dev/null)"
  echo "  servico de volta? painel responde HTTP $RECODE"
fi

titulo "14. LEITURA RAPIDA (palpite automatico)"
echo "  pasta instalada ............ $F_PASTA"
echo "  binario da ponte ........... $F_BIN"
echo "  plist do servico ........... $F_PLIST"
echo "  servico carregado .......... $F_SERVICO"
echo "  servico desabilitado ....... $F_DESABILITADO"
echo "  processos rodando .......... $F_PROC"
echo "  painel respondendo ......... $F_PAINEL"
echo "  bridge-error.log com texto . $F_ERRLOG"
echo "  banco integro .............. $F_BANCO"
echo "  ponte subiu na mao ......... $F_MANUAL"
echo "  Claude instalado ........... $F_CLAUDE"
echo "  MCP configurado ............ $F_MCP"
echo
if [ "$F_BIN" = nao ]; then
  echo "  >> O binario nao esta la. O instalador nao chegou a extrair o pacote."
elif [ "$F_DESABILITADO" = sim ]; then
  echo "  >> DIAGNOSTICO MAIS PROVAVEL: o servico esta marcado como DESABILITADO"
  echo "     no launchd. Esse marcador sobrevive a reinstalacao e a reinicio, e"
  echo "     enquanto ele estiver la nem bootstrap nem RunAtLoad sobem a ponte -"
  echo "     e por isso que reinstalar por cima nao resolve. Conserto:"
  echo "       launchctl enable gui/$MYUID/$LABEL"
  echo "       launchctl bootstrap gui/$MYUID \"$PLIST\""
elif [ "$F_MANUAL" = sim ] && [ "$F_SERVICO" = nao ]; then
  echo "  >> DIAGNOSTICO MAIS PROVAVEL: a ponte funciona, o SERVICO e que nao esta"
  echo "     carregado. O launchctl bootstrap do instalador falhou em silencio."
  echo "     Conserto: launchctl enable gui/$MYUID/$LABEL"
  echo "               launchctl bootstrap gui/$MYUID \"$PLIST\""
elif [ "$F_MANUAL" = nao ] && [ "$F_BIN" = sim ]; then
  echo "  >> DIAGNOSTICO MAIS PROVAVEL: o binario existe mas morre no arranque."
  echo "     Olhar a secao 13 e o bridge-error.log da secao 6."
  [ "$F_BANCO" = ruim ] && echo "     O quick_check do banco reprovou - forte candidato a causa."
elif [ "$F_PAINEL" = sim ]; then
  echo "  >> A ponte esta no ar. Se o Claude nao le, o problema e do lado do MCP:"
  echo "     conferir a secao 8 e fechar o Claude com Cmd+Q (a bolinha vermelha"
  echo "     nao recarrega o MCP)."
else
  echo "  >> Sem padrao obvio. Mandar este arquivo inteiro para o Diego."
fi

echo
echo "===== FIM DO DIAGNOSTICO ====="
}

# ------------------------------------------------------------------ execucao

clear 2>/dev/null
cat <<'CAB'

  Diagnostico Lumnia Zap (Mac)
  ----------------------------
  Isto leva cerca de 1 minuto e meio. Pode deixar a janela aberta.

  O WhatsApp fica fora do ar por uns 30 segundos no meio do teste
  e volta sozinho no final.

  Nada e instalado nem apagado. O conteudo das suas conversas NAO
  entra no relatorio.

CAB

etapa "coletando..."
coletar > "$TMPOUT" 2>&1

SAIDA="$TMPOUT"
DEST="$HOME/Desktop/diagnostico-lumnia-zap-$CARIMBO.txt"
if [ -d "$HOME/Desktop" ] && cp "$TMPOUT" "$DEST" 2>/dev/null; then
  SAIDA="$DEST"
fi

echo
if [ -f "$SAIDA" ]; then
  echo "  Pronto. O arquivo foi salvo em:"
  echo "    $SAIDA"
  echo
  echo "  Mande esse arquivo para o Diego."
  printf '%s' "$SAIDA" | pbcopy 2>/dev/null && echo "  (o caminho ja esta copiado na sua area de transferencia)"
  open -R "$SAIDA" 2>/dev/null
  echo
  echo "  --- resumo do que foi encontrado ---"
  sed -n '/LEITURA RAPIDA/,$p' "$SAIDA"
else
  echo "  ATENCAO: nao consegui salvar o arquivo."
  echo "  Tire uma foto desta janela e mande para o Diego."
fi

echo
read -r -p "  Aperte Enter para fechar. " _ < /dev/tty 2>/dev/null
