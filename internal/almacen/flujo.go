package almacen

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
)

// Cifrado en flujo para lo que no conviene tener entero en memoria (las copias de la base). Usa la
// misma clave maestra y el mismo AES-256-GCM que los objetos, pero en tramos de 1 MiB, cada uno
// autenticado por separado (el esquema STREAM de Hoang, Reyhanitabar, Rogaway y Vizár):
//
//	"FTS1" | prefijo del nonce (7) | tramo 0 | tramo 1 | ... | tramo final
//
// Cada tramo es AES-GCM(texto, nonce = prefijo | contador (4, big endian) | final (1)) con la
// cabecera como dato asociado. Todos los tramos tienen 1 MiB de texto menos el último, que tiene
// menos (puede tener cero) y va marcado como final: así no se puede cortar, reordenar ni pegar
// tramos de otro archivo sin que se note.

const (
	magiaFlujo   = "FTS1"
	tramoFlujo   = 1 << 20
	prefijoFlujo = 7
)

// ErrFlujoRoto: el archivo está cortado, cambiado o se cifró con otra clave.
var ErrFlujoRoto = errors.New("el archivo cifrado está cortado o cambiado, o es de otra FACTORUM_MASTER_KEY")

type escritorFlujo struct {
	w       io.Writer
	aead    cipher.AEAD
	cab     []byte
	prefijo []byte
	n       uint32
	buf     []byte
	cerrado bool
}

// CifrarFlujo devuelve un escritor que cifra lo que recibe y lo manda a w. Hay que cerrarlo para
// que escriba el último tramo (sin Close, el archivo queda inválido).
func (a *Almacen) CifrarFlujo(w io.Writer) (io.WriteCloser, error) {
	if a.aead == nil {
		return nil, errors.New("hace falta FACTORUM_MASTER_KEY para cifrar")
	}
	pre := make([]byte, prefijoFlujo)
	if _, err := rand.Read(pre); err != nil {
		return nil, err
	}
	cab := append([]byte(magiaFlujo), pre...)
	if _, err := w.Write(cab); err != nil {
		return nil, err
	}
	return &escritorFlujo{w: w, aead: a.aead, cab: cab, prefijo: pre, buf: make([]byte, 0, tramoFlujo)}, nil
}

func nonceFlujo(pre []byte, n uint32, final bool) []byte {
	nonce := make([]byte, 12)
	copy(nonce, pre)
	binary.BigEndian.PutUint32(nonce[prefijoFlujo:], n)
	if final {
		nonce[11] = 1
	}
	return nonce
}

func (e *escritorFlujo) sellar(final bool) error {
	if e.n == ^uint32(0) {
		return errors.New("archivo demasiado grande para un solo flujo")
	}
	out := e.aead.Seal(nil, nonceFlujo(e.prefijo, e.n, final), e.buf, e.cab)
	e.n++
	e.buf = e.buf[:0]
	_, err := e.w.Write(out)
	return err
}

func (e *escritorFlujo) Write(p []byte) (int, error) {
	if e.cerrado {
		return 0, errors.New("flujo cerrado")
	}
	total := len(p)
	for len(p) > 0 {
		lugar := tramoFlujo - len(e.buf)
		k := min(lugar, len(p))
		e.buf = append(e.buf, p[:k]...)
		p = p[k:]
		// Un tramo lleno nunca es el final: el final siempre tiene menos de 1 MiB (o nada).
		if len(e.buf) == tramoFlujo {
			if err := e.sellar(false); err != nil {
				return total - len(p), err
			}
		}
	}
	return total, nil
}

func (e *escritorFlujo) Close() error {
	if e.cerrado {
		return nil
	}
	e.cerrado = true
	return e.sellar(true)
}

type lectorFlujo struct {
	r       io.Reader
	aead    cipher.AEAD
	cab     []byte
	prefijo []byte
	n       uint32
	crudo   []byte
	listo   []byte
	fin     bool
	err     error
}

// DescifrarFlujo lee lo que escribió CifrarFlujo. Un error en cualquier tramo (o un final que falta)
// devuelve ErrFlujoRoto: lo que se leyó hasta ahí no se puede dar por bueno.
func (a *Almacen) DescifrarFlujo(r io.Reader) (io.Reader, error) {
	if a.aead == nil {
		return nil, errors.New("hace falta FACTORUM_MASTER_KEY para descifrar")
	}
	cab := make([]byte, len(magiaFlujo)+prefijoFlujo)
	if _, err := io.ReadFull(r, cab); err != nil || !bytes.Equal(cab[:4], []byte(magiaFlujo)) {
		return nil, errors.New("no es un archivo cifrado de Factorum")
	}
	return &lectorFlujo{r: r, aead: a.aead, cab: cab, prefijo: cab[4:], crudo: make([]byte, tramoFlujo+a.aead.Overhead())}, nil
}

func (l *lectorFlujo) siguiente() error {
	// Un tramo completo nunca es el final; uno más corto, sí (y si falta, el archivo está cortado).
	tam := tramoFlujo + l.aead.Overhead()
	n, err := io.ReadFull(l.r, l.crudo[:tam])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	final := n < tam
	if n < l.aead.Overhead() {
		return ErrFlujoRoto
	}
	texto, err := l.aead.Open(nil, nonceFlujo(l.prefijo, l.n, final), l.crudo[:n], l.cab)
	if err != nil {
		return ErrFlujoRoto
	}
	if final {
		// Después del final no puede venir nada.
		var uno [1]byte
		if m, _ := l.r.Read(uno[:]); m > 0 {
			return ErrFlujoRoto
		}
	}
	l.n++
	l.listo = texto
	l.fin = final
	return nil
}

func (l *lectorFlujo) Read(p []byte) (int, error) {
	for len(l.listo) == 0 {
		if l.err != nil {
			return 0, l.err
		}
		if l.fin {
			return 0, io.EOF
		}
		if err := l.siguiente(); err != nil {
			l.err = err
			return 0, err
		}
	}
	n := copy(p, l.listo)
	l.listo = l.listo[n:]
	return n, nil
}
