package main

import "strings"

// Ocupações da aba "CBO" da planilha do Observatório de Turismo (apenas as que têm código).
// A planilha também cita "hospitalidade", "gastronomia" e "transporte" sem códigos; essas
// linhas NÃO viram filtro por conta própria: quem escolhe os códigos é o usuário (aba Ocupações).
var cboPlanilha = []string{"511405", "511505", "511510", "122520", "122515", "354805", "354815", "354810"}

// cbosExtra devolve os códigos escolhidos pelo usuário (definido pelo App).
var cbosExtra = func() []string { return nil }

// codigosCboTurismo: "planilha" = as 8 da planilha; "minhas" = só as escolhidas pelo usuário; "ambas" = as duas listas.
func codigosCboTurismo(modo string) []string {
	var out []string
	if modo == "planilha" || modo == "ambas" {
		out = append(out, cboPlanilha...)
	}
	if modo == "minhas" || modo == "ambas" {
		out = append(out, cbosExtra()...)
	}
	// o CSV pode ter perdido o zero à esquerda: aceita as duas grafias
	for _, x := range append([]string(nil), out...) {
		if len(x) > 1 && x[0] == '0' {
			out = append(out, strings.TrimLeft(x, "0"))
		}
	}
	return out
}
