# Regras do mordomo do Beatitude

O texto das regras do mordomo pode mudar sem uma versão nova do Lumnia Zap.

- `mordomo.fonte.json` — o que você edita.
- `mordomo.json` — o que as pontes baixam: o pacote assinado. Não edite à mão.

As pontes com o Beatitude ligado buscam `mordomo.json` da branch `main` a cada
30 minutos, conferem a assinatura com a chave pública compilada nelas e só
então usam. Pacote inválido, mais velho que o em uso ou que pede uma ponte mais
nova é ignorado, e valem os textos compilados.

## O que o pacote muda

```json
{
  "versao": 2,
  "ponte_minima": "1.1.3",
  "nota": "o que mudou, em uma linha",
  "regras": { "bastao": "texto novo", "viagem": "" },
  "extras": ["regra nova, entra antes da hora atual"],
  "conectores": { "lovable": "texto novo da regra do Lovable" }
}
```

- `regras`: troca o texto de uma regra pela chave; `""` tira a regra. As
  chaves estão em `whatsapp-bridge/beatitude.go` (a lista em
  `beatitudeRodarClaude`): identidade, tom, formatacao, so_le, rascunhos,
  horario, anexo, voz, sem_instrucoes, responder, terceiros, fazer_direto,
  o_que_alcanco, aprender, caderno, chrome_nunca, navegador,
  navegador_leitura, viagem, restaurante, audio, economia, perfil,
  ignorar_grupo, pendentes, temas, bastao, negada, midia. A hora atual
  (`agora`) não se troca.
- `{maquina}` e `{grupo}` no texto viram o nome da máquina e o JID do grupo.
- `conectores`: texto da regra de cada conector, pelo ID do catálogo.

## O que o pacote não alcança, de propósito

Ferramentas liberadas e negadas, a trava do "sim", os tetos e quem envia o quê
ficam no binário. Um pacote adulterado muda o que o mordomo diz, nunca o que
ele pode fazer.

## Publicar

1. Edite `mordomo.fonte.json` e aumente `versao`.
2. `bash scripts/assinar-regras.sh` (chave em `~/.lumnia/regras.key` ou em
   `LUMNIA_REGRAS_CHAVE`).
3. Commit de `regras/` na `main`. Em até 30 minutos as pontes usam; o painel
   mostra a versão em uso (`regras.versao` no status do Beatitude).
