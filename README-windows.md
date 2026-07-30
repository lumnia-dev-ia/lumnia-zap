# Lumnia Zap — Windows (novo)

Porta do instalador de macOS para Windows 10/11 (64-bit). Feita numa sessão
sem acesso a um PC Windows real — compilada e testada (`go build`, `go vet`)
neste ambiente, mas **ainda não rodada num Windows de verdade**. Trate a
primeira instalação como um teste, não como algo já validado.

## O que é igual ao Mac

- `whatsapp-bridge`: **nenhuma linha de código mudou**. É o mesmo `main.go`/
  `panel.go`, compilado para Windows com `GOOS=windows` usando um
  cross-compilador mingw-w64 (`CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc`), sem
  trocar o driver sqlite (continua `mattn/go-sqlite3`, o mesmo do Mac). O
  `.exe` resultante só depende de `KERNEL32.dll` e `msvcrt.dll` — nada extra
  pra instalar.
- `whatsapp-mcp-server` (Python): usado sem nenhuma alteração. `uv` resolve o
  Python sozinho em qualquer SO.
- O painel (`http://localhost:8080`), a API, os quatro fixes do upstream —
  tudo igual, porque está no binário compartilhado.

## O que foi refeito para Windows (`installer-windows/`)

O instalador de Mac (`installer/main.go`) usa launchd, `.app` bundle e
`osascript` — tudo específico de macOS. O `installer-windows/main.go` é um
programa novo, em Go também, que faz o equivalente com mecanismos do
Windows:

| Mac | Windows |
|---|---|
| `~/Library/Application Support/LumniaZap` | `%LOCALAPPDATA%\LumniaZap` |
| `~/Library/Application Support/Claude/claude_desktop_config.json` | `%APPDATA%\Claude\claude_desktop_config.json` (mesmo formato/local documentado pela Anthropic) |
| LaunchAgent (`launchctl`) com `KeepAlive` | atalho `.lnk` na pasta **Startup** do usuário + um `watchdog.vbs` que reinicia a ponte se ela cair (checa `/api/panel/status` a cada 15s) |
| diálogos via `osascript` | prompts no console (é um app de console — a janela preta que abre ao dar duplo clique) + uma caixa de mensagem final via PowerShell (best-effort) |
| atalho `.app` na Mesa | atalho `.lnk` na Área de Trabalho (criado via COM do PowerShell, `WScript.Shell`) |
| `.icns` | `painel.ico` (convertido do `.icns` com Pillow) |
| assinado + notarizado pela Apple | **não assinado** — veja abaixo |

Não precisa de admin em nenhuma etapa (tudo roda no perfil do próprio
usuário, igual ao Mac).

## Importante: sem assinatura de código

O instalador de Mac é assinado e notarizado pela Apple, então abre sem
aviso. O instalador de Windows **não tem certificado de assinatura**
(Authenticode) — isso custa dinheiro (por volta de US$100–400/ano, emissores
como DigiCert, Sectigo, SignPath) e não dá pra resolver de graça. Sem isso,
o Windows SmartScreen vai mostrar "O Windows protegeu seu PC" na primeira
execução. Quem for instalar precisa clicar em **Mais informações** → **Executar
assim mesmo**. Vale avisar isso de antemão pra família não desistir achando
que é vírus.

## Como testar (num PC Windows de verdade)

1. Publique uma release com `bash scripts/release-windows.sh <versão>`
   (roda em Linux/WSL — não precisa de Windows pra compilar, só pra testar).
   Pré-requisitos: `sudo apt-get install gcc-mingw-w64-x86-64 zip`, `go`, `gh`
   autenticado. Isso sobe `Instalador-Lumnia-Zap-Windows.exe` e
   `lumnia-zap-windows-amd64.zip` pra mesma release do GitHub que já existe
   (`lumnia-dev-ia/lumnia-zap`).
2. No Windows: baixe o `.exe`, clique "Mais informações" → "Executar assim
   mesmo" no aviso do SmartScreen, e dê duplo clique.
3. Confirme no console (S/n), espere baixar/instalar, escaneie o QR code que
   abre no navegador.
4. Feche e abra o Claude Desktop de novo — o servidor `lumnia-zap` deve
   aparecer no indicador de MCP.
5. Reinicie o PC e confirme que a ponte volta sozinha (o atalho da pasta
   Startup deve disparar o `watchdog.vbs`).

## Pontos que merecem atenção ao testar (não pude verificar sem Windows)

- **Local exato do Claude Desktop**: a checagem no instalador é só
  informativa (não bloqueia) porque a instalação do Claude no Windows varia
  — Squirrel clássico ou pacote MSIX têm pastas diferentes. Se o aviso "não
  encontramos o Claude" aparecer com o Claude já instalado, não é bug — é só
  a checagem sendo conservadora.
- **`.lnk` via PowerShell/COM**: é o mecanismo padrão do Windows pra criar
  atalhos, mas depende do PowerShell estar disponível no PATH (é built-in em
  todo Windows 10/11, mas confirme).
- **`watchdog.vbs` via `wscript.exe //B`**: `//B` roda em modo "batch" (sem
  diálogos/prompts do próprio VBScript), o que deveria manter tudo invisível
  em segundo plano. Vale confirmar que nenhuma janela fica piscando.
- **Antivírus/Defender**: um `.exe` novo, não assinado, baixando outro `.exe`
  da internet e mexendo em `claude_desktop_config.json` é exatamente o tipo
  de comportamento que heurísticas de antivírus desconfiam. Pode ser que o
  Defender ou outro AV barre ou coloque em quarentena — se acontecer, vai
  precisar de uma exceção manual até (e se) o app ganhar reputação/assinatura.

## Depois de validar num Windows real

Se quiser deixar isso redondo (tipo o que já existe pro Mac), os próximos
passos naturais seriam: comprar um certificado de assinatura de código pra
sumir com o aviso do SmartScreen, e trocar o app de console por uma janela
gráfica de verdade (hoje mostra uma janela preta com texto — dá pra fazer,
mas exige uma lib de GUI, tipo `fyne` ou `webview`, e mais tempo de
desenvolvimento).
