# Copyright (c) 2026 Lumnia — Diego Penna Moreira
# SPDX-License-Identifier: MIT
"""Testes do list_chats (python3 -m unittest test_list_chats)."""
import os
import sqlite3
import tempfile
import unittest

import whatsapp


class TestListChats(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        db = os.path.join(self.dir, "messages.db")
        con = sqlite3.connect(db)
        con.execute("CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP)")
        con.execute("CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP, is_from_me BOOLEAN)")
        con.execute("INSERT INTO chats VALUES ('1@g.us', 'Beatitude', '2026-11-14T09:53:28-03:00')")
        con.execute("INSERT INTO messages VALUES ('m1', '1@g.us', '55', 'oi', '2026-11-14T09:53:28-03:00', 1)")
        con.commit()
        con.close()
        self._antigo = whatsapp.MESSAGES_DB_PATH
        whatsapp.MESSAGES_DB_PATH = db

    def tearDown(self):
        whatsapp.MESSAGES_DB_PATH = self._antigo

    def test_com_ultima_mensagem(self):
        r = whatsapp.list_chats(query="Beatitude")
        self.assertEqual(len(r), 1)
        self.assertEqual(r[0].last_message, "oi")

    def test_sem_ultima_mensagem_nao_volta_vazio(self):
        r = whatsapp.list_chats(query="Beatitude", include_last_message=False)
        self.assertEqual(len(r), 1)
        self.assertEqual(r[0].name, "Beatitude")
        self.assertIsNone(r[0].last_message)


if __name__ == "__main__":
    unittest.main()
