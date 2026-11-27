-- 0.10.0: el Libro IVA Digital recuerda lo que se exportó.
--
-- periodo_iva es el período del libro de cada comprobante (AAAA-MM): el mes de la fecha, o el que
-- se le asignó porque llegó después de presentar su mes, o el que eligió una persona en Revisar
-- (datos->>'periodo_iva'). Se guarda aparte para buscar por período; lo mantiene Factorum.
ALTER TABLE comprobantes ADD COLUMN periodo_iva text;
UPDATE comprobantes SET periodo_iva = coalesce(nullif(datos->>'periodo_iva', ''), to_char(fecha, 'YYYY-MM')) WHERE fecha IS NOT NULL;
CREATE INDEX comprobantes_cliente_periodo ON comprobantes (cliente_cuit, periodo_iva);

-- Cada exportación de un libro (bajar el ZIP): quién, cuándo, el SHA-256 de cada archivo y los
-- comprobantes que fueron, con la huella de lo que se escribió de cada uno. Un período con alguna
-- exportación queda «presentado»: se puede volver a exportar, y Factorum avisa qué cambió desde la
-- última vez.
CREATE TABLE libro_iva_exportaciones (
  id             bigserial PRIMARY KEY,
  cliente_cuit   text NOT NULL,
  periodo        text NOT NULL,
  libro          text NOT NULL CHECK (libro IN ('compras', 'ventas', 'importaciones')),
  usuario        text NOT NULL DEFAULT '',
  creado         timestamptz NOT NULL DEFAULT now(),
  solo_aprobados boolean NOT NULL DEFAULT false,
  cantidad       int NOT NULL,
  archivos       jsonb NOT NULL,
  comprobantes   jsonb NOT NULL
);
CREATE INDEX libro_iva_exportaciones_periodo ON libro_iva_exportaciones (cliente_cuit, libro, periodo, creado DESC);
