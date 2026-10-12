package main

import (
	_ "embed"
	"encoding/csv"
	"strings"
)

// Lista de Atividades Características do Turismo (ACTs), da aba "CNAEs Turismo OMT"
// da planilha do Observatório de Turismo. nucleo=1: metodologia IBGE/IPEA; nucleo=0: CNAEs
// extras (em verde na planilha), incluídos por constarem no Cadastur.
//
//go:embed assets/cnae_turismo.csv
var cnaeTurismoCSV []byte

type actInfo struct {
	Cod, Desc, Grupo string
	Nucleo           bool
}

type turismoLista struct {
	Por    map[string]actInfo // chave: 7 dígitos
	Ordem  []actInfo
	Grupos []string // na ordem da planilha
}

var turismo = carregaTurismo()

func carregaTurismo() *turismoLista {
	t := &turismoLista{Por: map[string]actInfo{}}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(cnaeTurismoCSV), "\xef\xbb\xbf")))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		return t
	}
	seen := map[string]bool{}
	for i, x := range rows {
		if i == 0 || len(x) < 4 {
			continue
		}
		a := actInfo{Cod: x[0], Desc: x[1], Grupo: x[2], Nucleo: x[3] == "1"}
		t.Por[a.Cod] = a
		t.Ordem = append(t.Ordem, a)
		if !seen[a.Grupo] {
			seen[a.Grupo] = true
			t.Grupos = append(t.Grupos, a.Grupo)
		}
	}
	return t
}

// norm7 devolve o código CNAE com 7 dígitos (só dígitos, zeros à esquerda) ou "".
func norm7(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	if len(b) == 0 || len(b) > 7 {
		return ""
	}
	return strings.Repeat("0", 7-len(b)) + string(b)
}

// codigosTurismo lista os códigos (com e sem zero à esquerda) aceitos pelo modo: "nucleo" ou "todas".
func codigosTurismo(modo string) []string {
	var out []string
	for _, a := range turismo.Ordem {
		if modo == "nucleo" && !a.Nucleo {
			continue
		}
		out = append(out, a.Cod)
		if strings.HasPrefix(a.Cod, "0") {
			out = append(out, strings.TrimLeft(a.Cod, "0"))
		}
	}
	return out
}
