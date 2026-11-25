// Factorum: lee comprobantes de ARCA a escala, sin depender de un LLM.
//
//	factorum serve                 servidor (API, UI, MCP, canales, cola)
//	factorum procesar [opciones] archivos/carpetas/zips...   sin base, por consola
//	factorum version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/diegoparras/contadorfacturas/internal/almacen"
	"github.com/diegoparras/contadorfacturas/internal/api"
	"github.com/diegoparras/contadorfacturas/internal/app"
	"github.com/diegoparras/contadorfacturas/internal/arcanum"
	"github.com/diegoparras/contadorfacturas/internal/config"
	"github.com/diegoparras/contadorfacturas/internal/export"
	"github.com/diegoparras/contadorfacturas/internal/extractor"
	"github.com/diegoparras/contadorfacturas/internal/mcpserver"
	"github.com/diegoparras/contadorfacturas/internal/modelo"
	"github.com/diegoparras/contadorfacturas/internal/pdfx"
	"github.com/diegoparras/contadorfacturas/internal/pipeline"
	"github.com/diegoparras/contadorfacturas/internal/store"
	"github.com/diegoparras/contadorfacturas/web"
)

func main() {
	if len(os.Args) < 2 {
		uso()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve", "servir":
		err = servir()
	case "procesar":
		err = procesar(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("factorum", config.Version)
	default:
		uso()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func uso() {
	fmt.Fprint(os.Stderr, `Factorum `+config.Version+`

  factorum serve
      Levanta la API, la UI, MCP, los canales y la cola. Se configura por variables de entorno
      (ver README). Necesita FACTORUM_DATABASE_URL y FACTORUM_API_KEY.

  factorum procesar [-xlsx salida.xlsx] [-csv salida.csv] [-json] [-ia] [-workers N] archivos...
      Procesa PDFs, imágenes, ZIPs o carpetas sin base de datos y arma un Excel o CSV.

  factorum version
`)
}

func logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func armarPipeline(c *config.Config, log *slog.Logger) *pipeline.Pipeline {
	motor := pdfx.NuevoPoppler(60 * time.Second)
	if err := motor.Disponible(); err != nil {
		log.Warn("sin poppler: no se van a poder leer PDFs", "error", err)
	}
	ocr := extractor.NuevoTesseract(c.OCRIdiomas, 90*time.Second)
	if ocr == nil {
		log.Info("tesseract no está instalado: sin OCR para escaneados")
	}
	var vis *extractor.Vision
	if c.IAConfigurada() {
		vis = &extractor.Vision{BaseURL: c.LLMBaseURL, APIKey: c.LLMAPIKey, Modelo: c.LLMModelo, Timeout: c.LLMTimeout}
		if !vis.Local() {
			log.Warn("el modelo de visión no parece local: los comprobantes que necesiten IA van a salir de este servidor", "url", c.LLMBaseURL)
		}
	}
	return pipeline.Nuevo(motor, ocr, vis, c.ConcOCR, c.ConcIA)
}

func servir() error {
	log := logger()
	slog.SetDefault(log)
	c, err := config.Leer()
	if err != nil {
		return err
	}
	if c.DatabaseURL == "" {
		return errors.New("falta FACTORUM_DATABASE_URL (postgres://usuario:clave@host:5432/factorum)")
	}
	if c.APIKey == "" {
		return errors.New("falta FACTORUM_API_KEY (openssl rand -hex 24)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Abrir(ctx, c.DatabaseURL, c.Workers*2+10)
	if err != nil {
		return err
	}
	defer st.Cerrar()

	var backend almacen.Backend = &almacen.Disco{Dir: filepath.Join(c.DirDatos, "almacen")}
	if c.R2Endpoint != "" {
		s3, err := almacen.NuevoS3(c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey, c.R2Region, c.R2Prefijo)
		if err != nil {
			return fmt.Errorf("R2: %w", err)
		}
		backend = s3
	}
	alm, err := almacen.Nuevo(backend, c.ClaveMaestra)
	if err != nil {
		return err
	}
	if !alm.Cifrado() {
		log.Warn("sin FACTORUM_MASTER_KEY: los archivos se guardan comprimidos pero sin cifrar")
	}

	a := &app.App{Cfg: c, Store: st, Almacen: alm, Pipeline: armarPipeline(c, log), Log: log}
	if c.ArcanumConfigurado() {
		a.Arcanum = arcanum.Nuevo(c.ArcanumURL, c.ArcanumAPIKey, c.ArcanumConsultante, c.ArcanumTimeout)
	}
	if err := a.IniciarCola(ctx); err != nil {
		return err
	}
	webhook := iniciarCanales(ctx, a, st, log)

	srv := &api.Server{App: a, Cfg: c, Log: log, Webhook: webhook}
	if sub, err := fs.Sub(web.Archivos, "."); err == nil {
		srv.Web = sub
	}
	srv.MCP = mcpserver.Handler(a, func(r *http.Request) (bool, bool) {
		rol := srv.Rol(r)
		return rol != api.RolNinguno, rol == api.RolOperador
	})
	hs := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.Puerto),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
		_ = a.River.Stop(sctx)
	}()
	log.Info("Factorum escuchando", "version", config.Version, "puerto", c.Puerto, "workers", c.Workers,
		"ocr", a.Pipeline.OCR != nil, "ia", a.Pipeline.Vision != nil, "arcanum", a.Arcanum != nil, "almacen", alm.Backend())
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ---------- CLI ----------

func procesar(args []string) error {
	fsf := flag.NewFlagSet("procesar", flag.ContinueOnError)
	xlsx := fsf.String("xlsx", "", "archivo Excel de salida")
	csvf := fsf.String("csv", "", "archivo CSV de salida")
	js := fsf.Bool("json", false, "imprimir el resultado completo en JSON (una línea por archivo)")
	ia := fsf.Bool("ia", false, "usar el modelo de visión si hace falta (LLM_API_BASE_URL, LLM_MODEL)")
	workers := fsf.Int("workers", runtime.NumCPU(), "archivos en paralelo")
	if err := fsf.Parse(args); err != nil {
		return err
	}
	if fsf.NArg() == 0 {
		return errors.New("pasá al menos un archivo o carpeta")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c, err := config.Leer()
	if err != nil {
		return err
	}
	p := armarPipeline(c, log)

	var archivos []string
	for _, a := range fsf.Args() {
		_ = filepath.WalkDir(a, func(ruta string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				archivos = append(archivos, ruta)
			}
			return nil
		})
	}
	sort.Strings(archivos)
	type item struct {
		nombre string
		datos  []byte
	}
	var items []item
	for _, ruta := range archivos {
		b, err := os.ReadFile(ruta)
		if err != nil {
			fmt.Fprintln(os.Stderr, ruta+":", err)
			continue
		}
		ok, desc := app.Expandir([]app.Archivo{{Nombre: filepath.Base(ruta), Datos: b}}, c.MaxArchivo)
		for _, d := range desc {
			fmt.Fprintln(os.Stderr, "se omite:", d)
		}
		for _, f := range ok {
			items = append(items, item{f.Nombre, f.Datos})
		}
	}

	t0 := time.Now()
	res := make([]modelo.Resultado, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, *workers))
	for i, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, it item) {
			defer wg.Done()
			defer func() { <-sem }()
			res[i] = p.Procesar(context.Background(), it.nombre, it.datos, pipeline.Opciones{MaxPaginas: c.MaxPaginas, IA: *ia})
		}(i, it)
	}
	wg.Wait()
	dur := time.Since(t0)

	var filas []export.Fila
	n := map[modelo.Estado]int{}
	errores := 0
	for _, r := range res {
		if *js {
			b, _ := json.Marshal(r)
			fmt.Println(string(b))
		}
		if r.Error != "" {
			errores++
			fmt.Fprintf(os.Stderr, "%s: %s\n", r.Archivo, r.Error)
			continue
		}
		for _, rc := range r.Comprobantes {
			n[rc.Estado]++
			filas = append(filas, export.Fila{Archivo: r.Archivo, Estado: rc.Estado, Hallazgos: rc.Hallazgos, C: rc.Comprobante})
			if !*js && rc.Estado != modelo.EstadoOK {
				var msgs []string
				for _, h := range rc.Hallazgos {
					if h.Nivel != modelo.NivelInfo {
						msgs = append(msgs, h.Mensaje)
					}
				}
				fmt.Fprintf(os.Stderr, "%s [%s] %05d-%08d: %s\n", r.Archivo, rc.Estado, rc.Comprobante.PuntoVenta, rc.Comprobante.Numero, strings.Join(msgs, " | "))
			}
		}
	}
	if *xlsx != "" {
		f, err := os.Create(*xlsx)
		if err != nil {
			return err
		}
		if err := export.Excel(f, filas); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	if *csvf != "" {
		f, err := os.Create(*csvf)
		if err != nil {
			return err
		}
		if err := export.CSV(f, filas); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	fmt.Fprintf(os.Stderr, "%d archivos, %d comprobantes (%d bien, %d para revisar, %d con errores), %d archivos ilegibles, en %s\n",
		len(items), len(filas), n[modelo.EstadoOK], n[modelo.EstadoRevisar], n[modelo.EstadoError], errores, dur.Round(time.Millisecond))
	return nil
}
