-- Fase 2: el correo. Lo que entra por mail (sin el cuerpo: es dato de un tercero y no se guarda),
-- los retenidos hasta que un admin decida, los links de un solo uso para restablecer la contraseña
-- e invitar, y los avisos ya mandados (para no mandarlos dos veces).

-- Cada mail que llegó. Sirve para no procesar dos veces el mismo (por Message-ID) y para la lista
-- de retenidos. Nunca lleva el cuerpo del mail.
CREATE TABLE correo_mensajes (
  id           bigserial PRIMARY KEY,
  message_id   text NOT NULL,
  forma        text NOT NULL,                  -- imap, cloudflare, mailgun, postmark, sendgrid, gmail
  remitente    text NOT NULL DEFAULT '',       -- email del From, en minúsculas
  nombre       text NOT NULL DEFAULT '',
  destinatario text NOT NULL DEFAULT '',
  asunto       text NOT NULL DEFAULT '',
  fecha        timestamptz,
  referencias  text NOT NULL DEFAULT '',       -- References, para contestar en el mismo hilo
  tamano       bigint NOT NULL DEFAULT 0,
  adjuntos     jsonb NOT NULL DEFAULT '[]',    -- [{nombre, mime, tamano}]
  descartados  text[] NOT NULL DEFAULT '{}',
  cliente_cuit text NOT NULL DEFAULT '',
  estado       text NOT NULL CHECK (estado IN ('recibiendo','procesado','retenido','ignorado','aceptado','descartado')),
  motivo       text NOT NULL DEFAULT '',
  lote_id      bigint REFERENCES lotes(id) ON DELETE SET NULL,
  creado       timestamptz NOT NULL DEFAULT now(),
  resuelto     timestamptz,
  resuelto_por text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX correo_mensajes_mid ON correo_mensajes (message_id);
CREATE INDEX correo_mensajes_estado ON correo_mensajes (estado, creado DESC);

-- Los adjuntos de un retenido, comprimidos y cifrados con FACTORUM_MASTER_KEY, hasta que un admin
-- lo acepte o lo descarte (o hasta los 30 días).
CREATE TABLE correo_adjuntos (
  mensaje_id bigint NOT NULL REFERENCES correo_mensajes(id) ON DELETE CASCADE,
  orden      int NOT NULL,
  nombre     text NOT NULL,
  mime       text NOT NULL DEFAULT '',
  datos      bytea NOT NULL,
  PRIMARY KEY (mensaje_id, orden)
);

-- Quién cargó el lote desde la pantalla (para el aviso de lote terminado).
ALTER TABLE lotes ADD COLUMN creado_por bigint REFERENCES usuarios(id) ON DELETE SET NULL;

-- Links de un solo uso que van por mail: restablecer la contraseña (30 minutos) e invitar a
-- alguien nuevo (72 horas). Se guarda el SHA-256 del token.
CREATE TABLE tokens_cuenta (
  hash       text PRIMARY KEY,
  usuario_id bigint NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  tipo       text NOT NULL CHECK (tipo IN ('restablecer','invitacion')),
  creado     timestamptz NOT NULL DEFAULT now(),
  vence      timestamptz NOT NULL,
  usado      timestamptz,
  ip         text NOT NULL DEFAULT ''
);
CREATE INDEX tokens_cuenta_usuario ON tokens_cuenta (usuario_id, tipo);
CREATE INDEX tokens_cuenta_vence ON tokens_cuenta (vence);

-- Avisos por mail ya mandados: una clave por aviso y destinatario.
CREATE TABLE avisos_enviados (
  clave  text PRIMARY KEY,
  creado timestamptz NOT NULL DEFAULT now()
);
