# Mudanças

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
