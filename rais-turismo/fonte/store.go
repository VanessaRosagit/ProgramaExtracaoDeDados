package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gdict struct {
	vals []string
	idx  map[string]uint16
}

func newGDict() *gdict { return &gdict{vals: []string{""}, idx: map[string]uint16{"": 0}} }

func (g *gdict) add(s string) uint16 {
	if c, ok := g.idx[s]; ok {
		return c
	}
	if len(g.vals) >= 65535 {
		return 65535
	}
	c := uint16(len(g.vals))
	g.vals = append(g.vals, s)
	g.idx[s] = c
	return c
}

type yearData struct {
	rf  *rcolFile
	cat map[string][]uint16
	num map[string][]float32
}

type Store struct {
	mu    sync.RWMutex
	dir   string
	years map[int]*yearData
	g     map[string]*gdict
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, years: map[int]*yearData{}, g: map[string]*gdict{}}
}

var reRcol = regexp.MustCompile(`^rais_(\d{4})\.rcol$`)

// Refresh lê os cabeçalhos dos arquivos de dados (rápido); colunas só são carregadas quando usadas.
func (s *Store) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	es, _ := os.ReadDir(s.dir)
	seen := map[int]bool{}
	for _, e := range es {
		m := reRcol.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		ano, _ := strconv.Atoi(m[1])
		p := filepath.Join(s.dir, e.Name())
		rf, err := openRcolMeta(p)
		if err != nil {
			continue
		}
		seen[ano] = true
		if old, ok := s.years[ano]; ok && old.rf.ModTime.Equal(rf.ModTime) && old.rf.Size == rf.Size {
			continue
		}
		s.years[ano] = &yearData{rf: rf, cat: map[string][]uint16{}, num: map[string][]float32{}}
	}
	for a := range s.years {
		if !seen[a] {
			delete(s.years, a)
		}
	}
}

func (s *Store) Years() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ys []int
	for a := range s.years {
		ys = append(ys, a)
	}
	sort.Ints(ys)
	return ys
}

type YearInfo struct {
	Ano    int    `json:"ano"`
	N      int    `json:"n"`
	Bytes  int64  `json:"bytes"`
	Criado string `json:"criado"`
}

func (s *Store) Info() []YearInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []YearInfo
	for a, y := range s.years {
		out = append(out, YearInfo{a, y.rf.Meta.N, y.rf.Size, y.rf.Meta.Criado})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ano < out[j].Ano })
	return out
}

func (s *Store) kindOf(col string) string {
	for _, y := range s.years {
		if c := y.rf.col(col); c != nil {
			return c.Kind
		}
	}
	return ""
}

// Columns: colunas presentes, na ordem canônica, com o tipo.
func (s *Store) Columns() []struct{ Name, Kind string } {
	s.mu.RLock()
	defer s.mu.RUnlock()
	have := map[string]string{}
	for _, y := range s.years {
		for _, c := range y.rf.Meta.Cols {
			have[c.Name] = c.Kind
		}
	}
	var out []struct{ Name, Kind string }
	for _, n := range canonicalOrder() {
		if k, ok := have[n]; ok {
			out = append(out, struct{ Name, Kind string }{n, k})
			delete(have, n)
		}
	}
	var rest []string
	for n := range have {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	for _, n := range rest {
		out = append(out, struct{ Name, Kind string }{n, have[n]})
	}
	return out
}

func sortKey(s string) string { return strings.ToLower(semAcentos(s)) }

// Values: valores distintos de uma coluna categórica (união dos anos), sem carregar os dados.
func (s *Store) Values(col string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]bool{}
	for _, y := range s.years {
		if c := y.rf.col(col); c != nil && c.Kind == "cat" {
			for _, v := range c.Dict {
				if v != "" {
					set[v] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return sortKey(out[i]) < sortKey(out[j]) })
	return out
}

// ensure carrega (se faltar) as colunas pedidas de todos os anos.
func (s *Store) ensure(cats, nums []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, y := range s.years {
		var f *os.File
		open := func() error {
			if f != nil {
				return nil
			}
			var err error
			f, err = os.Open(y.rf.Path)
			return err
		}
		for _, name := range cats {
			if _, done := y.cat[name]; done {
				continue
			}
			c := y.rf.col(name)
			if s.g[name] == nil {
				s.g[name] = newGDict()
			}
			if c == nil || c.Kind != "cat" {
				y.cat[name] = nil
				continue
			}
			if err := open(); err != nil {
				return err
			}
			blob, err := y.rf.readBlob(f, c)
			if err != nil {
				f.Close()
				return err
			}
			m := make([]uint16, len(c.Dict))
			for i, v := range c.Dict {
				m[i] = s.g[name].add(v)
			}
			codes := decodeU16(blob)
			for i, v := range codes {
				codes[i] = m[v]
			}
			y.cat[name] = codes
		}
		for _, name := range nums {
			if _, done := y.num[name]; done {
				continue
			}
			c := y.rf.col(name)
			if c == nil || c.Kind != "num" {
				y.num[name] = nil
				continue
			}
			if err := open(); err != nil {
				return err
			}
			blob, err := y.rf.readBlob(f, c)
			if err != nil {
				f.Close()
				return err
			}
			y.num[name] = decodeF32(blob)
		}
		if f != nil {
			f.Close()
		}
	}
	return nil
}

// ---------- consultas ----------

type Query struct {
	AnoIni  int                 `json:"a0"`
	AnoFim  int                 `json:"a1"`
	Ativos  bool                `json:"ativos"`
	Filtros map[string][]string `json:"filtros"`
	Group   []string            `json:"group"`
	Order   string              `json:"order"`
	Limit   int                 `json:"limit"`
	// Turismo: "" = todas as atividades; "nucleo" = ACTs (IBGE/IPEA); "todas" = ACTs + extras do Cadastur
	Turismo string `json:"turismo"`
	// CboTurismo: "" = todas; "planilha" = as 8 CBOs da aba CBO da planilha
	CboTurismo string `json:"cbo_turismo"`
	// ColsTurismo (só exportação): acrescenta turismo_nucleo, turismo_grupo e turismo_atividade às linhas
	ColsTurismo bool `json:"-"`
}

type QResult struct {
	Cols   []string        `json:"cols"`
	Rows   [][]interface{} `json:"rows"`
	Ms     int64           `json:"ms"`
	Avisos []string        `json:"avisos,omitempty"`
}

type agg struct {
	n, sSM, cSM, sDZ, cDZ, sId, cId, sTe, cTe float64
}

var metricCols = []string{"vinculos", "rem_media_sm", "rem_dez_nom", "idade_media", "tempo_emprego_medio"}

const colCNAE = "cnae20_subclasse"

// dimensões virtuais (derivadas da subclasse CNAE 2.0 pela lista de ACTs)
func isVirt(g string) bool { return g == "turismo_grupo" || g == "turismo_atividade" }

const colCBO = "cbo_2002"

func (s *Store) cnaeCol() (string, int) {
	if s.kindOf(colCNAE) == "cat" {
		return colCNAE, 7
	}
	return "", 0
}

func (s *Store) virtSrc(g string) string {
	c, _ := s.cnaeCol()
	return c
}

const errSemCNAE = "nenhum dos anos processados tem a coluna CNAE 2.0 subclasse reconhecida. Veja em dados\\diagnostico\\colunas_ANO.csv se a coluna aparece com “canon” preenchido; anos anteriores a 2006 só têm CNAE 1.0"

const colAtivo = "vinculo_ativo_3112"

func (s *Store) prepareFilters(q Query) (map[string][]string, error) {
	f := map[string][]string{}
	for k, v := range q.Filtros {
		if len(v) > 0 {
			f[k] = v
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k := range f {
		if s.kindOf(k) != "cat" {
			return nil, fmt.Errorf("filtro em coluna desconhecida: %s", k)
		}
	}
	if q.Turismo != "" {
		col, _ := s.cnaeCol()
		if col == "" {
			return nil, fmt.Errorf("%s", errSemCNAE)
		}
		f[col] = codigosTurismo(q.Turismo)
	}
	if q.CboTurismo != "" {
		if s.kindOf(colCBO) != "cat" {
			return nil, fmt.Errorf("os dados processados não têm a coluna CBO 2002 (anos anteriores a 2003 usam outra CBO)")
		}
		f[colCBO] = codigosCboTurismo(q.CboTurismo)
	}
	if q.Ativos && s.kindOf(colAtivo) == "cat" {
		f[colAtivo] = []string{"Sim", "1"}
	}
	return f, nil
}

func (s *Store) Query(q Query) (*QResult, error) {
	t0 := time.Now()
	if q.AnoFim < q.AnoIni {
		q.AnoIni, q.AnoFim = q.AnoFim, q.AnoIni
	}
	filtros, err := s.prepareFilters(q)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	for _, g := range q.Group {
		if isVirt(g) {
			if s.kindOf(s.virtSrc(g)) != "cat" {
				s.mu.RUnlock()
				return nil, fmt.Errorf("%s", errSemCNAE)
			}
			continue
		}
		if g != "ano" && s.kindOf(g) != "cat" {
			s.mu.RUnlock()
			return nil, fmt.Errorf("agrupamento em coluna desconhecida: %s", g)
		}
	}
	s.mu.RUnlock()

	var cats []string
	for _, g := range q.Group {
		if isVirt(g) {
			cats = append(cats, s.virtSrc(g))
		} else if g != "ano" {
			cats = append(cats, g)
		}
	}
	for k := range filtros {
		cats = append(cats, k)
	}
	if err := s.ensure(cats, []string{"remun_media_sm", "remun_dez_nom", "idade", "tempo_emprego"}); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	usaCNAE := q.Turismo != ""
	for _, g := range q.Group {
		if isVirt(g) {
			usaCNAE = true
		}
	}
	skip, avisos := s.anosSemClasse(q.AnoIni, q.AnoFim, usaCNAE, q.CboTurismo != "")

	type dim struct {
		isYear bool
		col    string
		size   uint64
		stride uint64
		src    string   // coluna lida (difere de col nas dimensões virtuais)
		vmap   []uint16 // código global -> índice do rótulo (virtuais)
		vlab   []string
	}
	nYears := q.AnoFim - q.AnoIni + 1
	dims := make([]dim, len(q.Group))
	space := uint64(1)
	for i, g := range q.Group {
		d := dim{col: g, src: g}
		if g == "ano" {
			d.isYear, d.size = true, uint64(nYears)
		} else if isVirt(g) {
			d.src = s.virtSrc(g)
			d.vmap, d.vlab = virtMap(g, q.Turismo, s.g[d.src].vals)
			d.size = uint64(len(d.vlab))
		} else {
			d.size = uint64(len(s.g[g].vals))
		}
		dims[i] = d
		if d.size > 0 && space > (1<<40)/d.size {
			return nil, fmt.Errorf("agrupamento grande demais; use menos colunas")
		}
		space *= d.size
	}
	stride := uint64(1)
	for i := len(dims) - 1; i >= 0; i-- {
		dims[i].stride = stride
		stride *= dims[i].size
	}
	useSlice := space <= 1<<22
	var sl []agg
	var mp map[uint64]*agg
	if useSlice {
		sl = make([]agg, space)
	} else {
		mp = map[uint64]*agg{}
	}

	type flt struct {
		col     string
		allowed []bool
	}
	var flts []flt
	for col, vals := range filtros {
		g := s.g[col]
		al := make([]bool, len(g.vals))
		for _, v := range vals {
			if c, ok := g.idx[v]; ok {
				al[c] = true
			}
		}
		flts = append(flts, flt{col, al})
	}

	for ano := q.AnoIni; ano <= q.AnoFim; ano++ {
		y, ok := s.years[ano]
		if !ok || skip[ano] {
			continue
		}
		n := y.rf.Meta.N
		fc := make([][]uint16, len(flts))
		for k, f := range flts {
			fc[k] = y.cat[f.col]
		}
		gc := make([][]uint16, len(dims))
		for k, d := range dims {
			if !d.isYear {
				gc[k] = y.cat[d.src]
			}
		}
		sm, dz, id, te := y.num["remun_media_sm"], y.num["remun_dez_nom"], y.num["idade"], y.num["tempo_emprego"]
		yearCode := uint64(ano - q.AnoIni)
	rows:
		for i := 0; i < n; i++ {
			for k := range flts {
				var c uint16
				if fc[k] != nil {
					c = fc[k][i]
				}
				if !flts[k].allowed[c] {
					continue rows
				}
			}
			var key uint64
			for k, d := range dims {
				var c uint64
				if d.isYear {
					c = yearCode
				} else if gc[k] != nil {
					c = uint64(gc[k][i])
					if d.vmap != nil {
						if int(c) < len(d.vmap) {
							c = uint64(d.vmap[c])
						} else {
							c = 0
						}
					}
				}
				key += c * d.stride
			}
			var a *agg
			if useSlice {
				a = &sl[key]
			} else {
				a = mp[key]
				if a == nil {
					a = &agg{}
					mp[key] = a
				}
			}
			a.n++
			if sm != nil {
				if v := float64(sm[i]); v > 0 && !math.IsNaN(v) {
					a.sSM += v
					a.cSM++
				}
			}
			if dz != nil {
				if v := float64(dz[i]); v > 0 && !math.IsNaN(v) {
					a.sDZ += v
					a.cDZ++
				}
			}
			if id != nil {
				if v := float64(id[i]); v > 0 && !math.IsNaN(v) {
					a.sId += v
					a.cId++
				}
			}
			if te != nil {
				if v := float64(te[i]); v >= 0 && !math.IsNaN(v) {
					a.sTe += v
					a.cTe++
				}
			}
		}
	}

	type outRow struct {
		labels []string
		years  []int
		a      agg
	}
	var out []outRow
	emit := func(key uint64, a *agg) {
		r := outRow{labels: make([]string, len(dims)), years: make([]int, len(dims))}
		for k, d := range dims {
			c := (key / d.stride) % d.size
			if d.isYear {
				r.years[k] = q.AnoIni + int(c)
			} else {
				if d.vlab != nil {
					r.labels[k] = d.vlab[c]
				} else {
					r.labels[k] = s.g[d.col].vals[c]
				}
			}
		}
		r.a = *a
		out = append(out, r)
	}
	if useSlice {
		for key := range sl {
			if sl[key].n > 0 {
				emit(uint64(key), &sl[key])
			}
		}
	} else {
		for key, a := range mp {
			emit(key, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		for k, d := range dims {
			if d.isYear {
				if out[i].years[k] != out[j].years[k] {
					return out[i].years[k] < out[j].years[k]
				}
				continue
			}
			a, b := out[i].labels[k], out[j].labels[k]
			if a != b {
				if a == "" {
					return false
				}
				if b == "" {
					return true
				}
				return sortKey(a) < sortKey(b)
			}
		}
		return false
	})
	if q.Order == "vinculos_desc" {
		sort.SliceStable(out, func(i, j int) bool { return out[i].a.n > out[j].a.n })
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	res := &QResult{Cols: append(append([]string{}, q.Group...), metricCols...), Rows: make([][]interface{}, 0, len(out)), Avisos: avisos}
	round := func(v float64) interface{} { return math.Round(v*100) / 100 }
	avg := func(sum, c float64) interface{} {
		if c == 0 {
			return nil
		}
		return round(sum / c)
	}
	for _, r := range out {
		row := make([]interface{}, 0, len(dims)+5)
		for k, d := range dims {
			if d.isYear {
				row = append(row, r.years[k])
			} else {
				row = append(row, r.labels[k])
			}
		}
		row = append(row, int64(r.a.n), avg(r.a.sSM, r.a.cSM), avg(r.a.sDZ, r.a.cDZ), avg(r.a.sId, r.a.cId), avg(r.a.sTe, r.a.cTe))
		res.Rows = append(res.Rows, row)
	}
	res.Ms = time.Since(t0).Milliseconds()
	return res, nil
}

// ---------- exportação de linhas ----------

func fmtNum(v float32) string {
	if v != v {
		return ""
	}
	return strings.Replace(strconv.FormatFloat(float64(v), 'f', -1, 32), ".", ",", 1)
}

// ExportCSV grava as linhas (todas as colunas) dos anos/filtros pedidos: ';' e vírgula decimal, UTF-8 com BOM.
func (s *Store) ExportCSV(w io.Writer, q Query) error {
	if q.AnoFim < q.AnoIni {
		q.AnoIni, q.AnoFim = q.AnoFim, q.AnoIni
	}
	filtros, err := s.prepareFilters(q)
	if err != nil {
		return err
	}
	s.mu.RLock()
	var files []*rcolFile
	have := map[string]bool{}
	var years []int
	for a := range s.years {
		if a >= q.AnoIni && a <= q.AnoFim {
			years = append(years, a)
		}
	}
	sort.Ints(years)
	for _, a := range years {
		rf := s.years[a].rf
		files = append(files, rf)
		for _, c := range rf.Meta.Cols {
			have[c.Name] = true
		}
	}
	s.mu.RUnlock()
	var cols []string
	for _, n := range canonicalOrder() {
		if have[n] {
			cols = append(cols, n)
			delete(have, n)
		}
	}
	var rest []string
	for n := range have {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)

	bw := bufio.NewWriterSize(w, 1<<20)
	defer bw.Flush()
	_, _ = bw.WriteString("\xEF\xBB\xBF")
	cw := csv.NewWriter(bw)
	cw.Comma = ';'
	cab := append([]string{"ano"}, cols...)
	if q.ColsTurismo {
		cab = append(cab, "turismo_nucleo", "turismo_grupo", "turismo_atividade")
	}
	_ = cw.Write(cab)
	for _, rf := range files {
		if err := exportYear(cw, rf, cols, filtros, q.ColsTurismo); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func exportYear(cw *csv.Writer, rf *rcolFile, cols []string, filtros map[string][]string, tur bool) error {
	f, err := os.Open(rf.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	n := rf.Meta.N
	type colData struct {
		dict  []string
		codes []uint16
		nums  []float32
	}
	data := make([]*colData, len(cols))
	for i, name := range cols {
		c := rf.col(name)
		if c == nil {
			continue
		}
		blob, err := rf.readBlob(f, c)
		if err != nil {
			return err
		}
		if c.Kind == "num" {
			data[i] = &colData{nums: decodeF32(blob)}
		} else {
			data[i] = &colData{dict: c.Dict, codes: decodeU16(blob)}
		}
	}
	type flt struct {
		codes   []uint16
		allowed map[uint16]bool
	}
	var flts []flt
	for col, vals := range filtros {
		c := rf.col(col)
		if c == nil || c.Kind != "cat" {
			return nil // coluna do filtro não existe neste ano: nenhuma linha passa
		}
		blob, err := rf.readBlob(f, c)
		if err != nil {
			return err
		}
		al := map[uint16]bool{}
		for i, d := range c.Dict {
			for _, v := range vals {
				if d == v {
					al[uint16(i)] = true
				}
			}
		}
		flts = append(flts, flt{decodeU16(blob), al})
	}
	nExtra := 0
	var tCodes []uint16
	var tNuc, tGrp, tAti []string
	if tur {
		nExtra = 3
		if c := rf.col(colCNAE); c != nil && c.Kind == "cat" {
			blob, err := rf.readBlob(f, c)
			if err != nil {
				return err
			}
			tCodes = decodeU16(blob)
			tNuc, tGrp, tAti = make([]string, len(c.Dict)), make([]string, len(c.Dict)), make([]string, len(c.Dict))
			for i, v := range c.Dict {
				if a, ok := turismo.Por[norm7(v)]; ok && v != "" {
					tNuc[i] = "Não"
					if a.Nucleo {
						tNuc[i] = "Sim"
					}
					tGrp[i] = a.Grupo
					tAti[i] = a.Cod[:2] + "." + a.Cod[2:4] + "-" + a.Cod[4:5] + "/" + a.Cod[5:] + " " + a.Desc
				}
			}
		}
	}
	rec := make([]string, len(cols)+1+nExtra)
	ano := strconv.Itoa(rf.Meta.Ano)
rows:
	for i := 0; i < n; i++ {
		for _, fl := range flts {
			if !fl.allowed[fl.codes[i]] {
				continue rows
			}
		}
		rec[0] = ano
		for j, d := range data {
			switch {
			case d == nil:
				rec[j+1] = ""
			case d.nums != nil:
				rec[j+1] = fmtNum(d.nums[i])
			default:
				rec[j+1] = d.dict[d.codes[i]]
			}
		}
		if tur {
			k := len(cols) + 1
			if tCodes != nil {
				rec[k], rec[k+1], rec[k+2] = tNuc[tCodes[i]], tGrp[tCodes[i]], tAti[tCodes[i]]
			} else {
				rec[k], rec[k+1], rec[k+2] = "", "", ""
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

// virtMap monta o mapa código-da-subclasse -> rótulo para as dimensões virtuais de turismo.
// Índice 0 = fora das ACTs (ou subclasse vazia).
func virtMap(g, modo string, vals []string) ([]uint16, []string) {
	lab := []string{"(fora das atividades de turismo)"}
	idx := map[string]uint16{}
	m := make([]uint16, len(vals))
	for c, v := range vals {
		a, ok := turismo.Por[norm7(v)]
		if !ok || (modo == "nucleo" && !a.Nucleo) {
			if v == "" || !codigoValido(v) {
				i, ok := idx[rotuloSemCNAE]
				if !ok {
					i = uint16(len(lab))
					lab = append(lab, rotuloSemCNAE)
					idx[rotuloSemCNAE] = i
				}
				m[c] = i
			}
			continue
		}
		name := a.Grupo
		if g == "turismo_atividade" {
			name = a.Cod[:2] + "." + a.Cod[2:4] + "-" + a.Cod[4:5] + "/" + a.Cod[5:] + " " + a.Desc
		}
		i, ok := idx[name]
		if !ok {
			i = uint16(len(lab))
			lab = append(lab, name)
			idx[name] = i
		}
		m[c] = i
	}
	return m, lab
}

// Cobertura: para cada ano e coluna-chave: o PDF prevê a coluna? ela veio no CSV? em que % dos vínculos está preenchida?
func (s *Store) Cobertura() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var anos []int
	for a := range s.years {
		anos = append(anos, a)
	}
	sort.Ints(anos)
	var cols []map[string]string
	for _, l := range layoutPDF {
		cols = append(cols, map[string]string{"canon": l.Canon, "rotulo": l.Rotulo})
	}
	rows := []map[string]interface{}{}
	for _, a := range anos {
		rf := s.years[a].rf
		r := map[string]interface{}{"ano": a, "n": rf.Meta.N}
		cs := map[string]interface{}{}
		for _, l := range layoutPDF {
			x := map[string]interface{}{"prevista": l.prevista(a)}
			if c := rf.col(l.Canon); c != nil {
				x["presente"] = true
				n := int64(rf.Meta.N)
				v := c.Ne
				if c.Kind == "cat" && (l.Canon == "municipio" || l.Canon == "mun_trab" || l.Canon == "cnae95_classe" || l.Canon == "cnae20_classe" ||
					l.Canon == "cnae20_subclasse" || l.Canon == "cbo_94" || l.Canon == "cbo_2002") {
					v = c.Val
				}
				if n > 0 {
					x["pct"] = float64(v) * 100 / float64(n)
				}
			} else {
				x["presente"] = false
			}
			cs[l.Canon] = x
		}
		r["cols"] = cs
		rows = append(rows, r)
	}
	return map[string]interface{}{"colunas": cols, "anos": rows}
}

const rotuloSemCNAE = "(CNAE 2.0 não informada)"

// anosSemClasse decide quais anos não podem entrar em cálculos de turismo e escreve os avisos (fatos, com números).
// Deve ser chamada com s.mu travado para leitura.
func (s *Store) anosSemClasse(ini, fim int, usaCNAE, usaCBO bool) (map[int]bool, []string) {
	skip := map[int]bool{}
	var avisos []string
	var anos []int
	for a := range s.years {
		if a >= ini && a <= fim {
			anos = append(anos, a)
		}
	}
	sort.Ints(anos)
	check := func(col, nome string, usa bool) {
		if !usa {
			return
		}
		var ausentes, vazios, parciais []string
		for _, a := range anos {
			rf := s.years[a].rf
			c := rf.col(col)
			switch {
			case c == nil:
				skip[a] = true
				ausentes = append(ausentes, strconv.Itoa(a))
			case c.Val == 0:
				skip[a] = true
				vazios = append(vazios, strconv.Itoa(a))
			case rf.Meta.N > 0 && float64(c.Val) < 0.98*float64(rf.Meta.N):
				parciais = append(parciais, fmt.Sprintf("%d (%.1f%%)", a, 100*float64(c.Val)/float64(rf.Meta.N)))
			}
		}
		if len(ausentes) > 0 {
			avisos = append(avisos, fmt.Sprintf("Fora do cálculo — o CSV não tem a coluna %s: %s.", nome, strings.Join(ausentes, ", ")))
		}
		if len(vazios) > 0 {
			avisos = append(avisos, fmt.Sprintf("Fora do cálculo — a coluna %s existe, mas nenhum vínculo tem código preenchido: %s.", nome, strings.Join(vazios, ", ")))
		}
		if len(parciais) > 0 {
			avisos = append(avisos, fmt.Sprintf("Atenção — %s preenchida só em parte dos vínculos (o turismo desses anos pode estar subestimado): %s.", nome, strings.Join(parciais, ", ")))
		}
	}
	check(colCNAE, "CNAE 2.0 subclasse", usaCNAE)
	check(colCBO, "CBO 2002", usaCBO)
	return skip, avisos
}
