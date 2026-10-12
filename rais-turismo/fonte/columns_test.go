package main

import (
	"strings"
	"testing"
)

const hdr2025 = `Bairros SP - Código,Bairros Fortaleza - Código,Bairros RJ - Código,Causa Afastamento 1 - Código,Causa Afastamento 2 - Código,Causa Afastamento 3 - Código,Faixa Rem Média (SM) - Código,Faixa Hora Contrat - Código,Faixa Rem Dez (SM) - Código,Faixa Tempo Emprego - Código,Escolaridade Após 2005 - Código,Qtd Hora Contr,Idade,Ind CEI Vinculado - Código,Ind Estabelecimento Participante SIMPLES - Código,Mês Admissão - Código,Mês Desligamento - Código,Município Trab - Código,Município - Código,Nacionalidade - Código,Natureza Jurídica - Código,Ind Portador Defic - Código,Qtd Dias Afastamento,Raça Cor - Código,Região Adm DF - Código,Vl Rem Dezembro Nom,Vl Rem Dezembro (SM),Vl Rem Média Nom,Vl Rem Média (SM),CNAE 2.0 Subclasse - Código,Sexo Trabalhador - Código,Tamanho Estabelecimento - Código,Tempo Emprego,Tipo Admissão Trabalhador - Código,Tipo Estabelecimento - Código,Tipo Estabelecimento - Nome,Tipo Deficiência - Código,Tipo Vínculo - Código,IBGE Subsetor - Código,Vl Rem Janeiro SC,Ano Chegada Brasil,Ind Trabalho Intermitente - Código,Ind Trabalho Parcial - Código,Ind Vínculo Abandonado - Código,Categoria Trabalhador - Código,CBO 2002 Ocupação - Código,CNAE 2.0 Classe - Código,CNAE 95 Classe - Código,Ind Vínculo Ativo 31/12 - Código,Motivo Desligamento - Código,Faixa Etária - Código`

func TestMapColumns2025(t *testing.T) {
	h := strings.Split(hdr2025, ",")
	got := map[string]bool{}
	for _, c := range mapColumns(h, 2025) {
		if c != "" {
			got[c] = true
		}
	}
	for _, c := range []string{"municipio", "mun_trab", "cbo_2002", "cnae20_classe", "cnae20_subclasse", "sexo", "escolaridade",
		"vinculo_ativo_3112", "tipo_vinculo", "remun_media_sm", "remun_dez_nom", "remun_media_nom", "faixa_etaria",
		"faixa_remun_media_sm", "tipo_admissao", "tipo_estab", "ibge_subsetor", "raca_cor", "motivo_desligamento", "idade"} {
		if !got[c] {
			t.Errorf("coluna %s não reconhecida", c)
		}
	}
}

func TestSplitQuotedParseNum(t *testing.T) {
	f := splitFields([]byte(`1,"1.234,56",abc,"x ""y"""`), ',', nil)
	if len(f) != 4 || string(f[1]) != "1.234,56" || string(f[3]) != `x "y"` {
		t.Fatalf("%q", f)
	}
	if v := parseNum([]byte("1.234,56")); v != 1234.56 {
		t.Fatal(v)
	}
	if v := parseNum([]byte("1234.5")); v != 1234.5 {
		t.Fatal(v)
	}
	if v := parseNum([]byte("7651,80")); v != 7651.8 {
		t.Fatal(v)
	}
}

func TestEscolaridadeAntiga(t *testing.T) {
	for _, h := range []string{"Grau Instrução 1985-2005", "Escolaridade 1985-2005", "Escolaridade até 2005", "Grau Instrução", "Escolaridade"} {
		c := mapColumns([]string{"Município", h}, 2001)
		if c[1] != "escolaridade" {
			t.Errorf("%q não reconhecida: %v", h, c)
		}
	}
	// com as duas colunas, o ano decide
	h := []string{"Município", "Escolaridade após 2005", "Grau Instrução 1985-2005"}
	if c := mapColumns(h, 2001); c[2] != "escolaridade" || c[1] != "" {
		t.Errorf("2001: %v", c)
	}
	if c := mapColumns(h, 2010); c[1] != "escolaridade" || c[2] != "" {
		t.Errorf("2010: %v", c)
	}
}

func TestParseCnae95(t *testing.T) {
	csv := "\xef\xbb\xbfSeção;Divisão;Grupo;Classe;Denominação\nA;;;;AGRICULTURA\n;01;;;AGRICULTURA, PECUÁRIA\n;;01.1;;Produção de lavouras\n;;;01.11-0;Cultivo de cereais\n74500;Seleção, agenciamento e locação de mão-de-obra\n"
	m := parseCnae95([]byte(csv))
	if m["01110"] != "Cultivo de cereais" || m["74500"] != "Seleção, agenciamento e locação de mão-de-obra" || len(m) != 2 {
		t.Fatalf("%v", m)
	}
}
