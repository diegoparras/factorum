// Package almacen guarda los archivos originales y los resultados.
//
// Cada objeto se direcciona por su SHA-256 (el mismo PDF subido dos veces se guarda una vez), se
// comprime con zstd y, si hay clave maestra, se cifra con AES-256-GCM antes de salir del proceso:
// el bucket (R2, S3) o el disco nunca ven una factura en claro. Formato del objeto:
//
//	"FTM1" | flags(1) | nonce(12, si cifrado) | datos (zstd, cifrado o no)
package almacen

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Backend guarda bytes opacos.
type Backend interface {
	Poner(ctx context.Context, clave string, datos []byte) error
	Traer(ctx context.Context, clave string) ([]byte, error)
	Existe(ctx context.Context, clave string) (bool, error)
	Nombre() string
}

// Almacen comprime y cifra sobre un Backend.
type Almacen struct {
	mu       sync.RWMutex
	b        Backend
	respaldo Backend // si se cambió de backend: de acá se lee lo que no está en el nuevo
	aead     cipher.AEAD
	enc      *zstd.Encoder
	dec      *zstd.Decoder
}

var ErrNoExiste = errors.New("el objeto no existe")

// Nuevo arma el almacén. clave (32 bytes) es opcional: sin clave, se comprime pero no se cifra.
func Nuevo(b Backend, clave []byte) (*Almacen, error) {
	a := &Almacen{b: b}
	if clave != nil {
		bl, err := aes.NewCipher(clave)
		if err != nil {
			return nil, err
		}
		a.aead, err = cipher.NewGCM(bl)
		if err != nil {
			return nil, err
		}
	}
	var err error
	a.enc, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	a.dec, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Almacen) backends() (Backend, Backend) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.b, a.respaldo
}

// Backend es el nombre del backend donde se guarda lo nuevo ("disco" o "r2").
func (a *Almacen) Backend() string {
	b, _ := a.backends()
	return b.Nombre()
}

// CambiarBackend pasa a guardar en b (se cambió R2 desde la pantalla). respaldo, si no es nil, es
// de donde se sigue leyendo lo que todavía no está en b (lo guardado antes del cambio).
func (a *Almacen) CambiarBackend(b, respaldo Backend) {
	a.mu.Lock()
	a.b, a.respaldo = b, respaldo
	a.mu.Unlock()
}
func (a *Almacen) Cifrado() bool { return a.aead != nil }

const magia = "FTM1"
const flagCifrado = 1

// Sellar comprime y cifra.
func (a *Almacen) Sellar(datos []byte) ([]byte, error) {
	comp := a.enc.EncodeAll(datos, make([]byte, 0, len(datos)/2))
	var out bytes.Buffer
	out.WriteString(magia)
	if a.aead == nil {
		out.WriteByte(0)
		out.Write(comp)
		return out.Bytes(), nil
	}
	out.WriteByte(flagCifrado)
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out.Write(nonce)
	// La cabecera va como dato asociado: no se puede cambiar el flag sin romper la autenticación.
	out.Write(a.aead.Seal(nil, nonce, comp, []byte(magia+"\x01")))
	return out.Bytes(), nil
}

// Abrir descifra y descomprime.
func (a *Almacen) Abrir(obj []byte) ([]byte, error) {
	if len(obj) < 5 || string(obj[:4]) != magia {
		return nil, errors.New("objeto con formato desconocido")
	}
	flags, cuerpo := obj[4], obj[5:]
	if flags&flagCifrado != 0 {
		if a.aead == nil {
			return nil, errors.New("el objeto está cifrado y no hay FACTORUM_MASTER_KEY")
		}
		n := a.aead.NonceSize()
		if len(cuerpo) < n {
			return nil, errors.New("objeto cifrado truncado")
		}
		var err error
		cuerpo, err = a.aead.Open(nil, cuerpo[:n], cuerpo[n:], []byte(magia+"\x01"))
		if err != nil {
			return nil, errors.New("no se pudo descifrar (¿cambió la master key?)")
		}
	}
	return a.dec.DecodeAll(cuerpo, nil)
}

// Guardar sella y sube. La clave suele ser "originales/<sha256>" o "resultados/<sha256>.json".
// Si ya existe, no se vuelve a subir.
func (a *Almacen) Guardar(ctx context.Context, clave string, datos []byte) (int, error) {
	b, _ := a.backends()
	if ok, err := b.Existe(ctx, clave); err == nil && ok {
		return 0, nil
	}
	obj, err := a.Sellar(datos)
	if err != nil {
		return 0, err
	}
	return len(obj), b.Poner(ctx, clave, obj)
}

// Leer baja y abre. Si no está en el backend actual (o está pero no abre: llegó roto) y hay
// respaldo, lo busca ahí. Así, durante y después del pase a R2, se lee de R2 y si no, del disco.
func (a *Almacen) Leer(ctx context.Context, clave string) ([]byte, error) {
	b, resp := a.backends()
	obj, err := b.Traer(ctx, clave)
	if err == nil {
		datos, err := a.Abrir(obj)
		if err == nil || resp == nil {
			return datos, err
		}
	}
	if resp == nil {
		return nil, err
	}
	if obj, err = resp.Traer(ctx, clave); err != nil {
		return nil, err
	}
	return a.Abrir(obj)
}

// borrador es un backend que sabe borrar (el disco y S3).
type borrador interface {
	Borrar(ctx context.Context, clave string) error
}

// Borrar saca un objeto de donde se guarda lo nuevo y del respaldo (el disco y R2): lo usa el
// borrado definitivo de la papelera. No es error si no estaba.
func (a *Almacen) Borrar(ctx context.Context, clave string) error {
	b, resp := a.backends()
	for _, x := range []Backend{b, resp} {
		if x == nil {
			continue
		}
		br, ok := x.(borrador)
		if !ok {
			return fmt.Errorf("el almacén %s no sabe borrar", x.Nombre())
		}
		if err := br.Borrar(ctx, clave); err != nil && !errors.Is(err, ErrNoExiste) {
			return err
		}
	}
	return nil
}

// ---- Disco ----

// Disco guarda en un directorio, repartido en subcarpetas por los dos primeros caracteres.
type Disco struct{ Dir string }

func (d *Disco) Nombre() string { return "disco" }

func (d *Disco) ruta(clave string) (string, error) {
	clave = filepath.ToSlash(clave)
	if strings.Contains(clave, "..") || strings.HasPrefix(clave, "/") {
		return "", fmt.Errorf("clave inválida %q", clave)
	}
	dir, base := filepath.Split(clave)
	sub := ""
	if len(base) >= 2 {
		sub = base[:2]
	}
	return filepath.Join(d.Dir, dir, sub, base), nil
}

func (d *Disco) Poner(_ context.Context, clave string, datos []byte) error {
	r, err := d.ruta(clave)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r), 0o700); err != nil {
		return err
	}
	tmp := r + ".tmp"
	if err := os.WriteFile(tmp, datos, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r)
}

func (d *Disco) Traer(_ context.Context, clave string) ([]byte, error) {
	r, err := d.ruta(clave)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(r)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoExiste
	}
	return b, err
}

func (d *Disco) Existe(_ context.Context, clave string) (bool, error) {
	r, err := d.ruta(clave)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(r)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// ---- R2 / S3 ----

// S3 habla con R2 (o cualquier S3 compatible).
type S3 struct {
	cli     *minio.Client
	bucket  string
	prefijo string
}

// NuevoS3 conecta. endpoint: "https://<cuenta>.r2.cloudflarestorage.com".
func NuevoS3(endpoint, bucket, access, secret, region, prefijo string) (*S3, error) {
	seguro := !strings.HasPrefix(endpoint, "http://")
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	cli, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Secure: seguro,
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	return &S3{cli: cli, bucket: bucket, prefijo: prefijo}, nil
}

func (s *S3) Nombre() string { return "r2" }

func (s *S3) Poner(ctx context.Context, clave string, datos []byte) error {
	_, err := s.cli.PutObject(ctx, s.bucket, s.prefijo+clave, bytes.NewReader(datos), int64(len(datos)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3) Traer(ctx context.Context, clave string) ([]byte, error) {
	o, err := s.cli.GetObject(ctx, s.bucket, s.prefijo+clave, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	b, err := io.ReadAll(o)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNoExiste
		}
		return nil, err
	}
	return b, nil
}

// Borrar saca un objeto (lo usan la prueba de la configuración de R2 y el borrado definitivo de
// la papelera). En S3, borrar lo que no está no es error.
func (s *S3) Borrar(ctx context.Context, clave string) error {
	return s.cli.RemoveObject(ctx, s.bucket, s.prefijo+clave, minio.RemoveObjectOptions{})
}

func (s *S3) Existe(ctx context.Context, clave string) (bool, error) {
	_, err := s.cli.StatObject(ctx, s.bucket, s.prefijo+clave, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Backends devuelve dónde se guarda lo nuevo y de dónde se lee lo que no está ahí (puede ser nil).
func (a *Almacen) Backends() (actual, respaldo Backend) { return a.backends() }

// R2 es el S3 donde se guarda lo nuevo (nil si se guarda en disco).
func (a *Almacen) R2() *S3 {
	b, _ := a.backends()
	s, _ := b.(*S3)
	return s
}

// ---- Disco: lo que usa el pase a R2 ----

// Recorrer llama a f con cada objeto guardado en el disco (clave y tamaño), en orden alfabético de
// la ruta. Saltea los archivos a medio escribir (.tmp).
func (d *Disco) Recorrer(f func(clave string, tamano int64) error) error {
	return filepath.WalkDir(d.Dir, func(ruta string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && ruta == d.Dir {
				return filepath.SkipDir
			}
			return err
		}
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(d.Dir, ruta)
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		return f(d.clave(filepath.ToSlash(rel)), info.Size())
	})
}

// clave deshace ruta(): "originales/ab/abcd..." -> "originales/abcd...".
func (d *Disco) clave(rel string) string {
	dir, base := path.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	if len(base) >= 2 {
		padre, sub := path.Split(dir)
		if sub == base[:2] {
			dir = strings.TrimSuffix(padre, "/")
		}
	}
	if dir == "" {
		return base
	}
	return dir + "/" + base
}

// Borrar saca un objeto del disco (no es error si ya no estaba) y la subcarpeta si quedó vacía.
func (d *Disco) Borrar(_ context.Context, clave string) error {
	r, err := d.ruta(clave)
	if err != nil {
		return err
	}
	if err := os.Remove(r); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dir := filepath.Dir(r); dir != filepath.Clean(d.Dir) {
		_ = os.Remove(dir) // solo si está vacía
	}
	return nil
}

// ---- S3: lo que usan el pase a R2 y las copias ----

// Tamano dice el tamaño de un objeto en el bucket (ErrNoExiste si no está).
func (s *S3) Tamano(ctx context.Context, clave string) (int64, error) {
	info, err := s.cli.StatObject(ctx, s.bucket, s.prefijo+clave, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return 0, ErrNoExiste
		}
		return 0, err
	}
	return info.Size, nil
}

// Abrir devuelve el objeto para leerlo de a poco (para las descargas grandes) y su tamaño.
func (s *S3) Abrir(ctx context.Context, clave string) (io.ReadCloser, int64, error) {
	o, err := s.cli.GetObject(ctx, s.bucket, s.prefijo+clave, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	info, err := o.Stat()
	if err != nil {
		o.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, ErrNoExiste
		}
		return nil, 0, err
	}
	return o, info.Size, nil
}

// SubirArchivo sube un archivo local (en partes, sin cargarlo entero en memoria).
func (s *S3) SubirArchivo(ctx context.Context, clave, ruta string) error {
	_, err := s.cli.FPutObject(ctx, s.bucket, s.prefijo+clave, ruta, minio.PutObjectOptions{ContentType: "application/octet-stream", PartSize: 16 << 20})
	return err
}

// ObjetoS3 es lo que devuelve Listar.
type ObjetoS3 struct {
	Clave      string
	Tamano     int64
	Modificado time.Time
}

// Listar devuelve los objetos cuya clave empieza con pre (sin el prefijo de la instalación).
func (s *S3) Listar(ctx context.Context, pre string) ([]ObjetoS3, error) {
	var res []ObjetoS3
	for o := range s.cli.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefijo + pre, Recursive: true}) {
		if o.Err != nil {
			return nil, o.Err
		}
		res = append(res, ObjetoS3{Clave: strings.TrimPrefix(o.Key, s.prefijo), Tamano: o.Size, Modificado: o.LastModified})
	}
	return res, nil
}

// Destino describe a dónde apunta (para darse cuenta si se cambió de bucket), sin credenciales.
func (s *S3) Destino() string {
	return s.cli.EndpointURL().Host + "/" + s.bucket + "/" + s.prefijo
}
