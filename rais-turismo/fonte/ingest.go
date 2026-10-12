package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type logger func(format string, args ...interface{})

// ---------- arquivos de entrada (CSVs anuais já extraídos e filtrados) ----------

type csvArq struct {
	Ano  int
	Path string
	Size int64
}

// anoDoNome devolve o ano (2000–2099) que aparece no nome do arquivo como número de 4 dígitos isolado
// (0 = nenhum; -1 = dois anos diferentes no mesmo nome, ex.: rais_2000_2025.csv).
func anoDoNome(nome string) int {
	nome = strings.TrimSuffix(nome, filepath.Ext(nome))
	isD := func(c byte) bool { return c >= '0' && c <= '9' }
	ano := 0
	for i := 0; i+4 <= len(nome); i++ {
		if !(nome[i] == '2' && nome[i+1] == '0' && isD(nome[i+2]) && isD(nome[i+3])) {
			continue
		}
		if i > 0 && isD(nome[i-1]) {
			continue
		}
		if i+4 < len(nome) && isD(nome[i+4]) {
			continue
		}
		y, _ := strconv.Atoi(nome[i : i+4])
		if ano != 0 && ano != y {
			return -1
		}
		ano = y
		i += 3
	}
	return ano
}

// varrerCSV lista os .csv da pasta (e subpastas) por ano. Várias candidatas para o mesmo ano: o log dirá qual foi usada.
func varrerCSV(pasta string) (map[int][]csvArq, []string, error) {
	por := map[int][]csvArq{}
	var semAno []string
	if pasta == "" {
		return por, nil, fmt.Errorf("pasta dos CSVs não informada")
	}
	st, err := os.Stat(pasta)
	if err != nil || !st.IsDir() {
		return por, nil, fmt.Errorf("a pasta %q não existe", pasta)
	}
	_ = filepath.WalkDir(pasta, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		y := anoDoNome(e.Name())
		if y <= 0 {
			semAno = append(semAno, p)
			return nil
		}
		por[y] = append(por[y], csvArq{Ano: y, Path: p, Size: info.Size()})
		return nil
	})
	for y := range por {
		sort.Slice(por[y], func(i, j int) bool { return por[y][i].Size > por[y][j].Size })
	}
	sort.Strings(semAno)
	return por, semAno, nil
}

// ---------- plano de colunas ----------

const (
	kCat = iota
	kNum
	kCod6
	kTrans
	kDerived
)

type outCol struct {
	b     *builder
	kind  int
	pos   int
	canon string
	trans map[string]string
	src   int
	look  map[string]string
	keyFn func(string) string
}

func buildPlan(canon []string, sup *Support, ano int) []*outCol {
	pos := map[string]int{}
	for i, c := range canon {
		if c != "" {
			pos[c] = i
		}
	}
	dic := sup.Dict.resolve(ano)
	var cols []*outCol
	idx := map[string]int{}
	add := func(oc *outCol) {
		idx[oc.b.name] = len(cols)
		cols = append(cols, oc)
	}
	ident := func(s string) string { return s }
	pad := func(n int) func(string) string { return func(s string) string { return padIfDigits(s, n) } }
	cbo := func(n int) func(string) string {
		return func(s string) string {
			p := padIfDigits(s, 6)
			if p == "" {
				return ""
			}
			return p[:n]
		}
	}
	for _, a := range aliases {
		p, ok := pos[a.canon]
		if !ok {
			continue
		}
		oc := &outCol{pos: p, canon: a.canon}
		switch {
		case numericCols[a.canon]:
			oc.kind, oc.b = kNum, newNum(a.canon)
		case a.canon == "municipio" || a.canon == "mun_trab":
			oc.kind, oc.b = kCod6, newCat(a.canon)
		case dic[a.canon] != nil:
			oc.kind, oc.b, oc.trans = kTrans, newCat(a.canon), dic[a.canon]
		default:
			oc.kind, oc.b = kCat, newCat(a.canon)
		}
		add(oc)
		src := idx[a.canon]
		der := func(name string, look map[string]string, fn func(string) string) {
			add(&outCol{b: newCat(name), kind: kDerived, src: src, look: look, keyFn: fn})
		}
		switch a.canon {
		case "municipio":
			der("municipio_nome", sup.NomeMun, ident)
		case "mun_trab":
			der("mun_trab_nome", sup.NomeMun, ident)
		case "cbo_2002":
			der("cbo_2002_nome", sup.CBO.Ocup, cbo(6))
			der("cbo_familia_nome", sup.CBO.Familia, cbo(4))
			der("cbo_subgrupo_nome", sup.CBO.SubGrupo, cbo(3))
			der("cbo_subgrupo_principal_nome", sup.CBO.SubPrincipal, cbo(2))
			der("cbo_grande_grupo_nome", sup.CBO.Grande, cbo(1))
		case "cnae95_classe":
			if len(sup.Cnae95) > 0 {
				der("cnae95_classe_nome", sup.Cnae95, pad(5))
			}
		case "cnae20_classe":
			der("cnae20_classe_nome", sup.Classe, pad(5))
		case "cnae20_subclasse":
			der("cnae20_subclasse_nome", sup.Subclas, pad(7))
		}
	}
	return cols
}

// ---------- leitura do texto bruto ----------

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func detectDelim(h []byte) byte {
	best, bn := byte(';'), -1
	for _, d := range []byte{';', ',', '\t', '|'} {
		if n := bytes.Count(h, []byte{d}); n > bn {
			best, bn = d, n
		}
	}
	return best
}

func splitFields(line []byte, delim byte, buf [][]byte) [][]byte {
	buf = buf[:0]
	if bytes.IndexByte(line, '"') >= 0 {
		return splitQuoted(line, delim, buf)
	}
	start := 0
	for i, c := range line {
		if c == delim {
			buf = append(buf, line[start:i])
			start = i + 1
		}
	}
	return append(buf, line[start:])
}

// splitQuoted trata campos entre aspas (CSV padrão), usado quando o arquivo as contém.
func splitQuoted(line []byte, delim byte, buf [][]byte) [][]byte {
	var cur []byte
	inq := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inq && c == '"':
			if i+1 < len(line) && line[i+1] == '"' {
				cur = append(cur, '"')
				i++
			} else {
				inq = false
			}
		case c == '"':
			inq = true
		case c == delim && !inq:
			buf = append(buf, append([]byte{}, cur...))
			cur = cur[:0]
		default:
			cur = append(cur, c)
		}
	}
	return append(buf, append([]byte{}, cur...))
}

// cod6Bytes calcula o código de 6 dígitos sem alocar; devolve nil se não for numérico.
func cod6Bytes(b []byte, out *[8]byte) []byte {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return nil
		}
	}
	if len(b) >= 7 {
		return b[:6]
	}
	n := 6 - len(b)
	for i := 0; i < n; i++ {
		out[i] = '0'
	}
	copy(out[n:], b)
	return out[:6]
}

func parseNum(b []byte) float32 {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return float32(math.NaN())
	}
	if strings.Contains(s, ",") { // vírgula decimal (e ponto de milhar, se houver)
		s = strings.Replace(strings.Replace(s, ".", "", -1), ",", ".", 1)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return float32(math.NaN())
	}
	return float32(f)
}

func processYear(ctx context.Context, cfg Config, sup *Support, ano int, refazer bool, logf logger) (string, error) {
	d := cfg.Dirs()
	d.Make()
	out := filepath.Join(d.Cols, fmt.Sprintf("rais_%d.rcol", ano))
	if !refazer {
		if _, err := os.Stat(out); err == nil {
			logf("%d: já processado (pulando).", ano)
			return "já existia", nil
		}
	}
	por, _, err := varrerCSV(cfg.PastaCSV)
	if err != nil {
		return "", err
	}
	cands := por[ano]
	if len(cands) == 0 {
		return "", fmt.Errorf("não achei CSV do ano %d em %s (o nome do arquivo precisa conter o ano, ex.: rais_%d.csv)", ano, cfg.PastaCSV, ano)
	}
	src := cands[0]
	for _, o := range cands[1:] {
		logf("%d: AVISO — também existe %s (%d MB); usando o maior, %s", ano, o.Path, o.Size>>20, src.Path)
	}
	f, err := os.Open(src.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	logf("%d: lendo %s (%d MB)", ano, src.Path, src.Size>>20)

	cr := &countReader{r: f}
	br := bufio.NewReaderSize(cr, 4<<20)
	hdr, err := br.ReadBytes('\n')
	if err != nil && len(hdr) == 0 {
		return "", fmt.Errorf("arquivo sem conteúdo")
	}
	hdr = bytes.TrimPrefix(hdr, []byte{0xEF, 0xBB, 0xBF})
	hdr = bytes.TrimRight(hdr, "\r\n")
	delim := detectDelim(hdr)
	enc := "latin-1"
	if hasHighBit(hdr) && utf8.Valid(hdr) {
		enc = "utf-8"
	}
	var header []string
	for _, f := range splitFields(hdr, delim, nil) {
		header = append(header, strings.Trim(strings.TrimSpace(decodeText(f)), `"`))
	}
	canon := mapColumns(header, ano)
	reconhecidas := 0
	rows := [][]string{{"pos", "bruto", "canon"}}
	for i, h := range header {
		if canon[i] != "" {
			reconhecidas++
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), h, canon[i]})
	}
	_ = writeCSVFile(filepath.Join(d.Diag, fmt.Sprintf("colunas_%d.csv", ano)), rows)
	logf("%d: %d colunas no arquivo, %d reconhecidas (%s, separador %q). Detalhe em diagnostico/colunas_%d.csv",
		ano, len(header), reconhecidas, enc, string(delim), ano)

	if ano <= 2005 && !contem(canon, "escolaridade") {
		var sobras []string
		for i, h := range header {
			if canon[i] == "" {
				sobras = append(sobras, h)
			}
		}
		logf("ATENÇÃO %d: coluna de escolaridade não encontrada no cabeçalho. Colunas não usadas: %s", ano, strings.Join(sobras, " | "))
	}
	munPos := -1
	for i, c := range canon {
		if c == "municipio" {
			munPos = i
			break
		}
	}
	if munPos < 0 {
		return "", fmt.Errorf("coluna de município não encontrada. Cabeçalho: %s", strings.Join(header, " | "))
	}
	plan := buildPlan(canon, sup, ano)

	alvo := sup.Alvo
	ufPrefix := ""
	for c := range alvo {
		ufPrefix = c[:2]
		break
	}
	counts := map[string]int{}
	vals := make([]string, len(plan))
	var fields [][]byte
	var cb [8]byte
	var total, kept, curtas, invalidos int64
	foraUF := map[string]int64{}
	foraMun := map[string]int64{}
	lastLog := time.Now()
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull { // linha gigante: lê o resto e segue
			full := append([]byte{}, line...)
			for err == bufio.ErrBufferFull {
				line, err = br.ReadSlice('\n')
				full = append(full, line...)
			}
			line = full
		}
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) > 0 {
				total++
				fields = splitFields(line, delim, fields)
				var mb []byte
				if munPos < len(fields) {
					mb = cod6Bytes(fields[munPos], &cb)
				}
				if munPos >= len(fields) {
					curtas++
				} else if mb == nil {
					invalidos++
				}
				if mb != nil {
					if _, ok := alvo[string(mb)]; !ok {
						if strings.HasPrefix(string(mb), ufPrefix) {
							foraMun[string(mb)]++
						} else {
							foraUF[string(mb[:2])]++
						}
					}
					if _, ok := alvo[string(mb)]; ok {
						kept++
						counts[string(mb)]++
						for i, oc := range plan {
							var raw []byte
							if oc.kind != kDerived && oc.pos < len(fields) {
								raw = bytes.TrimSpace(fields[oc.pos])
							}
							switch oc.kind {
							case kNum:
								oc.b.addNum(parseNum(raw))
							case kCod6:
								s := cod6(decodeText(raw))
								vals[i] = s
								oc.b.addCat(s)
							case kTrans:
								s := decodeText(raw)
								vals[i] = s
								if s != "" {
									if lab, ok := oc.trans[normKey(s)]; ok {
										s = lab
									}
								}
								oc.b.addCat(s)
							case kCat:
								s := decodeText(raw)
								vals[i] = s
								oc.b.addCat(s)
							case kDerived:
								oc.b.addCat(oc.look[oc.keyFn(vals[oc.src])])
							}
						}
					}
				}
			}
		}
		if total&0xFFFFF == 0 && total > 0 {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
		if time.Since(lastLog) > 10*time.Second {
			lastLog = time.Now()
			logf("%d: %d MB lidos, %d linhas, %d no recorte", ano, cr.n>>20, total, kept)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("erro de leitura: %w", err)
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if kept == 0 {
		return "", fmt.Errorf("nenhum vínculo nos municípios escolhidos (%d linhas lidas) — confira a UF e os municípios", total)
	}
	var sem []string
	for cod, nome := range alvo {
		if counts[cod] == 0 {
			sem = append(sem, nome)
		}
	}
	if len(sem) > 0 {
		sort.Strings(sem)
		logf("%d: AVISO — sem vínculos em: %s", ano, strings.Join(sem, ", "))
	}
	bs := make([]*builder, len(plan))
	for i, oc := range plan {
		bs[i] = oc.b
		if oc.b.overflow {
			logf("%d: AVISO — coluna %s tem mais de 65 mil valores distintos; excedentes agrupados em (outros)", ano, oc.b.name)
		}
	}
	if err := writeRcol(out, ano, int(kept), bs); err != nil {
		return "", err
	}
	logf("%d: OK — %d vínculos de %d linhas lidas.", ano, kept, total)
	if err := escreverConferencia(cfg, sup, ano, total, kept, curtas, invalidos, counts, foraMun, foraUF, logf); err != nil {
		logf("%d: AVISO — não consegui gravar a conferência: %v", ano, err)
	}

	return "ok", nil
}

// escreverConferencia grava diagnostico/conferencia_ANO.csv: para onde foram todas as linhas lidas.
func escreverConferencia(cfg Config, sup *Support, ano int, total, kept, curtas, invalidos int64, counts map[string]int, foraMun map[string]int64, foraUF map[string]int64, logf logger) error {
	d := cfg.Dirs()
	var b strings.Builder
	b.WriteString("\ufeffitem;codigo;nome;linhas\r\n")
	w := func(item, cod, nome string, n int64) { fmt.Fprintf(&b, "%s;%s;%s;%d\r\n", item, cod, nome, n) }
	w("linhas lidas no arquivo", "", "", total)
	w("no recorte (gravadas)", "", "", kept)
	var cods []string
	for c := range sup.Alvo {
		cods = append(cods, c)
	}
	sort.Strings(cods)
	var soma, fMun, fUF int64
	for _, c := range cods {
		w("município do recorte", c, sup.Alvo[c], int64(counts[c]))
		soma += int64(counts[c])
	}
	var fm []string
	for c := range foraMun {
		fm = append(fm, c)
		fMun += foraMun[c]
	}
	sort.Strings(fm)
	for _, c := range fm {
		w("fora do recorte (mesma UF)", c, "", foraMun[c])
	}
	var fu []string
	for c := range foraUF {
		fu = append(fu, c)
		fUF += foraUF[c]
	}
	sort.Strings(fu)
	for _, c := range fu {
		w("fora do recorte (outra UF, 2 primeiros dígitos)", c, "", foraUF[c])
	}
	w("linha curta (sem coluna de município)", "", "", curtas)
	w("município inválido/vazio", "", "", invalidos)
	diff := total - soma - fMun - fUF - curtas - invalidos
	w("DIFERENÇA (deve ser 0)", "", "", diff)
	if err := os.MkdirAll(d.Diag, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d.Diag, fmt.Sprintf("conferencia_%d.csv", ano)), []byte(b.String()), 0o644); err != nil {
		return err
	}
	logf("%d: CONFERÊNCIA — lidas %d = recorte %d + mesma UF fora do recorte %d + outra UF %d + curtas %d + inválidas %d (diferença %d). Detalhe em diagnostico/conferencia_%d.csv",
		ano, total, soma, fMun, fUF, curtas, invalidos, diff, ano)
	if diff != 0 || kept != soma {
		logf("%d: ATENÇÃO — a conferência não fecha; não confie neste ano.", ano)
	}
	return nil
}
