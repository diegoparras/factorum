// Package pruebas tiene lo que comparten las pruebas de varios paquetes: una base de Postgres nueva
// por prueba (para no pisarse con las otras que usan FACTORUM_TEST_DB) y un S3 falso en memoria
// para probar R2 sin red. Solo lo importan los _test.go.
package pruebas

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// BaseNueva crea una base vacía a partir de FACTORUM_TEST_DB y la borra al terminar. Sin
// FACTORUM_TEST_DB, la prueba se saltea.
func BaseNueva(t testing.TB) string {
	t.Helper()
	base := os.Getenv("FACTORUM_TEST_DB")
	if base == "" {
		t.Skip("sin FACTORUM_TEST_DB")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	nombre := "factorum_p_" + hex.EncodeToString(b)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+nombre); err != nil {
		t.Fatalf("no se pudo crear la base de prueba (¿el usuario tiene CREATEDB?): %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+nombre+" WITH (FORCE)")
	})
	u.Path = "/" + nombre
	return u.String()
}

// S3 es un S3 falso en memoria.
type S3 struct {
	URL, Bucket, Access, Secret string
	srv                         *httptest.Server
	back                        *corruptor
}

// S3Falso levanta un S3 en memoria con el bucket "factorum".
func S3Falso(t testing.TB) *S3 {
	t.Helper()
	back := &corruptor{Backend: s3mem.New(), malas: map[string]bool{}}
	f := gofakes3.New(back)
	srv := httptest.NewServer(f.Server())
	t.Cleanup(srv.Close)
	if err := back.CreateBucket("factorum"); err != nil {
		t.Fatal(err)
	}
	return &S3{URL: srv.URL, Bucket: "factorum", Access: "prueba", Secret: "prueba-secreta", srv: srv, back: back}
}

// Corromper hace que lo que se suba con esa clave (con el prefijo) llegue con un byte cambiado.
func (s *S3) Corromper(clave string) {
	s.back.mu.Lock()
	s.back.malas[clave] = true
	s.back.mu.Unlock()
}

// Subidas dice cuántas veces se subió cada clave.
func (s *S3) Subidas() map[string]int {
	s.back.mu.Lock()
	defer s.back.mu.Unlock()
	m := make(map[string]int, len(s.back.subidas))
	for k, v := range s.back.subidas {
		m[k] = v
	}
	return m
}

// Romper cambia en el bucket el contenido de un objeto que ya está: con el mismo tamaño o, con
// achicar, uno más corto.
func (s *S3) Romper(t testing.TB, clave string, achicar bool) {
	t.Helper()
	o, err := s.back.GetObject(s.Bucket, clave, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(o.Contents)
	o.Contents.Close()
	b[len(b)/2] ^= 0xff
	if achicar {
		b = b[:len(b)-1]
	}
	if _, err := s.back.Backend.PutObject(s.Bucket, clave, map[string]string{}, bytesReader(b), int64(len(b)), nil); err != nil {
		t.Fatal(err)
	}
}

// Claves lista lo que hay en el bucket.
func (s *S3) Claves(t testing.TB) []string {
	t.Helper()
	l, err := s.back.ListBucket(s.Bucket, nil, gofakes3.ListBucketPage{})
	if err != nil {
		t.Fatal(err)
	}
	var res []string
	for _, c := range l.Contents {
		res = append(res, c.Key)
	}
	return res
}

type corruptor struct {
	gofakes3.Backend
	mu      sync.Mutex
	malas   map[string]bool
	subidas map[string]int
}

func (c *corruptor) PutObject(bucket, key string, meta map[string]string, input io.Reader, size int64, cond *gofakes3.PutConditions) (gofakes3.PutObjectResult, error) {
	c.mu.Lock()
	mala := c.malas[key]
	if c.subidas == nil {
		c.subidas = map[string]int{}
	}
	c.subidas[key]++
	c.mu.Unlock()
	if mala {
		b, err := io.ReadAll(input)
		if err != nil {
			return gofakes3.PutObjectResult{}, err
		}
		if len(b) > 0 {
			b[0] ^= 0xff
		}
		input = bytesReader(b)
	}
	return c.Backend.PutObject(bucket, key, meta, input, size, cond)
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// String para los mensajes de las pruebas.
func (s *S3) String() string { return fmt.Sprintf("%s/%s", s.URL, s.Bucket) }
