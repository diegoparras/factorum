package almacen

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func alm(t *testing.T, b byte) *Almacen {
	t.Helper()
	a, err := Nuevo(nil, bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func cifrar(t *testing.T, a *Almacen, datos []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := a.CifrarFlujo(&out)
	if err != nil {
		t.Fatal(err)
	}
	// De a pedazos raros, para cruzar los bordes de los tramos.
	for p := datos; len(p) > 0; {
		k := min(len(p), 300_001)
		if _, err := w.Write(p[:k]); err != nil {
			t.Fatal(err)
		}
		p = p[k:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestFlujoIdaYVuelta(t *testing.T) {
	a := alm(t, 1)
	for _, n := range []int{0, 1, tramoFlujo - 1, tramoFlujo, tramoFlujo + 1, 3*tramoFlujo + 17} {
		datos := make([]byte, n)
		_, _ = rand.Read(datos)
		c := cifrar(t, a, datos)
		if bytes.Contains(c, datos[:min(n, 64)]) && n >= 64 {
			t.Fatalf("%d: el texto aparece en claro", n)
		}
		r, err := a.DescifrarFlujo(bytes.NewReader(c))
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		if !bytes.Equal(got, datos) {
			t.Fatalf("%d: no vuelve igual (%d bytes)", n, len(got))
		}
	}
}

func TestFlujoDetectaCambios(t *testing.T) {
	a := alm(t, 1)
	datos := make([]byte, 2*tramoFlujo+100)
	_, _ = rand.Read(datos)
	c := cifrar(t, a, datos)
	leer := func(b []byte, al *Almacen) error {
		r, err := al.DescifrarFlujo(bytes.NewReader(b))
		if err != nil {
			return err
		}
		_, err = io.ReadAll(r)
		return err
	}
	if err := leer(c, alm(t, 2)); !errors.Is(err, ErrFlujoRoto) {
		t.Fatalf("otra clave: %v", err)
	}
	cambiado := bytes.Clone(c)
	cambiado[len(c)/2] ^= 1
	if err := leer(cambiado, a); !errors.Is(err, ErrFlujoRoto) {
		t.Fatalf("un bit cambiado: %v", err)
	}
	// Cortado justo en el borde de un tramo: falta el final.
	borde := len(magiaFlujo) + prefijoFlujo + 2*(tramoFlujo+16)
	if err := leer(c[:borde], a); !errors.Is(err, ErrFlujoRoto) {
		t.Fatalf("cortado en un borde: %v", err)
	}
	if err := leer(c[:len(c)-5], a); !errors.Is(err, ErrFlujoRoto) {
		t.Fatalf("cortado: %v", err)
	}
	if err := leer(append(bytes.Clone(c), 'x'), a); !errors.Is(err, ErrFlujoRoto) {
		t.Fatalf("con algo pegado al final: %v", err)
	}
}

func TestDiscoRecorrerYBorrar(t *testing.T) {
	d := &Disco{Dir: t.TempDir()}
	ctx := context.Background()
	claves := []string{"originales/abcdef", "resultados/abcdef.json", "x", "otra/y/zz9"}
	for _, k := range claves {
		if err := d.Poner(ctx, k, []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(d.Dir, "originales", "ab", "abcdef.tmp"), []byte("a medias"), 0o600)
	got := map[string]int64{}
	if err := d.Recorrer(func(k string, n int64) error { got[k] = n; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(claves) {
		t.Fatalf("recorrido: %v", got)
	}
	for _, k := range claves {
		if got[k] != int64(len(k)) {
			t.Fatalf("falta %q: %v", k, got)
		}
		if _, err := d.Traer(ctx, k); err != nil {
			t.Fatalf("la clave recorrida %q no se puede leer: %v", k, err)
		}
	}
	if err := d.Borrar(ctx, "originales/abcdef"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.Existe(ctx, "originales/abcdef"); ok {
		t.Fatal("no se borró")
	}
	if err := d.Borrar(ctx, "originales/abcdef"); err != nil {
		t.Fatal("borrar dos veces no es error")
	}
	vacio := &Disco{Dir: filepath.Join(t.TempDir(), "no-existe")}
	if err := vacio.Recorrer(func(string, int64) error { return nil }); err != nil {
		t.Fatalf("un disco sin nada: %v", err)
	}
}
