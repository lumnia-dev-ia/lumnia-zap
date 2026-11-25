// Copyright (c) 2026 Lumnia — Diego Penna Moreira
// SPDX-License-Identifier: MIT

// assinar — assina o pacote de regras do mordomo do Beatitude.
//
//	bash scripts/assinar-regras.sh                        # o caminho de sempre (assina e confere)
//	go run . assinar ../mordomo.json                      # de dentro de regras/assinar: gera mordomo.json de mordomo.fonte.json
//	go run . conferir ../mordomo.json                     # confere com a chave pública
//	go run . chave                                        # cria um par novo (troca exige versão nova do Zap)
//
// A chave privada vem de LUMNIA_REGRAS_CHAVE (o conteúdo) ou do arquivo
// ~/.lumnia/regras.key. Nunca entra no repositório.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// a mesma da ponte (whatsapp-bridge/beatitude_regras.go)
const chavePublica = "jLsYXoQ3EkISBnWbLXfSAQavlcz5/FKM9jwpCEBDgd4="

type arquivo struct {
	Pacote     string `json:"pacote"`
	Assinatura string `json:"assinatura"`
}

func falhar(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "erro: "+f+"\n", a...)
	os.Exit(1)
}

func privada() ed25519.PrivateKey {
	txt := strings.TrimSpace(os.Getenv("LUMNIA_REGRAS_CHAVE"))
	if txt == "" {
		casa, _ := os.UserHomeDir()
		b, err := os.ReadFile(filepath.Join(casa, ".lumnia", "regras.key"))
		if err != nil {
			falhar("sem chave: defina LUMNIA_REGRAS_CHAVE ou crie ~/.lumnia/regras.key")
		}
		txt = strings.TrimSpace(string(b))
	}
	seed, err := base64.StdEncoding.DecodeString(txt)
	if err != nil || len(seed) != ed25519.SeedSize {
		falhar("chave privada inválida")
	}
	return ed25519.NewKeyFromSeed(seed)
}

func main() {
	if len(os.Args) < 2 {
		falhar("uso: assinar|conferir <regras/mordomo.json> | chave")
	}
	switch os.Args[1] {
	case "chave":
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		fmt.Println("privada (guarde fora do repositório):", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("pública (cole em beatitude_regras.go e aqui):", base64.StdEncoding.EncodeToString(pub))
	case "assinar":
		if len(os.Args) < 3 {
			falhar("diga o arquivo de saída: regras/mordomo.json")
		}
		saida := os.Args[2]
		fonte := strings.TrimSuffix(saida, ".json") + ".fonte.json"
		corpo, err := os.ReadFile(fonte)
		if err != nil {
			falhar("não li %s: %v", fonte, err)
		}
		var conf struct {
			Versao int `json:"versao"`
		}
		if err := json.Unmarshal(corpo, &conf); err != nil {
			falhar("%s não é JSON válido: %v", fonte, err)
		}
		if conf.Versao <= 0 {
			falhar("%s precisa de \"versao\" maior que zero", fonte)
		}
		if anterior, err := os.ReadFile(saida); err == nil {
			var a arquivo
			if json.Unmarshal(anterior, &a) == nil {
				if b, err := base64.StdEncoding.DecodeString(a.Pacote); err == nil {
					var v struct {
						Versao int `json:"versao"`
					}
					if json.Unmarshal(b, &v) == nil && conf.Versao <= v.Versao && !bytes.Equal(b, corpo) {
						falhar("aumente \"versao\" (publicado: %d; as pontes recusam pacote mais velho ou igual)", v.Versao)
					}
				}
			}
		}
		priv := privada()
		if base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)) != chavePublica {
			falhar("esta chave privada não corresponde à chave pública das pontes")
		}
		out, _ := json.MarshalIndent(arquivo{
			Pacote:     base64.StdEncoding.EncodeToString(corpo),
			Assinatura: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, corpo)),
		}, "", "  ")
		if err := os.WriteFile(saida, append(out, '\n'), 0o644); err != nil {
			falhar("%v", err)
		}
		fmt.Printf("assinado: %s (versão %d)\n", saida, conf.Versao)
	case "conferir":
		if len(os.Args) < 3 {
			falhar("diga o arquivo: regras/mordomo.json")
		}
		b, err := os.ReadFile(os.Args[2])
		if err != nil {
			falhar("%v", err)
		}
		var a arquivo
		if err := json.Unmarshal(b, &a); err != nil {
			falhar("arquivo ilegível: %v", err)
		}
		pub, _ := base64.StdEncoding.DecodeString(chavePublica)
		corpo, err1 := base64.StdEncoding.DecodeString(a.Pacote)
		sig, err2 := base64.StdEncoding.DecodeString(a.Assinatura)
		if err1 != nil || err2 != nil || !ed25519.Verify(pub, corpo, sig) {
			falhar("assinatura NÃO confere")
		}
		fmt.Println("assinatura confere")
	default:
		falhar("comando desconhecido: %s", os.Args[1])
	}
}
