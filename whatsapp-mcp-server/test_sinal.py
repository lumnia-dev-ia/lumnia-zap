# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
"""Testes do sinal (python3 -m unittest test_sinal)."""
import io
import os
import sqlite3
import sys
import tempfile
import time
import unittest
from contextlib import redirect_stdout

import sinal


class TestSinal(unittest.TestCase):
    def setUp(self):
        sinal._ultimo = 0.0
        sinal._db_id = None
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "messages.db")
        con = sqlite3.connect(self.db)
        con.execute("CREATE TABLE panel_meta (chave TEXT PRIMARY KEY, valor TEXT)")
        con.execute("INSERT INTO panel_meta VALUES ('db_id', 'abc-123')")
        con.commit()
        con.close()

    def test_db_id_e_banco_antigo(self):
        self.assertEqual(sinal.db_id(self.db), "abc-123")
        sinal._db_id = None
        antigo = os.path.join(self.dir, "antigo.db")
        sqlite3.connect(antigo).close()
        self.assertEqual(sinal.db_id(antigo), "")

    def test_limite_de_5s_e_teste_passa_sempre(self):
        enviados = []
        corpo = sinal.sinalizar("list_chats", [], self.db, agora=100.0, enviar=enviados.append)
        self.assertEqual(corpo["db_id"], "abc-123")
        self.assertIsNone(sinal.sinalizar("list_chats", [], self.db, agora=102.0, enviar=enviados.append))
        c = sinal.sinalizar("list_messages", "Teste do Lumnia Zap · código 4821", self.db, agora=103.0, enviar=enviados.append)
        self.assertEqual(c["teste_codigo"], "4821")
        self.assertIsNotNone(sinal.sinalizar("x", [], self.db, agora=109.0, enviar=enviados.append))

    def test_ponte_fora_do_ar_nao_trava(self):
        sinal.URL_SINAL = "http://127.0.0.1:9/nada"
        t0 = time.monotonic()
        sinal._enviar({"db_id": ""})
        self.assertLess(time.monotonic() - t0, 2)

    def test_embrulho_preserva_resultado_assinatura_e_stdout(self):
        def ferramenta(a: int, b: str = "x") -> dict:
            """doc"""
            return {"a": a, "b": b}
        emb = sinal.com_sinal(ferramenta, self.db)
        saida = io.StringIO()
        with redirect_stdout(saida):
            r = emb(1, b="y")
        self.assertEqual(r, {"a": 1, "b": "y"})
        self.assertEqual(saida.getvalue(), "")  # stdout é do protocolo
        import inspect
        self.assertEqual(list(inspect.signature(emb).parameters), ["a", "b"])
        self.assertEqual(emb.__doc__, "doc")

    def test_cliente_marcado_pelo_mordomo(self):
        os.environ["LUMNIA_ZAP_CLIENTE"] = "beatitude"
        try:
            self.assertEqual(sinal.cliente(None), "beatitude")
            c = sinal.sinalizar("x", [], self.db, agora=500.0, enviar=lambda _: None, quem=sinal.cliente(None))
            self.assertEqual(c["cliente"], "beatitude")
        finally:
            del os.environ["LUMNIA_ZAP_CLIENTE"]
        self.assertEqual(sinal.cliente(None), "")  # sem contexto MCP: vazio, sem erro

    def test_codigo_dentro_de_lista_de_mensagens(self):
        resultado = [{"content": "Teste do Lumnia Zap · código 0007"}]
        self.assertEqual(sinal.codigo_no_resultado(resultado), "0007")
        self.assertEqual(sinal.codigo_no_resultado("nada aqui"), "")


if __name__ == "__main__":
    unittest.main()
