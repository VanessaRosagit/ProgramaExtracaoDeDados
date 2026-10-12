package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

type App struct {
	cfg    *ConfigStore
	mu     sync.Mutex
	store  *Store
	stDir  string
	job    *Job
	port   int
	logmu  sync.Mutex
	logOut *os.File
}

func NewApp(cfg *ConfigStore) *App {
	a := &App{cfg: cfg}
	a.job = &Job{app: a, resultados: map[int]string{}}
	cbosExtra = func() []string {
		var out []string
		for _, c := range a.cfg.Get().CbosExtra {
			out = append(out, c.Cod)
		}
		return out
	}
	a.Store() // inicializa
	return a
}

// Store devolve o armazenamento da pasta de dados atual (recriado se a pasta mudar).
func (a *App) Store() *Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.cfg.Get().Dirs()
	if a.store == nil || a.stDir != d.Cols {
		d.Make()
		a.store = NewStore(d.Cols)
		a.stDir = d.Cols
		a.store.Refresh()
	}
	return a.store
}

func (a *App) logFile(line string) {
	a.logmu.Lock()
	defer a.logmu.Unlock()
	d := a.cfg.Get().Dirs()
	if a.logOut == nil {
		d.Make()
		a.logOut, _ = os.OpenFile(filepath.Join(d.Logs, "programa.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	}
	if a.logOut != nil {
		fmt.Fprintln(a.logOut, line)
	}
}

// ---------- processamento em segundo plano ----------

type Job struct {
	app        *App
	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	anoAtual   int
	log        []string
	base       int
	resultados map[int]string
	done       chan struct{}
}

func (j *Job) logf(format string, args ...interface{}) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	j.mu.Lock()
	j.log = append(j.log, line)
	if len(j.log) > 5000 {
		cut := len(j.log) - 4000
		j.log = j.log[cut:]
		j.base += cut
	}
	j.mu.Unlock()
	fmt.Println(line)
	j.app.logFile(line)
}

func (j *Job) Start(anos []int, refazer bool) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("já existe um processamento em andamento")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.running, j.cancel, j.done = true, cancel, make(chan struct{})
	j.resultados = map[int]string{}
	j.mu.Unlock()
	go j.run(ctx, anos, refazer)
	return nil
}

// StartPacote gera o pacote de turismo em segundo plano (mesmo controle de "em andamento" do processamento).
func (j *Job) StartPacote() error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("já existe um processamento em andamento")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.running, j.cancel, j.done = true, cancel, make(chan struct{})
	j.mu.Unlock()
	go func() {
		defer func() {
			j.mu.Lock()
			j.running, j.anoAtual = false, 0
			close(j.done)
			j.mu.Unlock()
		}()
		j.logf("Gerando o pacote de turismo…")
		if err := gerarPacote(ctx, j.app, j.logf); err != nil {
			j.logf("PACOTE FALHOU — %v", err)
		}
	}()
	return nil
}

func (j *Job) Cancel() {
	j.mu.Lock()
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Unlock()
}

func (j *Job) Wait() {
	j.mu.Lock()
	d := j.done
	j.mu.Unlock()
	if d != nil {
		<-d
	}
}

func (j *Job) run(ctx context.Context, anos []int, refazer bool) {
	defer func() {
		j.mu.Lock()
		j.running, j.anoAtual = false, 0
		close(j.done)
		j.mu.Unlock()
	}()
	cfg := j.app.cfg.Get()
	sort.Ints(anos)
	j.logf("Iniciando: %d ano(s). CSVs lidos de %s; dados gravados em %s", len(anos), cfg.PastaCSV, cfg.PastaDados)
	sup, err := loadSupport(cfg, j.logf)
	if err != nil {
		j.logf("ERRO: %v", err)
		for _, a := range anos {
			j.setRes(a, "falhou: "+err.Error())
		}
		return
	}
	nok, nfail := 0, 0
	for _, ano := range anos {
		if ctx.Err() != nil {
			j.setRes(ano, "cancelado")
			continue
		}
		j.mu.Lock()
		j.anoAtual = ano
		j.mu.Unlock()
		st, err := processYear(ctx, cfg, sup, ano, refazer, j.logf)
		switch {
		case err == nil:
			j.setRes(ano, st)
			nok++
			j.app.Store().Refresh()
		case ctx.Err() != nil:
			j.setRes(ano, "cancelado")
			j.logf("%d: cancelado.", ano)
		default:
			j.setRes(ano, "falhou: "+err.Error())
			j.logf("%d: FALHOU — %v", ano, err)
			nfail++
		}
	}
	j.logf("Fim: %d ok, %d com falha.", nok, nfail)
}

func (j *Job) setRes(ano int, s string) {
	j.mu.Lock()
	j.resultados[ano] = s
	j.mu.Unlock()
}

// ---------- HTTP ----------

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"erro": msg})
}

func (a *App) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
			http.Error(w, "host não permitido", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-RAIS") != "1" {
			http.Error(w, "requisição não permitida", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func readJSON(r *http.Request, v interface{}) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(indexHTML)
	}))
	mux.HandleFunc("/api/estado", a.guard(a.hEstado))
	mux.HandleFunc("/api/config", a.guard(a.hConfig))
	mux.HandleFunc("/api/processar", a.guard(a.hProcessar))
	mux.HandleFunc("/api/cancelar", a.guard(func(w http.ResponseWriter, r *http.Request) {
		a.job.Cancel()
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/job", a.guard(a.hJob))
	mux.HandleFunc("/api/meta", a.guard(a.hMeta))
	mux.HandleFunc("/api/turismo", a.guard(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, turismo.Ordem) }))
	mux.HandleFunc("/api/cobertura", a.guard(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.Store().Cobertura()) }))
	mux.HandleFunc("/api/valores", a.guard(a.hValores))
	mux.HandleFunc("/api/consulta", a.guard(a.hConsulta))
	mux.HandleFunc("/api/exportar", a.guard(a.hExportar))
	mux.HandleFunc("/api/pacote", a.guard(func(w http.ResponseWriter, r *http.Request) {
		if err := a.job.StartPacote(); err != nil {
			httpErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/saida", a.guard(a.hSaida))
	mux.HandleFunc("/api/abrir-pasta", a.guard(a.hAbrirPasta))
	mux.HandleFunc("/api/cbo/busca", a.guard(a.hCboBusca))
	mux.HandleFunc("/api/cbo/minhas", a.guard(a.hCboMinhas))
	return mux
}

func (a *App) hEstado(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	info := map[int]YearInfo{}
	for _, y := range a.Store().Info() {
		info[y.Ano] = y
	}
	a.job.mu.Lock()
	res := map[int]string{}
	for k, v := range a.job.resultados {
		res[k] = v
	}
	running, atual := a.job.running, a.job.anoAtual
	a.job.mu.Unlock()
	porAno, semAno, errCSV := varrerCSV(cfg.PastaCSV)
	type row struct {
		Ano       int    `json:"ano"`
		Estado    string `json:"estado"`
		N         int    `json:"n"`
		Bytes     int64  `json:"bytes"`
		Resultado string `json:"resultado,omitempty"`
		Arquivo   string `json:"arquivo,omitempty"`
		ArqMB     int64  `json:"arq_mb,omitempty"`
		Outros    int    `json:"outros,omitempty"`
	}
	seen := map[int]bool{}
	var anos []row
	for a := cfg.AnoInicial; a <= cfg.AnoFinal; a++ {
		seen[a] = true
		x := row{Ano: a, Estado: "sem csv", Resultado: res[a]}
		if c := porAno[a]; len(c) > 0 {
			x.Estado, x.Arquivo, x.ArqMB, x.Outros = "pendente", filepath.Base(c[0].Path), c[0].Size>>20, len(c)-1
		}
		if y, ok := info[a]; ok {
			x.Estado, x.N, x.Bytes = "pronto", y.N, y.Bytes
		}
		anos = append(anos, x)
	}
	var extra []int
	for a := range info {
		if !seen[a] {
			extra = append(extra, a)
		}
	}
	sort.Ints(extra)
	for _, a := range extra {
		anos = append(anos, row{Ano: a, Estado: "pronto", N: info[a].N, Bytes: info[a].Bytes})
	}
	csvErr := ""
	if errCSV != nil {
		csvErr = errCSV.Error()
	}
	writeJSON(w, 200, map[string]interface{}{"config": cfg, "anos": anos, "rodando": running, "ano_atual": atual, "versao": versao,
		"csv_erro": csvErr, "csv_sem_ano": semAno})
}

func (a *App) hConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var c Config
		if err := readJSON(r, &c); err != nil {
			httpErr(w, 400, err.Error())
			return
		}
		a.job.mu.Lock()
		busy := a.job.running
		a.job.mu.Unlock()
		if busy {
			httpErr(w, 409, "aguarde o processamento terminar para alterar a configuração")
			return
		}
		old := a.cfg.Get()
		c.UF = old.UF
		c.CbosExtra = old.CbosExtra
		c.PastaCSV = strings.TrimSpace(c.PastaCSV)
		c.PastaDados = strings.TrimSpace(c.PastaDados)
		if c.PortaPreferida == 0 {
			c.PortaPreferida = old.PortaPreferida
		}
		a.cfg.Set(c)
		a.Store()
	}
	writeJSON(w, 200, a.cfg.Get())
}

func (a *App) hProcessar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Anos    []int `json:"anos"`
		Refazer bool  `json:"refazer"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Anos) == 0 {
		httpErr(w, 400, "informe os anos")
		return
	}
	if err := a.job.Start(req.Anos, req.Refazer); err != nil {
		httpErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hJob(w http.ResponseWriter, r *http.Request) {
	desde, _ := strconv.Atoi(r.URL.Query().Get("desde"))
	j := a.job
	j.mu.Lock()
	defer j.mu.Unlock()
	start := desde - j.base
	if start < 0 {
		start = 0
	}
	var lines []string
	if start < len(j.log) {
		lines = append(lines, j.log[start:]...)
	}
	res := map[string]string{}
	for k, v := range j.resultados {
		res[strconv.Itoa(k)] = v
	}
	writeJSON(w, 200, map[string]interface{}{
		"rodando": j.running, "ano_atual": j.anoAtual, "linhas": lines,
		"proximo": j.base + len(j.log), "resultados": res,
	})
}

func (a *App) hMeta(w http.ResponseWriter, r *http.Request) {
	st := a.Store()
	type col struct {
		Nome string `json:"nome"`
		Tipo string `json:"tipo"`
	}
	cols := []col{}
	for _, c := range st.Columns() {
		cols = append(cols, col{c.Name, c.Kind})
	}
	writeJSON(w, 200, map[string]interface{}{"anos": append([]int{}, st.Years()...), "colunas": cols})
}

func (a *App) hValores(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.Store().Values(r.URL.Query().Get("col")))
}

func (a *App) hConsulta(w http.ResponseWriter, r *http.Request) {
	var q Query
	if err := readJSON(r, &q); err != nil {
		httpErr(w, 400, err.Error())
		return
	}
	res, err := a.Store().Query(q)
	if err != nil {
		httpErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (a *App) hExportar(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := Query{}
	q.AnoIni, _ = strconv.Atoi(qs.Get("a0"))
	q.AnoFim, _ = strconv.Atoi(qs.Get("a1"))
	q.Ativos = qs.Get("ativos") == "1"
	q.Turismo = qs.Get("t")
	q.CboTurismo = qs.Get("c")
	if f := qs.Get("f"); f != "" {
		if err := json.Unmarshal([]byte(f), &q.Filtros); err != nil {
			httpErr(w, 400, "filtros inválidos")
			return
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rais_%d_%d.csv"`, q.AnoIni, q.AnoFim))
	if err := a.Store().ExportCSV(w, q); err != nil {
		fmt.Println("erro na exportação:", err)
	}
}

// hSaida lista os arquivos já gerados no pacote.
func (a *App) hSaida(w http.ResponseWriter, r *http.Request) {
	d := a.cfg.Get().Dirs()
	type f struct {
		Nome string `json:"nome"`
		KB   int64  `json:"kb"`
	}
	out := []f{}
	es, _ := os.ReadDir(d.Saida)
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		if i, err := e.Info(); err == nil {
			out = append(out, f{e.Name(), (i.Size() + 1023) / 1024})
		}
	}
	writeJSON(w, 200, map[string]interface{}{"pasta": d.Saida, "arquivos": out})
}

// hAbrirPasta abre a pasta de saída (ou de dados) no Explorer do Windows.
func (a *App) hAbrirPasta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErr(w, 405, "use POST")
		return
	}
	d := a.cfg.Get().Dirs()
	p := d.Saida
	if r.URL.Query().Get("qual") == "dados" {
		p = d.Raiz
	}
	_ = os.MkdirAll(p, 0o755)
	abrirPasta(p)
	writeJSON(w, 200, map[string]string{"pasta": p})
}

func (a *App) hCboBusca(w http.ResponseWriter, r *http.Request) {
	carregaCboBusca(a.cfg.Get().Dirs().Apoio)
	writeJSON(w, 200, buscaCBO(r.URL.Query().Get("q"), 80))
}

// hCboMinhas: GET lista; POST {acao: add|remove, cod, grupo}.
func (a *App) hCboMinhas(w http.ResponseWriter, r *http.Request) {
	carregaCboBusca(a.cfg.Get().Dirs().Apoio)
	if r.Method == http.MethodPost {
		var req struct {
			Acao  string `json:"acao"`
			Cod   string `json:"cod"`
			Grupo string `json:"grupo"`
		}
		if err := readJSON(r, &req); err != nil {
			httpErr(w, 400, err.Error())
			return
		}
		nome, ok := cboNomes[req.Cod]
		if !ok {
			httpErr(w, 400, "código CBO 2002 inexistente na tabela oficial: "+req.Cod)
			return
		}
		c := a.cfg.Get()
		var nova []CboSel
		for _, x := range c.CbosExtra {
			if x.Cod != req.Cod {
				nova = append(nova, x)
			}
		}
		if req.Acao == "add" {
			nova = append(nova, CboSel{Cod: req.Cod, Nome: nome, Grupo: strings.TrimSpace(req.Grupo)})
		}
		sort.Slice(nova, func(i, j int) bool { return nova[i].Cod < nova[j].Cod })
		c.CbosExtra = nova
		a.cfg.Set(c)
	}
	l := a.cfg.Get().CbosExtra
	if l == nil {
		l = []CboSel{}
	}
	writeJSON(w, 200, l)
}
