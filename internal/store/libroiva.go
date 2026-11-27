package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/diegoparras/factorum/internal/libroiva"
	"github.com/diegoparras/factorum/internal/modelo"
)

// consultor es lo que tienen en común el pool y una transacción.
type consultor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ArchivoExportado es un archivo de una exportación del Libro IVA Digital.
type ArchivoExportado struct {
	Nombre string `json:"nombre"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// CbteExportado es un comprobante que fue en una exportación: cómo se identifica y la huella de lo
// que se escribió (si cambia, cambió el registro).
type CbteExportado struct {
	Clave       string `json:"clave"`
	ID          int64  `json:"id,omitempty"`
	Huella      string `json:"huella"`
	Descripcion string `json:"descripcion"`
}

// Exportacion es una vez que se bajó el ZIP de un libro.
type Exportacion struct {
	ID            int64              `json:"id"`
	Cliente       string             `json:"cliente"`
	Periodo       string             `json:"periodo"`
	Libro         string             `json:"libro"`
	Usuario       string             `json:"usuario"`
	Creado        time.Time          `json:"creado"`
	SoloAprobados bool               `json:"solo_aprobados"`
	Cantidad      int                `json:"cantidad"`
	Archivos      []ArchivoExportado `json:"archivos"`
	Comprobantes  []CbteExportado    `json:"-"`
}

// RegistrarExportacion guarda una exportación (el período queda presentado).
func (s *Store) RegistrarExportacion(ctx context.Context, e *Exportacion) (int64, error) {
	if !libroiva.LibroValido(e.Libro) {
		return 0, fmt.Errorf("%w: libro %q", ErrFiltro, e.Libro)
	}
	arch, _ := json.Marshal(e.Archivos)
	cbs, _ := json.Marshal(e.Comprobantes)
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO libro_iva_exportaciones (cliente_cuit, periodo, libro, usuario, solo_aprobados, cantidad, archivos, comprobantes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, creado`, e.Cliente, e.Periodo, e.Libro, e.Usuario, e.SoloAprobados, e.Cantidad, arch, cbs).Scan(&id, &e.Creado)
	e.ID = id
	return id, err
}

// Exportaciones devuelve las exportaciones de un libro y un período, la más nueva primero.
func (s *Store) Exportaciones(ctx context.Context, cliente, libro, periodo string, limite int) ([]Exportacion, error) {
	if limite <= 0 {
		limite = 20
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, cliente_cuit, periodo, libro, usuario, creado, solo_aprobados, cantidad, archivos, comprobantes
		FROM libro_iva_exportaciones WHERE cliente_cuit=$1 AND libro=$2 AND periodo=$3 ORDER BY creado DESC, id DESC LIMIT $4`, cliente, libro, periodo, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Exportacion{}
	for rows.Next() {
		var e Exportacion
		var arch, cbs []byte
		if err := rows.Scan(&e.ID, &e.Cliente, &e.Periodo, &e.Libro, &e.Usuario, &e.Creado, &e.SoloAprobados, &e.Cantidad, &arch, &cbs); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(arch, &e.Archivos)
		_ = json.Unmarshal(cbs, &e.Comprobantes)
		res = append(res, e)
	}
	return res, rows.Err()
}

// PeriodosPresentados son los períodos de un libro del cliente con alguna exportación.
func (s *Store) PeriodosPresentados(ctx context.Context, cliente, libro string) (map[string]bool, error) {
	return periodosPresentados(ctx, s.Pool, cliente, libro)
}

func periodosPresentados(ctx context.Context, q consultor, cliente, libro string) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT periodo FROM libro_iva_exportaciones WHERE cliente_cuit=$1 AND libro=$2`, cliente, libro)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		res[p] = true
	}
	return res, rows.Err()
}

// fueExportado dice si el comprobante (por su clave o por su id) fue en la última exportación de
// ese período.
func fueExportado(ctx context.Context, q consultor, cliente, libro, periodo, clave string, id int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT coalesce((SELECT ($4 <> '' AND comprobantes @> jsonb_build_array(jsonb_build_object('clave', $4::text)))
			OR ($5::bigint > 0 AND comprobantes @> jsonb_build_array(jsonb_build_object('id', $5::bigint)))
		FROM libro_iva_exportaciones WHERE cliente_cuit=$1 AND libro=$2 AND periodo=$3 ORDER BY creado DESC, id DESC LIMIT 1), false)`,
		cliente, libro, periodo, clave, id).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// CodigoLlegoTarde es el hallazgo de un comprobante que llegó después de presentar su mes.
const CodigoLlegoTarde = "llego_tarde"

// AsignarPeriodo decide el período del libro de un comprobante nuevo (o restaurado, o al que le
// cambiaron la fecha): si su mes ya está presentado para el cliente (en el libro que le toca), va
// al primer período abierto posterior, con el hallazgo «Llegó después de presentar <mes>: va en
// <mes>». Si no, el de su fecha. Lo que eligió una persona (origen manual) no se toca. Saca el
// hallazgo viejo si ya no corresponde. Devuelve si cambió algo.
//
// Si el comprobante (por su clave, o por su id si id no es 0) fue en la última exportación de su
// mes, se queda ahí: se había presentado con él (un archivo que se vuelve a procesar, o la copia de
// uno que ya se presentó).
func AsignarPeriodo(ctx context.Context, q consultor, cliente string, c *modelo.Comprobante, hall *[]modelo.Hallazgo, exportadoID int64) (bool, error) {
	if c.Origen("periodo_iva") == modelo.OrigenManual && c.PeriodoIVA != "" {
		return false, nil
	}
	antes := c.PeriodoIVA
	sacar := func() {
		var h []modelo.Hallazgo
		for _, x := range *hall {
			if x.Codigo != CodigoLlegoTarde {
				h = append(h, x)
			}
		}
		*hall = h
	}
	limpiar := func() bool {
		c.PeriodoIVA = ""
		if c.Origenes != nil {
			delete(c.Origenes, "periodo_iva")
		}
		sacar()
		return antes != ""
	}
	if _, err := time.Parse("2006-01-02", c.Fecha); err != nil || cliente == "" {
		return limpiar(), nil
	}
	mes := c.Fecha[:7]
	libro := libroiva.LibroDe(c.Tipo)
	pres, err := periodosPresentados(ctx, q, cliente, libro)
	if err != nil {
		return false, err
	}
	if !pres[mes] {
		return limpiar(), nil
	}
	if ok, err := fueExportado(ctx, q, cliente, libro, mes, libroiva.ClaveCompra(c), exportadoID); err != nil {
		return false, err
	} else if ok {
		return limpiar(), nil
	}
	p := libroiva.PrimerPeriodoAbierto(mes, pres)
	c.PeriodoIVA = p
	c.Marcar("periodo_iva", modelo.OrigenCalculo)
	sacar()
	*hall = append(*hall, modelo.Hallazgo{Codigo: CodigoLlegoTarde, Nivel: modelo.NivelInfo, Campo: "periodo_iva",
		Mensaje: fmt.Sprintf("Llegó después de presentar %s: va en %s.", libroiva.NombreMes(mes), libroiva.NombreMes(p))})
	return antes != p, nil
}

// AsignarPeriodo con el pool (para la app).
func (s *Store) AsignarPeriodo(ctx context.Context, cliente string, c *modelo.Comprobante, hall *[]modelo.Hallazgo, exportadoID int64) (bool, error) {
	return AsignarPeriodo(ctx, s.Pool, cliente, c, hall, exportadoID)
}

// reasignarPeriodos vuelve a decidir el período de esos comprobantes (los restaurados de la
// papelera) dentro de tx y guarda lo que cambió.
func reasignarPeriodos(ctx context.Context, tx pgx.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id, cliente_cuit, datos, hallazgos FROM comprobantes WHERE id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return err
	}
	type fila struct {
		id      int64
		cliente string
		c       modelo.Comprobante
		h       []modelo.Hallazgo
	}
	var fs []fila
	for rows.Next() {
		var f fila
		var d, h []byte
		if err := rows.Scan(&f.id, &f.cliente, &d, &h); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(d, &f.c); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(h, &f.h)
		fs = append(fs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range fs {
		f := &fs[i]
		cambio, err := AsignarPeriodo(ctx, tx, f.cliente, &f.c, &f.h, f.id)
		if err != nil {
			return err
		}
		if !cambio {
			continue
		}
		if f.h == nil {
			f.h = []modelo.Hallazgo{}
		}
		d, _ := json.Marshal(f.c)
		h, _ := json.Marshal(f.h)
		if _, err := tx.Exec(ctx, `UPDATE comprobantes SET datos=$2, hallazgos=$3, periodo_iva=$4, actualizado=now() WHERE id=$1`, f.id, d, h, periodoValidoONull(&f.c)); err != nil {
			return err
		}
	}
	return nil
}

// DelClientePeriodo trae los comprobantes de un cliente que van en el libro de ese período (por su
// período del libro, no por la fecha), hasta tope. tipos, si no es nil, limita a esos tipos; sin,
// si no es nil, saca esos.
func (s *Store) DelClientePeriodo(ctx context.Context, cliente, periodo string, tipos, sin []int, tope int) ([]Cbte, error) {
	rows, err := s.Pool.Query(ctx, selCbte+` WHERE c.cliente_cuit=$1 AND c.borrado_en IS NULL AND c.periodo_iva=$2
		AND ($3::int[] IS NULL OR c.tipo = ANY($3)) AND ($4::int[] IS NULL OR c.tipo IS NULL OR NOT c.tipo = ANY($4)) ORDER BY c.id LIMIT $5`,
		cliente, periodo, enteros(tipos), enteros(sin), tope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Cbte{}
	for rows.Next() {
		c, err := scanCbte(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *c)
	}
	return res, rows.Err()
}

func enteros(v []int) any {
	if v == nil {
		return nil
	}
	r := make([]int32, len(v))
	for i, x := range v {
		r[i] = int32(x)
	}
	return r
}
