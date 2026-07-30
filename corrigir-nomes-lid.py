#!/usr/bin/env python3
"""
Corrige retroativamente os nomes das conversas identificadas por LID.

O WhatsApp usa identificadores opacos (@lid) em vez do numero de telefone em
varias conversas. A ponte grava o LID cru como nome do chat, o que quebra a
busca por nome. Este script resolve o LID para o telefone (usando a tabela de
mapeamento que o proprio whatsmeow mantem) e grava o nome do contato salvo.

Idempotente: so altera linhas cujo nome ainda e o LID cru.
Rode com a ponte parada ou rodando — usa busy_timeout para evitar conflito.
"""
import os
import sqlite3
import sys

BASE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "whatsapp-bridge", "store")
MESSAGES_DB = os.path.join(BASE, "messages.db")
WHATSAPP_DB = os.path.join(BASE, "whatsapp.db")

for caminho in (MESSAGES_DB, WHATSAPP_DB):
    if not os.path.exists(caminho):
        sys.exit(f"ERRO: banco nao encontrado: {caminho}")

w = sqlite3.connect(f"file:{WHATSAPP_DB}?mode=ro", uri=True)
m = sqlite3.connect(MESSAGES_DB, timeout=20)
m.execute("PRAGMA busy_timeout=20000")

lidmap = dict(w.execute("SELECT lid, pn FROM whatsmeow_lid_map"))

contatos = {}
for their, first, full, push, biz in w.execute(
    "SELECT their_jid, first_name, full_name, push_name, business_name FROM whatsmeow_contacts"
):
    pn = their.split("@")[0].split(":")[0]
    nome = (full or biz or push or first or "").strip()
    if nome and pn not in contatos:
        contatos[pn] = nome

alvos = m.execute(
    "SELECT jid, name FROM chats "
    "WHERE jid LIKE '%@lid' AND name = substr(jid, 1, instr(jid, '@') - 1)"
).fetchall()

com_nome, com_telefone = [], []
for jid, _ in alvos:
    pn = lidmap.get(jid.split("@")[0])
    if not pn:
        continue
    if contatos.get(pn):
        com_nome.append((contatos[pn], jid))
    else:
        com_telefone.append((pn, jid))

m.executemany("UPDATE chats SET name = ? WHERE jid = ?", com_nome + com_telefone)
m.commit()

print(f"conversas LID sem nome legivel .... {len(alvos)}")
print(f"corrigidas com nome do contato ... {len(com_nome)}")
print(f"corrigidas com numero de telefone  {len(com_telefone)}")
print(f"sem mapeamento disponivel ........ {len(alvos) - len(com_nome) - len(com_telefone)}")

if com_nome:
    print("\nexemplos:")
    for nome, jid in com_nome[:10]:
        print(f"  {jid.split('@')[0]} -> {nome}")
