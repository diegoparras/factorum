package copias

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/diegoparras/factorum/internal/almacen"
)

// OpcionesRestaurar dice cómo restaurar.
type OpcionesRestaurar struct {
	// Confirmar: sin esto, solo se verifica la copia y no se toca la base.
	Confirmar bool
	// Migraciones que conoce este Factorum: una copia con otras (de una versión más nueva) no se
	// restaura.
	Migraciones []string
	// PrepararTablas crea las tablas que no son de las migraciones de Factorum y faltan en una base
	// vacía (las de la sesión de WhatsApp).
	PrepararTablas func(ctx context.Context, faltan []string) error
	// Antes corre con la copia ya verificada y controlada contra la base, antes de tocarla (para
	// hacer una copia de lo que hay).
	Antes func(ctx context.Context, m *Manifiesto) error
	// Despues corre dentro de la misma transacción, con todo cargado (para volver a encolar lo que
	// quedó a medio procesar). Devuelve un texto para el informe.
	Despues func(ctx context.Context, tx pgx.Tx) (string, error)
}

// Informe de una restauración.
type Informe struct {
	Manifiesto *Manifiesto
	Restaurada bool
	Avisos     []string
	Despues    string
}

// Restaurar verifica la copia y, con Confirmar, borra los datos de la base y carga los de la copia
// en una sola transacción: si algo falla, la base queda como estaba. abrir se llama dos veces (una
// para verificar todo antes de tocar nada y otra para cargar).
func Restaurar(ctx context.Context, pool *pgxpool.Pool, abrir func() (io.ReadCloser, error), sello *almacen.Almacen, opc OpcionesRestaurar) (*Informe, error) {
	r, err := abrir()
	if err != nil {
		return nil, err
	}
	m, err := Leer(r, sello)
	r.Close()
	if err != nil {
		return nil, err
	}
	inf := &Informe{Manifiesto: m}
	if nuevas := m.MigracionesDesconocidas(opc.Migraciones); len(nuevas) > 0 && opc.Migraciones != nil {
		return inf, fmt.Errorf("la copia es de un Factorum más nuevo (%s, con %s): restaurala con esa versión o una posterior", m.Factorum, strings.Join(nuevas, ", "))
	}
	if !opc.Confirmar {
		return inf, nil
	}

	destino, _, err := tablasDeDatos(ctx, pool)
	if err != nil {
		return inf, err
	}
	faltan := faltantes(m, destino)
	if len(faltan) > 0 && opc.PrepararTablas != nil {
		if err := opc.PrepararTablas(ctx, faltan); err != nil {
			return inf, fmt.Errorf("preparando las tablas %s: %w", strings.Join(faltan, ", "), err)
		}
		if destino, _, err = tablasDeDatos(ctx, pool); err != nil {
			return inf, err
		}
		faltan = faltantes(m, destino)
	}
	if len(faltan) > 0 {
		return inf, fmt.Errorf("la base no tiene las tablas %s: ¿la copia es de otra versión de Factorum (%s)?", strings.Join(faltan, ", "), m.Factorum)
	}
	// Cada columna de la copia tiene que estar, con el mismo tipo. Las que la base tiene de más (de
	// una migración posterior a la copia) quedan con su valor por defecto.
	cols := map[string]map[string]string{}
	var nombres []string
	for _, t := range destino {
		cols[t.nombre] = map[string]string{}
		for _, c := range t.columnas {
			cols[t.nombre][c.Nombre] = c.Tipo
		}
		nombres = append(nombres, ident(t.nombre))
	}
	for _, t := range m.Tablas {
		for _, c := range t.Columnas {
			tipo, ok := cols[t.Nombre][c.Nombre]
			if !ok {
				return inf, fmt.Errorf("la tabla %s de la base no tiene la columna %s de la copia", t.Nombre, c.Nombre)
			}
			if tipo != c.Tipo {
				return inf, fmt.Errorf("la columna %s.%s es %s en la base y %s en la copia", t.Nombre, c.Nombre, tipo, c.Tipo)
			}
		}
		if len(t.Columnas) < len(cols[t.Nombre]) {
			inf.Avisos = append(inf.Avisos, fmt.Sprintf("la tabla %s tiene columnas nuevas que quedan con su valor por defecto", t.Nombre))
		}
	}
	for _, t := range destino {
		if !slices.ContainsFunc(m.Tablas, func(x Tabla) bool { return x.Nombre == t.nombre }) {
			inf.Avisos = append(inf.Avisos, fmt.Sprintf("la tabla %s no está en la copia: queda vacía", t.nombre))
		}
	}

	if opc.Antes != nil {
		if err := opc.Antes(ctx, m); err != nil {
			return inf, err
		}
	}
	porArchivo := map[string]Tabla{}
	for _, t := range m.Tablas {
		porArchivo[t.Archivo] = t
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '60s'`); err != nil {
			return err
		}
		var cola bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('river_job') IS NOT NULL`).Scan(&cola); err != nil {
			return err
		}
		vaciar := slices.Clone(nombres)
		if cola {
			// Los trabajos de la cola apuntan a documentos de la base de antes.
			vaciar = append(vaciar, "river_job")
		}
		if len(vaciar) > 0 {
			if _, err := tx.Exec(ctx, `TRUNCATE `+strings.Join(vaciar, ", ")); err != nil {
				if strings.Contains(err.Error(), "lock timeout") {
					return errors.New("la base está ocupada: pará Factorum (o esperá a que termine lo que está haciendo) y probá de nuevo")
				}
				return fmt.Errorf("vaciando la base: %w", err)
			}
		}
		r, err := abrir()
		if err != nil {
			return err
		}
		defer r.Close()
		segundo, err := recorrer(r, sello, func(archivo string, datos io.Reader) error {
			t, ok := porArchivo[archivo]
			if !ok {
				return fmt.Errorf("la copia cambió entre la verificación y la carga (%s)", archivo)
			}
			if _, err := tx.Conn().PgConn().CopyFrom(ctx, datos, "COPY "+ident(t.Nombre)+" ("+listaColumnas(t.Columnas)+") FROM STDIN"); err != nil {
				return fmt.Errorf("cargando %s: %w", t.Nombre, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if segundo.Filas != m.Filas || len(segundo.Tablas) != len(m.Tablas) {
			return errors.New("la copia cambió entre la verificación y la carga")
		}
		for _, t := range m.Tablas {
			for _, s := range t.Secuencias {
				if _, err := tx.Exec(ctx, `SELECT setval(seq, $3, $4) FROM (SELECT pg_get_serial_sequence($1, $2) AS seq) x WHERE seq IS NOT NULL`,
					ident(t.Nombre), s.Columna, s.Valor, s.Llamado); err != nil {
					return fmt.Errorf("secuencia de %s.%s: %w", t.Nombre, s.Columna, err)
				}
			}
		}
		if opc.Despues != nil {
			msg, err := opc.Despues(ctx, tx)
			if err != nil {
				return err
			}
			inf.Despues = msg
		}
		return nil
	})
	if err != nil {
		return inf, err
	}
	inf.Restaurada = true
	return inf, nil
}

func faltantes(m *Manifiesto, destino []tablaBase) []string {
	var res []string
	for _, t := range m.Tablas {
		if !slices.ContainsFunc(destino, func(x tablaBase) bool { return x.nombre == t.Nombre }) {
			res = append(res, t.Nombre)
		}
	}
	return res
}
