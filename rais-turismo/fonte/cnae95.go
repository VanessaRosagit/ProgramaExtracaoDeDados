package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Nomes da CNAE 1.0 (a "CNAE 95" da RAIS 2000–2005, classe de 5 dígitos).
// O dicionario.csv da RAIS não traz esses nomes e a API do IBGE só tem a CNAE 2.0, então a fonte é a
// tabela oficial do IBGE/CONCLA (https://concla.ibge.gov.br/images/concla/downloads/CNAE1.0.xls),
// que o usuário salva como CSV em apoio/cnae95_classes.csv. Nada é inventado: sem o arquivo, ficam só os códigos.
const arquivoCnae95 = "cnae95_classes.csv"

var reCnae95Pontuado = regexp.MustCompile(`^\d{2}\.\d{2}-\d$`)

// parseCnae95 lê o CSV/TXT (separador ; , ou tab) e devolve código(5 dígitos) -> denominação.
// Em cada linha, procura um campo "01.11-0" ou "01110" e usa o próximo campo não vazio como denominação.
func parseCnae95(raw []byte) map[string]string {
	out := map[string]string{}
	txt := strings.ReplaceAll(strings.TrimPrefix(decodeText(raw), "\xef\xbb\xbf"), "\r", "")
	for _, ln := range strings.Split(txt, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		delim := byte(';')
		best := -1
		for _, d := range []byte{';', '\t', ','} {
			if c := strings.Count(ln, string(d)); c > best {
				delim, best = d, c
			}
		}
		fields := splitFields([]byte(ln), delim, nil)
		for i, f := range fields {
			c := strings.Trim(strings.TrimSpace(string(f)), `"`)
			var cod string
			switch {
			case reCnae95Pontuado.MatchString(c):
				cod = strings.NewReplacer(".", "", "-", "").Replace(c)
			case len(c) == 5 && isDigits(c):
				cod = c
			default:
				continue
			}
			for _, g := range fields[i+1:] {
				d := strings.TrimSpace(strings.Trim(strings.TrimSpace(string(g)), `"`))
				if d != "" {
					out[cod] = d
					break
				}
			}
			break
		}
	}
	return out
}

func loadCnae95(apoio string) map[string]string {
	b, err := os.ReadFile(filepath.Join(apoio, arquivoCnae95))
	if err != nil {
		return map[string]string{}
	}
	return parseCnae95(b)
}
