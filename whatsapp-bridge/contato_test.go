package main

import (
	"testing"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

func TestFormatContactCard(t *testing.T) {
	vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:;Quiropraxia Cristiano Pires;;;\r\nFN:Quiropraxia Cristiano Pires\r\nitem1.TEL;waid=5511971306657:+55 11 97130-6657\r\nitem1.X-ABLabel:Celular\r\nEND:VCARD"
	got := formatContactCard("Quiropraxia Cristiano Pires", vcard)
	want := "[contato] Quiropraxia Cristiano Pires — +55 11 97130-6657 (WhatsApp: 5511971306657)"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}

	// sem nome de exibição, telefone sem waid
	got = formatContactCard("", "BEGIN:VCARD\nFN:Fulano\nTEL;TYPE=CELL:+55 47 98888-0000\nEND:VCARD")
	if got != "[contato] Fulano — +55 47 98888-0000" {
		t.Fatalf("got %q", got)
	}

	if formatContactCard("", "") != "" {
		t.Fatal("cartão vazio deveria dar string vazia")
	}
}

func TestExtractTextContentContacts(t *testing.T) {
	c := &waProto.ContactMessage{
		DisplayName: proto.String("A"),
		Vcard:       proto.String("BEGIN:VCARD\nTEL;waid=551100000000:+55 11 0000-0000\nEND:VCARD"),
	}
	if got := extractTextContent(&waProto.Message{ContactMessage: c}); got != "[contato] A — +55 11 0000-0000 (WhatsApp: 551100000000)" {
		t.Fatalf("um contato: %q", got)
	}

	arr := &waProto.ContactsArrayMessage{Contacts: []*waProto.ContactMessage{c, {DisplayName: proto.String("B")}}}
	if got := extractTextContent(&waProto.Message{ContactsArrayMessage: arr}); got != "[contato] A — +55 11 0000-0000 (WhatsApp: 551100000000)\n[contato] B" {
		t.Fatalf("vários contatos: %q", got)
	}
}
