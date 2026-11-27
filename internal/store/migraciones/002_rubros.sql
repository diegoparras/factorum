-- Rubro de gasto que una persona eligió para un emisor (capa 3 del rubro). Cuando alguien corrige
-- el rubro de un comprobante, Factorum lo recuerda por CUIT del emisor y lo usa en los que vengan.
CREATE TABLE rubros_cuit (
  cuit_emisor text PRIMARY KEY,
  rubro       text NOT NULL,
  usuario     text NOT NULL DEFAULT '',
  actualizado timestamptz NOT NULL DEFAULT now()
);

-- Filtrar facturas o tickets va por tipo; el rubro vive en datos (jsonb).
CREATE INDEX comprobantes_tipo ON comprobantes (tipo);
CREATE INDEX comprobantes_rubro ON comprobantes ((datos->>'rubro'));
