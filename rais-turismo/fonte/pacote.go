package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Pacote de turismo: tudo que dá para extrair dos CSVs processados, em arquivos CSV (; e vírgula decimal, UTF-8 com BOM)
// dentro de <pasta_dados>/saida_turismo. Nada é estimado: ano sem CNAE 2.0 preenchida fica fora e é listado em 07_cobertura.csv.

var escopos = []struct{ Cod, Rotulo string }{
	{"nucleo", "ACTs – metodologia IBGE/IPEA (60 CNAEs)"},
	{"todas", "ACTs + extras do Cadastur (104 CNAEs)"},
}

const semInfo = "(sem informação)"

func fnum(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strings.Replace(strconv.FormatFloat(x, 'f', 2, 64), ".", ",", 1)
	}
	return fmt.Sprint(v)
}

type csvOut struct {
	f  *os.File
	bw *bufio.Writer
	cw *csv.Writer
}

func novoCSV(path string, cab []string) (*csvOut, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	_, _ = bw.WriteString("\xEF\xBB\xBF")
	cw := csv.NewWriter(bw)
	cw.Comma = ';'
	_ = cw.Write(cab)
	return &csvOut{f, bw, cw}, nil
}

func (o *csvOut) Write(r []string) { _ = o.cw.Write(r) }
func (o *csvOut) Close() error {
	o.cw.Flush()
	if err := o.cw.Error(); err != nil {
		o.f.Close()
		return err
	}
	if err := o.bw.Flush(); err != nil {
		o.f.Close()
		return err
	}
	return o.f.Close()
}

// dupla roda a consulta para todos os vínculos e para os ativos em 31/12, juntando as contagens.
// Linhas: [grupos..., vinculos, ativos, rem_media_sm, rem_dez_nom, idade_media, tempo_emprego_medio]
func dupla(st *Store, q Query) ([][]interface{}, []string, error) {
	r1, err := st.Query(q)
	if err != nil {
		return nil, nil, err
	}
	qa := q
	qa.Ativos = true
	r2, err := st.Query(qa)
	if err != nil {
		return nil, nil, err
	}
	ng := len(q.Group)
	chave := func(row []interface{}) string {
		var b strings.Builder
		for i := 0; i < ng; i++ {
			fmt.Fprintf(&b, "%v\x00", row[i])
		}
		return b.String()
	}
	at := map[string]int64{}
	for _, r := range r2.Rows {
		at[chave(r)] = r[ng].(int64)
	}
	var out [][]interface{}
	for _, r := range r1.Rows {
		row := make([]interface{}, 0, len(r)+1)
		row = append(row, r[:ng]...)
		row = append(row, r[ng])        // vinculos
		row = append(row, at[chave(r)]) // ativos
		row = append(row, r[ng+1:]...)
		out = append(out, row)
	}
	return out, r1.Avisos, nil
}

func cell(v interface{}) string {
	if s, ok := v.(string); ok {
		if s == "" {
			return semInfo
		}
		return s
	}
	return fnum(v)
}

func gerarPacote(ctx context.Context, a *App, logf logger) error {
	cfg := a.cfg.Get()
	d := cfg.Dirs()
	d.Make()
	st := a.Store()
	st.Refresh()
	infos := st.Info()
	if len(infos) == 0 {
		return fmt.Errorf("nenhum ano processado ainda — importe os CSVs na aba Dados")
	}
	a0, a1 := infos[0].Ano, infos[len(infos)-1].Ano
	out := d.Saida
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	logf("Pacote de turismo: anos %d–%d, gravando em %s", a0, a1, out)

	// ---- 07 cobertura (fatos sobre o que veio nos CSVs)
	cob := st.Cobertura()
	cf, err := novoCSV(filepath.Join(out, "07_cobertura.csv"), []string{"ano", "vinculos_no_csv", "coluna", "prevista_no_pdf", "presente_no_csv", "pct_preenchido"})
	if err != nil {
		return err
	}
	for _, r := range cob["anos"].([]map[string]interface{}) {
		cs := r["cols"].(map[string]interface{})
		for _, l := range layoutPDF {
			x := cs[l.Canon].(map[string]interface{})
			pct := ""
			if p, ok := x["pct"].(float64); ok {
				pct = strings.Replace(strconv.FormatFloat(p, 'f', 1, 64), ".", ",", 1)
			}
			cf.Write([]string{fmt.Sprint(r["ano"]), fmt.Sprint(r["n"]), l.Rotulo, simNao(x["prevista"].(bool)), simNao(x["presente"].(bool)), pct})
		}
	}
	if err := cf.Close(); err != nil {
		return err
	}

	// ---- totais da RMS (denominador da participação)
	tot, _, err := dupla(st, Query{AnoIni: a0, AnoFim: a1, Group: []string{"ano"}})
	if err != nil {
		return err
	}
	totais := map[int][2]int64{}
	for _, r := range tot {
		totais[r[0].(int)] = [2]int64{r[1].(int64), r[2].(int64)}
	}

	metr := []string{"vinculos", "ativos_3112", "rem_media_sm", "rem_dez_nom", "idade_media", "tempo_emprego_medio_meses"}
	var avisosGerais []string
	abre := func(nome string, grupos []string) (*csvOut, error) {
		return novoCSV(filepath.Join(out, nome), append(append([]string{"escopo"}, grupos...), metr...))
	}
	roda := func(arq string, grupos []string, group []string, filtraCBO string) error {
		o, err := abre(arq, grupos)
		if err != nil {
			return err
		}
		for _, e := range escopos {
			if ctx.Err() != nil {
				o.Close()
				return ctx.Err()
			}
			q := Query{AnoIni: a0, AnoFim: a1, Group: group, Turismo: e.Cod}
			if filtraCBO != "" {
				q.Turismo = ""
				q.CboTurismo = filtraCBO
			}
			rows, av, err := dupla(st, q)
			if err != nil {
				o.Close()
				logf("  %s: %v", arq, err)
				return nil
			}
			avisosGerais = append(avisosGerais, av...)
			rot := e.Rotulo
			if filtraCBO != "" {
				rot = "ocupações CBO 2002 (" + filtraCBO + "), em qualquer atividade econômica"
			}
			for _, r := range rows {
				rec := []string{rot}
				for i := range group {
					rec = append(rec, cell(r[i]))
				}
				for _, v := range r[len(group):] {
					rec = append(rec, fnum(v))
				}
				o.Write(rec)
			}
			if filtraCBO != "" {
				break
			}
		}
		logf("  gravado %s", arq)
		return o.Close()
	}

	// ---- 01 resumo por ano (com participação no total da RMS)
	{
		o, err := novoCSV(filepath.Join(out, "01_resumo_por_ano.csv"), append(append([]string{"escopo", "ano"}, metr...), "vinculos_total_rms", "participacao_turismo_pct", "observacao"))
		if err != nil {
			return err
		}
		classif := map[int]bool{}
		st.mu.RLock()
		for _, y := range infos {
			c := st.years[y.Ano].rf.col(colCNAE)
			classif[y.Ano] = c != nil && c.Val > 0
		}
		st.mu.RUnlock()
		for _, e := range escopos {
			rows, av, err := dupla(st, Query{AnoIni: a0, AnoFim: a1, Group: []string{"ano"}, Turismo: e.Cod})
			if err != nil {
				logf("  01_resumo_por_ano: %v", err)
				continue
			}
			avisosGerais = append(avisosGerais, av...)
			por := map[int][]interface{}{}
			for _, r := range rows {
				por[r[0].(int)] = r[1:]
			}
			for _, y := range infos {
				rec := []string{e.Rotulo, strconv.Itoa(y.Ano)}
				if !classif[y.Ano] {
					rec = append(rec, "", "", "", "", "", "", fnum(totais[y.Ano][0]), "", "sem CNAE 2.0 subclasse preenchida neste ano: turismo não classificável (nada foi estimado)")
					o.Write(rec)
					continue
				}
				r := por[y.Ano]
				var vin int64
				if r != nil {
					for _, v := range r {
						rec = append(rec, fnum(v))
					}
					vin = r[0].(int64)
				} else {
					rec = append(rec, "0", "0", "", "", "", "")
				}
				part := ""
				if t := totais[y.Ano][0]; t > 0 {
					part = fnum(float64(vin) * 100 / float64(t))
				}
				rec = append(rec, fnum(totais[y.Ano][0]), part, "")
				o.Write(rec)
			}
		}
		if err := o.Close(); err != nil {
			return err
		}
		logf("  gravado 01_resumo_por_ano.csv")
	}
	if err := roda("02_por_municipio.csv", []string{"ano", "municipio"}, []string{"ano", "municipio_nome"}, ""); err != nil {
		return err
	}
	if err := roda("03_por_grupo_atividade.csv", []string{"ano", "grupo_de_atividade"}, []string{"ano", "turismo_grupo"}, ""); err != nil {
		return err
	}
	if err := roda("04_por_cnae.csv", []string{"ano", "cnae_atividade"}, []string{"ano", "turismo_atividade"}, ""); err != nil {
		return err
	}
	if err := roda("08_municipio_x_grupo.csv", []string{"ano", "municipio", "grupo_de_atividade"}, []string{"ano", "municipio_nome", "turismo_grupo"}, ""); err != nil {
		return err
	}

	// ---- 05 perfil
	{
		dims := []struct{ Col, Rot string }{
			{"sexo", "sexo"}, {"faixa_etaria", "faixa etária"}, {"escolaridade", "escolaridade"}, {"raca_cor", "raça/cor"},
			{"tipo_vinculo", "tipo de vínculo"}, {"faixa_tempo_emprego", "faixa de tempo de emprego"},
			{"faixa_remun_media_sm", "faixa de remuneração média (SM)"}, {"cbo_grande_grupo_nome", "CBO – grande grupo"},
			{"tamanho_estab", "tamanho do estabelecimento"}, {"natureza_juridica", "natureza jurídica"},
		}
		o, err := novoCSV(filepath.Join(out, "05_perfil.csv"), append([]string{"escopo", "ano", "dimensao", "categoria"}, metr...))
		if err != nil {
			return err
		}
		for _, dm := range dims {
			if st.kindOf(dm.Col) != "cat" {
				logf("  perfil: coluna %s não existe em nenhum CSV — pulada", dm.Col)
				continue
			}
			for _, e := range escopos {
				if ctx.Err() != nil {
					o.Close()
					return ctx.Err()
				}
				rows, av, err := dupla(st, Query{AnoIni: a0, AnoFim: a1, Group: []string{"ano", dm.Col}, Turismo: e.Cod})
				if err != nil {
					logf("  perfil %s: %v", dm.Col, err)
					break
				}
				avisosGerais = append(avisosGerais, av...)
				for _, r := range rows {
					rec := []string{e.Rotulo, fnum(r[0]), dm.Rot, cell(r[1])}
					for _, v := range r[2:] {
						rec = append(rec, fnum(v))
					}
					o.Write(rec)
				}
			}
		}
		if err := o.Close(); err != nil {
			return err
		}
		logf("  gravado 05_perfil.csv")
	}

	// ---- 06 ocupações (CBO 2002), em qualquer atividade econômica
	for _, m := range []struct{ Cod, Arq string }{{"planilha", "06_ocupacoes_cbo_planilha.csv"}, {"minhas", "06b_ocupacoes_cbo_minhas.csv"}} {
		if m.Cod == "minhas" && len(cbosExtra()) == 0 {
			continue
		}
		if err := roda(m.Arq, []string{"ano", "cbo_codigo", "cbo_ocupacao"}, []string{"ano", "cbo_2002", "cbo_2002_nome"}, m.Cod); err != nil {
			return err
		}
	}

	// ---- vínculos de turismo (uma linha por vínculo), um arquivo por ano
	for _, y := range infos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		st.mu.RLock()
		c := st.years[y.Ano].rf.col(colCNAE)
		st.mu.RUnlock()
		if c == nil || c.Val == 0 {
			continue
		}
		p := filepath.Join(out, fmt.Sprintf("turismo_vinculos_%d.csv", y.Ano))
		f, err := os.Create(p)
		if err != nil {
			return err
		}
		err = st.ExportCSV(f, Query{AnoIni: y.Ano, AnoFim: y.Ano, Turismo: "todas", ColsTurismo: true})
		f.Close()
		if err != nil {
			logf("  %d: erro ao gravar vínculos: %v", y.Ano, err)
			continue
		}
		logf("  gravado turismo_vinculos_%d.csv", y.Ano)
	}

	// ---- leia-me do pacote (avisos únicos)
	seen := map[string]bool{}
	var av []string
	for _, x := range avisosGerais {
		if !seen[x] {
			seen[x] = true
			av = append(av, x)
		}
	}
	sort.Strings(av)
	if err := os.WriteFile(filepath.Join(out, "LEIAME_pacote.txt"), []byte(textoLeiame(infos, av)), 0o644); err != nil {
		return err
	}
	logf("Pacote pronto: %s", out)
	return nil
}

func simNao(b bool) string {
	if b {
		return "sim"
	}
	return "não"
}

func textoLeiame(infos []YearInfo, avisos []string) string {
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBFPACOTE DE TURISMO – RAIS, RMS de Sorocaba\r\n\r\n")
	b.WriteString("Tudo aqui vem dos CSVs de cada ano que você forneceu; nada foi estimado, completado ou aproximado.\r\n\r\n")
	b.WriteString("ARQUIVOS\r\n")
	b.WriteString("01_resumo_por_ano.csv ......... vínculos, ativos em 31/12, remuneração, idade, tempo de emprego e participação no total da RMS, por ano\r\n")
	b.WriteString("02_por_municipio.csv .......... idem, por município\r\n")
	b.WriteString("03_por_grupo_atividade.csv .... idem, por grupo de atividade da planilha (11 grupos)\r\n")
	b.WriteString("04_por_cnae.csv ............... idem, por CNAE 2.0 subclasse da lista\r\n")
	b.WriteString("05_perfil.csv ................. sexo, faixa etária, escolaridade, raça/cor, tipo de vínculo, tempo de emprego, faixa de remuneração, CBO grande grupo, tamanho do estabelecimento, natureza jurídica\r\n")
	b.WriteString("06_ocupacoes_cbo_*.csv ........ ocupações CBO 2002 da planilha (e as que você escolheu), em qualquer atividade econômica\r\n")
	b.WriteString("07_cobertura.csv .............. para cada ano e coluna: o PDF prevê? veio no CSV? em quanto % dos vínculos está preenchida?\r\n")
	b.WriteString("08_municipio_x_grupo.csv ...... município × grupo de atividade\r\n")
	b.WriteString("turismo_vinculos_ANO.csv ...... uma linha por vínculo de turismo (104 CNAEs), todas as colunas do CSV de origem + turismo_nucleo/grupo/atividade\r\n\r\n")
	b.WriteString("DEFINIÇÕES\r\n")
	b.WriteString("- Vínculo = vínculo empregatício registrado na RAIS (uma pessoa com dois empregos conta duas vezes). 'Postos de trabalho' no sentido estrito = vínculos ativos em 31/12 (coluna ativos_3112).\r\n")
	b.WriteString("- Turismo = estabelecimento cuja CNAE 2.0 subclasse está na lista da planilha do Observatório (aba 'CNAEs Turismo OMT'): 60 do núcleo IBGE/IPEA + 44 extras do Cadastur.\r\n")
	b.WriteString("- Médias de remuneração, idade e tempo ignoram valores zerados/vazios. Valores nominais (reais da época), sem correção.\r\n")
	b.WriteString("- Escopo 'nucleo' é parte de 'todas': não some os dois.\r\n\r\n")
	b.WriteString("LIMITES (o que a RAIS pública não permite)\r\n")
	b.WriteString("- Anos sem CNAE 2.0 subclasse preenchida não podem ser classificados como turismo (só têm CNAE 1.0); aparecem em branco no resumo e na cobertura.\r\n")
	b.WriteString("- CBO 2002 só a partir de 2003 (antes é CBO 94, sem tabela de equivalência neste pacote).\r\n")
	b.WriteString("- Não há número de empresas/estabelecimentos (a RAIS pública não identifica o estabelecimento), nem informalidade, nem previdência, nem local de residência do trabalhador.\r\n")
	b.WriteString("- A planilha cita CBOs de hospitalidade, gastronomia e transporte sem códigos: só entram se você as escolher na aba Ocupações.\r\n\r\n")
	b.WriteString("ANOS PROCESSADOS: ")
	for i, y := range infos {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d (%d vínculos)", y.Ano, y.N)
	}
	b.WriteString("\r\n")
	if len(avisos) > 0 {
		b.WriteString("\r\nAVISOS DESTA GERAÇÃO\r\n")
		for _, x := range avisos {
			b.WriteString("- " + x + "\r\n")
		}
	}
	return b.String()
}
