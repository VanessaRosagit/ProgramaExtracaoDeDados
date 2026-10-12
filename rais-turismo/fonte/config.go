package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// CboSel é uma ocupação escolhida pelo usuário (a planilha cita grupos sem código; quem decide os códigos é ele).
type CboSel struct {
	Cod   string `json:"cod"`   // 6 dígitos
	Nome  string `json:"nome"`  // título oficial da CBO 2002
	Grupo string `json:"grupo"` // rótulo livre: hospitalidade, gastronomia, transporte…
}

type Config struct {
	PastaCSV       string   `json:"pasta_csv"`   // onde estão os CSVs de cada ano (somente leitura)
	PastaDados     string   `json:"pasta_dados"` // onde o programa grava seus arquivos
	UF             string   `json:"uf"`
	AnoInicial     int      `json:"ano_inicial"`
	AnoFinal       int      `json:"ano_final"`
	IBGEAPI        string   `json:"ibge_api"` // só para nomes de CNAE fora do turismo (opcional)
	PortaPreferida int      `json:"porta"`
	CbosExtra      []CboSel `json:"cbos_extra"`
}

func defaultConfig(exeDir string) Config {
	return Config{
		PastaCSV:       filepath.Join(exeDir, "csv"),
		PastaDados:     filepath.Join(exeDir, "dados_turismo"),
		UF:             "SP",
		AnoInicial:     2000,
		AnoFinal:       2025,
		IBGEAPI:        "https://servicodados.ibge.gov.br",
		PortaPreferida: 8766,
	}
}

type ConfigStore struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func loadConfigStore(exeDir string) *ConfigStore {
	cs := &ConfigStore{path: filepath.Join(exeDir, "config.json"), cfg: defaultConfig(exeDir)}
	if b, err := os.ReadFile(cs.path); err == nil {
		c := cs.cfg
		if json.Unmarshal(b, &c) == nil {
			cs.cfg = c
		}
	}
	cs.fix()
	cs.save()
	return cs
}

func (cs *ConfigStore) fix() {
	c := &cs.cfg
	if c.PastaDados == "" {
		c.PastaDados = "dados_turismo"
	}
	if c.UF == "" {
		c.UF = "SP"
	}
	if c.AnoInicial == 0 {
		c.AnoInicial = 2000
	}
	if c.AnoFinal == 0 || c.AnoFinal < c.AnoInicial {
		c.AnoFinal = c.AnoInicial
	}
	if c.IBGEAPI == "" {
		c.IBGEAPI = "https://servicodados.ibge.gov.br"
	}
	if c.PortaPreferida == 0 {
		c.PortaPreferida = 8766
	}
}

func (cs *ConfigStore) Get() Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	c := cs.cfg
	c.CbosExtra = append([]CboSel(nil), cs.cfg.CbosExtra...)
	return c
}

func (cs *ConfigStore) Set(c Config) {
	cs.mu.Lock()
	cs.cfg = c
	cs.fix()
	cs.mu.Unlock()
	cs.save()
}

func (cs *ConfigStore) save() {
	cs.mu.RLock()
	b, _ := json.MarshalIndent(cs.cfg, "", "  ")
	cs.mu.RUnlock()
	_ = os.WriteFile(cs.path, b, 0o644)
}

// pastas derivadas (tudo dentro de pasta_dados; os CSVs de entrada nunca são alterados)
type Dirs struct{ Raiz, Cols, Apoio, Diag, Logs, Saida string }

func (c Config) Dirs() Dirs {
	r := c.PastaDados
	return Dirs{
		Raiz: r, Cols: filepath.Join(r, "cols"), Apoio: filepath.Join(r, "apoio"),
		Diag: filepath.Join(r, "diagnostico"), Logs: filepath.Join(r, "logs"),
		Saida: filepath.Join(r, "saida_turismo"),
	}
}

func (d Dirs) Make() {
	for _, p := range []string{d.Raiz, d.Cols, d.Apoio, d.Diag, d.Logs, d.Saida} {
		_ = os.MkdirAll(p, 0o755)
	}
}
