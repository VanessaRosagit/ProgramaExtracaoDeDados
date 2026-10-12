package main

// Layout das colunas por ano, conforme o PDF "Colunas disponíveis no RAIS — 2000 a 2025":
// primeiro (e, se houver, último) ano em que a coluna existe nos arquivos da RAIS.
// Serve para comparar o que o PDF diz que deveria existir com o que realmente veio preenchido nos seus CSVs.
type colLayout struct {
	Canon, Rotulo string
	De, Ate       int // Ate 0 = até 2025
}

var layoutPDF = []colLayout{
	{"municipio", "Município", 2000, 0},
	{"cnae95_classe", "CNAE 95 (1.0) classe", 2000, 0},
	{"cnae20_classe", "CNAE 2.0 classe", 2004, 0},
	{"cnae20_subclasse", "CNAE 2.0 subclasse", 2004, 0},
	{"cbo_94", "CBO 94", 2000, 2002},
	{"cbo_2002", "CBO 2002", 2003, 0},
	{"escolaridade", "Escolaridade", 2000, 0},
	{"raca_cor", "Raça/cor", 2006, 0},
	{"sexo", "Sexo", 2000, 0},
	{"idade", "Idade", 2000, 0},
	{"faixa_etaria", "Faixa etária", 2000, 0},
	{"tipo_vinculo", "Tipo de vínculo", 2000, 0},
	{"vinculo_ativo_3112", "Vínculo ativo 31/12", 2000, 0},
	{"tempo_emprego", "Tempo de emprego", 2000, 0},
	{"remun_media_sm", "Remuneração média (SM)", 2000, 0},
	{"remun_dez_nom", "Remuneração dezembro (nominal)", 2000, 0},
	{"natureza_juridica", "Natureza jurídica", 2000, 0},
	{"tamanho_estab", "Tamanho do estabelecimento", 2000, 0},
	{"mun_trab", "Município de trabalho", 2002, 0},
	{"ind_portador_defic", "Portador de deficiência", 2007, 0},
	{"ibge_subsetor", "Subsetor IBGE", 2015, 0},
}

func (l colLayout) prevista(ano int) bool {
	return ano >= l.De && (l.Ate == 0 || ano <= l.Ate)
}
