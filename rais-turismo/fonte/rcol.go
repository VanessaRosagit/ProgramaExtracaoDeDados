package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

// Formato colunar simples (um arquivo por ano):
//   "RCOL1\n" | uint64 tamanho do cabeçalho | cabeçalho JSON | blobs
// Coluna "cat": uint16 por linha (0 = vazio) com dicionário no cabeçalho.
// Coluna "num": float32 por linha (NaN = vazio).

const rcolMagic = "RCOL1\n"

type rcolCol struct {
	Name string   `json:"name"`
	Kind string   `json:"kind"` // cat | num
	Dict []string `json:"dict,omitempty"`
	Off  int64    `json:"off"`
	Len  int64    `json:"len"`
	Ne   int64    `json:"ne,omitempty"`  // linhas com valor preenchido (coluna cat)
	Val  int64    `json:"val,omitempty"` // linhas com código numérico diferente de zero (CNAE, CBO…)
}

type rcolMeta struct {
	Versao int       `json:"versao"`
	Ano    int       `json:"ano"`
	N      int       `json:"n"`
	Criado string    `json:"criado"`
	Cols   []rcolCol `json:"cols"`
}

type builder struct {
	name     string
	num      bool
	vals     []string // vals[0] == ""
	idx      map[string]uint16
	codes    []uint16
	nums     []float32
	overflow bool
}

func newCat(name string) *builder {
	return &builder{name: name, vals: []string{""}, idx: map[string]uint16{"": 0}}
}

func newNum(name string) *builder { return &builder{name: name, num: true} }

func (b *builder) addCat(s string) {
	c, ok := b.idx[s]
	if !ok {
		if len(b.vals) < 65535 {
			c = uint16(len(b.vals))
			b.vals = append(b.vals, s)
			b.idx[s] = c
		} else {
			if !b.overflow {
				b.overflow = true
				b.vals = append(b.vals, "(outros)") // ocupa o índice 65535
				b.idx["(outros)"] = 65535
			}
			c = 65535
		}
	}
	b.codes = append(b.codes, c)
}

func (b *builder) addNum(f float32) { b.nums = append(b.nums, f) }

func writeRcol(path string, ano, n int, bs []*builder) error {
	meta := rcolMeta{Versao: 1, Ano: ano, N: n, Criado: time.Now().Format(time.RFC3339)}
	var off int64
	for _, b := range bs {
		c := rcolCol{Name: b.name, Off: off}
		if b.num {
			c.Kind, c.Len = "num", int64(n)*4
			for _, v := range b.nums {
				if v == v {
					c.Ne++
					if v > 0 {
						c.Val++
					}
				}
			}
		} else {
			c.Kind, c.Len, c.Dict = "cat", int64(n)*2, b.vals
			ne := make([]bool, len(b.vals))
			val := make([]bool, len(b.vals))
			for i, v := range b.vals {
				ne[i] = i != 0 && v != ""
				val[i] = ne[i] && codigoValido(v)
			}
			for _, code := range b.codes {
				if int(code) < len(ne) {
					if ne[code] {
						c.Ne++
					}
					if val[code] {
						c.Val++
					}
				}
			}
		}
		off += c.Len
		meta.Cols = append(meta.Cols, c)
	}
	hdr, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	w := &errWriter{w: f}
	w.write([]byte(rcolMagic))
	var l [8]byte
	binary.LittleEndian.PutUint64(l[:], uint64(len(hdr)))
	w.write(l[:])
	w.write(hdr)
	buf := make([]byte, 0, 1<<20)
	for _, b := range bs {
		buf = buf[:0]
		if b.num {
			if len(b.nums) != n {
				f.Close()
				return fmt.Errorf("coluna %s com %d valores (esperado %d)", b.name, len(b.nums), n)
			}
			for _, v := range b.nums {
				buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
				if len(buf) >= 1<<20-8 {
					w.write(buf)
					buf = buf[:0]
				}
			}
		} else {
			if len(b.codes) != n {
				f.Close()
				return fmt.Errorf("coluna %s com %d valores (esperado %d)", b.name, len(b.codes), n)
			}
			for _, v := range b.codes {
				buf = binary.LittleEndian.AppendUint16(buf, v)
				if len(buf) >= 1<<20-8 {
					w.write(buf)
					buf = buf[:0]
				}
			}
		}
		w.write(buf)
	}
	if w.err != nil {
		f.Close()
		return w.err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) write(b []byte) {
	if e.err == nil && len(b) > 0 {
		_, e.err = e.w.Write(b)
	}
}

type rcolFile struct {
	Path      string
	Meta      rcolMeta
	DataStart int64
	ModTime   time.Time
	Size      int64
}

func openRcolMeta(path string) (*rcolFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, _ := f.Stat()
	magic := make([]byte, len(rcolMagic))
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != rcolMagic {
		return nil, fmt.Errorf("%s não é um arquivo de dados válido", path)
	}
	var l [8]byte
	if _, err := io.ReadFull(f, l[:]); err != nil {
		return nil, err
	}
	hl := binary.LittleEndian.Uint64(l[:])
	if hl > 1<<28 {
		return nil, fmt.Errorf("cabeçalho absurdo em %s", path)
	}
	hdr := make([]byte, hl)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, err
	}
	rf := &rcolFile{Path: path, DataStart: int64(len(rcolMagic)) + 8 + int64(hl), ModTime: st.ModTime(), Size: st.Size()}
	if err := json.Unmarshal(hdr, &rf.Meta); err != nil {
		return nil, err
	}
	return rf, nil
}

func (rf *rcolFile) col(name string) *rcolCol {
	for i := range rf.Meta.Cols {
		if rf.Meta.Cols[i].Name == name {
			return &rf.Meta.Cols[i]
		}
	}
	return nil
}

func (rf *rcolFile) readBlob(f *os.File, c *rcolCol) ([]byte, error) {
	b := make([]byte, c.Len)
	if _, err := f.ReadAt(b, rf.DataStart+c.Off); err != nil && !(err == io.EOF && int64(len(b)) == c.Len) {
		return nil, err
	}
	return b, nil
}

func decodeU16(b []byte) []uint16 {
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return out
}

func decodeF32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// codigoValido: só dígitos e pelo menos um diferente de zero (a RAIS usa 0000000, -1 ou {ñ class} quando não há código).
func codigoValido(v string) bool {
	nz := false
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
		if v[i] != '0' {
			nz = true
		}
	}
	return nz
}
