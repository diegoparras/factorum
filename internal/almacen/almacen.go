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
	"os"
	"path/filepath"
	"strings"

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
	b    Backend
	aead cipher.AEAD
	enc  *zstd.Encoder
	dec  *zstd.Decoder
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

func (a *Almacen) Backend() string { return a.b.Nombre() }
func (a *Almacen) Cifrado() bool   { return a.aead != nil }

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
	if ok, err := a.b.Existe(ctx, clave); err == nil && ok {
		return 0, nil
	}
	obj, err := a.Sellar(datos)
	if err != nil {
		return 0, err
	}
	return len(obj), a.b.Poner(ctx, clave, obj)
}

// Leer baja y abre.
func (a *Almacen) Leer(ctx context.Context, clave string) ([]byte, error) {
	obj, err := a.b.Traer(ctx, clave)
	if err != nil {
		return nil, err
	}
	return a.Abrir(obj)
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
