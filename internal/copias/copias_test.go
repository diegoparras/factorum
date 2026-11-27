package copias_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/canal/whatsapp"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/pruebas"
	"github.com/diegoparras/factorum/internal/store"
)

func sello(t *testing.T, b byte) *almacen.Almacen {
	t.Helper()
	a, err := almacen.Nuevo(nil, bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// base nueva con el esquema completo de Factorum (migraciones, cola y WhatsApp).
func baseLista(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Abrir(ctx, pruebas.BaseNueva(t), 6)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Cerrar)
	mig, err := rivermigrate.New(riverpgxv5.New(st.Pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	return st
}

func cargarDatos(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if err := whatsapp.PrepararTablas(ctx, stdlib.OpenDBFromPool(p)); err != nil {
		t.Fatal(err)
	}
	raro := "tab\tsalto\nbarra\\ comillas \" ' ñandú € \x01"
	sqls := []string{
		`INSERT INTO lotes (nombre, cliente_cuit, origen) VALUES ('Noviembre', '20222222223', 'web'), ($1, '', 'whatsapp')`,
		`INSERT INTO documentos (lote_id, nombre, tamano, sha256, estado) VALUES (1, 'a.pdf', 10, 'aa', 'listo'), (1, 'b.pdf', 11, 'bb', 'pendiente'), (2, $1, 12, 'cc', 'repetido')`,
		`UPDATE documentos SET repetido_de=1, detalle='{"x": [1, 2, {"y": null}]}' WHERE id=3`,
		`INSERT INTO comprobantes (documento_id, lote_id, datos, estado, total, fecha) VALUES (1, 1, '{"tipo": 1, "razon": "ACME \"SA\""}', 'ok', 12100, '2026-11-01'), (3, 2, $1::jsonb, 'revisar', NULL, NULL)`,
		`UPDATE comprobantes SET duplicado_de=1 WHERE id=2`,
		`INSERT INTO correcciones (comprobante_id, campo, antes, despues, usuario) VALUES (1, 'total', '1', $1, 'diego')`,
		`INSERT INTO usuarios (email, nombre, rol, cuits, clave_hash) VALUES ('diego@ejemplo.com', 'Diego', 'superadmin', NULL, '$argon2id$x'), ('ana@ejemplo.com', $1, 'lectura', ARRAY['20222222223','30711111111'], NULL)`,
		`INSERT INTO claves_api (nombre, prefijo, hash, rol, usuario_id) VALUES ('arcanum', 'fct_ab', 'hash1', 'usuario', 1)`,
		`INSERT INTO configuracion (clave, valor, secreto) VALUES ('general.nombre', '"Estudio"', NULL), ('arcanum.api_key', NULL, '\x0001ff'::bytea)`,
		`INSERT INTO auditoria (actor, accion, objeto, detalle) VALUES ('diego', 'sesion.entrar', '', '{}'), ('diego', 'config.cambiar', $1, '{"claves": ["a"]}')`,
		`INSERT INTO rubros_cuit (cuit_emisor, rubro) VALUES ('30711111111', 'supermercado')`,
		`INSERT INTO whatsmeow_device (jid, lid, registration_id, noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig, adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig, platform, business_name, push_name)
			VALUES ('549@s.whatsapp.net', '', 1, decode(repeat('01',32),'hex'), decode(repeat('02',32),'hex'), decode(repeat('03',32),'hex'), 1, decode(repeat('04',64),'hex'), '\x05', '\x06', decode(repeat('07',64),'hex'), decode(repeat('08',32),'hex'), decode(repeat('09',64),'hex'), '', '', '')`,
		// Lo que no se copia: el registro de copias y la cola.
		`INSERT INTO copias (estado, destino) VALUES ('error', 'disco')`,
		`DELETE FROM lotes WHERE false`,
	}
	for i, q := range sqls {
		var args []any
		switch {
		case strings.Contains(q, "$1::jsonb"):
			args = []any{`{"nota": "x\ty \\ \"ñ\" \n"}`}
		case strings.Contains(q, "$1"):
			args = []any{raro}
		}
		if _, err := p.Exec(ctx, q, args...); err != nil {
			t.Fatalf("dato %d: %v", i, err)
		}
	}
	// Un hueco en la secuencia: la restaurada tiene que seguir desde 10, no desde 3.
	if _, err := p.Exec(ctx, `SELECT setval(pg_get_serial_sequence('lotes','id'), 10)`); err != nil {
		t.Fatal(err)
	}
}

// huellas: filas y un hash de cada tabla de datos.
func huellas(t *testing.T, p *pgxpool.Pool) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := p.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	nombres, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	res := map[string]string{}
	for _, n := range nombres {
		if copias.Excluida(n) {
			continue
		}
		var h string
		var c int64
		id := pgx.Identifier{n}.Sanitize()
		if err := p.QueryRow(ctx, `SELECT count(*), md5(coalesce(string_agg(t::text, E'\n' ORDER BY t::text), '')) FROM `+id+` t`).Scan(&c, &h); err != nil {
			t.Fatal(err)
		}
		res[n] = h + "/" + string(rune('0'+min(c, 9)))
	}
	return res
}

func volcar(t *testing.T, p *pgxpool.Pool, s *almacen.Almacen) ([]byte, *copias.Manifiesto) {
	t.Helper()
	var buf bytes.Buffer
	m, err := copias.Volcar(context.Background(), p, s, &buf, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), m
}

func abrirBytes(b []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

func migraciones(t *testing.T, p *pgxpool.Pool) []string {
	rows, err := p.Query(context.Background(), `SELECT nombre FROM factorum_migraciones ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	l, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestIdaYVueltaEnBaseVacia(t *testing.T) {
	ctx := context.Background()
	s := sello(t, 9)
	origen := baseLista(t)
	cargarDatos(t, origen.Pool)
	antes := huellas(t, origen.Pool)

	copia, m := volcar(t, origen.Pool, s)
	if m.Filas == 0 || len(m.Tablas) < 10 || m.Factorum != config.Version || len(m.Esquema.Migraciones) == 0 || m.Esquema.River == 0 || m.Esquema.WhatsApp == 0 {
		t.Fatalf("manifiesto: %+v", m.Esquema)
	}
	if bytes.Contains(copia, []byte("diego@ejemplo.com")) || bytes.Contains(copia, []byte("Noviembre")) {
		t.Fatal("la copia tiene datos en claro")
	}
	// Orden de las claves foráneas: lotes antes de documentos y de comprobantes.
	pos := map[string]int{}
	for i, n := range m.NombresTablas() {
		pos[n] = i
	}
	if !(pos["lotes"] < pos["documentos"] && pos["documentos"] < pos["comprobantes"] && pos["usuarios"] < pos["claves_api"]) {
		t.Fatalf("orden: %v", m.NombresTablas())
	}
	for _, n := range m.NombresTablas() {
		if copias.Excluida(n) {
			t.Fatalf("se copió %s", n)
		}
	}

	// Una base nueva, vacía de todo: ni las tablas de WhatsApp.
	destino := baseLista(t)
	var whats bool
	_ = destino.Pool.QueryRow(ctx, `SELECT to_regclass('whatsmeow_device') IS NOT NULL`).Scan(&whats)
	if whats {
		t.Fatal("la base nueva no tenía que tener las tablas de WhatsApp")
	}
	// Sin confirmar, solo verifica.
	inf, err := copias.Restaurar(ctx, destino.Pool, abrirBytes(copia), s, copias.OpcionesRestaurar{Migraciones: migraciones(t, destino.Pool)})
	if err != nil || inf.Restaurada {
		t.Fatalf("sin confirmar: %v %+v", err, inf)
	}
	var lotes int
	_ = destino.Pool.QueryRow(ctx, `SELECT count(*) FROM lotes`).Scan(&lotes)
	if lotes != 0 {
		t.Fatal("sin confirmar no se toca la base")
	}
	inf, err = copias.Restaurar(ctx, destino.Pool, abrirBytes(copia), s, copias.OpcionesRestaurar{
		Confirmar: true, Migraciones: migraciones(t, destino.Pool),
		PrepararTablas: func(ctx context.Context, faltan []string) error {
			return whatsapp.PrepararTablas(ctx, stdlib.OpenDBFromPool(destino.Pool))
		},
		Despues: func(ctx context.Context, tx pgx.Tx) (string, error) {
			var n int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM documentos WHERE estado='pendiente'`).Scan(&n)
			return "pendientes: " + string(rune('0'+n)), err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inf.Restaurada || inf.Despues != "pendientes: 1" {
		t.Fatalf("informe: %+v", inf)
	}
	despues := huellas(t, destino.Pool)
	for n, h := range antes {
		if despues[n] != h {
			t.Errorf("la tabla %s no quedó igual", n)
		}
	}
	if len(despues) != len(antes) {
		t.Fatalf("tablas: %d antes, %d después", len(antes), len(despues))
	}
	// Las secuencias siguen desde donde estaban.
	var id int64
	if err := destino.Pool.QueryRow(ctx, `INSERT INTO lotes (nombre) VALUES ('nuevo') RETURNING id`).Scan(&id); err != nil || id != 11 {
		t.Fatalf("nuevo lote: %d %v", id, err)
	}
	if err := destino.Pool.QueryRow(ctx, `INSERT INTO usuarios (email, rol) VALUES ('nuevo@ejemplo.com', 'lectura') RETURNING id`).Scan(&id); err != nil || id != 3 {
		t.Fatalf("nueva persona: %d %v", id, err)
	}
	// El registro de copias no viaja.
	var nc int
	_ = destino.Pool.QueryRow(ctx, `SELECT count(*) FROM copias`).Scan(&nc)
	if nc != 0 {
		t.Fatal("el registro de copias no se restaura")
	}

	// Restaurar encima de una base con otros datos: queda igual a la copia (y la cola, vacía).
	if _, err := destino.Pool.Exec(ctx, `INSERT INTO river_job (kind, args, queue, state, max_attempts, priority) VALUES ('procesar_documento', '{"documento_id": 99}', 'default', 'available', 5, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := copias.Restaurar(ctx, destino.Pool, abrirBytes(copia), s, copias.OpcionesRestaurar{Confirmar: true, Migraciones: migraciones(t, destino.Pool)}); err != nil {
		t.Fatal(err)
	}
	otra := huellas(t, destino.Pool)
	for n, h := range antes {
		if otra[n] != h {
			t.Errorf("encima: la tabla %s no quedó igual", n)
		}
	}
	var trabajos int
	_ = destino.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&trabajos)
	if trabajos != 0 {
		t.Fatal("los trabajos de la base de antes no pueden quedar")
	}
}

func TestCopiaRotaNoSeRestaura(t *testing.T) {
	ctx := context.Background()
	s := sello(t, 9)
	origen := baseLista(t)
	cargarDatos(t, origen.Pool)
	copia, _ := volcar(t, origen.Pool, s)
	antes := huellas(t, origen.Pool)
	opc := copias.OpcionesRestaurar{Confirmar: true}

	if _, err := copias.Restaurar(ctx, origen.Pool, abrirBytes(copia), sello(t, 8), opc); err == nil {
		t.Fatal("con otra clave no se puede")
	}
	rota := bytes.Clone(copia)
	rota[len(rota)/2] ^= 1
	if _, err := copias.Restaurar(ctx, origen.Pool, abrirBytes(rota), s, opc); !errors.Is(err, almacen.ErrFlujoRoto) {
		t.Fatalf("rota: %v", err)
	}
	if _, err := copias.Restaurar(ctx, origen.Pool, abrirBytes(copia[:len(copia)-100]), s, opc); err == nil {
		t.Fatal("cortada no se puede")
	}
	// De un Factorum más nuevo (con una migración que este no conoce).
	if _, err := copias.Restaurar(ctx, origen.Pool, abrirBytes(copia), s, copias.OpcionesRestaurar{Confirmar: true, Migraciones: []string{"001_inicial.sql"}}); err == nil || !strings.Contains(err.Error(), "más nuevo") {
		t.Fatalf("más nueva: %v", err)
	}
	// Nada de eso tocó la base.
	despues := huellas(t, origen.Pool)
	for n, h := range antes {
		if despues[n] != h {
			t.Fatalf("una copia rechazada tocó %s", n)
		}
	}
}

func servicio(t *testing.T, st *store.Store, r2 *almacen.S3) (*copias.Servicio, *config.Config) {
	cfg := &config.Config{CopiasActivas: true, CopiasHora: "03:30", CopiasDiarias: 3, CopiasMensuales: 2}
	return &copias.Servicio{Pool: st.Pool, Sello: sello(t, 4), Config: func() *config.Config { return cfg }, Dir: t.TempDir(),
		R2: func() *almacen.S3 { return r2 }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, cfg
}

func TestHacerAlDiscoYAR2(t *testing.T) {
	ctx := context.Background()
	st := baseLista(t)
	cargarDatos(t, st.Pool)
	s, _ := servicio(t, st, nil)
	var avisos []copias.Copia
	s.AlTerminar(func(_ context.Context, c copias.Copia) { avisos = append(avisos, c) })
	var auditadas []string
	s.Auditar = func(_ context.Context, accion, _, actor string, _ map[string]any) {
		auditadas = append(auditadas, accion+":"+actor)
	}

	c, err := s.Hacer(ctx, "diego@ejemplo.com", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Estado != copias.OK || c.Destino != copias.DestinoDisco || !c.Disponible || *c.Filas == 0 || *c.Tamano == 0 || c.SHA256 == "" {
		t.Fatalf("copia al disco: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, filepath.Base(c.Clave))); err != nil {
		t.Fatal(err)
	}
	if len(avisos) != 1 || avisos[0].Estado != copias.OK || len(auditadas) != 1 || auditadas[0] != "copia.ok:diego@ejemplo.com" {
		t.Fatalf("avisos %v, auditoría %v", avisos, auditadas)
	}
	// Lo que se descarga es la copia cifrada, y se restaura.
	r, n, _, err := s.Abrir(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if int64(len(b)) != n || n != *c.Tamano {
		t.Fatal("tamaño de la descarga")
	}
	if m, err := copias.Leer(bytes.NewReader(b), s.Sello); err != nil || m.Filas != *c.Filas {
		t.Fatalf("la descargada no se lee: %v", err)
	}

	// Con R2: va al bucket.
	falso := pruebas.S3Falso(t)
	r2, _ := almacen.NuevoS3(falso.URL, falso.Bucket, falso.Access, falso.Secret, "auto", "fct/")
	s.R2 = func() *almacen.S3 { return r2 }
	c2, err := s.Hacer(ctx, "", false, "2026-11-27")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Destino != copias.DestinoR2 || !c2.Disponible || c2.Programada != "2026-11-27" {
		t.Fatalf("copia a R2: %+v", c2)
	}
	if ks := falso.Claves(t); len(ks) != 1 || !strings.HasPrefix(ks[0], "fct/copias/factorum-") {
		t.Fatalf("en el bucket: %v", ks)
	}
	r, _, _, err = s.Abrir(ctx, c2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copias.Leer(r, s.Sello); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if auditadas[1] != "copia.ok:copia diaria" {
		t.Fatalf("auditoría de la diaria: %v", auditadas)
	}

	// Falla (sin la clave maestra): queda registrada con error, se audita y se avisa.
	s.Sello, _ = almacen.Nuevo(nil, nil)
	if _, err := s.Hacer(ctx, "diego@ejemplo.com", true, ""); err == nil {
		t.Fatal("sin clave no hay copia")
	}
	u, err := s.Ultima(ctx)
	if err != nil || u == nil || u.Estado != copias.Error || !strings.Contains(u.Error, "MASTER_KEY") {
		t.Fatalf("última: %+v %v", u, err)
	}
	if avisos[len(avisos)-1].Estado != copias.Error || auditadas[len(auditadas)-1] != "copia.error:diego@ejemplo.com" {
		t.Fatalf("avisos %v, auditoría %v", avisos, auditadas)
	}
	// Restaurar en una base nueva: el registro se rearma con lo que hay en los destinos.
	if _, err := st.Pool.Exec(ctx, `DELETE FROM copias`); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lista(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || !l[0].Disponible || !l[1].Disponible {
		t.Fatalf("lista rearmada: %+v", l)
	}
}

func TestPodarYHoraDeLaCopia(t *testing.T) {
	ctx := context.Background()
	st := baseLista(t)
	s, cfg := servicio(t, st, nil)
	// Copias de mentira: una por día durante 100 días.
	base := time.Date(2026, 8, 1, 3, 30, 0, 0, copias.Zona)
	for i := range 100 {
		dia := base.AddDate(0, 0, i)
		nom := "factorum-" + dia.Format("20060102-150405") + copias.Extension
		if err := os.WriteFile(filepath.Join(s.Dir, nom), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Pool.Exec(ctx, `INSERT INTO copias (estado, destino, clave, creado) VALUES ('ok','disco',$1,$2)`, "copias/"+nom, dia); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Podar(ctx); err != nil {
		t.Fatal(err)
	}
	l, err := s.Lista(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var quedan []string
	for _, c := range l {
		quedan = append(quedan, c.Creado.In(copias.Zona).Format("01-02"))
	}
	// Las 3 últimas (7, 8 y 6 de noviembre) y la primera de los 2 últimos meses (1/11 y 1/10).
	if strings.Join(quedan, " ") != "11-08 11-07 11-06 11-01 10-01" {
		t.Fatalf("quedaron: %v", quedan)
	}
	ents, _ := os.ReadDir(s.Dir)
	if len(ents) != 5 {
		t.Fatalf("en el disco quedaron %d", len(ents))
	}

	// La hora de la copia diaria.
	s.Ahora = func() time.Time { return time.Date(2026, 11, 27, 3, 29, 0, 0, copias.Zona) }
	if _, toca, _ := s.HoyToca(ctx); toca {
		t.Fatal("antes de la hora no toca")
	}
	s.Ahora = func() time.Time { return time.Date(2026, 11, 27, 9, 0, 0, 0, copias.Zona) }
	dia, toca, err := s.HoyToca(ctx)
	if err != nil || !toca || dia != "2026-11-27" {
		t.Fatalf("después de la hora (servidor apagado a las 3:30): %s %v %v", dia, toca, err)
	}
	var encoladas []copias.Args
	s.Encolar = func(_ context.Context, a copias.Args, o *river.InsertOpts) error {
		if o == nil || !o.UniqueOpts.ByArgs {
			t.Fatal("la diaria tiene que ser única por día")
		}
		encoladas = append(encoladas, a)
		return nil
	}
	pctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	s.Programar(pctx)
	if len(encoladas) != 1 || encoladas[0].Programada != "2026-11-27" {
		t.Fatalf("encoladas: %+v", encoladas)
	}
	if _, err := st.Pool.Exec(ctx, `INSERT INTO copias (estado, destino, programada) VALUES ('error','disco','2026-11-27')`); err != nil {
		t.Fatal(err)
	}
	if _, toca, _ := s.HoyToca(ctx); toca {
		t.Fatal("ya se intentó hoy")
	}
	cfg.CopiasActivas = false
	s.Ahora = func() time.Time { return time.Date(2026, 11, 28, 9, 0, 0, 0, copias.Zona) }
	if _, toca, _ := s.HoyToca(ctx); toca {
		t.Fatal("apagadas no toca")
	}
}
