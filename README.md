# Lumnia Zap

Your personal WhatsApp, inside Claude. Read and reply to your own messages by
asking — no bot, no auto-responder, no business API.

Lumnia Zap adds an operational layer on top of
[whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) by Luke Harries, whose
code does the hard part: speaking WhatsApp's multi-device protocol and exposing
it to Claude over MCP. What this project adds is everything needed to turn that
into something a non-developer can actually run and keep running.

**What's new here**

A local web panel (served by the bridge itself, no extra process) with
connection status, one-click reconnect, an in-browser QR code for re-pairing,
daily counters, and scheduled messages — once, daily, weekly, or yearly, the
last one being what birthday greetings need. A background service so the bridge
survives reboots and restarts itself if it dies: a launchd agent on macOS, a
Startup shortcut plus a watchdog script on Windows. An installer, an app icon,
and a two-click setup aimed at family members — signed and notarized by Apple on
macOS, unsigned and still in beta on Windows.

It also carries four upstream bug fixes, described below, without which the
original project does not currently run.

---

## Português

### Instalação

**macOS** (Apple Silicon — M1 ou mais novo). Baixe o
[Instalador-Lumnia-Zap.zip](https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/Instalador-Lumnia-Zap.zip),
descompacte e dê duplo clique. O instalador é assinado e notarizado pela Apple,
então não aparece aviso de segurança. Ele não pede senha de administrador.

**Windows 10/11 64-bit — beta.** Baixe o
[Instalador-Lumnia-Zap-Windows.exe](https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/Instalador-Lumnia-Zap-Windows.exe)
e dê duplo clique. Ainda não há assinatura Authenticode, então o SmartScreen vai
mostrar "O Windows protegeu seu PC" na primeira execução — clique em *Mais
informações* → *Executar assim mesmo*. Detalhes em
[README-windows.md](README-windows.md).

Nos dois casos é preciso ter o [Claude Desktop](https://claude.ai/download)
instalado. Depois de instalar, escaneie o QR code que abre no navegador e
reinicie o Claude Desktop.

### O que ele faz

Conecta como um aparelho vinculado, igual ao WhatsApp Web — não é a API
comercial da Meta. Suas conversas ficam num banco local na sua máquina, e vão
para o Claude apenas quando você pede para ler ou processar algo.

O painel abre em `http://localhost:8080` e é inacessível de fora da máquina.

### Aviso honesto

Ao instalar, você dá ao seu assistente acesso de leitura e escrita ao seu
WhatsApp pessoal. Isso é útil e é o objetivo — mas é bom saber exatamente o que
se está aceitando. Evite pedir que o Claude processe conteúdo de origem
duvidosa enquanto essa capacidade está ativa.

---

## The four upstream fixes

These are bug fixes to the original project, not features. They are being
offered back upstream.

**Outdated client library.** WhatsApp periodically rejects old clients with
`Client outdated (405)`, which surfaces as
`websocket: close 1006 (abnormal closure)` — so the QR code never appears.
Fixed by bumping `whatsmeow` and adding the `context.Context` argument that
five call sites now require.

**Media downloads always failed with HTTP 403.** `extractDirectPathFromURL`
stripped the query string from the media URL. But `whatsmeow` builds the final
URL as `directPath + "&hash=…&mms-type=…"`, using `&` as the separator because
it assumes the WhatsApp-signed query (`?ccb=…&oh=…&oe=…`) is already there.
Stripping it produced a malformed URL that the media host rejected. Fixed by
preserving the query string.

**Contact search missed LID-identified chats.** WhatsApp identifies many chats
by an opaque `@lid` identifier instead of a phone number. The code looked up
contacts using that identifier, but the contact store is keyed by phone JID, so
lookups always missed and the raw 15-digit LID became the chat name. Fixed by
resolving LID to phone JID via `Store.GetAltJID` before the lookup, with a
fallback chain of full name → business name → push name → phone number.

**Blocking connect killed the process when run as a service.** The original
code blocked waiting for a QR scan and returned after a 3-minute timeout. Under
launchd that means dying and restarting forever, with no way to see a QR code.
Fixed by making the connection asynchronous so the HTTP server always comes up.

---

## Credits

Built on [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) by
Luke Harries, and on [whatsmeow](https://github.com/tulir/whatsmeow) by Tulir
Asokan, which does the actual protocol work.

MIT licensed — see [LICENSE](LICENSE). Copyright is shared: Luke Harries for the
original work, Lumnia for what was added here.

## Status and expectations

This is a personal project shared openly, not a product with support. WhatsApp
will periodically break old clients, and when that happens the fix is a
dependency bump and a rebuild. Issues and pull requests are welcome — a shared
project rots slower than a private one.

Not affiliated with, endorsed by, or connected to WhatsApp or Meta.
