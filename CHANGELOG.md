# Mudanças

## 1.1.5 — novembro de 2026

**O app mudou de endereço: beatitude.ai**
- Desde 25/11, por volta das 11h45, beatitude.lumnia.dev redireciona para
  beatitude.ai. Num redirecionamento para outro domínio o Zap perdia o token, o
  app respondia "token_invalido" a cada minuto, o Cockpit mostrava o Mac sem
  sinal e o brief não chegava ao grupo. A ponte passa a falar direto com
  beatitude.ai, e o modo rápido configura o conector em beatitude.ai/mcp.
- Pareamento que tenha o endereço antigo gravado passa a usar o novo sozinho.
- Se o app mudar de endereço de novo, a ponte não manda a chamada sem o token:
  o painel e o log dizem "o app mudou de endereço: X redireciona para Y".
- Quem aplicou à mão o `"url": "https://beatitude.ai"` em
  `store/beatitude-ponte.json` pode deixar como está.

Vem das reclamações de 24/11 no grupo: o pedido de mensagem voltava com a
descrição do pedido em vez do texto, e às vezes sem cartão nenhum.

**Programada sai sem o "sim"**
- Mensagem pedida para um horário a mais de 5 minutos de distância já nasce
  programada: a ponte agenda na hora e mostra o cartão com destinatário,
  horário e texto. Para mudar: *envia N: novo texto* (mantém o horário). Para
  desistir: *cancela N* ou ❌ no cartão, a qualquer hora.
- Continuam pedindo o *sim*: envio imediato, horário em até 5 minutos e número
  sem conversa anterior.

**Uma mensagem só, com o texto**
- Resposta do mordomo e cartão da ponte saem juntos. O mordomo só escreve
  quando há algo que o cartão não mostra (qual contato escolheu, um alerta).
- Rascunho descartado deixa de ser silencioso: destino que não existe, bloco
  com defeito ou resposta que cita um rascunho sem trazê-lo viram um aviso de
  que nada foi preparado.
- Número sem conversa anterior (contato encaminhado, número ditado) passa a
  ser aceito quando o WhatsApp confirma o número; o cartão mostra o telefone
  e espera o *sim*.
- Conversa sem nome mostra o nome da agenda de contatos ou o telefone legível
  ("+55 11 95038-0882"), não mais "5511950380882".
- A marca "◆ Beatitude" não sai mais repetida.

**Números que não se repetem**
- As programadas em aberto reservam seus números; o pedido seguinte continua
  a contagem (antes todo rascunho era o 1 e *cancela 1* só alcançava o último).
- *programadas* (ou *agendadas*) lista no grupo o que vai sair, sem chamar o
  Claude.
- "SIM, enviar amanhã, 8h" e parecidos valem como *sim*.

**Chave nova das regras do mordomo**
- A chave privada de 16/11 se perdeu. A ponte passa a conferir o pacote de
  regras com uma chave pública nova; a privada fica em `~/.lumnia/regras.key`
  no Mac do Diego. Pontes da 1.1.4 ou anteriores ignoram os pacotes novos e
  seguem com as regras compiladas.

**Cancelar em frase livre e espelho no app (pacote de 17/11)**
- Cancelar em frase livre funciona sem passar pelo Claude: "cancelar o envio
  da mensagem para a Jianny", "não precisa mais mandar pra Marina". A ponte
  acha a programada pelo nome ou pelo número; com mais de uma possível,
  pergunta qual e nada é cancelado. Só vale para mensagem: "cancela a reunião"
  segue para o mordomo, e relato ("ela cancelou") não é ordem.
- *cancela agendada N* e *cancela a programada N* valem como *cancela N*. "O que
  vai sair?" também lista as programadas.
- Regra nova do mordomo (`agendadas`): não diz que "não há mensagem
  programada" sem olhar a lista que recebe, nem cita telas que não existem.
- Com o computador pareado, a ponte manda ao app as programadas (para quem,
  quando, o texto) e as mudanças de estado das últimas 48 h, em
  `/api/ponte/agendados`: é o que preenche o quadro "Programadas", até aqui
  vazio para todos. O texto passa a ficar no servidor do app até o envio.
- A ponte aplica o que a pessoa fizer nesse quadro: cancelar, enviar agora,
  trocar o texto, trocar a hora (até 7 dias à frente). Cada pedido é avisado
  no grupo ("Pedido feito no app: …") e confirmado ao app. Servidor sem a rota
  (404): a ponte espera 30 minutos e segue normal.

**O bastão roda sozinho neste computador (executor)**
- Em 27/11 um pedido com planilha voltou como texto para colar numa conversa
  do Claude; ninguém colou e nada rodou. Com o executor ligado, a ponte abre
  um segundo Claude Code que faz o trabalho numa pasta só do pedido
  (`~/Beatitude/Pedidos/B1`, com `entrada/` e `saida/`) e devolve o resultado
  ao grupo: o texto e, anexados, até 5 arquivos de até 20 MB. Se houver a
  pasta `Beatitude` do Google Drive ou do OneDrive sincronizada no computador
  (ou das duas; no OneDrive ela fica em `Apps/<nome do app>/Beatitude`), `saida/` é copiada para `Beatitude/Pedidos/B1`.
- **Desligado de fábrica.** Liga-se por máquina no painel (Beatitude →
  Executor). Desligado, nada muda: o bastão segue como texto para colar.
- Falta um dado: ele pergunta no grupo e espera até 24 h. *continua B1* e a
  resposta retoma a mesma conversa. *para B1* interrompe (mata o processo e o
  que ele abriu no terminal); *para B1 manda para eu colar* devolve o texto do
  plano B. *bastões* mostra o que está rodando, na fila ou esperando.
- Um por vez, sem travar o mordomo. Teto de 10 por dia e 45 minutos por
  rodada; acima do teto, sem sandbox ou em pedido que depende de projeto do
  claude.ai, volta a colar.
- Travas no código: o executor não tem ferramenta de envio (quem posta é a
  ponte); o terminal roda no sandbox do Claude Code, escreve só na pasta do
  pedido e não lê a pasta pessoal fora dela; Read e Edit só nessa pasta; sem
  sandbox o Claude Code nem sobe. A rede do terminal é uma lista (PyPI,
  GitHub, npm, gov.br), ajustável em `executor_dominios`. Por padrão ele não
  lê o WhatsApp: o que precisa vem no pedido e em `entrada/`.
- O Chrome é de um por vez: com um pedido rodando, a tarefa de navegador
  avisa e espera o *abre* de novo.
- Com o executor ligado, o mordomo deixa de narrar a passagem ("passei ao
  Claude do computador"): quem diz o que aconteceu é a ponte.
- Linux: precisa de `bubblewrap` e `socat`. Windows: não há executor.

**Google, Microsoft ou os dois**
- O app passou a aceitar entrada e conexões do Google e da Microsoft, juntas ou
  não. Na ponte: o teste do modo rápido reconhece também o conector Microsoft
  365, e a tarefa de navegador lê a agenda do Outlook como já lia a do Google.
- O mordomo passa a achar e ler arquivos do OneDrive e do SharePoint pelo
  conector Microsoft 365, só leitura, como já fazia com o Google Drive.

## 1.1.4 — 24 de novembro de 2026

**O WhatsApp voltou a aceitar a conexão**
- Em 23/11, por volta das 20h05 (Brasília), o WhatsApp aposentou a versão do
  WhatsApp Web que o Zap apresentava (2.3000.1043636855, da whatsmeow de
  julho) e passou a recusar a conexão com "Client outdated (405)". Todas as
  instalações caíram e não voltaram sozinhas. A sessão não se perdeu: depois
  de atualizar, o Zap reconecta sem ler o QR de novo.
- whatsmeow atualizada para a de 16/11/2026 (compila com 2.3000.1054751086),
  com 111 commits de protocolo, reconexão, keepalive e app state. Exige Go
  1.26 (`toolchain go1.26.11` no `go.mod`). O banco da sessão ganha duas
  colunas na primeira vez que sobe (schema v16); a migração é automática.

**Para não cair de novo do mesmo jeito**
- Ao iniciar e a cada 6 h, a ponte lê a versão atual em web.whatsapp.com e
  passa a usá-la se for mais nova (nunca desce; sem internet, segue a
  compilada). Em 24/11 era 2.3000.1056029132.
- Se o WhatsApp recusar mesmo assim (405), a ponte tenta de novo em 5 s, 1 min,
  5 min, 15 min, 30 min e depois a cada 30 min, e reconecta na mesma sessão
  assim que houver versão mais nova. Antes, ficava parada até reiniciar.
- O painel diz "O WhatsApp recusou a versão — tentando de novo sozinho" e "não
  precisa ler o QR de novo", em vez de "Reconectando…". Depois de 1 h, diz que
  é preciso atualizar o Lumnia Zap. Onde a ponte está pareada ao Beatitude,
  o suporte recebe um aviso depois de 5 min.
- `LUMNIA_WA_VERSAO=2.3000.N` força uma versão (suporte).
- Nuvem: o `atualizar.sh` instala o Go que o `go.mod` pede antes de compilar;
  o `instalar.sh` passa a instalar o Go 1.26.11.

## 1.1.3 — novembro de 2026

**Arquivo junto da mensagem**
- O mordomo manda uma mensagem e, na sequência, um arquivo. O arquivo é o que a
  pessoa mandou ou encaminhou para o grupo Beatitude nos últimos 7 dias (foto,
  PDF, Word, planilha); do e-mail ou do OneDrive, pelo Compartilhar do celular.
- O cartão do rascunho mostra o arquivo ("com o arquivo comparativo.pdf"). No
  "sim" sai o texto e logo depois o arquivo; com horário, o agendador faz o
  mesmo na hora marcada.
- O mordomo só escolhe o código do arquivo da lista (A1, A2…); quem acha o
  arquivo é a ponte. Código que não existe derruba o rascunho com aviso. Se o
  arquivo não estiver disponível no "sim", nem o texto sai. Se o texto sair e o
  arquivo falhar, o grupo é avisado para anexar à mão.
- Word, planilha e outros que o leitor não abre deixam de receber "não consegui
  ler": a ponte avisa que recebeu e que pode mandá-los junto de uma mensagem.
- Agendador do painel: agendamento pode levar arquivo (coluna `media_path`).

**Aprovar com 👍 e escolher numa enquete**
- Até 3 rascunhos: 👍 no cartão do mordomo aprova os rascunhos daquele cartão
  (qualquer tom de pele); ❌ cancela. Outras reações não fazem nada.
- 4 ou mais: vai junto uma enquete aberta, uma opção por rascunho ("3 · Marina
  · sai às 20:00"). Marcar não envia; a pessoa confirma com 👍 na enquete ou
  escrevendo *envia*. Vale o último voto.
- Reação e voto viram o mesmo comando de texto ("envia 1 e 3", "cancela 2") e
  passam pelas mesmas travas: horário, arquivo, teto. Só valem no cartão da
  última lista e por até 2 horas. Eventos repetidos na reconexão são ignorados.

**Atualizações sem trabalho manual**
- Máquinas da nuvem: atualização automática toda madrugada (3h40) pela branch
  instalada. Compila à parte, troca o binário, confere se o Zap subiu e, se
  não, volta sozinho ao anterior. Adia se houver mensagem agendada para os
  próximos 10 minutos. Para segurar uma máquina:
  `touch /home/beatitude/.config/lumnia/sem-atualizacao`.
- Release: uma tag `vX.Y.Z` publica Mac (assinado e notarizado) e Windows pelo
  GitHub Actions (`.github/workflows/release.yml`). O `release.sh` aceita a
  chave da API da App Store Connect no lugar do perfil do Keychain.
- Regras do mordomo sem versão nova: o texto das regras vem de um pacote
  assinado (`regras/mordomo.json`, Ed25519) que as pontes buscam a cada 30 min.
  Troca, tira e acrescenta regras e o texto de cada conector. Ferramentas,
  trava do "sim" e tetos continuam no binário. Ver `regras/LEIA-ME.md`.

## 1.1.2 — novembro de 2026

Mais do Claude e do Mac pelo grupo do Beatitude.

**O bastão**
- Quando o pedido passa do alcance do mordomo (um projeto do claude.ai,
  arquivos do computador, o código de um app, um conector fora da lista), o
  grupo recebe um aviso e, em mensagem separada, o texto pronto para colar numa
  conversa do Claude, nova ou num projeto. A resposta volta ao grupo com o
  código (B12) pela ferramenta `devolver_ao_grupo` do Beatitude. Mensagem a
  terceiros nunca sai por aí: vira ação que espera o "sim N".
- Comando *bastões* lista o que está esperando volta (valem 24 h).
- O mordomo passa a saber o que alcança nesta máquina (conectores, Chrome,
  transcritor) e responde isso sem inventar.

**Chrome**
- Usa só o Chrome deste computador (marca da extensão), nunca o de outro Mac
  ligado à mesma conta.
- Login, código ou CAPTCHA: a tarefa para na aba, avisa no grupo com o link
  "Ver a máquina" (endereço salvo no app) e retoma a mesma sessão com
  *continua N*. Nunca digita senha nem código. Parada há 30 min, é cancelada.

**Mac sempre pronto**
- Com o mordomo ligado, a ponte reabre o Claude e o Chrome em segundo plano
  quando fecham (o Claude só depois de duas conferências seguidas fechado, para não atrapalhar o
  instalador nem o Reparar); avisa no grupo se não conseguir.
- "Pronta para rodar sozinha" ganha Claude aberto, Chrome aberto e, em
  amarelo, FileVault e entrada automática (o que decide se a ponte volta
  sozinha depois de uma queda de energia). Tudo vai no sinal de vida.

**Voz**
- Mac com Apple Silicon: Whisper large-v3-turbo no chip (mlx), com
  vocabulário fixo (Beatitude, Lumnia, Timão…) depois dos nomes do círculo.
  O small segue como reserva e no Linux.

**Conectores**
- Lovable e Read AI, só leitura.

**Correção**
- `list_chats` com `include_last_message=false` devolvia lista vazia.

## 1.1.1 — novembro de 2026

Primeira versão com Mac, Windows (Store e manual) e Linux (nuvem do Beatitude)
saindo do mesmo código. A Store recebe o pacote 1.1.1.0.

**Segurança**
- A ponte passa a recusar pedidos que não vêm do painel local nem do servidor
  MCP: um site aberto no navegador conseguia mandar um formulário para
  `localhost:8080/api/send` e enviar mensagens pelo WhatsApp da pessoa (CSRF),
  e um domínio apontado para 127.0.0.1 conseguia ler o painel (DNS rebinding).

**Ligação com o Claude**
- Painel: cartão "Ligação com o Claude" (quando o Claude leu, e se leu o banco
  desta instalação), botão "Testar com o Claude" (código para o próprio
  número; enviada → gravada → o Claude achou) e cartão "Primeiros passos".
- Mac e Windows manual: o painel confere a configuração do Claude e oferece
  "Reparar" (só com o Claude fechado).
- Servidor MCP manda à ponte um sinal a cada ferramenta (id do banco e nome da
  ferramenta; nunca conteúdo).

**Instaladores (Mac e Windows manual)**
- Pedem para fechar o Claude antes de gravar a configuração; gravação atômica e
  conferência no fim; backup do original só na primeira vez; nome do
  computador na tela final e no relatório.
- Mac: só o aplicativo conta como "Claude instalado"; arruma os restos do
  incidente de setembro (binário no lugar da pasta, histórico em `.antigo`).
- Windows manual: recusa instalar com a versão da Store presente.

**Windows — Microsoft Store**
- Ao abrir, oferece passar o histórico e a conexão da instalação manual para a
  da Store.
- O pacote passa a ser montado por um workflow no GitHub.

**Beatitude (Mac e Linux)**
- O mordomo alcança o Timão (lista fechada; sem mensagens a terceiros nem
  exclusões).
- Imagens e PDFs mandados no grupo são lidos por um leitor separado, sem
  nenhum MCP, e entram no pedido como dado.
- Ferramenta de conector negada vira uma linha fixa no grupo e um aviso ao
  suporte, em vez de instruções técnicas para a pessoa.
- Catálogo de conectores (Google, Microsoft 365, Timão), escolhidos por
  máquina no painel.
- Já na linha desde 31/10: rascunho com horário, teto de 30 pedidos por hora
  só para o grupo, envios gravados no banco, falas do grupo espelhadas no app.
