package main

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// storeDeTeste abre um banco em memoria com o mesmo esquema da ponte.
func storeDeTeste(t *testing.T) *MessageStore {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
			is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
			file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));`)
	if err != nil {
		t.Fatal(err)
	}
	return &MessageStore{db: db}
}

func TestContextoPronto(t *testing.T) {
	antigo := panel.store
	panel.store = storeDeTeste(t)
	defer func() { panel.store = antigo }()
	db := panel.store.db
	agora := time.Now()
	grupo := "1203@g.us"
	ins := func(jid, nome string, msgs ...[2]string) {
		_, _ = db.Exec(`INSERT INTO chats VALUES (?, ?, ?)`, jid, nome, agora)
		for i, m := range msgs {
			_, _ = db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename) VALUES (?, ?, '', ?, ?, ?, '', '')`,
				jid+"-"+string(rune('a'+i)), jid, m[1], agora.Add(time.Duration(i-10)*time.Minute), m[0] == "eu")
		}
	}
	ins("5511999990001@s.whatsapp.net", "Lisiane", [2]string{"eu", "oi amor"}, [2]string{"ela", "chega que horas?"})
	ins("5511999990002@s.whatsapp.net", "Bernardo Sócio", [2]string{"ela", "viu o contrato?"}, [2]string{"eu", "vi, respondo amanhã"})
	ins("5511999990003@s.whatsapp.net", "Loja XYZ", [2]string{"ela", "promoção imperdível"})
	ins(grupo, "Beatitude", [2]string{"eu", "pendentes"})

	// pendentes: Lisiane e Loja terminam com a outra pessoa; Bernardo nao; o grupo nunca
	p := beatitudeCandidatasPendentes(grupo)
	for _, quer := range []string{"Lisiane", "Loja XYZ", "chega que horas?"} {
		if !strings.Contains(p, quer) {
			t.Errorf("pendentes sem %q:\n%s", quer, p)
		}
	}
	for _, naoQuer := range []string{"Bernardo", "Beatitude ·"} {
		if strings.Contains(p, naoQuer) {
			t.Errorf("pendentes não devia ter %q:\n%s", naoQuer, p)
		}
	}

	// citadas: acha "Lisiane" sem acento e caixa; nao acha "amor" (3+ letras mas nao e nome)
	c := beatitudeConversasCitadas(grupo, "responde a LISIANE que chego às 20h")
	if !strings.Contains(c, "5511999990001@s.whatsapp.net") || strings.Contains(c, "Bernardo") {
		t.Errorf("citadas errado:\n%s", c)
	}
	if beatitudeConversasCitadas(grupo, "pendentes") != "" {
		t.Error("pendentes não cita ninguém")
	}

	// sem circulo, quem-e-quem fica vazio e o contexto ainda sai pelas outras partes
	ponte.mu.Lock()
	ponteCirculo.em = agora
	ponteCirculo.telefones = map[string]bool{}
	ponte.mu.Unlock()
	if q := beatitudeQuemEQuem(grupo); q != "" {
		t.Errorf("quem-é-quem sem círculo devia ser vazio: %s", q)
	}
	todo := beatitudeContextoPronto(grupo, "pendentes")
	if !strings.HasPrefix(todo, "Contexto pronto") || !strings.Contains(todo, "Candidatas") {
		t.Errorf("contexto pronto errado:\n%s", todo)
	}

	// com circulo: Lisiane entra, Loja nao
	ponte.mu.Lock()
	ponteCirculo.telefones = map[string]bool{"5511999990001": true}
	ponte.mu.Unlock()
	q := beatitudeQuemEQuem(grupo)
	if !strings.Contains(q, "Lisiane") || strings.Contains(q, "Loja") {
		t.Errorf("quem-é-quem errado:\n%s", q)
	}
}

func TestPedeModeloForte(t *testing.T) {
	for _, s := range []string{"pensa melhor nisso", "Pense bem antes", "faz com calma", "capricha na mensagem"} {
		if !beatitudePedeModeloForte(s) {
			t.Errorf("%q devia pedir o modelo forte", s)
		}
	}
	for _, s := range []string{"pendentes", "responde a Lisiane que chego às 20h", "o que penso sobre isso"} {
		if beatitudePedeModeloForte(s) {
			t.Errorf("%q não devia pedir o modelo forte", s)
		}
	}
}

// Regressao do bug achado em 15-10: MAX()/MIN() de timestamp voltam como
// texto e o Scan em *time.Time falhava em silencio; o retrato e as "ultimas"
// saiam sempre vazios.
func TestTempoSQLDeAgregacao(t *testing.T) {
	st := storeDeTeste(t)
	agora := time.Now().Truncate(time.Second)
	_, _ = st.db.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename) VALUES ('a','x@s.whatsapp.net','', 'oi', ?, 0, '', '')`, agora)
	var bruto interface{}
	if err := st.db.QueryRow(`SELECT MAX(timestamp) FROM messages`).Scan(&bruto); err != nil {
		t.Fatal(err)
	}
	em, ok := ponteTempoSQL(bruto)
	if !ok || !em.Equal(agora) {
		t.Fatalf("MAX(timestamp): veio %v (%T), ok=%v, queria %v", bruto, bruto, ok, agora)
	}
	if _, ok := ponteTempoSQL(nil); ok {
		t.Error("nil não é tempo")
	}
	// e o caminho antigo de fato falha (documenta o porquê do helper)
	var direto *time.Time
	if err := st.db.QueryRow(`SELECT MAX(timestamp) FROM messages`).Scan(&direto); err == nil {
		t.Log("o driver passou a parsear agregações; o helper continua correto, só deixou de ser necessário")
	}
}
