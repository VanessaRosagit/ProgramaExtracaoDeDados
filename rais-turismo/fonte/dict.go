package main

import (
	"bytes"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed assets/dicionario.csv assets/CBO2002*.csv assets/municipios_rms.csv
var assets embed.FS

type interval struct{ ini, fim float64 }

// parseCobertura entende: "(1)" todos | "2006(1)" de 2006 em diante | "(1)1993" até 1993 |
// "1985(1)1993" intervalo | "2023" ano único | vários separados por vírgula.
func parseCobertura(s string) []interval {
	s = strings.TrimSpace(s)
	all := []interval{{math.Inf(-1), math.Inf(1)}}
	if s == "" {
		return all
	}
	var out []interval
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if i := strings.Index(p, "(1)"); i >= 0 {
			l, r := strings.TrimSpace(p[:i]), strings.TrimSpace(p[i+3:])
			iv := interval{math.Inf(-1), math.Inf(1)}
			if l != "" {
				v, err := strconv.Atoi(l)
				if err != nil {
					return all
				}
				iv.ini = float64(v)
			}
			if r != "" {
				v, err := strconv.Atoi(r)
				if err != nil {
					return all
				}
				iv.fim = float64(v)
			}
			out = append(out, iv)
		} else if len(p) == 4 && isDigits(p) {
			v, _ := strconv.Atoi(p)
			out = append(out, interval{float64(v), float64(v)})
		} else {
			return all
		}
	}
	return out
}

func (iv interval) dist(y float64) float64 {
	if y >= iv.ini && y <= iv.fim {
		return 0
	}
	d := math.Inf(1)
	if !math.IsInf(iv.ini, 0) {
		d = math.Min(d, math.Abs(y-iv.ini))
	}
	if !math.IsInf(iv.fim, 0) {
		d = math.Min(d, math.Abs(y-iv.fim))
	}
	return d
}

type dicEntry struct {
	iv    interval
	valor string
}

// Dict guarda os rótulos do dicionario.csv por coluna canônica e chave normalizada.
type Dict struct {
	entries map[string]map[string][]dicEntry // canon -> chave -> entradas
}

func loadDict(b []byte) (*Dict, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("dicionario.csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("dicionario.csv vazio")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimSpace(h)] = i
	}
	for _, need := range []string{"nome_coluna", "chave", "cobertura_temporal", "valor"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("dicionario.csv sem a coluna %s", need)
		}
	}
	inv := map[string][]string{} // nome no dicionário -> canons
	for canon, names := range deParaDic {
		for _, n := range names {
			inv[n] = append(inv[n], canon)
		}
	}
	d := &Dict{entries: map[string]map[string][]dicEntry{}}
	get := func(rec []string, name string) string {
		i := col[name]
		if i < len(rec) {
			return rec[i]
		}
		return ""
	}
	for _, rec := range rows[1:] {
		if idt, ok := col["id_tabela"]; ok && idt < len(rec) && rec[idt] != "microdados_vinculos" {
			continue
		}
		chave := strings.TrimSpace(get(rec, "chave"))
		if chave == "" {
			continue
		}
		for _, canon := range inv[get(rec, "nome_coluna")] {
			if d.entries[canon] == nil {
				d.entries[canon] = map[string][]dicEntry{}
			}
			k := normKey(chave)
			for _, iv := range parseCobertura(get(rec, "cobertura_temporal")) {
				d.entries[canon][k] = append(d.entries[canon][k], dicEntry{iv, get(rec, "valor")})
			}
		}
	}
	return d, nil
}

// resolve devolve, para o ano, o rótulo de cada chave conforme a cobertura temporal do dicionário.
// Chave sem rótulo válido naquele ano não entra: o valor fica como o código original.
func (d *Dict) resolve(ano int) map[string]map[string]string {
	out := map[string]map[string]string{}
	for canon, keys := range d.entries {
		m := make(map[string]string, len(keys))
		for k, es := range keys {
			for _, e := range es {
				if e.iv.dist(float64(ano)) == 0 {
					m[k] = e.valor
					break
				}
			}
		}
		out[canon] = m
	}
	return out
}

// ---------- CBO ----------

func parseCBO(b []byte) map[string]string {
	m := map[string]string{}
	r := csv.NewReader(strings.NewReader(decodeText(b)))
	r.Comma = ';'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, _ := r.ReadAll()
	for i, rec := range rows {
		if i == 0 || len(rec) < 2 {
			continue
		}
		code := strings.TrimSpace(rec[0])
		if code != "" {
			m[code] = strings.TrimSpace(rec[1])
		}
	}
	return m
}

type CBO struct{ Ocup, Familia, SubGrupo, SubPrincipal, Grande map[string]string }

func loadCBO(apoioOverride string) (*CBO, error) {
	read := func(name string) (map[string]string, error) {
		if apoioOverride != "" {
			if b, err := os.ReadFile(filepath.Join(apoioOverride, name)); err == nil {
				return parseCBO(b), nil
			}
		}
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return nil, err
		}
		return parseCBO(b), nil
	}
	var c CBO
	var err error
	for _, x := range []struct {
		dst  *map[string]string
		name string
	}{
		{&c.Ocup, "CBO2002 - Ocupacao.csv"}, {&c.Familia, "CBO2002 - Familia.csv"},
		{&c.SubGrupo, "CBO2002 - SubGrupo.csv"}, {&c.SubPrincipal, "CBO2002 - SubGrupo Principal.csv"},
		{&c.Grande, "CBO2002 - Grande Grupo.csv"},
	} {
		if *x.dst, err = read(x.name); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// ---------- municípios e CNAE (API do IBGE, com cópia local) ----------

type Muni struct{ Cod6, Nome string }

var httpClient = &http.Client{Timeout: 10 * time.Second}

func getJSON(url string, v interface{}) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d em %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeCSVFile(path string, rows [][]string) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	_ = w.WriteAll(rows)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func readCSVFile(path string) ([][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(bytes.NewReader(b))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

// munisRMS: os 29 municípios da RMS de Sorocaba (código de 6 dígitos e nome, extraídos do próprio arquivo RAIS 2001).
func munisRMS() []Muni {
	b, err := assets.ReadFile("assets/municipios_rms.csv")
	if err != nil {
		return nil
	}
	var out []Muni
	for i, ln := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		if i == 0 || strings.TrimSpace(ln) == "" {
			continue
		}
		if c, n, ok := strings.Cut(ln, ";"); ok {
			out = append(out, Muni{c, n})
		}
	}
	return out
}

func loadCnae(cfg Config, apoio, endpoint string, width int) map[string]string {
	cache := filepath.Join(apoio, "cnae20_"+endpoint+".csv")
	out := map[string]string{}
	if rows, err := readCSVFile(cache); err == nil && len(rows) > 1 {
		for _, r := range rows[1:] {
			if len(r) >= 2 {
				out[r[0]] = r[1]
			}
		}
		return out
	}
	var raw []struct {
		ID        interface{} `json:"id"`
		Descricao string      `json:"descricao"`
	}
	if err := getJSON(cfg.IBGEAPI+"/api/v2/cnae/"+endpoint, &raw); err != nil {
		return out // sem nomes de CNAE; o resto segue
	}
	rows := [][]string{{"codigo", "titulo"}}
	for _, x := range raw {
		var id string
		switch v := x.ID.(type) {
		case string:
			id = v
		case float64:
			id = strconv.FormatInt(int64(v), 10)
		}
		id = zpad(strings.TrimSpace(id), width)
		out[id] = x.Descricao
		rows = append(rows, []string{id, x.Descricao})
	}
	_ = writeCSVFile(cache, rows)
	return out
}

// Support reúne tudo que o processamento de um ano precisa.
type Support struct {
	Dict    *Dict
	CBO     *CBO
	Munis   []Muni            // todos da UF
	Alvo    map[string]string // cod6 -> nome (recorte escolhido)
	Classe  map[string]string
	Subclas map[string]string
	Cnae95  map[string]string
	NomeMun map[string]string
}

func loadSupport(cfg Config, logf func(string, ...interface{})) (*Support, error) {
	d := cfg.Dirs()
	d.Make()
	dicBytes, err := os.ReadFile(filepath.Join(d.Apoio, "dicionario.csv"))
	if err != nil {
		dicBytes, err = assets.ReadFile("assets/dicionario.csv")
		if err != nil {
			return nil, err
		}
	} else {
		logf("Usando dicionario.csv da pasta apoio.")
	}
	dict, err := loadDict(dicBytes)
	if err != nil {
		return nil, err
	}
	cbo, err := loadCBO(d.Apoio)
	if err != nil {
		return nil, err
	}
	munis := munisRMS()
	s := &Support{Dict: dict, CBO: cbo, Munis: munis, Alvo: map[string]string{}, NomeMun: map[string]string{}}
	for _, m := range munis {
		s.NomeMun[m.Cod6] = m.Nome
		s.Alvo[m.Cod6] = m.Nome
	}
	s.Classe = loadCnae(cfg, d.Apoio, "classes", 5)
	s.Subclas = loadCnae(cfg, d.Apoio, "subclasses", 7)
	s.Cnae95 = loadCnae95(d.Apoio)
	if len(s.Cnae95) == 0 {
		logf("AVISO: sem nomes da CNAE 1.0 (anos 2000–2005): salve a tabela oficial do IBGE (CNAE1.0.xls, https://concla.ibge.gov.br/images/concla/downloads/CNAE1.0.xls) como CSV em %s e rode de novo (Refazer). Sem ela ficam só os códigos.", filepath.Join(d.Apoio, arquivoCnae95))
	} else {
		logf("CNAE 1.0: %d classes com nome carregadas de apoio/%s.", len(s.Cnae95), arquivoCnae95)
	}
	// nomes literais da planilha do Observatório para as 104 CNAEs de turismo (se o IBGE não os trouxe)
	if s.Subclas == nil {
		s.Subclas = map[string]string{}
	}
	for cod, a := range turismo.Por {
		if _, ok := s.Subclas[cod]; !ok {
			s.Subclas[cod] = a.Desc
		}
	}
	logf("Recorte: %d municípios da RMS de Sorocaba.", len(s.Alvo))
	return s, nil
}
