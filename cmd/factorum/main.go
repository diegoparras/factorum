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

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/api"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/mcpserver"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/pdfx"
	"github.com/diegoparras/factorum/internal/pipeline"
	"github.com/diegoparras/factorum/internal/store"
	"github.com/diegoparras/factorum/web"
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
	case "salud":
		err = salud()
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

// salud consulta /healthz del servidor local (para el HEALTHCHECK de Docker, sin curl ni wget).
func salud() error {
	puerto := os.Getenv("FACTORUM_PUERTO")
	if puerto == "" {
		puerto = "8095"
	}
	cli := &http.Client{Timeout: 4 * time.Second}
	r, err := cli.Get("http://127.0.0.1:" + puerto + "/healthz")
	if err != nil {
		return err
	}
	defer r.Body.Close()
	var b struct {
		OK       bool `json:"ok"`
		Base     bool `json:"base"`
		Arranque bool `json:"arranque"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || !b.OK || !b.Base {
		return errors.New("Factorum no está sano (sin base de datos)")
	}
	if b.Arranque {
		dir := os.Getenv("FACTORUM_DATOS")
		if dir == "" {
			dir = "/data"
		}
		cod, err := os.ReadFile(filepath.Join(dir, api.ArchivoArranque))
		if err == nil {
			fmt.Printf("Falta crear el primer superadmin. Entrá a Factorum y usá este código de un solo uso: %s\n", strings.TrimSpace(string(cod)))
		} else {
			fmt.Println("Falta crear el primer superadmin: el código de un solo uso está en el log del servidor.")
		}
	}
	return nil
}

func uso() {
	fmt.Fprint(os.Stderr, `Factorum `+config.Version+`

  factorum serve
      Levanta la API, la UI, MCP, los canales y la cola. Necesita FACTORUM_DATABASE_URL y
      FACTORUM_MASTER_KEY; lo demás se configura desde la pantalla (o por variables de entorno, que
      ganan). Sin usuarios, escribe en el log el código para crear el primer superadmin.

  factorum salud
      Consulta el servidor local (para el HEALTHCHECK) y, si falta el primer superadmin, muestra el
      código de arranque.

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
	return pipeline.Nuevo(motor, ocr, armarVision(c, log), c.ConcOCR, c.ConcIA)
}

func armarVision(c *config.Config, log *slog.Logger) *extractor.Vision {
	if !c.IAConfigurada() {
		return nil
	}
	vis := &extractor.Vision{BaseURL: c.LLMBaseURL, APIKey: c.LLMAPIKey, Modelo: c.LLMModelo, Timeout: c.LLMTimeout}
	if !vis.Local() {
		log.Warn("el modelo de visión no parece local: los comprobantes que necesiten IA van a salir de este servidor", "url", c.LLMBaseURL)
	}
	return vis
}

func armarArcanum(c *config.Config) *arcanum.Cliente {
	if !c.ArcanumConfigurado() {
		return nil
	}
	return arcanum.Nuevo(c.ArcanumURL, c.ArcanumAPIKey, c.ArcanumConsultante, c.ArcanumTimeout)
}

func firmaArcanum(c *config.Config) string {
	return strings.Join([]string{c.ArcanumURL, c.ArcanumAPIKey, c.ArcanumConsultante, c.ArcanumTimeout.String()}, "|")
}

func firmaIA(c *config.Config) string {
	return strings.Join([]string{c.LLMBaseURL, c.LLMAPIKey, c.LLMModelo, c.LLMTimeout.String()}, "|")
}

func firmaR2(c *config.Config) string {
	return strings.Join([]string{c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey, c.R2Region, c.R2Prefijo}, "|")
}

func armarS3(c *config.Config) (*almacen.S3, error) {
	return almacen.NuevoS3(c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey, c.R2Region, c.R2Prefijo)
}

func servir() error {
	log := logger()
	slog.SetDefault(log)
	base, err := config.Leer()
	if err != nil {
		return err
	}
	if base.DatabaseURL == "" {
		return errors.New("falta FACTORUM_DATABASE_URL (postgres://usuario:clave@host:5432/factorum)")
	}
	if base.ClaveMaestra == nil {
		return errors.New("falta FACTORUM_MASTER_KEY (openssl rand -hex 32): cifra los archivos y la configuración secreta. Guardala fuera del servidor")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Abrir(ctx, base.DatabaseURL, base.Workers*2+10)
	if err != nil {
		return err
	}
	defer st.Cerrar()

	// Lo configurado desde la pantalla pisa al entorno, salvo las variables que están puestas.
	aj, err := ajustes.Nuevo(st, base, log)
	if err != nil {
		return err
	}
	if err := aj.Cargar(ctx); err != nil {
		return fmt.Errorf("leyendo la configuración guardada: %w", err)
	}
	c := aj.Actual()

	disco := &almacen.Disco{Dir: filepath.Join(c.DirDatos, "almacen")}
	alm, err := almacen.Nuevo(disco, c.ClaveMaestra)
	if err != nil {
		return err
	}
	var s3 *almacen.S3
	if c.R2Endpoint != "" {
		if s3, err = armarS3(c); err != nil {
			return fmt.Errorf("R2: %w", err)
		}
		alm.CambiarBackend(s3, disco)
	}

	a := &app.App{Cfg: c, Store: st, Almacen: alm, Pipeline: armarPipeline(c, log), Log: log}
	a.SetArcanum(armarArcanum(c))
	if err := a.IniciarCola(ctx); err != nil {
		return err
	}
	canales := nuevoGestor(ctx, a, st, log)
	canales.Aplicar(c)

	srv := &api.Server{App: a, Cfg: base, Log: log, Webhook: canales.Webhook, Ajustes: aj}
	if sub, err := fs.Sub(web.Archivos, "."); err == nil {
		srv.Web = sub
	}
	srv.MCP = mcpserver.Handler(a, srv.IdentificarClave)
	if err := srv.PrepararArranque(ctx); err != nil {
		return err
	}

	// Recarga en caliente: cada componente se rearma solo si cambió lo suyo. Los avisos llegan de a
	// uno (el servicio de ajustes los serializa).
	aj.Suscribir(func(viejo, nuevo *config.Config) {
		a.SetConfig(nuevo)
		if firmaArcanum(viejo) != firmaArcanum(nuevo) {
			a.SetArcanum(armarArcanum(nuevo))
			log.Info("Arcanum reconfigurado", "conectado", a.Arcanum() != nil)
		}
		if firmaIA(viejo) != firmaIA(nuevo) {
			a.Pipeline.SetVision(armarVision(nuevo, log))
			log.Info("modelo de visión reconfigurado", "habilitado", a.Pipeline.Vision() != nil)
		}
		if firmaR2(viejo) != firmaR2(nuevo) {
			if nuevo.R2Endpoint == "" {
				var resp almacen.Backend
				if s3 != nil {
					resp = s3 // lo que se guardó en R2 se sigue pudiendo leer
				}
				alm.CambiarBackend(disco, resp)
				s3 = nil
			} else if n, err := armarS3(nuevo); err != nil {
				log.Error("R2 mal configurado: se sigue guardando donde estaba", "error", err)
			} else {
				s3 = n
				alm.CambiarBackend(n, disco) // lo guardado en disco se sigue leyendo (la migración es aparte)
			}
			log.Info("almacén reconfigurado", "almacen", alm.Backend())
		}
		canales.Aplicar(nuevo)
	})

	// Sesiones y pases vencidos.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if err := st.Limpiar(ctx); err != nil && ctx.Err() == nil {
				log.Warn("no se pudieron limpiar las sesiones vencidas", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

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
		"ocr", a.Pipeline.OCR != nil, "ia", a.Pipeline.Vision() != nil, "arcanum", a.Arcanum() != nil, "almacen", alm.Backend())
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
