#!/bin/bash
# assinar-regras.sh — assina regras/mordomo.fonte.json em regras/mordomo.json.
# A chave privada vem de LUMNIA_REGRAS_CHAVE ou de ~/.lumnia/regras.key.
# Depois: git add regras/ && git commit && git push (na main). As pontes pegam em até 30 min.
set -euo pipefail
RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$RAIZ/regras/assinar"
go run . assinar ../mordomo.json
go run . conferir ../mordomo.json
