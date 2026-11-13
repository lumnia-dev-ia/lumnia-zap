# Lumnia Zap — Windows

Desde novembro de 2026 o caminho principal no Windows é a **Microsoft Store**.
O instalador manual (`.exe`) continua existindo para PCs que bloqueiam a Store.
As duas formas usam a mesma ponte, o mesmo painel e o mesmo servidor MCP; o que
muda é como chegam ao PC e como o Claude encontra o servidor.

## Microsoft Store (recomendado)

Página: <https://apps.microsoft.com/detail/9NNDPSDP0G34> (Store ID
`9NNDPSDP0G34`, publicador Lumnia). Quem assina o pacote é a Microsoft: não há
aviso do SmartScreen e não precisa de administrador.

1. Instale pela Store e abra o **Lumnia Zap** pelo Menu Iniciar. Abrir uma vez
   é o que liga a inicialização automática nos próximos logins.
2. Escaneie o QR code com o WhatsApp do celular (Configurações → Aparelhos
   conectados).
3. Instale no Claude a extensão
   [lumnia-zap.mcpb](https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/lumnia-zap.mcpb):
   abra o arquivo com o Claude Desktop (ou arraste para a janela dele) e
   confirme.
4. Saia do Claude pela bandeja (botão direito no ícone perto do relógio →
   **Sair**; o X só esconde a janela) e abra de novo.
5. No painel, use **Testar com o Claude**.

Onde ficam os dados: `%LOCALAPPDATA%\Packages\Lumnia.LumniaZap_27md6avg2vwam\LocalState\`
(`whatsapp-bridge\store\` e `logs\`). **Desinstalar o app da Store apaga essa
pasta, inclusive a sessão do WhatsApp.** Atualizar pela Store preserva.

Como o Claude acha o servidor: o pacote expõe o alias
`%LOCALAPPDATA%\Microsoft\WindowsApps\lumnia-zap-mcp.exe`, que roda o servidor
MCP com um Python embutido (sem `uv`, sem baixar nada). A extensão `.mcpb`
aponta para esse alias.

### Se o PC já tinha a instalação manual

Ao abrir o Lumnia Zap da Store, ele pergunta se quer passar o histórico e a
conexão do WhatsApp para a versão da Store. Respondendo **Sim**:

- a Tarefa Agendada "Lumnia Zap" e o atalho de inicialização da versão antiga
  saem; os processos dela são encerrados;
- o Claude é fechado (abra de novo no fim);
- o histórico (`store\`) é copiado para a pasta da Store; o que a Store já
  tinha vira `store.bak-<data>`;
- a configuração do Claude que apontava para a versão antiga passa a apontar
  para o alias da Store;
- a pasta antiga (`%LOCALAPPDATA%\LumniaZap`) fica guardada, intacta.

Se o painel pedir o QR code depois, escaneie de novo: o histórico fica.
Enquanto a passagem não for feita, a versão da Store não sobe (as duas
disputam a porta 8080) e o painel que abre é o antigo, que avisa disso.

## Instalador manual (`.exe`)

Para PC que bloqueia a Store. Baixe o
[Instalador-Lumnia-Zap-Windows.exe](https://github.com/lumnia-dev-ia/lumnia-zap/releases/latest/download/Instalador-Lumnia-Zap-Windows.exe).
Não tem assinatura Authenticode, então o SmartScreen avisa na primeira
execução: *Mais informações* → *Executar assim mesmo*.

- Recusa instalar quando a versão da Store já está no PC (`--forcar` só para
  suporte).
- Se o Claude estiver aberto, pede para fechar antes de gravar a configuração.
- Grava o `lumnia-zap` em todos os lugares onde o Claude pode ler a
  configuração: `%APPDATA%\Claude`,
  `%LOCALAPPDATA%\Packages\Claude_*\LocalCache\Roaming\Claude` e
  `%LOCALAPPDATA%\Claude-Data\claude_*` (Claude da Microsoft Store em versões
  recentes). Gravação atômica, UTF-8 sem BOM, backup do original na primeira vez.
- No fim, confere se a entrada continua lá e mostra o nome do computador.
- Dados em `%LOCALAPPDATA%\LumniaZap`; inicia com o Windows por uma Tarefa
  Agendada (ou, se a política do PC bloquear, um atalho na pasta Inicializar);
  log em `%LOCALAPPDATA%\LumniaZap\logs\bridge.log`.

## Beatitude

O módulo Beatitude é só macOS e Linux. No Windows a ponte não o inicia e o
painel não o mostra.

## Diagnóstico

- Painel: `http://localhost:8080` (só responde no próprio PC). O cartão
  **Ligação com o Claude** diz se o Claude leu e de qual banco.
- Logs do Claude: `%LOCALAPPDATA%\Claude\logs\` (`mcp.log`,
  `mcp-server-lumnia-zap.log`).
- Instalação manual: `scripts/diagnostico-lumnia-zap.cmd` gera um relatório
  na Área de Trabalho, sem conteúdo de conversas.

## Para quem publica

- `.exe` e `.zip` da instalação manual: workflow **Build & publish Windows
  release** (`release-windows.sh`), na mesma tag do Mac.
- `.msix` da Store: workflow **Build MSIX (Microsoft Store)** (`build-msix.sh
  <a.b.c.0> --store`), que entrega o pacote sem assinatura como artefato. O
  envio é manual no Partner Center (upload acima de 10 MB).
- `lumnia-zap.mcpb`: anexado a toda release pelo `release-windows.sh`.
