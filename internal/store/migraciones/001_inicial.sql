-- Lotes: un envío de uno o más archivos (subida web, chat, MCP, Arcanum).
CREATE TABLE lotes (
  id           bigserial PRIMARY KEY,
  nombre       text NOT NULL DEFAULT '',
  cliente_cuit text NOT NULL DEFAULT '',
  origen       text NOT NULL DEFAULT 'web',
  canal        text,
  chat         text,
  ia           boolean NOT NULL DEFAULT true,
  constatar    boolean NOT NULL DEFAULT false,
  creado       timestamptz NOT NULL DEFAULT now(),
  actualizado  timestamptz NOT NULL DEFAULT now(),
  terminado    timestamptz,
  notificado   boolean NOT NULL DEFAULT false
);
CREATE INDEX lotes_chat ON lotes (canal, chat, actualizado DESC);

-- Documentos: cada archivo recibido. El contenido vive en el almacén (disco o R2), por su SHA-256.
CREATE TABLE documentos (
  id          bigserial PRIMARY KEY,
  lote_id     bigint NOT NULL REFERENCES lotes(id) ON DELETE CASCADE,
  nombre      text NOT NULL,
  mime        text NOT NULL DEFAULT '',
  tamano      bigint NOT NULL,
  sha256      text NOT NULL,
  estado      text NOT NULL DEFAULT 'pendiente',  -- pendiente, procesando, listo, error, repetido
  error       text,
  repetido_de bigint REFERENCES documentos(id),
  paginas     int,
  extractores text[],
  duracion_ms bigint,
  detalle     jsonb,
  creado      timestamptz NOT NULL DEFAULT now(),
  procesado   timestamptz
);
CREATE INDEX documentos_lote ON documentos (lote_id, estado);
CREATE INDEX documentos_sha ON documentos (sha256);

-- Comprobantes leídos. "datos" es el modelo completo; lo demás, para filtrar y sumar.
CREATE TABLE comprobantes (
  id                  bigserial PRIMARY KEY,
  documento_id        bigint NOT NULL REFERENCES documentos(id) ON DELETE CASCADE,
  lote_id             bigint NOT NULL REFERENCES lotes(id) ON DELETE CASCADE,
  cliente_cuit        text NOT NULL DEFAULT '',
  orden               int NOT NULL DEFAULT 0,
  tipo                int,
  punto_venta         int,
  numero              bigint,
  fecha               date,
  cuit_emisor         text,
  razon_social_emisor text,
  total               bigint,                      -- centavos
  datos               jsonb NOT NULL,
  hallazgos           jsonb NOT NULL DEFAULT '[]',
  estado              text NOT NULL,
  confianza           real NOT NULL DEFAULT 0,
  constatacion        jsonb,
  duplicado_de        bigint REFERENCES comprobantes(id) ON DELETE SET NULL,
  revisado_por        text,
  revisado_en         timestamptz,
  creado              timestamptz NOT NULL DEFAULT now(),
  actualizado         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comprobantes_lote ON comprobantes (lote_id);
CREATE INDEX comprobantes_cliente_fecha ON comprobantes (cliente_cuit, fecha);
CREATE INDEX comprobantes_clave ON comprobantes (cliente_cuit, cuit_emisor, tipo, punto_venta, numero);
CREATE INDEX comprobantes_estado ON comprobantes (estado);
CREATE INDEX comprobantes_emisor ON comprobantes (cuit_emisor);

-- Papel de trabajo: cada corrección hecha por una persona.
CREATE TABLE correcciones (
  id             bigserial PRIMARY KEY,
  comprobante_id bigint NOT NULL REFERENCES comprobantes(id) ON DELETE CASCADE,
  campo          text NOT NULL,
  antes          text NOT NULL,
  despues        text NOT NULL,
  usuario        text NOT NULL DEFAULT '',
  creado         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX correcciones_cbte ON correcciones (comprobante_id);
