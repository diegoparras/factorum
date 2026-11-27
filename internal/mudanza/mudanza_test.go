package mudanza_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/mudanza"
	"github.com/diegoparras/factorum/internal/pruebas"
	"github.com/diegoparras/factorum/internal/store"
)

type entorno struct {
	st     *store.Store
	disco  *almacen.Disco
	s3     *almacen.S3
	falso  *pruebas.S3
	alm    *almacen.Almacen
	claves []string
	datos  map[string][]byte
	audit  *atomic.Value
}

const prefijo = "fct/"

func armar(t *testing.T, n int) *entorno {
	t.Helper()
	url := pruebas.BaseNueva(t)
	ctx := context.Background()
	st, err := store.Abrir(ctx, url, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Cerrar)
	falso := pruebas.S3Falso(t)
	s3, err := almacen.NuevoS3(falso.URL, falso.Bucket, falso.Access, falso.Secret, "auto", prefijo)
	if err != nil {
		t.Fatal(err)
	}
	disco := &almacen.Disco{Dir: t.TempDir()}
	alm, err := almacen.Nuevo(disco, bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	e := &entorno{st: st, disco: disco, s3: s3, falso: falso, alm: alm, datos: map[string][]byte{}, audit: &atomic.Value{}}
	// Lo de antes de R2: originales y resultados en el disco, sellados como siempre.
	for i := range n {
		clave := fmt.Sprintf("originales/%064x", i*7919+1)
		if i%4 == 3 {
			clave = fmt.Sprintf("resultados/%064x.json", i)
		}
		d := bytes.Repeat([]byte(fmt.Sprintf("factura %d ", i)), 50+i%40)
		if _, err := alm.Guardar(ctx, clave, d); err != nil {
			t.Fatal(err)
		}
		e.claves = append(e.claves, clave)
		e.datos[clave] = d
	}
	// R2 activo: lo nuevo va a R2 y lo viejo se lee del disco.
	alm.CambiarBackend(s3, disco)
	return e
}

func (e *entorno) servicio() *mudanza.Servicio {
	return &mudanza.Servicio{Pool: e.st.Pool, Disco: e.disco, R2: func() *almacen.S3 { return e.s3 },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Concurrencia: 4, Reintentos: 2, Espera: 5 * time.Millisecond, Intervalo: 20 * time.Millisecond,
		Auditar: func(_ context.Context, accion string, _ map[string]any) { e.audit.Store(accion) }}
}

// todoSeLee: cada objeto se abre igual que antes, esté donde esté.
func (e *entorno) todoSeLee(t *testing.T, cuando string) {
	t.Helper()
	for _, k := range e.claves {
		b, err := e.alm.Leer(context.Background(), k)
		if err != nil {
			t.Fatalf("%s: no se puede leer %s: %v", cuando, k, err)
		}
		if !bytes.Equal(b, e.datos[k]) {
			t.Fatalf("%s: %s no es igual", cuando, k)
		}
	}
}

func estado(t *testing.T, s *mudanza.Servicio) *mudanza.Estado {
	t.Helper()
	e, err := s.Estado(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPaseCortadoYReanudado(t *testing.T) {
	e := armar(t, 200)
	ctx := context.Background()
	s := e.servicio()
	if err := s.Liberar(ctx, "liberar"); err == nil {
		t.Fatal("no se puede liberar antes de pasar")
	}
	if err := s.Iniciar(ctx, "diego@ejemplo.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.Iniciar(ctx, "diego@ejemplo.com"); !errors.Is(err, mudanza.ErrNoSePuede) {
		t.Fatalf("iniciar dos veces: %v", err)
	}

	// Se corta a la mitad (como un apagón: el contexto se cancela sin pausar).
	cctx, cortar := context.WithCancel(ctx)
	err := s.Correr(cctx, func(est *mudanza.Estado) {
		if est.Hechos.Objetos >= 100 {
			cortar()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cortado: %v", err)
	}
	mitad := estado(t, s)
	if mitad.Estado != mudanza.Corriendo || mitad.Total.Objetos != 200 || mitad.Hechos.Objetos < 100 || mitad.Hechos.Objetos == 200 {
		t.Fatalf("a la mitad: %+v", mitad)
	}
	if mitad.PuedeLiberar {
		t.Fatal("a la mitad no se puede liberar")
	}
	e.todoSeLee(t, "a la mitad")

	// "Reinicio": otro servicio (otro proceso) sigue desde donde quedó.
	s2 := e.servicio()
	var avances int
	if err := s2.Correr(ctx, func(*mudanza.Estado) { avances++ }); err != nil {
		t.Fatal(err)
	}
	fin := estado(t, s2)
	if fin.Estado != mudanza.Terminada || fin.Hechos.Objetos != 200 || fin.Errores.Objetos != 0 || fin.Pendientes.Objetos != 0 {
		t.Fatalf("al terminar: %+v", fin)
	}
	if !fin.PuedeLiberar || fin.ParaLiberar.Objetos != 200 {
		t.Fatalf("tiene que poder liberar: %+v", fin)
	}
	if e.audit.Load() != "r2.migrar_fin" {
		t.Fatalf("auditoría del fin: %v", e.audit.Load())
	}
	// Lo verificado no se vuelve a subir: cada objeto se subió una vez (los que se cortaron en el
	// medio de la subida se encontraron en R2 y solo se verificaron).
	for k, n := range e.falso.Subidas() {
		if n != 1 {
			t.Fatalf("%s se subió %d veces", k, n)
		}
	}
	if len(e.falso.Subidas()) != 200 {
		t.Fatalf("subidos: %d", len(e.falso.Subidas()))
	}
	// Están tal cual: el mismo objeto sellado del disco, sin volver a cifrar.
	for _, k := range e.claves[:5] {
		enDisco, _ := e.disco.Traer(ctx, k)
		enR2, _ := e.s3.Traer(ctx, k)
		if !bytes.Equal(enDisco, enR2) {
			t.Fatalf("%s: en R2 no es el mismo objeto que en el disco", k)
		}
	}
	e.todoSeLee(t, "al terminar")

	// Volver a correrlo no sube nada (idempotente).
	if err := s2.Iniciar(ctx, "diego@ejemplo.com"); err != nil {
		t.Fatal(err)
	}
	if err := s2.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for k, n := range e.falso.Subidas() {
		if n != 1 {
			t.Fatalf("otra vuelta: %s se subió %d veces", k, n)
		}
	}

	// Liberar: hay que escribir «liberar».
	if err := s2.Liberar(ctx, "si"); !errors.Is(err, mudanza.ErrNoSePuede) {
		t.Fatalf("sin la confirmación: %v", err)
	}
	// Uno se rompió en R2 (más corto): ese no se borra del disco.
	roto := e.claves[7]
	e.falso.Romper(t, prefijo+roto, true)
	if err := s2.Liberar(ctx, "liberar"); err != nil {
		t.Fatal(err)
	}
	if err := s2.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	lib := estado(t, s2)
	if lib.Estado != mudanza.Liberada || lib.Liberados.Objetos != 199 || lib.Pendientes.Objetos != 1 {
		t.Fatalf("liberado: %+v", lib)
	}
	if !strings.Contains(lib.Mensaje, "1 no estaban bien") {
		t.Fatalf("mensaje: %s", lib.Mensaje)
	}
	var quedan []string
	_ = e.disco.Recorrer(func(k string, _ int64) error { quedan = append(quedan, k); return nil })
	if len(quedan) != 1 || quedan[0] != roto {
		t.Fatalf("en el disco quedó: %v", quedan)
	}
	// El roto se vuelve a pasar con Seguir (se detecta que no coincide y se sube de nuevo).
	if err := s2.Seguir(ctx, "diego@ejemplo.com"); err != nil {
		t.Fatal(err)
	}
	if err := s2.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if fin := estado(t, s2); fin.Estado != mudanza.Terminada || fin.Hechos.Objetos != 200 || !fin.PuedeLiberar || fin.ParaLiberar.Objetos != 1 {
		t.Fatalf("después de seguir: %+v", fin)
	}
	// Las páginas y los originales se siguen viendo: ahora casi todo sale de R2.
	e.todoSeLee(t, "después de liberar")
}

func TestPausarYSeguir(t *testing.T) {
	e := armar(t, 120)
	ctx := context.Background()
	s := e.servicio()
	s.Concurrencia = 1
	if err := s.Iniciar(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	listo := make(chan error, 1)
	var pausado atomic.Bool
	go func() {
		listo <- s.Correr(ctx, func(est *mudanza.Estado) {
			if est.Hechos.Objetos >= 20 && pausado.CompareAndSwap(false, true) {
				if err := s.Pausar(ctx); err != nil {
					t.Error(err)
				}
			}
		})
	}()
	select {
	case err := <-listo:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pausar no frenó el pase")
	}
	p := estado(t, s)
	if p.Estado != mudanza.Pausada || p.Hechos.Objetos >= 120 || p.Trabajando {
		t.Fatalf("pausado: %+v", p)
	}
	// Mientras está pausado, Correr no hace nada.
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if estado(t, s).Hechos.Objetos != p.Hechos.Objetos {
		t.Fatal("pausado no tiene que avanzar")
	}
	if err := s.Pausar(ctx); !errors.Is(err, mudanza.ErrNoSePuede) {
		t.Fatalf("pausar lo pausado: %v", err)
	}
	if err := s.Seguir(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f := estado(t, s); f.Estado != mudanza.Terminada || f.Hechos.Objetos != 120 {
		t.Fatalf("seguido: %+v", f)
	}
}

func TestObjetoCorruptoSeDetecta(t *testing.T) {
	e := armar(t, 30)
	ctx := context.Background()
	s := e.servicio()
	// R2 guarda mal un objeto (siempre): no se puede verificar y queda con error.
	malo := e.claves[3]
	e.falso.Corromper(prefijo + malo)
	if err := s.Iniciar(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	f := estado(t, s)
	if f.Estado != mudanza.Terminada || f.Errores.Objetos != 1 || f.Hechos.Objetos != 29 {
		t.Fatalf("con uno corrupto: %+v", f)
	}
	if len(f.UltErrores) != 1 || f.UltErrores[0].Clave != malo || !strings.Contains(f.UltErrores[0].Error, "no coincide") {
		t.Fatalf("errores: %+v", f.UltErrores)
	}
	if n := e.falso.Subidas()[prefijo+malo]; n != 2 {
		t.Fatalf("se reintentó %d veces, tenían que ser 2", n)
	}
	// Uno que en R2 cambió sin cambiar de tamaño, pero que figuraba a medias: se detecta por el
	// SHA-256 y se vuelve a subir.
	otro := e.claves[10]
	e.falso.Romper(t, prefijo+otro, false)
	if _, err := e.st.Pool.Exec(ctx, `UPDATE r2_objetos SET estado='pendiente' WHERE clave=$1`, otro); err != nil {
		t.Fatal(err)
	}
	if err := s.Seguir(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if n := e.falso.Subidas()[prefijo+otro]; n != 2 {
		t.Fatalf("el cambiado se subió %d veces", n)
	}
	enDisco, _ := e.disco.Traer(ctx, otro)
	enR2, _ := e.s3.Traer(ctx, otro)
	if !bytes.Equal(enDisco, enR2) {
		t.Fatal("el cambiado tiene que quedar bien en R2")
	}
	// El corrupto no se libera: sigue en el disco y se sigue leyendo.
	if err := s.Liberar(ctx, "liberar"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.disco.Existe(ctx, malo); !ok {
		t.Fatal("el que no se verificó no se puede borrar del disco")
	}
	if ok, _ := e.disco.Existe(ctx, otro); ok {
		t.Fatal("el verificado se tenía que liberar")
	}
	e.todoSeLee(t, "con uno corrupto en R2")
}

func TestSinR2YDestinoCambiado(t *testing.T) {
	e := armar(t, 5)
	ctx := context.Background()
	s := e.servicio()
	var r2 atomic.Pointer[almacen.S3]
	s.R2 = func() *almacen.S3 { return r2.Load() }
	if err := s.Iniciar(ctx, "diego"); !errors.Is(err, mudanza.ErrSinR2) {
		t.Fatalf("sin R2: %v", err)
	}
	r2.Store(e.s3)
	if err := s.Iniciar(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	// Se apagó R2 antes de que corra: queda con error, sin romper nada.
	r2.Store(nil)
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f := estado(t, s); f.Estado != mudanza.ConError || !strings.Contains(f.Mensaje, "R2 dejó de estar activo") {
		t.Fatalf("R2 apagado: %+v", f)
	}
	// Otro bucket: lo verificado era en el otro, se empieza de cero.
	r2.Store(e.s3)
	if err := s.Seguir(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	otro, _ := almacen.NuevoS3(e.falso.URL, e.falso.Bucket, e.falso.Access, e.falso.Secret, "auto", "otro/")
	r2.Store(otro)
	if err := s.Iniciar(ctx, "diego"); err != nil {
		t.Fatal(err)
	}
	if f := estado(t, s); f.Hechos.Objetos != 0 || f.Pendientes.Objetos != 5 {
		t.Fatalf("otro destino: %+v", f)
	}
	if err := s.Correr(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f := estado(t, s); f.Hechos.Objetos != 5 || f.Destino != otro.Destino() {
		t.Fatalf("otro destino, terminado: %+v", f)
	}
}
