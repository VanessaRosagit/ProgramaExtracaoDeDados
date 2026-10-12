package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Busca de ocupações CBO 2002 por texto: títulos oficiais (CBO2002 - Ocupacao.csv) e sinônimos (CBO2002 - Sinonimo.csv).
// Serve para o usuário escolher, com os olhos dele, os códigos de hospitalidade, gastronomia, transporte etc.
// que a planilha do Observatório cita sem código. O programa não decide nada por ele.

type cboItem struct {
	Cod, Nome string
	Sins      []string
}

var (
	cboOnce  sync.Once
	cboLista []cboItem
	cboNomes map[string]string
)

func carregaCboBusca(apoio string) {
	cboOnce.Do(func() {
		c, err := loadCBO(apoio)
		if err != nil {
			return
		}
		cboNomes = c.Ocup
		sins := map[string][]string{}
		var raw []byte
		if apoio != "" {
			raw, _ = os.ReadFile(filepath.Join(apoio, "CBO2002 - Sinonimo.csv"))
		}
		if raw == nil {
			raw, _ = assets.ReadFile("assets/CBO2002 - Sinonimo.csv")
		}
		r := csv.NewReader(strings.NewReader(decodeText(raw)))
		r.Comma = ';'
		r.LazyQuotes = true
		r.FieldsPerRecord = -1
		rows, _ := r.ReadAll()
		for i, rec := range rows {
			if i == 0 || len(rec) < 2 {
				continue
			}
			sins[strings.TrimSpace(rec[0])] = append(sins[strings.TrimSpace(rec[0])], strings.TrimSpace(rec[1]))
		}
		for cod, nome := range c.Ocup {
			cboLista = append(cboLista, cboItem{cod, nome, sins[cod]})
		}
		sort.Slice(cboLista, func(i, j int) bool { return cboLista[i].Cod < cboLista[j].Cod })
	})
}

type cboAchado struct {
	Cod     string `json:"cod"`
	Nome    string `json:"nome"`
	Achado  string `json:"achado"` // "título" ou o sinônimo que casou
	Familia string `json:"familia"`
}

func buscaCBO(q string, max int) []cboAchado {
	nq := normBusca(q)
	if nq == "" {
		return nil
	}
	var out []cboAchado
	for _, it := range cboLista {
		onde := ""
		if strings.Contains(normBusca(it.Nome), nq) || strings.HasPrefix(it.Cod, strings.TrimSpace(q)) && len(strings.TrimSpace(q)) >= 3 {
			onde = "título"
		} else {
			for _, s := range it.Sins {
				if strings.Contains(normBusca(s), nq) {
					onde = "sinônimo: " + s
					break
				}
			}
		}
		if onde != "" {
			out = append(out, cboAchado{Cod: it.Cod, Nome: it.Nome, Achado: onde})
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func normBusca(s string) string { return strings.ToLower(strings.TrimSpace(semAcentos(s))) }
