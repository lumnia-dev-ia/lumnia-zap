package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestNotaNome(t *testing.T) {
	casos := []struct {
		nome, termo string
		nota        int
	}{
		{"Diego Penna Moreira", "Diego Penna Moreira", 3},
		{"Diègo", "diego", 3},
		{"Diego Penna Moreira", "Diego Moreira", 2},
		{"Diego Filho 🙂", "Diego", 1},
		{"Paulo Diego", "Diego", 0},
		{"Diegão", "Diego", 0},
		{"", "Diego", 0},
	}
	for _, c := range casos {
		if got := notaNome(c.nome, c.termo); got != c.nota {
			t.Errorf("notaNome(%q, %q) = %d, quer %d", c.nome, c.termo, got, c.nota)
		}
	}
}

func TestCasarContatosOrdemELimite(t *testing.T) {
	agora := time.Now()
	locais := []contatoLocal{
		{Telefone: "5511900000001", Nomes: []string{"Diego Academia"}, Ultima: agora.Add(-90 * 24 * time.Hour)},
		{Telefone: "5511945331771", Nomes: []string{"Diego", "Diego Penna Moreira"}, Ultima: agora.Add(-time.Hour)},
		{Telefone: "5511900000002", Nomes: []string{"Diego Contador"}, Ultima: agora.Add(-24 * time.Hour)},
		{Telefone: "5511900000003", Nomes: []string{"Diego Uber"}},
		{Telefone: "5511900000004", Nomes: []string{"Paula"}, Ultima: agora},
	}
	got := casarContatos(locais, []string{"Diego"}, 3)
	if len(got) != 3 {
		t.Fatalf("quer 3 candidatos, veio %d", len(got))
	}
	if got[0].Telefone != "5511945331771" {
		t.Fatalf("nome igual deveria vir primeiro, veio %s", got[0].Telefone)
	}
	if got[1].Telefone != "5511900000002" || got[2].Telefone != "5511900000001" {
		t.Fatalf("na mesma nota, mais recente primeiro: %v", got)
	}
	for _, c := range got {
		if c.Telefone == "5511900000004" {
			t.Fatal("Paula não casa com Diego")
		}
	}
	if n := melhorNome(got[0], []string{"Diego"}); n != "Diego" {
		t.Fatalf("melhorNome = %q", n)
	}
}

func TestCasarContatosApelido(t *testing.T) {
	locais := []contatoLocal{{Telefone: "5547999990000", Nomes: []string{"Lise ❤️"}}}
	if got := casarContatos(locais, []string{"Lisiane", "Lise"}, 3); len(got) != 1 {
		t.Fatalf("apelido deveria casar: %v", got)
	}
}

func TestCortarUTF16(t *testing.T) {
	if got := cortarUTF16("Diego", 80); got != "Diego" {
		t.Fatal(got)
	}
	longo := strings.Repeat("😀", 60) // 120 unidades
	got := cortarUTF16(longo, 80)
	if n := len(utf16.Encode([]rune(got))); n > 80 {
		t.Fatalf("passou de 80: %d", n)
	}
	exato := strings.Repeat("a", 80)
	if cortarUTF16(exato, 80) != exato {
		t.Fatal("80 cabe inteiro")
	}
}
