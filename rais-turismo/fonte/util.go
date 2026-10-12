package main

import (
	"strings"
	"unicode/utf8"
)

var accentPairs = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ç': 'c', 'ñ': 'n',
	'Á': 'A', 'À': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A', 'É': 'E', 'È': 'E', 'Ê': 'E', 'Ë': 'E',
	'Í': 'I', 'Ì': 'I', 'Î': 'I', 'Ï': 'I', 'Ó': 'O', 'Ò': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O',
	'Ú': 'U', 'Ù': 'U', 'Û': 'U', 'Ü': 'U', 'Ç': 'C', 'Ñ': 'N',
}

// semAcentos remove acentos de letras latinas.
func semAcentos(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if x, ok := accentPairs[r]; ok {
			b.WriteRune(x)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normNome: sem acento, maiúsculo, só A-Z e 0-9 (chave de comparação de nomes e cabeçalhos).
func normNome(s string) string {
	s = strings.ToUpper(semAcentos(s))
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func zpad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat("0", n-len(s)) + s
}

// normKey normaliza chaves de dicionário: "07" e "7" são iguais.
func normKey(s string) string {
	s = strings.TrimSpace(s)
	t, neg := s, false
	if strings.HasPrefix(t, "-") {
		neg, t = true, t[1:]
	}
	if !isDigits(t) {
		return s
	}
	t = strings.TrimLeft(t, "0")
	if t == "" {
		return "0"
	}
	if neg {
		return "-" + t
	}
	return t
}

func latin1ToUTF8(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + 8)
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

func hasHighBit(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

// decodeText devolve UTF-8; bytes inválidos são tratados como Latin-1.
func decodeText(b []byte) string {
	if !hasHighBit(b) || utf8.Valid(b) {
		return string(b)
	}
	return latin1ToUTF8(b)
}

// cod6: código IBGE de município com 6 dígitos (aceita 7 dígitos).
func cod6(raw string) string {
	raw = strings.TrimSpace(raw)
	if !isDigits(raw) {
		return raw
	}
	if len(raw) >= 7 {
		return raw[:6]
	}
	return zpad(raw, 6)
}

func padIfDigits(raw string, n int) string {
	raw = strings.TrimSpace(raw)
	if isDigits(raw) && len(raw) <= n {
		return zpad(raw, n)
	}
	return ""
}
