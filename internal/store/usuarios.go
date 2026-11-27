package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/diegoparras/factorum/internal/auth"
)

// ErrEmailUsado: ya hay una persona con ese email.
var ErrEmailUsado = errors.New("ya hay una persona con ese email")

// ErrPaseInvalido: el pase no existe, venció o ya se usó.
var ErrPaseInvalido = errors.New("el pase venció o ya se usó")

// Usuario es una persona que entra a Factorum.
type Usuario struct {
	ID            int64      `json:"id"`
	Email         string     `json:"email"`
	Nombre        string     `json:"nombre"`
	Rol           auth.Rol   `json:"rol"`
	CUITs         []string   `json:"cuits"` // nil = todos
	ClaveHash     string     `json:"-"`
	TieneClave    bool       `json:"tiene_clave"`
	DebeCambiar   bool       `json:"debe_cambiar"`
	Activo        bool       `json:"activo"`
	Origen        string     `json:"origen"`
	Creado        time.Time  `json:"creado"`
	UltimoIngreso *time.Time `json:"ultimo_ingreso"`
}

// UsuarioNuevo es lo que hace falta para dar de alta a una persona.
type UsuarioNuevo struct {
	Email, Nombre, ClaveHash, Origen string
	Rol                              auth.Rol
	CUITs                            []string
	DebeCambiar                      bool
}

// cuitsDB pasa nil (todos) a NULL.
func itoa(n int) string { return strconv.Itoa(n) }

func cuitsDB(l []string) any {
	if l == nil {
		return nil
	}
	return l
}

// cuitsDeDB: NULL o vacío = todos (claves de API y pases).
func cuitsDeDB(l []string) []string {
	if len(l) == 0 {
		return nil
	}
	return l
}

// cuitsPersonaDeDB: los de una persona. NULL = todos; vacío = ninguno todavía (no ve nada hasta que
// un admin le asigne clientes).
func cuitsPersonaDeDB(l []string, nulo bool) []string {
	if nulo {
		return nil
	}
	if l == nil {
		return []string{}
	}
	return l
}

func rolDeDB(s string) auth.Rol {
	r, _ := auth.ParsearRol(s)
	return r
}

func esUnico(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

const colsUsuario = `id, email, nombre, rol, cuits, cuits IS NULL, coalesce(clave_hash,''), debe_cambiar, activo, origen, creado, ultimo_ingreso`

const selUsuario = `SELECT ` + colsUsuario + ` FROM usuarios`

func scanUsuario(r pgx.Row) (*Usuario, error) {
	var u Usuario
	var rol string
	var cuits []string
	var todos bool
	if err := r.Scan(&u.ID, &u.Email, &u.Nombre, &rol, &cuits, &todos, &u.ClaveHash, &u.DebeCambiar, &u.Activo, &u.Origen, &u.Creado, &u.UltimoIngreso); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoEncontrado
		}
		return nil, err
	}
	u.Rol, u.CUITs, u.TieneClave = rolDeDB(rol), cuitsPersonaDeDB(cuits, todos), u.ClaveHash != ""
	return &u, nil
}

// ContarUsuarios cuenta todas las personas (activas o no). Con cero, Factorum está por estrenar.
func (s *Store) ContarUsuarios(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM usuarios`).Scan(&n)
	return n, err
}

// ContarSuperadmins cuenta los superadmin activos.
func (s *Store) ContarSuperadmins(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM usuarios WHERE rol='superadmin' AND activo`).Scan(&n)
	return n, err
}

// CrearUsuario da de alta a una persona. El email no distingue mayúsculas.
func (s *Store) CrearUsuario(ctx context.Context, n UsuarioNuevo) (*Usuario, error) {
	if n.Origen == "" {
		n.Origen = "local"
	}
	var hash any
	if n.ClaveHash != "" {
		hash = n.ClaveHash
	}
	u, err := scanUsuario(s.Pool.QueryRow(ctx, `INSERT INTO usuarios (email, nombre, rol, cuits, clave_hash, debe_cambiar, origen)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+colsUsuario,
		strings.TrimSpace(n.Email), strings.TrimSpace(n.Nombre), n.Rol.String(), cuitsDB(n.CUITs), hash, n.DebeCambiar, n.Origen))
	if esUnico(err) {
		return nil, ErrEmailUsado
	}
	return u, err
}

// CrearPrimerSuperadmin da de alta al primer superadmin solo si todavía no hay nadie (dos pedidos a
// la vez no pueden crear dos).
func (s *Store) CrearPrimerSuperadmin(ctx context.Context, n UsuarioNuevo) (*Usuario, error) {
	var u *Usuario
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE usuarios IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		var hay bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM usuarios)`).Scan(&hay); err != nil {
			return err
		}
		if hay {
			return errors.New("ya hay personas cargadas: entrá con tu email y contraseña")
		}
		var err error
		u, err = scanUsuario(tx.QueryRow(ctx, `INSERT INTO usuarios (email, nombre, rol, clave_hash, origen) VALUES ($1,$2,'superadmin',$3,'local')
			RETURNING `+colsUsuario,
			strings.TrimSpace(n.Email), strings.TrimSpace(n.Nombre), n.ClaveHash))
		return err
	})
	return u, err
}

// UsuarioPorEmail busca a una persona por email, sin distinguir mayúsculas.
func (s *Store) UsuarioPorEmail(ctx context.Context, email string) (*Usuario, error) {
	return scanUsuario(s.Pool.QueryRow(ctx, selUsuario+` WHERE lower(email)=lower($1)`, strings.TrimSpace(email)))
}

// Usuario busca a una persona por id.
func (s *Store) Usuario(ctx context.Context, id int64) (*Usuario, error) {
	return scanUsuario(s.Pool.QueryRow(ctx, selUsuario+` WHERE id=$1`, id))
}

// Usuarios lista a todas las personas, por nombre.
func (s *Store) Usuarios(ctx context.Context) ([]Usuario, error) {
	rows, err := s.Pool.Query(ctx, selUsuario+` ORDER BY activo DESC, lower(nombre), lower(email)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Usuario{}
	for rows.Next() {
		u, err := scanUsuario(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *u)
	}
	return res, rows.Err()
}

// CambiosUsuario: lo que se cambia de una persona (nil = no se toca).
type CambiosUsuario struct {
	Nombre     *string
	Rol        *auth.Rol
	CUITs      *[]string // puntero a nil = todos
	Activo     *bool
	ClaveHash  *string
	DebeCambia *bool
}

// ActualizarUsuario aplica los cambios. Si la persona queda desactivada o le cambia la contraseña,
// se cierran sus sesiones (salvo "excepto", la sesión de quien cambió su propia contraseña).
func (s *Store) ActualizarUsuario(ctx context.Context, id int64, c CambiosUsuario, excepto string) (*Usuario, error) {
	var u *Usuario
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var rol, hash any
		if c.Rol != nil {
			rol = c.Rol.String()
		}
		if c.ClaveHash != nil {
			hash = *c.ClaveHash
		}
		var cuits any
		tocarCUITs := c.CUITs != nil
		if tocarCUITs {
			cuits = cuitsDB(*c.CUITs)
		}
		var err error
		u, err = scanUsuario(tx.QueryRow(ctx, `UPDATE usuarios SET
				nombre = coalesce($2, nombre), rol = coalesce($3, rol),
				cuits = CASE WHEN $4 THEN $5::text[] ELSE cuits END,
				activo = coalesce($6, activo), clave_hash = coalesce($7, clave_hash), debe_cambiar = coalesce($8, debe_cambiar),
				actualizado = now()
			WHERE id=$1 RETURNING `+colsUsuario,
			id, c.Nombre, rol, tocarCUITs, cuits, c.Activo, hash, c.DebeCambia))
		if err != nil {
			return err
		}
		if (c.Activo != nil && !*c.Activo) || c.ClaveHash != nil || c.Rol != nil || tocarCUITs {
			_, err = tx.Exec(ctx, `DELETE FROM sesiones WHERE usuario_id=$1 AND id <> $2`, id, excepto)
		}
		return err
	})
	return u, err
}

// MarcarIngreso anota la última entrada de una persona.
func (s *Store) MarcarIngreso(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE usuarios SET ultimo_ingreso=now() WHERE id=$1`, id)
	return err
}

// ---------- Sesiones ----------

// Sesion del navegador.
type Sesion struct {
	ID        string // hash del token
	UsuarioID int64
	Vence     time.Time
	UltimoUso time.Time
	IP        string
	UserAgent string
	Origen    string
	RolTope   auth.Rol // Ninguno = sin tope
	CUITs     []string // nil = los de la persona
	TieneCUIT bool     // la sesión trae su propio alcance de CUITs
}

// CrearSesion guarda una sesión nueva.
func (s *Store) CrearSesion(ctx context.Context, q Sesion) error {
	return crearSesion(ctx, s.Pool, q)
}

type ejecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func crearSesion(ctx context.Context, db ejecutor, q Sesion) error {
	if q.Origen == "" {
		q.Origen = "local"
	}
	var tope any
	if q.RolTope != auth.Ninguno {
		tope = q.RolTope.String()
	}
	var cuits any
	if q.TieneCUIT {
		cuits = q.CUITs
		if q.CUITs == nil {
			cuits = nil
		}
	}
	if len(q.UserAgent) > 300 {
		q.UserAgent = q.UserAgent[:300]
	}
	_, err := db.Exec(ctx, `INSERT INTO sesiones (id, usuario_id, vence, ip, user_agent, origen, rol_tope, cuits) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		q.ID, q.UsuarioID, q.Vence, q.IP, q.UserAgent, q.Origen, tope, cuits)
	return err
}

// SesionValida devuelve la sesión (no vencida) y su persona.
func (s *Store) SesionValida(ctx context.Context, id string, ahora time.Time) (*Sesion, *Usuario, error) {
	var q Sesion
	var tope *string
	var cuits []string
	var u Usuario
	var rol string
	var ucuits []string
	var todos bool
	err := s.Pool.QueryRow(ctx, `SELECT s.id, s.usuario_id, s.vence, s.ultimo_uso, s.ip, s.origen, s.rol_tope, s.cuits, s.cuits IS NOT NULL,
			u.id, u.email, u.nombre, u.rol, u.cuits, u.cuits IS NULL, u.clave_hash IS NOT NULL, u.debe_cambiar, u.activo, u.origen, u.creado, u.ultimo_ingreso
		FROM sesiones s JOIN usuarios u ON u.id = s.usuario_id WHERE s.id=$1 AND s.vence > $2`, id, ahora).
		Scan(&q.ID, &q.UsuarioID, &q.Vence, &q.UltimoUso, &q.IP, &q.Origen, &tope, &cuits, &q.TieneCUIT,
			&u.ID, &u.Email, &u.Nombre, &rol, &ucuits, &todos, &u.TieneClave, &u.DebeCambiar, &u.Activo, &u.Origen, &u.Creado, &u.UltimoIngreso)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, nil, err
	}
	if tope != nil {
		q.RolTope = rolDeDB(*tope)
	}
	q.CUITs = cuits
	if q.TieneCUIT && q.CUITs == nil {
		q.CUITs = []string{}
	}
	u.Rol, u.CUITs = rolDeDB(rol), cuitsPersonaDeDB(ucuits, todos)
	return &q, &u, nil
}

// TocarSesion corre el vencimiento (las sesiones vencen por inactividad).
func (s *Store) TocarSesion(ctx context.Context, id string, vence time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sesiones SET ultimo_uso=now(), vence=$2 WHERE id=$1`, id, vence)
	return err
}

// BorrarSesion cierra una sesión.
func (s *Store) BorrarSesion(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sesiones WHERE id=$1`, id)
	return err
}

// Limpiar borra sesiones y pases vencidos.
func (s *Store) Limpiar(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sesiones WHERE vence < now() - interval '1 day'`)
	if err == nil {
		_, err = s.Pool.Exec(ctx, `DELETE FROM pases_suite WHERE vence < now() - interval '1 day'`)
	}
	return err
}

// ---------- Claves de API ----------

// ClaveAPI es una clave guardada (sin la clave: solo su prefijo).
type ClaveAPI struct {
	ID        int64      `json:"id"`
	Nombre    string     `json:"nombre"`
	Prefijo   string     `json:"prefijo"`
	Rol       auth.Rol   `json:"rol"`
	CUITs     []string   `json:"cuits"`
	UsuarioID *int64     `json:"usuario_id"`
	Dueno     string     `json:"dueno"` // email de quien la creó
	Creado    time.Time  `json:"creado"`
	Vence     *time.Time `json:"vence"`
	UltimoUso *time.Time `json:"ultimo_uso"`
	Revocada  *time.Time `json:"revocada"`
	Suite     bool       `json:"suite"`
	SuiteTope auth.Rol   `json:"suite_tope"`

	// De la dueña, para recortar lo que puede la clave.
	DuenoActivo bool     `json:"-"`
	DuenoRol    auth.Rol `json:"-"`
	DuenoCUITs  []string `json:"-"`
}

// ClaveNueva es lo que hace falta para crear una clave.
type ClaveNueva struct {
	Nombre, Prefijo, Hash string
	Rol, SuiteTope        auth.Rol
	CUITs                 []string
	UsuarioID             int64
	Vence                 *time.Time
	Suite                 bool
}

const selClave = `SELECT k.id, k.nombre, k.prefijo, k.rol, k.cuits, k.usuario_id, coalesce(u.email,''), k.creado, k.vence, k.ultimo_uso, k.revocada,
	k.suite, k.suite_tope, coalesce(u.activo, true), coalesce(u.rol, ''), u.cuits, u.cuits IS NULL, u.id IS NOT NULL
	FROM claves_api k LEFT JOIN usuarios u ON u.id = k.usuario_id`

func scanClave(r pgx.Row) (*ClaveAPI, error) {
	var k ClaveAPI
	var rol, tope, urol string
	var cuits, ucuits []string
	var tieneDueno, duenoTodos bool
	if err := r.Scan(&k.ID, &k.Nombre, &k.Prefijo, &rol, &cuits, &k.UsuarioID, &k.Dueno, &k.Creado, &k.Vence, &k.UltimoUso, &k.Revocada,
		&k.Suite, &tope, &k.DuenoActivo, &urol, &ucuits, &duenoTodos, &tieneDueno); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoEncontrado
		}
		return nil, err
	}
	k.Rol, k.CUITs, k.SuiteTope = rolDeDB(rol), cuitsDeDB(cuits), rolDeDB(tope)
	k.DuenoRol, k.DuenoCUITs = auth.Superadmin, nil
	if tieneDueno {
		k.DuenoRol, k.DuenoCUITs = rolDeDB(urol), cuitsPersonaDeDB(ucuits, duenoTodos)
	}
	return &k, nil
}

// CrearClave guarda una clave nueva.
func (s *Store) CrearClave(ctx context.Context, n ClaveNueva) (*ClaveAPI, error) {
	var dueno any
	if n.UsuarioID > 0 {
		dueno = n.UsuarioID
	}
	if n.SuiteTope == auth.Ninguno {
		n.SuiteTope = auth.Usuario
	}
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO claves_api (nombre, prefijo, hash, rol, cuits, usuario_id, vence, suite, suite_tope)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		strings.TrimSpace(n.Nombre), n.Prefijo, n.Hash, n.Rol.String(), cuitsDB(n.CUITs), dueno, n.Vence, n.Suite, n.SuiteTope.String()).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Clave(ctx, id)
}

// Clave busca una clave por id.
func (s *Store) Clave(ctx context.Context, id int64) (*ClaveAPI, error) {
	return scanClave(s.Pool.QueryRow(ctx, selClave+` WHERE k.id=$1`, id))
}

// ClavePorHash busca una clave vigente (no revocada ni vencida) por el SHA-256 de la clave.
func (s *Store) ClavePorHash(ctx context.Context, hash string, ahora time.Time) (*ClaveAPI, error) {
	return scanClave(s.Pool.QueryRow(ctx, selClave+` WHERE k.hash=$1 AND k.revocada IS NULL AND (k.vence IS NULL OR k.vence > $2)`, hash, ahora))
}

// Claves lista todas las claves, las vigentes primero.
func (s *Store) Claves(ctx context.Context) ([]ClaveAPI, error) {
	rows, err := s.Pool.Query(ctx, selClave+` ORDER BY k.revocada IS NOT NULL, k.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []ClaveAPI{}
	for rows.Next() {
		k, err := scanClave(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *k)
	}
	return res, rows.Err()
}

// RevocarClave deja una clave sin efecto (no se borra: queda para la auditoría).
func (s *Store) RevocarClave(ctx context.Context, id int64) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE claves_api SET revocada=now() WHERE id=$1 AND revocada IS NULL`, id)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNoEncontrado
	}
	return err
}

// TocarClave anota el último uso (como mucho una vez por minuto).
func (s *Store) TocarClave(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE claves_api SET ultimo_uso=now() WHERE id=$1 AND (ultimo_uso IS NULL OR ultimo_uso < now() - interval '1 minute')`, id)
	return err
}

// ---------- Pases de la suite ----------

// Pase de la suite (solo el hash del token).
type Pase struct {
	Hash      string
	UsuarioID int64
	App       string
	Rol       auth.Rol
	CUITs     []string // nil = los de la persona sin recorte
	ClaveID   int64
	Vence     time.Time
}

// CrearPase guarda un pase nuevo.
func (s *Store) CrearPase(ctx context.Context, p Pase) error {
	var clave any
	if p.ClaveID > 0 {
		clave = p.ClaveID
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO pases_suite (hash, usuario_id, app, rol, cuits, clave_id, vence) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.Hash, p.UsuarioID, p.App, p.Rol.String(), cuitsDB(p.CUITs), clave, p.Vence)
	return err
}

// CanjearPase valida el pase, lo marca usado y abre la sesión, todo en la misma transacción. q trae
// el id, el vencimiento, la IP y el user agent de la sesión; el resto sale del pase.
func (s *Store) CanjearPase(ctx context.Context, hash string, q Sesion) (*Usuario, *Pase, error) {
	var u *Usuario
	var p Pase
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var rol string
		var cuits []string
		var tieneCUITs bool
		err := tx.QueryRow(ctx, `UPDATE pases_suite SET usado=now() WHERE hash=$1 AND usado IS NULL AND vence > now()
			RETURNING usuario_id, app, rol, cuits, cuits IS NOT NULL`, hash).Scan(&p.UsuarioID, &p.App, &rol, &cuits, &tieneCUITs)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPaseInvalido
		}
		if err != nil {
			return err
		}
		p.Hash, p.Rol = hash, rolDeDB(rol)
		if tieneCUITs {
			p.CUITs = cuits
			if p.CUITs == nil {
				p.CUITs = []string{}
			}
		}
		u, err = scanUsuario(tx.QueryRow(ctx, selUsuario+` WHERE id=$1 FOR UPDATE`, p.UsuarioID))
		if err != nil {
			return err
		}
		if !u.Activo {
			return ErrPaseInvalido
		}
		q.UsuarioID, q.Origen, q.RolTope = u.ID, "suite:"+p.App, p.Rol
		q.CUITs, q.TieneCUIT = p.CUITs, p.CUITs != nil
		if err := crearSesion(ctx, tx, q); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE usuarios SET ultimo_ingreso=now() WHERE id=$1`, u.ID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return u, &p, nil
}

// ---------- Configuración ----------

// FilaConfig es un valor de configuración guardado desde la pantalla.
type FilaConfig struct {
	Clave          string          `json:"clave"`
	Valor          json.RawMessage `json:"valor"`
	Secreto        []byte          `json:"-"` // cifrado
	ActualizadoPor string          `json:"actualizado_por"`
	Actualizado    time.Time       `json:"actualizado"`
}

// ---------- Aviso de la IA ----------

// AvisoIAVisto dice si la persona ya vio el aviso de la IA externa de ese host, en esa versión.
func (s *Store) AvisoIAVisto(ctx context.Context, usuario int64, host string, version int) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ia_avisos_vistos WHERE usuario_id=$1 AND host=$2 AND version=$3)`, usuario, host, version).Scan(&ok)
	return ok, err
}

// MarcarAvisoIAVisto anota que la persona vio el aviso de ese host y esa versión.
func (s *Store) MarcarAvisoIAVisto(ctx context.Context, usuario int64, host string, version int) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO ia_avisos_vistos (usuario_id, host, version) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, usuario, host, version)
	return err
}

// Configuracion trae todo lo guardado.
func (s *Store) Configuracion(ctx context.Context) (map[string]FilaConfig, error) {
	rows, err := s.Pool.Query(ctx, `SELECT clave, valor, secreto, actualizado_por, actualizado FROM configuracion`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]FilaConfig{}
	for rows.Next() {
		var f FilaConfig
		if err := rows.Scan(&f.Clave, &f.Valor, &f.Secreto, &f.ActualizadoPor, &f.Actualizado); err != nil {
			return nil, err
		}
		res[f.Clave] = f
	}
	return res, rows.Err()
}

// GuardarConfiguracion escribe (o borra, con Valor y Secreto vacíos) varios valores juntos.
func (s *Store) GuardarConfiguracion(ctx context.Context, filas []FilaConfig, quien string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, f := range filas {
			if len(f.Valor) == 0 && len(f.Secreto) == 0 {
				if _, err := tx.Exec(ctx, `DELETE FROM configuracion WHERE clave=$1`, f.Clave); err != nil {
					return err
				}
				continue
			}
			var valor, secreto any
			if len(f.Valor) > 0 {
				valor = []byte(f.Valor)
			}
			if len(f.Secreto) > 0 {
				secreto = f.Secreto
			}
			if _, err := tx.Exec(ctx, `INSERT INTO configuracion (clave, valor, secreto, actualizado_por, actualizado) VALUES ($1,$2,$3,$4,now())
				ON CONFLICT (clave) DO UPDATE SET valor=EXCLUDED.valor, secreto=EXCLUDED.secreto, actualizado_por=EXCLUDED.actualizado_por, actualizado=now()`,
				f.Clave, valor, secreto, quien); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- Auditoría ----------

// Evento de la auditoría.
type Evento struct {
	ID        int64          `json:"id"`
	Creado    time.Time      `json:"creado"`
	Actor     string         `json:"actor"`
	ActorTipo string         `json:"actor_tipo"`
	UsuarioID *int64         `json:"usuario_id"`
	Accion    string         `json:"accion"`
	Objeto    string         `json:"objeto"`
	Detalle   map[string]any `json:"detalle"`
	IP        string         `json:"ip"`
}

// Auditar anota un evento. Un error al anotar no corta lo que se estaba haciendo: se devuelve para
// que quien llama lo loguee.
func (s *Store) Auditar(ctx context.Context, e Evento) error {
	if e.Detalle == nil {
		e.Detalle = map[string]any{}
	}
	d, _ := json.Marshal(e.Detalle)
	_, err := s.Pool.Exec(ctx, `INSERT INTO auditoria (actor, actor_tipo, usuario_id, accion, objeto, detalle, ip) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.Actor, e.ActorTipo, e.UsuarioID, e.Accion, e.Objeto, d, e.IP)
	return err
}

// FiltroAuditoria para la pantalla.
type FiltroAuditoria struct {
	Desde, Hasta   string // AAAA-MM-DD, en hora de Buenos Aires
	Persona        string // parte del email o de la clave
	Accion         string // exacta o prefijo terminado en "." (sesion.)
	Limite, Offset int
}

// Auditoria lista eventos, del más nuevo al más viejo.
func (s *Store) Auditoria(ctx context.Context, f FiltroAuditoria) ([]Evento, int, error) {
	var w []string
	var a []any
	add := func(cond string, v any) {
		a = append(a, v)
		w = append(w, strings.ReplaceAll(cond, "?", "$"+itoa(len(a))))
	}
	const tz = `'America/Argentina/Buenos_Aires'`
	if f.Desde != "" {
		add("(creado AT TIME ZONE "+tz+")::date >= ?::date", f.Desde)
	}
	if f.Hasta != "" {
		add("(creado AT TIME ZONE "+tz+")::date <= ?::date", f.Hasta)
	}
	if p := strings.TrimSpace(f.Persona); p != "" {
		add("actor ILIKE '%' || ? || '%'", p)
	}
	if ac := strings.TrimSpace(f.Accion); ac != "" {
		if strings.HasSuffix(ac, ".") {
			add("accion LIKE ? || '%'", ac)
		} else {
			add("accion = ?", ac)
		}
	}
	where := ""
	if len(w) > 0 {
		where = " WHERE " + strings.Join(w, " AND ")
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM auditoria`+where, a...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 50
	}
	a = append(a, f.Limite, max(f.Offset, 0))
	rows, err := s.Pool.Query(ctx, `SELECT id, creado, actor, actor_tipo, usuario_id, accion, objeto, detalle, ip FROM auditoria`+where+
		` ORDER BY id DESC LIMIT $`+itoa(len(a)-1)+` OFFSET $`+itoa(len(a)), a...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := []Evento{}
	for rows.Next() {
		var e Evento
		var d []byte
		if err := rows.Scan(&e.ID, &e.Creado, &e.Actor, &e.ActorTipo, &e.UsuarioID, &e.Accion, &e.Objeto, &d, &e.IP); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(d, &e.Detalle)
		res = append(res, e)
	}
	return res, total, rows.Err()
}

// AccionesAuditadas lista las acciones que aparecen en la auditoría (para el filtro).
func (s *Store) AccionesAuditadas(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT accion FROM auditoria ORDER BY accion`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}
