package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- Correo entrante ----------

// AdjuntoInfo es lo que se muestra de un adjunto (nunca su contenido).
type AdjuntoInfo struct {
	Nombre string `json:"nombre"`
	Mime   string `json:"mime"`
	Tamano int    `json:"tamano"`
}

// CorreoNuevo es un mail que acaba de llegar.
type CorreoNuevo struct {
	MessageID, Forma, Remitente, Nombre, Destinatario, Asunto, Referencias, ClienteCUIT string
	Fecha                                                                               time.Time
	Tamano                                                                              int64
	Adjuntos                                                                            []AdjuntoInfo
	Descartados                                                                         []string
}

// MensajeCorreo es un mail guardado (sin el cuerpo).
type MensajeCorreo struct {
	ID           int64         `json:"id"`
	MessageID    string        `json:"message_id"`
	Forma        string        `json:"forma"`
	Remitente    string        `json:"remitente"`
	Nombre       string        `json:"nombre"`
	Destinatario string        `json:"destinatario"`
	Asunto       string        `json:"asunto"`
	Fecha        *time.Time    `json:"fecha"`
	Referencias  string        `json:"-"`
	Tamano       int64         `json:"tamano"`
	Adjuntos     []AdjuntoInfo `json:"adjuntos"`
	Descartados  []string      `json:"descartados"`
	ClienteCUIT  string        `json:"cliente_cuit"`
	Estado       string        `json:"estado"`
	Motivo       string        `json:"motivo,omitempty"`
	LoteID       *int64        `json:"lote_id"`
	Creado       time.Time     `json:"creado"`
	Resuelto     *time.Time    `json:"resuelto,omitempty"`
	ResueltoPor  string        `json:"resuelto_por,omitempty"`
}

// AdjuntoSellado es un adjunto de un retenido, ya comprimido y cifrado.
type AdjuntoSellado struct {
	Nombre, Mime string
	Datos        []byte
}

// ErrCorreoRepetido: ese Message-ID ya se recibió.
var ErrCorreoRepetido = errors.New("ese mail ya se recibió")

// ErrNoRetenido: el mail ya no está retenido (otro admin lo resolvió o venció).
var ErrNoRetenido = errors.New("ese mail ya no está retenido")

// ReservarCorreo anota un mail que llegó, en estado «recibiendo». Si el Message-ID ya estaba,
// devuelve ErrCorreoRepetido, salvo que haya quedado a medias hace más de 10 minutos (se cayó el
// proceso): entonces lo retoma.
func (s *Store) ReservarCorreo(ctx context.Context, n CorreoNuevo) (int64, error) {
	adj, _ := json.Marshal(n.Adjuntos)
	if n.Adjuntos == nil {
		adj = []byte("[]")
	}
	if n.Descartados == nil {
		n.Descartados = []string{}
	}
	var fecha any
	if !n.Fecha.IsZero() {
		fecha = n.Fecha
	}
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO correo_mensajes (message_id, forma, remitente, nombre, destinatario, asunto, fecha, referencias, tamano, adjuntos, descartados, cliente_cuit, estado)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'recibiendo')
		ON CONFLICT (message_id) DO UPDATE SET creado=now(), forma=EXCLUDED.forma
			WHERE correo_mensajes.estado='recibiendo' AND correo_mensajes.creado < now() - interval '10 minutes'
		RETURNING id`,
		n.MessageID, n.Forma, n.Remitente, n.Nombre, n.Destinatario, n.Asunto, fecha, n.Referencias, n.Tamano, adj, n.Descartados, n.ClienteCUIT).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrCorreoRepetido
	}
	return id, err
}

// TerminarCorreo deja el mail en su estado final (procesado o ignorado).
func (s *Store) TerminarCorreo(ctx context.Context, id int64, estado, motivo string, lote int64) error {
	var l any
	if lote > 0 {
		l = lote
	}
	_, err := s.Pool.Exec(ctx, `UPDATE correo_mensajes SET estado=$2, motivo=$3, lote_id=$4 WHERE id=$1`, id, estado, motivo, l)
	return err
}

// OlvidarCorreo borra la reserva de un mail que no se pudo procesar (para que se pueda reintentar).
func (s *Store) OlvidarCorreo(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM correo_mensajes WHERE id=$1 AND estado='recibiendo'`, id)
	return err
}

// RetenerCorreo guarda los adjuntos (ya sellados) y deja el mail retenido.
func (s *Store) RetenerCorreo(ctx context.Context, id int64, motivo string, adj []AdjuntoSellado) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for i, a := range adj {
			if _, err := tx.Exec(ctx, `INSERT INTO correo_adjuntos (mensaje_id, orden, nombre, mime, datos) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (mensaje_id, orden) DO UPDATE SET nombre=EXCLUDED.nombre, mime=EXCLUDED.mime, datos=EXCLUDED.datos`, id, i, a.Nombre, a.Mime, a.Datos); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE correo_mensajes SET estado='retenido', motivo=$2 WHERE id=$1`, id, motivo)
		return err
	})
}

const selCorreo = `SELECT id, message_id, forma, remitente, nombre, destinatario, asunto, fecha, referencias, tamano, adjuntos, descartados, cliente_cuit,
	estado, motivo, lote_id, creado, resuelto, resuelto_por FROM correo_mensajes`

func scanCorreo(r pgx.Row) (*MensajeCorreo, error) {
	var m MensajeCorreo
	var adj []byte
	if err := r.Scan(&m.ID, &m.MessageID, &m.Forma, &m.Remitente, &m.Nombre, &m.Destinatario, &m.Asunto, &m.Fecha, &m.Referencias, &m.Tamano, &adj,
		&m.Descartados, &m.ClienteCUIT, &m.Estado, &m.Motivo, &m.LoteID, &m.Creado, &m.Resuelto, &m.ResueltoPor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoEncontrado
		}
		return nil, err
	}
	_ = json.Unmarshal(adj, &m.Adjuntos)
	if m.Adjuntos == nil {
		m.Adjuntos = []AdjuntoInfo{}
	}
	if m.Descartados == nil {
		m.Descartados = []string{}
	}
	return &m, nil
}

// Correo trae un mail guardado.
func (s *Store) Correo(ctx context.Context, id int64) (*MensajeCorreo, error) {
	return scanCorreo(s.Pool.QueryRow(ctx, selCorreo+` WHERE id=$1`, id))
}

// CorreosRetenidos lista los retenidos, del más nuevo al más viejo. alcance: CUITs de cliente
// visibles (nil = todos; con alcance, los que no tienen cliente no se ven).
func (s *Store) CorreosRetenidos(ctx context.Context, alcance []string, limite, offset int) ([]MensajeCorreo, int, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	rows, err := s.Pool.Query(ctx, selCorreo+` WHERE estado='retenido' AND ($1::text[] IS NULL OR cliente_cuit = ANY($1)) ORDER BY id DESC LIMIT $2 OFFSET $3`, alcance, limite, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := []MensajeCorreo{}
	for rows.Next() {
		m, err := scanCorreo(rows)
		if err != nil {
			return nil, 0, err
		}
		res = append(res, *m)
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM correo_mensajes WHERE estado='retenido' AND ($1::text[] IS NULL OR cliente_cuit = ANY($1))`, alcance).Scan(&total); err != nil {
		return nil, 0, err
	}
	return res, total, rows.Err()
}

// AdjuntosRetenidos trae los adjuntos sellados de un retenido.
func (s *Store) AdjuntosRetenidos(ctx context.Context, id int64) ([]AdjuntoSellado, error) {
	rows, err := s.Pool.Query(ctx, `SELECT nombre, mime, datos FROM correo_adjuntos WHERE mensaje_id=$1 ORDER BY orden`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []AdjuntoSellado
	for rows.Next() {
		var a AdjuntoSellado
		if err := rows.Scan(&a.Nombre, &a.Mime, &a.Datos); err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

// TomarRetenido pasa un retenido a «recibiendo» para aceptarlo (si dos admins tocan a la vez, uno
// solo gana). Devuelve ErrNoRetenido si ya no estaba retenido.
func (s *Store) TomarRetenido(ctx context.Context, id int64) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE correo_mensajes SET estado='recibiendo' WHERE id=$1 AND estado='retenido'`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNoRetenido
	}
	return err
}

// DevolverRetenido lo vuelve a retenido (si aceptarlo falló a mitad de camino).
func (s *Store) DevolverRetenido(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE correo_mensajes SET estado='retenido' WHERE id=$1 AND estado='recibiendo'`, id)
	return err
}

// ResolverRetenido lo deja aceptado (con su lote) o descartado, y borra sus adjuntos.
func (s *Store) ResolverRetenido(ctx context.Context, id int64, estado, quien string, lote int64) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var l any
		if lote > 0 {
			l = lote
		}
		ct, err := tx.Exec(ctx, `UPDATE correo_mensajes SET estado=$2, resuelto=now(), resuelto_por=$3, lote_id=coalesce($4, lote_id)
			WHERE id=$1 AND estado IN ('retenido','recibiendo')`, id, estado, quien, l)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNoRetenido
		}
		_, err = tx.Exec(ctx, `DELETE FROM correo_adjuntos WHERE mensaje_id=$1`, id)
		return err
	})
}

// LimpiarCorreo borra los retenidos de más de 30 días (con sus adjuntos), los mails de más de un
// año, los links de cuenta vencidos y los avisos viejos. Devuelve cuántos retenidos borró.
func (s *Store) LimpiarCorreo(ctx context.Context) (int64, error) {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM correo_mensajes WHERE estado='retenido' AND creado < now() - interval '30 days'`)
	if err != nil {
		return 0, err
	}
	for _, q := range []string{
		`DELETE FROM correo_mensajes WHERE creado < now() - interval '400 days'`,
		`DELETE FROM tokens_cuenta WHERE vence < now() - interval '1 day'`,
		`DELETE FROM avisos_enviados WHERE creado < now() - interval '90 days'`,
	} {
		if _, err := s.Pool.Exec(ctx, q); err != nil {
			return ct.RowsAffected(), err
		}
	}
	return ct.RowsAffected(), nil
}

// ---------- Avisos ----------

// AvisoEnviado dice si ese aviso ya se mandó.
func (s *Store) AvisoEnviado(ctx context.Context, clave string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM avisos_enviados WHERE clave=$1)`, clave).Scan(&ok)
	return ok, err
}

// MarcarAviso anota que el aviso se mandó.
func (s *Store) MarcarAviso(ctx context.Context, clave string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO avisos_enviados (clave) VALUES ($1) ON CONFLICT DO NOTHING`, clave)
	return err
}

// Admins son las personas activas de rol admin o superadmin.
func (s *Store) Admins(ctx context.Context) ([]Usuario, error) {
	rows, err := s.Pool.Query(ctx, selUsuario+` WHERE activo AND rol IN ('admin','superadmin') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Usuario
	for rows.Next() {
		u, err := scanUsuario(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *u)
	}
	return res, rows.Err()
}

// ResumenPeriodo es lo que cuenta el resumen semanal.
type ResumenPeriodo struct {
	Lotes      int
	PorEstado  map[string]int
	Apocrifos  int
	Pendientes int
	PorCliente []ClientePeriodo
}

// ClientePeriodo es una fila del resumen por cliente.
type ClientePeriodo struct {
	Cliente    string
	Total      int
	Pendientes int
}

// Periodo cuenta lo cargado entre desde y hasta, y lo pendiente de revisar (de cualquier fecha),
// con el alcance de quien lo va a recibir (nil = todos).
func (s *Store) Periodo(ctx context.Context, desde, hasta time.Time, alcance []string) (*ResumenPeriodo, error) {
	r := &ResumenPeriodo{PorEstado: map[string]int{}}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM lotes WHERE creado >= $1 AND creado < $2 AND ($3::text[] IS NULL OR cliente_cuit = ANY($3))`,
		desde, hasta, alcance).Scan(&r.Lotes); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT estado, count(*), count(*) FILTER (WHERE hallazgos @> '[{"codigo":"emisor_apocrifo"}]') FROM comprobantes
		WHERE creado >= $1 AND creado < $2 AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) GROUP BY estado`, desde, hasta, alcance)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e string
		var n, a int
		if err := rows.Scan(&e, &n, &a); err != nil {
			rows.Close()
			return nil, err
		}
		r.PorEstado[e] = n
		r.Apocrifos += a
	}
	rows.Close()
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM comprobantes WHERE estado IN ('revisar','error') AND ($1::text[] IS NULL OR cliente_cuit = ANY($1))`,
		alcance).Scan(&r.Pendientes); err != nil {
		return nil, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT cliente_cuit, count(*) FILTER (WHERE creado >= $1 AND creado < $2), count(*) FILTER (WHERE estado IN ('revisar','error'))
		FROM comprobantes WHERE ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND ((creado >= $1 AND creado < $2) OR estado IN ('revisar','error'))
		GROUP BY cliente_cuit ORDER BY 2 DESC, 3 DESC LIMIT 30`, desde, hasta, alcance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ClientePeriodo
		if err := rows.Scan(&c.Cliente, &c.Total, &c.Pendientes); err != nil {
			return nil, err
		}
		r.PorCliente = append(r.PorCliente, c)
	}
	return r, rows.Err()
}

// ---------- Links de cuenta (restablecer e invitar) ----------

// Tipos de link de cuenta.
const (
	TokenRestablecer = "restablecer"
	TokenInvitacion  = "invitacion"
)

// CrearTokenCuenta guarda un link nuevo (el SHA-256 del token). Los anteriores del mismo tipo de
// esa persona dejan de servir.
func (s *Store) CrearTokenCuenta(ctx context.Context, hash string, usuario int64, tipo string, vence time.Time, ip string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE tokens_cuenta SET usado=now() WHERE usuario_id=$1 AND tipo=$2 AND usado IS NULL`, usuario, tipo); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO tokens_cuenta (hash, usuario_id, tipo, vence, ip) VALUES ($1,$2,$3,$4,$5)`, hash, usuario, tipo, vence, ip)
		return err
	})
}

// ContarTokensRecientes cuenta los links pedidos para una persona en la última hora.
func (s *Store) ContarTokensRecientes(ctx context.Context, usuario int64, tipo string) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM tokens_cuenta WHERE usuario_id=$1 AND tipo=$2 AND creado > now() - interval '1 hour'`, usuario, tipo).Scan(&n)
	return n, err
}

// TokenCuenta devuelve la persona y el tipo de un link vigente (sin usarlo).
func (s *Store) TokenCuenta(ctx context.Context, hash string, ahora time.Time) (*Usuario, string, error) {
	var uid int64
	var tipo string
	err := s.Pool.QueryRow(ctx, `SELECT usuario_id, tipo FROM tokens_cuenta WHERE hash=$1 AND usado IS NULL AND vence > $2`, hash, ahora).Scan(&uid, &tipo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrPaseInvalido
	}
	if err != nil {
		return nil, "", err
	}
	u, err := s.Usuario(ctx, uid)
	if err != nil {
		return nil, "", err
	}
	if !u.Activo {
		return nil, "", ErrPaseInvalido
	}
	return u, tipo, nil
}

// CanjearTokenCuenta usa el link: pone la contraseña nueva, cierra todas las sesiones de esa
// persona y deja sin efecto sus otros links. Todo junto o nada.
func (s *Store) CanjearTokenCuenta(ctx context.Context, hash, claveHash string, ahora time.Time) (*Usuario, string, error) {
	var u *Usuario
	var tipo string
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var uid int64
		err := tx.QueryRow(ctx, `UPDATE tokens_cuenta SET usado=now() WHERE hash=$1 AND usado IS NULL AND vence > $2 RETURNING usuario_id, tipo`, hash, ahora).Scan(&uid, &tipo)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPaseInvalido
		}
		if err != nil {
			return err
		}
		u, err = scanUsuario(tx.QueryRow(ctx, `UPDATE usuarios SET clave_hash=$2, debe_cambiar=false, actualizado=now() WHERE id=$1 AND activo
			RETURNING `+colsUsuario, uid, claveHash))
		if errors.Is(err, ErrNoEncontrado) {
			return ErrPaseInvalido
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tokens_cuenta SET usado=now() WHERE usuario_id=$1 AND usado IS NULL`, uid); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sesiones WHERE usuario_id=$1`, uid)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return u, tipo, nil
}
