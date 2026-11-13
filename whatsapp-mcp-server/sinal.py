# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
"""Sinal do Claude para a ponte (1.1.1; doc 11, fase 0).

O servidor MCP le o SQLite direto do disco: sem isto, a ponte nao sabe se o
Claude esta lendo, nem QUAL banco. A cada ferramenta, manda a ponte o db_id do
banco aberto e o nome da ferramenta — nunca conteudo, contato ou telefone.

Regras: nunca escreve no stdout (e do protocolo JSON-RPC); nunca falha a
ferramenta por causa do sinal; no maximo um sinal a cada 5 s (o do teste com o
Claude passa sempre); ponte desligada = sinal ignorado.
"""
import functools
import json
import os
import re
import sqlite3
import sys
import threading
import time
import urllib.request

VERSAO_MCP = "1.1.1"
URL_SINAL = "http://127.0.0.1:8080/api/panel/claude-sinal"
INTERVALO = 5.0
_CODIGO = re.compile(r"Teste do Lumnia Zap · código (\d{4})")

_trava = threading.Lock()
_ultimo = 0.0
_db_id = None


def _log(*a):
    print(*a, file=sys.stderr)


def db_id(caminho_db):
    """db_id do banco que este servidor le; vazio quando o banco e antigo."""
    global _db_id
    if _db_id:
        return _db_id
    try:
        con = sqlite3.connect(f"file:{caminho_db}?mode=ro", uri=True, timeout=0.5)
        try:
            linha = con.execute("SELECT valor FROM panel_meta WHERE chave='db_id'").fetchone()
        finally:
            con.close()
        _db_id = linha[0] if linha and linha[0] else ""
    except Exception:
        return ""
    return _db_id


def codigo_no_resultado(resultado):
    """O codigo do teste, se a ferramenta devolveu a mensagem de teste."""
    try:
        texto = resultado if isinstance(resultado, str) else json.dumps(resultado, ensure_ascii=False, default=str)
    except Exception:
        texto = str(resultado)
    m = _CODIGO.search(texto)
    return m.group(1) if m else ""


# Sem proxy: um HTTP_PROXY do sistema (PC corporativo) mandaria o localhost
# para fora e o sinal nunca chegaria a ponte.
_abridor = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def _enviar(corpo):
    try:
        req = urllib.request.Request(URL_SINAL, data=json.dumps(corpo).encode(), method="POST",
                                     headers={"Content-Type": "application/json"})
        _abridor.open(req, timeout=0.5).read()
    except Exception:
        pass  # ponte fora do ar ou antiga (404): o sinal e opcional


def cliente(mcp=None):
    """Quem esta usando o servidor: LUMNIA_ZAP_CLIENTE (o mordomo do Beatitude
    define "beatitude") ou o nome que o cliente MCP deu no initialize
    (claude-ai, codex...). Vazio se nao der para saber."""
    marcado = os.environ.get("LUMNIA_ZAP_CLIENTE", "").strip()
    if marcado:
        return marcado[:40]
    try:
        info = mcp.get_context().session.client_params.clientInfo
        return (info.name or "")[:40]
    except Exception:
        return ""


def sinalizar(ferramenta, resultado, caminho_db, agora=None, enviar=None, quem=""):
    """Decide e dispara o sinal em segundo plano. Devolve o corpo enviado (ou
    None), para os testes."""
    global _ultimo
    agora = time.monotonic() if agora is None else agora
    codigo = codigo_no_resultado(resultado)
    with _trava:
        if not codigo and agora - _ultimo < INTERVALO:
            return None
        _ultimo = agora
    corpo = {"db_id": db_id(caminho_db), "ferramenta": ferramenta, "versao_mcp": VERSAO_MCP}
    if quem:
        corpo["cliente"] = quem
    if codigo:
        corpo["teste_codigo"] = codigo
    alvo = enviar or _enviar
    threading.Thread(target=alvo, args=(corpo,), daemon=True).start()
    return corpo


def com_sinal(fn, caminho_db, mcp=None):
    """Embrulha uma ferramenta: o resultado volta intacto; o sinal vai depois."""
    @functools.wraps(fn)
    def embrulho(*args, **kwargs):
        resultado = fn(*args, **kwargs)
        try:
            sinalizar(fn.__name__, resultado, caminho_db, quem=cliente(mcp))
        except Exception as e:  # nunca derruba a ferramenta
            _log("sinal falhou:", e)
        return resultado
    return embrulho


def instalar(mcp, caminho_db):
    """Troca mcp.tool por uma versao que embrulha cada ferramenta registrada
    depois desta chamada."""
    original = mcp.tool

    def tool(*a, **k):
        decorador = original(*a, **k)

        def aplicar(fn):
            return decorador(com_sinal(fn, caminho_db, mcp))
        return aplicar
    mcp.tool = tool
