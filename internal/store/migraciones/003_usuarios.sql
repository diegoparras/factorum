-- Fase 1: usuarios, sesiones, claves de API, configuración desde la pantalla, auditoría y el pase
-- entre apps de la suite. Ningún secreto se guarda en claro: contraseñas con argon2id, tokens por
-- su SHA-256 y la configuración secreta cifrada con FACTORUM_MASTER_KEY.

CREATE TABLE usuarios (
  id             bigserial PRIMARY KEY,
  email          text NOT NULL,
  nombre         text NOT NULL DEFAULT '',
  rol            text NOT NULL CHECK (rol IN ('lectura','usuario','admin','superadmin')),
  cuits          text[],                     -- CUITs de cliente permitidos; NULL = todos
  clave_hash     text,                       -- argon2id; NULL = no entra con contraseña (llegó por la suite)
  debe_cambiar   boolean NOT NULL DEFAULT false,
  activo         boolean NOT NULL DEFAULT true,
  origen         text NOT NULL DEFAULT 'local' CHECK (origen IN ('local','arcanum','lockatus')),
  creado         timestamptz NOT NULL DEFAULT now(),
  actualizado    timestamptz NOT NULL DEFAULT now(),
  ultimo_ingreso timestamptz
);
CREATE UNIQUE INDEX usuarios_email ON usuarios (lower(email));

-- Sesiones del navegador. El id es el SHA-256 del token de la cookie.
CREATE TABLE sesiones (
  id          text PRIMARY KEY,
  usuario_id  bigint NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  creado      timestamptz NOT NULL DEFAULT now(),
  vence       timestamptz NOT NULL,
  ultimo_uso  timestamptz NOT NULL DEFAULT now(),
  ip          text NOT NULL DEFAULT '',
  user_agent  text NOT NULL DEFAULT '',
  origen      text NOT NULL DEFAULT 'local',  -- local | suite:arcanum
  rol_tope    text,                           -- una sesión abierta con un pase no pasa de este rol
  cuits       text[]                          -- ni de estos CUITs (NULL = los de la persona)
);
CREATE INDEX sesiones_usuario ON sesiones (usuario_id);
CREATE INDEX sesiones_vence ON sesiones (vence);

-- Claves de API (fct_...). Se guarda solo el SHA-256; el prefijo es para reconocerla.
CREATE TABLE claves_api (
  id          bigserial PRIMARY KEY,
  nombre      text NOT NULL,
  prefijo     text NOT NULL,
  hash        text NOT NULL UNIQUE,
  rol         text NOT NULL CHECK (rol IN ('lectura','usuario','admin','superadmin')),
  cuits       text[],
  usuario_id  bigint REFERENCES usuarios(id) ON DELETE SET NULL,  -- quién la creó (su dueña)
  creado      timestamptz NOT NULL DEFAULT now(),
  vence       timestamptz,
  ultimo_uso  timestamptz,
  revocada    timestamptz,
  suite       boolean NOT NULL DEFAULT false,  -- puede abrir sesiones de la suite (POST /api/suite/pase)
  suite_tope  text NOT NULL DEFAULT 'usuario' CHECK (suite_tope IN ('lectura','usuario','admin','superadmin'))
);

-- Configuración hecha desde la pantalla. Lo que es secreto va cifrado en "secreto" y nunca en "valor".
CREATE TABLE configuracion (
  clave           text PRIMARY KEY,
  valor           jsonb,
  secreto         bytea,
  actualizado_por text NOT NULL DEFAULT '',
  actualizado     timestamptz NOT NULL DEFAULT now()
);

-- Auditoría: quién hizo qué y cuándo. Nunca lleva contraseñas, tokens, claves ni pases.
CREATE TABLE auditoria (
  id          bigserial PRIMARY KEY,
  creado      timestamptz NOT NULL DEFAULT now(),
  actor       text NOT NULL DEFAULT '',
  actor_tipo  text NOT NULL DEFAULT '',
  usuario_id  bigint,
  accion      text NOT NULL,
  objeto      text NOT NULL DEFAULT '',
  detalle     jsonb NOT NULL DEFAULT '{}',
  ip          text NOT NULL DEFAULT ''
);
CREATE INDEX auditoria_creado ON auditoria (creado DESC);
CREATE INDEX auditoria_accion ON auditoria (accion, creado DESC);
CREATE INDEX auditoria_actor ON auditoria (lower(actor), creado DESC);

-- Pases de la suite: un solo uso, 60 segundos. Se guarda el SHA-256 del token.
CREATE TABLE pases_suite (
  hash        text PRIMARY KEY,
  usuario_id  bigint NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  app         text NOT NULL,
  rol         text NOT NULL,
  cuits       text[],
  clave_id    bigint REFERENCES claves_api(id) ON DELETE SET NULL,
  creado      timestamptz NOT NULL DEFAULT now(),
  vence       timestamptz NOT NULL,
  usado       timestamptz
);
CREATE INDEX pases_vence ON pases_suite (vence);
