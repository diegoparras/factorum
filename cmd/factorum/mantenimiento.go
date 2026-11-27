package main

// Lo que se hace por consola sobre el almacenamiento y la base: pasar lo del disco a R2, hacer una
// copia y restaurarla.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/canal/whatsapp"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/mudanza"
	"github.com/diegoparras/factorum/internal/store"
)

// auditarSistema anota en la auditoría lo que hace Factorum solo (o la consola).
func auditarSistema(st *store.Store, actor string, log *slog.Logger) func(ctx context.Context, accion, objeto, quien string, d map[string]any) {
	return func(ctx context.Context, accion, objeto, quien string, d map[string]any) {
		if quien == "" {
			quien = actor
		}
		if err := st.Auditar(ctx, store.Evento{Actor: quien, ActorTipo: "sistema", Accion: accion, Objeto: objeto, Detalle: d}); err != nil {
			log.Error("no se pudo anotar en la auditoría", "accion", accion, "error", err)
		}
	}
}

func servicioMudanza(st *store.Store, disco *almacen.Disco, r2 func() *almacen.S3, actor string, log *slog.Logger) *mudanza.Servicio {
	aud := auditarSistema(st, actor, log)
	return &mudanza.Servicio{Pool: st.Pool, Disco: disco, R2: r2, Log: log.With("componente", "r2"), Tramo: 30 * time.Minute,
		Auditar: func(ctx context.Context, accion string, d map[string]any) { aud(ctx, accion, "", "", d) }}
}

func servicioCopias(st *store.Store, alm *almacen.Almacen, cfg func() *config.Config, dir string, actor string, log *slog.Logger) *copias.Servicio {
	return &copias.Servicio{Pool: st.Pool, Sello: alm, Config: cfg, R2: alm.R2, Dir: filepath.Join(dir, copias.Carpeta),
		Log: log.With("componente", "copias"), Auditar: auditarSistema(st, actor, log)}
}

// consola es lo que necesitan los comandos: la base, la configuración (la guardada desde la
// pantalla también, para tener R2) y el almacén.
type consola struct {
	cfg   *config.Config
	st    *store.Store
	disco *almacen.Disco
	alm   *almacen.Almacen
	s3    *almacen.S3
	log   *slog.Logger
}

func abrirConsola(ctx context.Context) (*consola, error) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	base, err := config.Leer()
	if err != nil {
		return nil, err
	}
	if base.DatabaseURL == "" {
		return nil, errors.New("falta FACTORUM_DATABASE_URL")
	}
	if base.ClaveMaestra == nil {
		return nil, errors.New("falta FACTORUM_MASTER_KEY: es la que cifra los archivos y las copias")
	}
	st, err := store.Abrir(ctx, base.DatabaseURL, 8)
	if err != nil {
		return nil, err
	}
	aj, err := ajustes.Nuevo(st, base, log)
	if err != nil {
		st.Cerrar()
		return nil, err
	}
	if err := aj.Cargar(ctx); err != nil {
		st.Cerrar()
		return nil, err
	}
	c := aj.Actual()
	k := &consola{cfg: c, st: st, log: log, disco: &almacen.Disco{Dir: filepath.Join(c.DirDatos, "almacen")}}
	if k.alm, err = almacen.Nuevo(k.disco, c.ClaveMaestra); err != nil {
		st.Cerrar()
		return nil, err
	}
	if c.R2Endpoint != "" {
		if k.s3, err = armarS3(c); err != nil {
			st.Cerrar()
			return nil, fmt.Errorf("R2: %w", err)
		}
		k.alm.CambiarBackend(k.s3, k.disco)
	}
	return k, nil
}

func tamano(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(b)/1024)
	case b < 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f MB", float64(b)/(1<<20)), ".", ",", 1)
	}
	return strings.Replace(fmt.Sprintf("%.2f GB", float64(b)/(1<<30)), ".", ",", 1)
}

func duracion(seg int64) string {
	d := time.Duration(seg) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", seg)
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
	}
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

// lineaPase es el avance en una línea.
func lineaPase(e *mudanza.Estado) string {
	if e.Estado == mudanza.Liberada {
		return fmt.Sprintf("Disco liberado: %d objetos (%s) están solo en R2.", e.Liberados.Objetos, tamano(e.Liberados.Bytes))
	}
	if e.Estado == mudanza.Liberando {
		return fmt.Sprintf("Liberando el disco: %d de %d objetos borrados del disco (%s), %d por borrar.",
			e.Liberados.Objetos, e.Liberados.Objetos+e.ParaLiberar.Objetos, tamano(e.Liberados.Bytes), e.ParaLiberar.Objetos)
	}
	s := fmt.Sprintf("Pasando a R2: %d de %d objetos (%s de %s), %d con error", e.Hechos.Objetos, e.Total.Objetos,
		tamano(e.Hechos.Bytes), tamano(e.Total.Bytes), e.Errores.Objetos)
	if e.EstimadoSeg != nil {
		s += ", faltan unos " + duracion(*e.EstimadoSeg)
	}
	return s + "."
}

func usoR2() error {
	return errors.New(`uso:
  factorum r2 migrar            pasa a R2 lo guardado en el disco (Ctrl+C pausa; volvé a correrlo para seguir)
  factorum r2 migrar --liberar  borra del disco lo que ya está verificado en R2
  factorum r2 estado            cómo va`)
}

func comandoR2(args []string) error {
	if len(args) == 0 {
		return usoR2()
	}
	fsf := flag.NewFlagSet("r2 "+args[0], flag.ContinueOnError)
	liberar := fsf.Bool("liberar", false, "borrar del disco lo que ya está verificado en R2")
	if err := fsf.Parse(args[1:]); err != nil {
		return err
	}
	if args[0] != "migrar" && args[0] != "estado" {
		return usoR2()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	k, err := abrirConsola(ctx)
	if err != nil {
		return err
	}
	defer k.st.Cerrar()
	svc := servicioMudanza(k.st, k.disco, func() *almacen.S3 { return k.s3 }, "consola", k.log)
	e, err := svc.Estado(ctx)
	if err != nil {
		return err
	}
	if args[0] == "estado" {
		fmt.Println(resumenPase(e))
		return nil
	}
	if k.s3 == nil {
		return errors.New("R2 no está configurado (ni en Configuración ni con FACTORUM_R2_*)")
	}
	aud := auditarSistema(k.st, "consola", k.log)
	switch {
	case *liberar && e.Estado == mudanza.Liberando:
		fmt.Println("Se sigue liberando el disco.")
	case *liberar:
		if err := svc.Liberar(ctx, mudanza.ConfirmacionLiberar); err != nil {
			return errors.New(strings.TrimPrefix(err.Error(), mudanza.ErrNoSePuede.Error()+": "))
		}
		aud(ctx, "r2.liberar_inicio", k.s3.Destino(), "", map[string]any{"para_liberar": e.ParaLiberar.Objetos})
		fmt.Printf("Se borran del disco %d objetos (%s) que están verificados en R2.\n", e.ParaLiberar.Objetos, tamano(e.ParaLiberar.Bytes))
	case e.Estado == mudanza.Corriendo:
		fmt.Println("El pase había quedado a medias: se sigue desde donde quedó.")
	case e.Estado == mudanza.Liberando:
		return errors.New("se está liberando el disco: seguí con factorum r2 migrar --liberar")
	case e.Estado == "":
		if err := svc.Iniciar(ctx, "consola"); err != nil {
			return err
		}
		aud(ctx, "r2.migrar_inicio", k.s3.Destino(), "", nil)
		fmt.Println("Empieza el pase de lo del disco a R2. Ctrl+C lo pausa; volvé a correr este comando para seguir.")
	default:
		if err := svc.Seguir(ctx, "consola"); err != nil {
			return errors.New(strings.TrimPrefix(err.Error(), mudanza.ErrNoSePuede.Error()+": "))
		}
		aud(ctx, "r2.migrar_seguir", k.s3.Destino(), "", nil)
		fmt.Println("Se sigue con el pase a R2 (lo verificado no se vuelve a subir).")
	}

	listo := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-listo:
				return
			case <-t.C:
				if e, err := svc.Estado(context.WithoutCancel(ctx)); err == nil {
					fmt.Println(lineaPase(e))
				}
			}
		}
	}()
	err = svc.Correr(ctx, nil)
	close(listo)
	bg := context.WithoutCancel(ctx)
	switch {
	case errors.Is(err, mudanza.ErrOcupado):
		return errors.New("el servidor ya lo está haciendo: mirá el avance con «factorum r2 estado» o en Configuración, Almacenamiento R2")
	case ctx.Err() != nil:
		if perr := svc.Pausar(bg); perr == nil {
			aud(bg, "r2.migrar_pausa", k.s3.Destino(), "", nil)
			fmt.Println("Pausado. Para seguir: factorum r2 migrar")
		}
		return nil
	case err != nil:
		return err
	}
	e, err = svc.Estado(bg)
	if err != nil {
		return err
	}
	fmt.Println(resumenPase(e))
	if e.Estado == mudanza.Pausada {
		fmt.Println("Lo pausaron desde la pantalla. Para seguir: factorum r2 migrar")
	}
	return nil
}

func resumenPase(e *mudanza.Estado) string {
	if e.Estado == "" {
		return "Todavía no se pasó nada a R2. Para empezar: factorum r2 migrar"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Estado: %s\n%s", e.Estado, lineaPase(e))
	if e.Mensaje != "" {
		b.WriteString("\n" + e.Mensaje)
	}
	for _, x := range e.UltErrores {
		fmt.Fprintf(&b, "\n  %s: %s", x.Clave, x.Error)
	}
	if e.PuedeLiberar {
		fmt.Fprintf(&b, "\nSe pueden liberar %d objetos del disco (%s): factorum r2 migrar --liberar", e.ParaLiberar.Objetos, tamano(e.ParaLiberar.Bytes))
	}
	return b.String()
}

// ---------- Copias ----------

func comandoCopia(args []string) error {
	fsf := flag.NewFlagSet("copia", flag.ContinueOnError)
	if err := fsf.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	k, err := abrirConsola(ctx)
	if err != nil {
		return err
	}
	defer k.st.Cerrar()
	svc := servicioCopias(k.st, k.alm, func() *config.Config { return k.cfg }, k.cfg.DirDatos, "consola", k.log)
	c, err := svc.Hacer(ctx, "consola", true, "")
	if err != nil {
		return err
	}
	donde := filepath.Join(svc.Dir, filepath.Base(c.Clave))
	if c.Destino == copias.DestinoR2 {
		donde = "R2: " + c.Clave
	}
	fmt.Printf("Copia hecha: %s (%s, %d filas). Está en %s\n", filepath.Base(c.Clave), tamano(*c.Tamano), *c.Filas, donde)
	return nil
}

func comandoRestaurar(args []string) error {
	fsf := flag.NewFlagSet("restaurar", flag.ContinueOnError)
	confirmar := fsf.Bool("confirmar", false, "restaurar de verdad (sin esto solo se verifica la copia)")
	sinPrevia := fsf.Bool("sin-copia-previa", false, "no hacer una copia de lo que hay en la base antes de restaurar")
	// Las opciones pueden ir antes o después del archivo.
	var pos []string
	resto := args
	for len(resto) > 0 {
		if err := fsf.Parse(resto); err != nil {
			return err
		}
		resto = fsf.Args()
		if len(resto) > 0 {
			pos = append(pos, resto[0])
			resto = resto[1:]
		}
	}
	if len(pos) != 1 {
		return errors.New(`uso: factorum restaurar <archivo.copia o copias/factorum-AAAAMMDD-HHMMSS.copia en R2> [--confirmar]

  Sin --confirmar, solo verifica la copia y muestra qué tiene.
  Con --confirmar, hace una copia de lo que hay ahora (salvo con --sin-copia-previa), borra los datos
  de la base y carga los de la copia, todo en una transacción. Después, reiniciá Factorum.`)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	k, err := abrirConsola(ctx)
	if err != nil {
		return err
	}
	defer k.st.Cerrar()
	pool := k.st.Pool
	// La cola tiene que existir (en una base vacía la crea el servidor al arrancar).
	mig, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	if _, err := mig.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("preparando la cola: %w", err)
	}

	origen := pos[0]
	ruta := origen
	if _, err := os.Stat(origen); err != nil {
		// No es un archivo: es una clave en R2.
		if k.s3 == nil {
			return fmt.Errorf("no existe el archivo %s (y R2 no está configurado para buscarlo ahí)", origen)
		}
		clave := strings.TrimPrefix(origen, "/")
		if !strings.Contains(clave, "/") {
			clave = copias.Carpeta + "/" + clave
		}
		r, n, err := k.s3.Abrir(ctx, clave)
		if err != nil {
			return fmt.Errorf("no se pudo bajar %s de R2: %w", clave, err)
		}
		f, err := os.CreateTemp("", "factorum-restaurar-*"+copias.Extension)
		if err != nil {
			r.Close()
			return err
		}
		defer os.Remove(f.Name())
		fmt.Printf("Bajando %s de R2 (%s)...\n", clave, tamano(n))
		_, err = io.Copy(f, r)
		r.Close()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("bajando de R2: %w", err)
		}
		ruta = f.Name()
	}
	abrir := func() (io.ReadCloser, error) { return os.Open(ruta) }

	var filasAntes int64
	opc := copias.OpcionesRestaurar{
		Confirmar:   *confirmar,
		Migraciones: store.Migraciones(),
		PrepararTablas: func(ctx context.Context, faltan []string) error {
			for _, t := range faltan {
				if strings.HasPrefix(t, "whatsmeow_") {
					return whatsapp.PrepararTablas(ctx, stdlib.OpenDBFromPool(pool))
				}
			}
			return nil
		},
		Antes: func(ctx context.Context, m *copias.Manifiesto) error {
			for _, t := range []string{"lotes", "usuarios", "configuracion"} {
				var n int64
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n); err == nil {
					filasAntes += n
				}
			}
			if filasAntes == 0 || *sinPrevia {
				return nil
			}
			fmt.Println("La base tiene datos: primero se hace una copia de lo que hay (por las dudas).")
			svc := servicioCopias(k.st, k.alm, func() *config.Config { return k.cfg }, k.cfg.DirDatos, "consola", k.log)
			c, err := svc.Hacer(ctx, "antes de restaurar", true, "")
			if err != nil {
				return fmt.Errorf("no se pudo hacer la copia previa (usá --sin-copia-previa si no la querés): %w", err)
			}
			fmt.Printf("Copia previa: %s (%s, en %s).\n", filepath.Base(c.Clave), tamano(*c.Tamano), c.Destino)
			return nil
		},
		Despues: func(ctx context.Context, tx pgx.Tx) (string, error) {
			// Lo que quedó a medio procesar en la copia vuelve a la cola (lo que estaba en la papelera
			// sigue ahí: se vuelve a encolar si alguien lo restaura).
			rows, err := tx.Query(ctx, `UPDATE documentos SET estado='pendiente' WHERE estado IN ('pendiente','procesando') AND borrado_en IS NULL RETURNING id`)
			if err != nil {
				return "", err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return "", err
			}
			if len(ids) > 0 {
				cli, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
				if err != nil {
					return "", err
				}
				var jobs []river.InsertManyParams
				for _, id := range ids {
					jobs = append(jobs, river.InsertManyParams{Args: app.ProcesarArgs{DocumentoID: id}})
				}
				if _, err := cli.InsertManyTx(ctx, tx, jobs); err != nil {
					return "", err
				}
			}
			_, err = tx.Exec(ctx, `INSERT INTO auditoria (actor, actor_tipo, accion, objeto, detalle) VALUES ('consola', 'sistema', 'copia.restaurar', $1, $2)`,
				filepath.Base(origen), map[string]any{"version": config.Version})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d documentos que habían quedado a medio procesar vuelven a la cola.", len(ids)), nil
		},
	}
	inf, err := copias.Restaurar(ctx, pool, abrir, k.alm, opc)
	if inf != nil && inf.Manifiesto != nil {
		m := inf.Manifiesto
		fmt.Printf("Copia de Factorum %s del %s: %d tablas, %d filas. Esquema: %s.\n", m.Factorum,
			m.Creada.In(copias.Zona).Format("02/01/2006 15:04"), len(m.Tablas), m.Filas, strings.Join(m.Esquema.Migraciones, ", "))
	}
	if err != nil {
		return err
	}
	for _, a := range inf.Avisos {
		fmt.Println("Ojo:", a)
	}
	if !inf.Restaurada {
		fmt.Println("La copia está bien: se pudo descifrar y cada tabla coincide con el manifiesto.")
		fmt.Println("Para restaurarla, agregá --confirmar: se borran los datos que hay ahora en la base y se cargan los de la copia.")
		return nil
	}
	if inf.Despues != "" {
		fmt.Println(inf.Despues)
	}
	fmt.Println("Listo: la base quedó como en la copia. Reiniciá Factorum para que tome la configuración restaurada.")
	return nil
}
