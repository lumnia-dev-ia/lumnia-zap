# Mudanças

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
