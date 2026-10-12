package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const versao = "1.0"

func abrirNavegador(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// abrirPasta abre uma pasta no gerenciador de arquivos do sistema.
func abrirPasta(p string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	_ = cmd.Start()
}

func main() {
	porta := flag.Int("porta", 0, "porta do painel (padrão: a da configuração)")
	semNav := flag.Bool("sem-navegador", false, "não abrir o navegador")
	dirCfg := flag.String("config-dir", "", "pasta do config.json (padrão: pasta do programa)")
	processar := flag.String("processar", "", "processa anos sem abrir o painel (ex.: 2005,2010-2012) e sai")
	refazer := flag.Bool("refazer", false, "com -processar: refaz anos já prontos")
	pacote := flag.Bool("pacote", false, "gera o pacote de turismo (CSV) sem abrir o painel e sai")
	flag.Parse()

	exeDir := *dirCfg
	if exeDir == "" {
		if p, err := os.Executable(); err == nil {
			exeDir = filepath.Dir(p)
		} else {
			exeDir, _ = os.Getwd()
		}
	}
	cfg := loadConfigStore(exeDir)
	app := NewApp(cfg)

	if *pacote {
		if err := app.job.StartPacote(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		app.job.Wait()
		return
	}
	if *processar != "" {
		anos, err := parseAnos(*processar)
		if err != nil {
			fmt.Println("anos inválidos:", err)
			os.Exit(2)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		go func() { <-ctx.Done(); app.job.Cancel() }()
		if err := app.job.Start(anos, *refazer); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		app.job.Wait()
		for _, a := range anos {
			if strings.HasPrefix(app.job.resultados[a], "falhou") {
				os.Exit(1)
			}
		}
		return
	}

	p := *porta
	if p == 0 {
		p = cfg.Get().PortaPreferida
	}
	var ln net.Listener
	var err error
	for i := 0; i < 30; i++ {
		ln, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p+i))
		if err == nil {
			p += i
			break
		}
	}
	if err != nil {
		fmt.Println("não consegui abrir uma porta local:", err)
		os.Exit(1)
	}
	app.port = p
	url := fmt.Sprintf("http://127.0.0.1:%d/", p)
	fmt.Printf("RAIS Turismo — painel e extração (v%s)\n", versao)
	fmt.Printf("Abra %s no navegador. Para encerrar, feche esta janela (Ctrl+C).\n", url)
	fmt.Printf("Dados em: %s\n\n", cfg.Get().PastaDados)
	if !*semNav {
		go func() { time.Sleep(400 * time.Millisecond); abrirNavegador(url) }()
	}
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 15 * time.Second}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Println(err)
	}
}

// parseAnos aceita "2005,2010-2012".
func parseAnos(s string) ([]int, error) {
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if a, b, ok := strings.Cut(p, "-"); ok {
			x, e1 := strconv.Atoi(a)
			y, e2 := strconv.Atoi(b)
			if e1 != nil || e2 != nil || y < x {
				return nil, fmt.Errorf("intervalo inválido: %s", p)
			}
			for i := x; i <= y; i++ {
				out = append(out, i)
			}
		} else {
			x, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("ano inválido: %s", p)
			}
			out = append(out, x)
		}
	}
	return out, nil
}
