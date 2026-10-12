# RAIS Turismo – RMS de Sorocaba

Programa para Windows (`rais-turismo.exe`) que lê os **CSVs de cada ano que você já extraiu** (29 municípios da RMS de Sorocaba) e extrai **tudo sobre turismo**, usando só o que está nos dados. Não baixa nada da internet e **nunca altera nem apaga seus CSVs**.

## Como usar
1. Dê dois cliques em `rais-turismo.exe` (abre o painel no navegador; a janela preta precisa ficar aberta).
2. Aba **Configurações**: informe a *pasta dos CSVs* (ex.: `D:\Nova pasta (3)\rais-rms\dados`) e salve. Por padrão é a pasta `csv` ao lado do programa.
3. Aba **Dados**: marque os anos e clique **Importar marcados**. O programa acha os arquivos pelo **ano no nome** (`rais_2001_2001.csv`, `dados_rais_rms_2022_traduzido.csv`…), em qualquer subpasta. Nomes com dois anos diferentes (`rais_2000_2025.csv`) ou sem ano são ignorados e listados na tela.
4. Ainda em **Dados**, veja a tabela *“O que o PDF prevê × o que veio nos seus CSVs”*.
5. Aba **Turismo › Extrair tudo sobre turismo › Gerar pacote de turismo**. Os arquivos saem em `dados_turismo\saida_turismo` (botão *Abrir pasta*).

## O que o pacote traz (CSV com `;`, vírgula decimal, UTF‑8 com BOM – abre direto no Excel)
| Arquivo | Conteúdo |
|---|---|
| `01_resumo_por_ano.csv` | vínculos, ativos em 31/12, remuneração média (SM) e de dezembro, idade, tempo de emprego, participação no total da RMS |
| `02_por_municipio.csv` / `03_por_grupo_atividade.csv` / `04_por_cnae.csv` / `08_municipio_x_grupo.csv` | os mesmos indicadores por município, grupo, CNAE e município × grupo |
| `05_perfil.csv` | sexo, faixa etária, escolaridade, raça/cor, tipo de vínculo, tempo de emprego, faixa de remuneração, CBO grande grupo (qualificação), tamanho do estabelecimento, natureza jurídica |
| `06_ocupacoes_cbo_planilha.csv` (e `06b_…minhas.csv`) | as 8 ocupações da planilha (e as que você escolher) em qualquer atividade econômica |
| `07_cobertura.csv` | por ano e coluna: o PDF prevê? veio no CSV? em quanto % está preenchida? |
| `turismo_vinculos_ANO.csv` | uma linha por vínculo de turismo (104 CNAEs), todas as colunas + `turismo_nucleo`, `turismo_grupo`, `turismo_atividade` |
| `LEIAME_pacote.txt` | definições, limites e avisos da geração |

Todos os indicadores saem nos dois escopos: **ACTs núcleo IBGE/IPEA (60 CNAEs)** e **ACTs + extras do Cadastur (104 CNAEs)** (o núcleo é parte do outro: não some os dois).

## Regras de rigor (nada inventado)
- Turismo = CNAE 2.0 **subclasse** na lista da aba “CNAEs Turismo OMT”. Sem aproximar por classe nem por CNAE 1.0.
- **Ano sem CNAE 2.0 subclasse preenchida não é classificado**: fica em branco, com a observação, e é avisado em todas as telas. Pelo PDF das colunas, a CNAE 2.0 só existe a partir de 2004; antes só há CNAE 95/1.0. Mesmo em 2004+ o programa confere se a coluna veio **preenchida** (a coluna existir não basta). Anos com preenchimento parcial (< 98%) são avisados como possível subestimação.
- Ocupações: CBO 2002 só a partir de 2003 (antes é CBO 94, sem tabela de equivalência). A planilha cita *hospitalidade, gastronomia e transporte* sem códigos: **não foram inventados**. Na aba **Ocupações** você busca pelo nome (títulos e sinônimos oficiais da CBO 2002) e escolhe quais entram.
- Escolaridade: “Grau Instrução 2005‑1985” até 2005 e “Escolaridade após 2005” depois (nomes do PDF). Rótulos do `dicionario.csv` só quando a chave existe no ano; senão fica o código.
- Médias ignoram zeros/vazios; valores nominais (reais da época).
- Conferência por ano em `dados_turismo\diagnostico\conferencia_ANO.csv`: linhas lidas = municípios do recorte + outros municípios + outras UF + curtas + inválidas (diferença deve ser 0). Colunas reconhecidas em `colunas_ANO.csv`.

## O que a RAIS pública não permite (e portanto o programa não entrega)
Número de empresas/estabelecimentos (não há identificador), informalidade, previdência, origem/residência do trabalhador (movimento pendular) – a coluna “Município Trab” é o município de trabalho, não a residência –, ISS/ICMS, Cadastur, CAGED.

## Formatos de CSV aceitos
Separador `;` `,` ou tab; UTF‑8 (com/sem BOM) ou Latin‑1; decimal com vírgula ou ponto. Cabeçalhos: os nomes originais da RAIS por ano (os do PDF, inclusive `… - Código` de 2023+) **e** os nomes canônicos do programa anterior (`cnae20_subclasse`, `cbo_2002`, `vinculo_ativo_3112`…). Colunas extras (`ano`, `municipio_codigo`, `municipio_nome`, `act_grupo`) são ignoradas; o ano vem do nome do arquivo. Valores traduzidos (Sim/Não, “Masculino”) ou códigos brutos funcionam.

## Linha de comando (opcional)
`rais-turismo.exe -processar 2004-2023` importa sem painel · `rais-turismo.exe -pacote` gera o pacote · `-refazer` reimporta anos prontos.

## Testado / não testado
Testado em Linux com CSVs sintéticos nos 4 layouts (RAIS original 2000/2003/2004/2010, 2023+ com aspas e “- Código”, programa anterior), conferindo contagens de turismo e CBO contra um cálculo independente, e o painel no Chromium. **Não testado no Windows nem com os seus CSVs reais.** Se algum ano der “colunas não reconhecidas”, mande a linha do log (ou `colunas_ANO.csv`).

## Compilar
Pasta `fonte`: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w"` (só biblioteca padrão do Go).
