-- 0.4.0: el pase de lo guardado en disco a R2 y las copias de seguridad de la base.
--
-- Estas tablas describen el almacenamiento y las copias mismas, no los datos: las copias de
-- seguridad no las incluyen y restaurar no las toca.

-- Estado del pase a R2 (una sola fila, id = 1).
CREATE TABLE r2_migracion (
  id            int PRIMARY KEY CHECK (id = 1),
  estado        text NOT NULL CHECK (estado IN ('corriendo','pausada','terminada','liberando','liberada','error')),
  destino       text NOT NULL DEFAULT '',   -- endpoint/bucket/prefijo de R2 (sin credenciales)
  por           text NOT NULL DEFAULT '',   -- quién lo empezó
  iniciada      timestamptz NOT NULL DEFAULT now(),
  terminada     timestamptz,
  tramo_inicio  timestamptz NOT NULL DEFAULT now(),  -- para el tiempo estimado: desde cuándo corre
  tramo_bytes   bigint NOT NULL DEFAULT 0,           -- y cuántos bytes estaban hechos entonces
  mensaje       text NOT NULL DEFAULT '',
  actualizado   timestamptz NOT NULL DEFAULT now()
);

-- Cada objeto del disco. verificado = está en R2 con el mismo tamaño y el mismo SHA-256;
-- liberado = además se borró del disco.
CREATE TABLE r2_objetos (
  clave       text PRIMARY KEY,
  tamano      bigint NOT NULL,
  sha256      text NOT NULL DEFAULT '',
  estado      text NOT NULL DEFAULT 'pendiente' CHECK (estado IN ('pendiente','verificado','error','liberado')),
  subido      boolean NOT NULL DEFAULT false,  -- false = ya estaba en R2 y solo se verificó
  intentos    int NOT NULL DEFAULT 0,
  error       text NOT NULL DEFAULT '',
  actualizado timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX r2_objetos_estado ON r2_objetos (estado, clave);

-- Registro de las copias de seguridad de la base.
CREATE TABLE copias (
  id          bigserial PRIMARY KEY,
  creado      timestamptz NOT NULL DEFAULT now(),
  terminado   timestamptz,
  estado      text NOT NULL CHECK (estado IN ('haciendo','ok','error')),
  manual      boolean NOT NULL DEFAULT false,
  programada  date,                          -- el día (Buenos Aires) de la copia diaria
  por         text NOT NULL DEFAULT '',
  destino     text NOT NULL DEFAULT '',      -- r2 o disco
  clave       text,                          -- copias/factorum-AAAAMMDD-HHMMSS.copia
  tamano      bigint,
  filas       bigint,
  tablas      int,
  sha256      text,                          -- del archivo cifrado (lo que se descarga)
  version     text NOT NULL DEFAULT '',      -- de Factorum
  error       text NOT NULL DEFAULT '',
  duracion_ms bigint
);
CREATE UNIQUE INDEX copias_clave ON copias (destino, clave) WHERE clave IS NOT NULL;
CREATE INDEX copias_creado ON copias (creado DESC);
CREATE INDEX copias_programada ON copias (programada);
