package main

import (
	"regexp"
	"strconv"
	"strings"
)

// Colunas "canônicas" (nome que fica no arquivo de dados) com os nomes possíveis do
// cabeçalho bruto, já normalizados (normNome). A ordem aqui é a ordem das colunas na saída.
type aliasDef struct {
	canon string
	names []string
}

var aliases = []aliasDef{
	{"municipio", []string{"MUNICIPIO", "MUNICIPIOCODIGO"}},
	{"mun_trab", []string{"MUNTRAB", "MUNICIPIOTRAB"}},
	{"cbo_2002", []string{"CBOOCUPACAO2002", "CBO2002OCUPACAO", "CBO2002"}},
	{"cbo_94", []string{"CBO94OCUPACAO", "CBOOCUPACAO94", "CBO94"}},
	{"cnae20_classe", []string{"CNAE20CLASSE"}},
	{"cnae20_subclasse", []string{"CNAE20SUBCLASSE"}},
	{"cnae95_classe", []string{"CNAE95CLASSE"}},
	{"ibge_subsetor", []string{"IBGESUBSETOR"}},
	{"natureza_juridica", []string{"NATUREZAJURIDICA"}},
	{"vinculo_ativo_3112", []string{"VINCULOATIVO3112", "INDVINCULOATIVO3112"}},
	{"tipo_vinculo", []string{"TIPOVINCULO"}},
	{"tipo_admissao", []string{"TIPOADMISSAO", "TIPOADMISSAOTRABALHADOR"}},
	{"tipo_salario", []string{"TIPOSALARIO"}},
	{"motivo_desligamento", []string{"MOTIVODESLIGAMENTO"}},
	{"mes_admissao", []string{"MESADMISSAO"}},
	{"mes_desligamento", []string{"MESDESLIGAMENTO"}},
	{"data_admissao", []string{"DATAADMISSAODECLARADA"}},
	{"causa_afastamento_1", []string{"CAUSAAFASTAMENTO1"}},
	{"causa_afastamento_2", []string{"CAUSAAFASTAMENTO2"}},
	{"causa_afastamento_3", []string{"CAUSAAFASTAMENTO3"}},
	{"qtd_dias_afastamento", []string{"QTDDIASAFASTAMENTO"}},
	{"sexo", []string{"SEXOTRABALHADOR", "SEXO"}},
	{"raca_cor", []string{"RACACOR"}},
	{"nacionalidade", []string{"NACIONALIDADE"}},
	{"ano_chegada_brasil", []string{"ANOCHEGADABRASIL"}},
	{"idade", []string{"IDADE"}},
	{"faixa_etaria", []string{"FAIXAETARIA"}},
	{"escolaridade", []string{"ESCOLARIDADEAPOS2005", "ESCOLARIDADE", "GRAUINSTRUCAO2005",
		"GRAUINSTRUCAOAPOS2005", "GRAUINSTRUCAO", "ESCOLARIDADEATE2005"}},
	{"ind_portador_defic", []string{"INDPORTADORDEFIC"}},
	{"tipo_defic", []string{"TIPODEFIC", "TIPODEFICIENCIA"}},
	{"faixa_hora_contrat", []string{"FAIXAHORACONTRAT"}},
	{"qtd_hora_contr", []string{"QTDHORACONTR"}},
	{"faixa_remun_dez_sm", []string{"FAIXAREMUNDEZEMSM", "FAIXAREMDEZSM", "FAIXAREMDEZEMBROSM"}},
	{"faixa_remun_media_sm", []string{"FAIXAREMUNMEDIASM", "FAIXAREMMEDIASM"}},
	{"remun_dez_nom", []string{"VLREMUNDEZEMBRONOM", "VLREMDEZEMBRONOM"}},
	{"remun_dez_sm", []string{"VLREMUNDEZEMBROSM", "VLREMDEZEMBROSM"}},
	{"remun_media_nom", []string{"VLREMUNMEDIANOM", "VLREMMEDIANOM"}},
	{"remun_media_sm", []string{"VLREMUNMEDIASM", "VLREMMEDIASM"}},
	{"salario_contratual", []string{"VLSALARIOCONTRATUAL"}},
	{"faixa_tempo_emprego", []string{"FAIXATEMPOEMPREGO"}},
	{"tempo_emprego", []string{"TEMPOEMPREGO"}},
	{"tamanho_estab", []string{"TAMANHOESTABELECIMENTO"}},
	{"tipo_estab", []string{"TIPOESTAB", "TIPOESTABELECIMENTO"}},
	{"tipo_estab_2", []string{"TIPOESTAB_2", "TIPOESTAB1"}},
	{"ind_cei_vinculado", []string{"INDCEIVINCULADO"}},
	{"ind_simples", []string{"INDSIMPLES", "INDESTABELECIMENTOPARTICIPANTESIMPLES"}},
	{"regioes_adm_df", []string{"REGIOESADMDF", "REGIAOADMDF"}},
	{"ind_trab_intermitente", []string{"INDTRABINTERMITENTE", "INDTRABALHOINTERMITENTE"}},
	{"ind_trab_parcial", []string{"INDTRABPARCIAL", "INDTRABALHOPARCIAL"}},
	{"rem_jan_sc", []string{"VLREMJANEIROSC"}},
	{"rem_fev_sc", []string{"VLREMFEVEREIROSC"}},
	{"rem_mar_sc", []string{"VLREMMARCOSC"}},
	{"rem_abr_sc", []string{"VLREMABRILSC"}},
	{"rem_mai_sc", []string{"VLREMMAIOSC"}},
	{"rem_jun_sc", []string{"VLREMJUNHOSC"}},
	{"rem_jul_sc", []string{"VLREMJULHOSC"}},
	{"rem_ago_sc", []string{"VLREMAGOSTOSC"}},
	{"rem_set_sc", []string{"VLREMSETEMBROSC"}},
	{"rem_out_sc", []string{"VLREMOUTUBROSC"}},
	{"rem_nov_sc", []string{"VLREMNOVEMBROSC"}},
}

// colunas numéricas (a RAIS usa vírgula decimal)
var numericCols = map[string]bool{
	"idade": true, "qtd_hora_contr": true, "qtd_dias_afastamento": true, "tempo_emprego": true,
	"remun_dez_nom": true, "remun_dez_sm": true, "remun_media_nom": true, "remun_media_sm": true,
	"salario_contratual": true,
	"rem_jan_sc":         true, "rem_fev_sc": true, "rem_mar_sc": true, "rem_abr_sc": true, "rem_mai_sc": true,
	"rem_jun_sc": true, "rem_jul_sc": true, "rem_ago_sc": true, "rem_set_sc": true, "rem_out_sc": true,
	"rem_nov_sc": true,
}

// coluna canônica -> nome(s) em dicionario.csv
var deParaDic = map[string][]string{
	"causa_afastamento_1":   {"causa_desligamento_1"},
	"causa_afastamento_2":   {"causa_desligamento_2"},
	"causa_afastamento_3":   {"causa_desligamento_3"},
	"motivo_desligamento":   {"motivo_desligamento"},
	"faixa_etaria":          {"faixa_etaria"},
	"faixa_hora_contrat":    {"faixa_horas_contratadas"},
	"faixa_remun_dez_sm":    {"faixa_remuneracao_dezembro_sm"},
	"faixa_remun_media_sm":  {"faixa_remuneracao_media_sm"},
	"faixa_tempo_emprego":   {"faixa_tempo_emprego"},
	"escolaridade":          {"grau_instrucao_apos_2005", "grau_instrucao_1985_2005"},
	"ind_cei_vinculado":     {"indicador_cei_vinculado"},
	"ind_simples":           {"indicador_simples"},
	"nacionalidade":         {"nacionalidade"},
	"ind_portador_defic":    {"indicador_portador_deficiencia"},
	"raca_cor":              {"raca_cor"},
	"sexo":                  {"sexo"},
	"tamanho_estab":         {"tamanho_estabelecimento"},
	"tipo_admissao":         {"tipo_admissao"},
	"tipo_estab":            {"tipo_estabelecimento"},
	"tipo_estab_2":          {"tipo_estabelecimento"},
	"tipo_defic":            {"tipo_deficiencia"},
	"tipo_vinculo":          {"tipo_vinculo"},
	"tipo_salario":          {"tipo_salario"},
	"ibge_subsetor":         {"subsetor_ibge"},
	"vinculo_ativo_3112":    {"vinculo_ativo_3112"},
	"ind_trab_intermitente": {"indicador_trabalho_intermitente"},
	"ind_trab_parcial":      {"indicador_trabalho_parcial"},
	"regioes_adm_df":        {"regioes_administrativas_df"},
}

// colunas derivadas (nomes) e a coluna da qual dependem
var derivedAfter = map[string][]string{
	"municipio":        {"municipio_nome"},
	"mun_trab":         {"mun_trab_nome"},
	"cbo_2002":         {"cbo_2002_nome", "cbo_familia_nome", "cbo_subgrupo_nome", "cbo_subgrupo_principal_nome", "cbo_grande_grupo_nome"},
	"cnae95_classe":    {"cnae95_classe_nome"},
	"cnae20_classe":    {"cnae20_classe_nome"},
	"cnae20_subclasse": {"cnae20_subclasse_nome"},
}

// mapColumns liga cada coluna do cabeçalho a um nome canônico ("" = não usada).
func mapColumns(header []string, ano int) []string {
	n := len(header)
	keys := make([]string, n)
	seen := map[string]int{}
	for i, h := range header {
		base := normNome(limpaSufixo(h))
		if sufixoNome.MatchString(semAcentos(h)) {
			base = "IGNORADA" + strconv.Itoa(i)
		}
		seen[base]++
		if seen[base] > 1 {
			keys[i] = base + "_" + strconv.Itoa(seen[base])
		} else {
			keys[i] = base
		}
	}
	canon := make([]string, n)
	for _, a := range aliases {
		names := append(append([]string{}, a.names...), normNome(a.canon)) // nomes do PDF/RAIS e também o nome canônico (CSV do programa anterior)
		if a.canon == "escolaridade" {
			names = nomesEscolaridade(ano)
		}
		// a ordem de names é a prioridade: o primeiro nome que existir no cabeçalho vence
	achou:
		for _, nm := range names {
			for i := 0; i < n; i++ {
				if canon[i] == "" && keys[i] == nm {
					canon[i] = a.canon
					break achou
				}
			}
		}
		if a.canon == "escolaridade" && !contem(canon, "escolaridade") {
			// reserva: qualquer cabeçalho que comece por ESCOLARIDADE / GRAUINSTRU (ex.: "Grau Instrução 1985-2005")
			for i := 0; i < n; i++ {
				if canon[i] == "" && (strings.HasPrefix(keys[i], "ESCOLARIDADE") || strings.HasPrefix(keys[i], "GRAUINSTRU")) {
					canon[i] = a.canon
					break
				}
			}
		}
	}
	return canon
}

// ordem canônica das colunas de saída (para exportação)
func canonicalOrder() []string {
	var out []string
	for _, a := range aliases {
		out = append(out, a.canon)
		out = append(out, derivedAfter[a.canon]...)
	}
	return out
}

// A RAIS recente nomeia as colunas "Município - Código" / "Tipo Estabelecimento - Nome".
// Usamos a coluna de código e ignoramos a de nome.
var (
	sufixoCodigo = regexp.MustCompile(`(?i)\s*-\s*codigo\s*$`)
	sufixoNome   = regexp.MustCompile(`(?i)\s-\s*(nome|descricao)\s*$`)
)

func limpaSufixo(h string) string {
	h = strings.TrimSpace(h)
	if sufixoCodigo.MatchString(semAcentos(h)) {
		if i := strings.LastIndex(h, "-"); i > 0 {
			return strings.TrimSpace(h[:i])
		}
	}
	return h
}

func contem(v []string, x string) bool {
	for _, e := range v {
		if e == x {
			return true
		}
	}
	return false
}

// A RAIS tem duas colunas de escolaridade ("até 2005" e "após 2005", dicionário: grau_instrucao_1985_2005 e
// grau_instrucao_apos_2005). Até 2004 vale a antiga; de 2005 em diante, a nova. Se só existir uma delas
// no arquivo, ela é usada de qualquer forma.
func nomesEscolaridade(ano int) []string {
	antigas := []string{"GRAUINSTRUCAO20051985", "GRAUINSTRUCAO19852005", "ESCOLARIDADE20051985", "ESCOLARIDADE19852005", "GRAUINSTRUCAO1985A2005", "ESCOLARIDADE1985A2005",
		"ESCOLARIDADEATE2005", "GRAUINSTRUCAOATE2005", "GRAUINSTRUCAO", "ESCOLARIDADE"}
	novas := []string{"ESCOLARIDADEAPOS2005", "GRAUINSTRUCAOAPOS2005", "GRAUINSTRUCAO2005", "ESCOLARIDADE2005"}
	if ano > 0 && ano <= 2004 {
		return append(antigas, novas...)
	}
	return append(novas, antigas...)
}
