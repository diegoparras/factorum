-- El buscador de la cabecera (Ctrl+K): comprobantes por número, CUIT, razón social e importe, y
-- lotes por nombre. Los índices son parciales: lo de la papelera no se busca.
CREATE INDEX comprobantes_buscar_numero ON comprobantes (numero, punto_venta) WHERE borrado_en IS NULL;
CREATE INDEX comprobantes_buscar_total ON comprobantes (total) WHERE borrado_en IS NULL;
CREATE INDEX comprobantes_buscar_emisor ON comprobantes (cuit_emisor text_pattern_ops) WHERE borrado_en IS NULL;
CREATE INDEX comprobantes_buscar_cliente ON comprobantes (cliente_cuit text_pattern_ops) WHERE borrado_en IS NULL;

-- La razón social y el nombre del lote se buscan por un pedazo (ILIKE '%...%'): con pg_trgm, con un
-- índice de trigramas. Si la extensión no se puede instalar (sin permiso), se sigue sin ese índice:
-- la búsqueda anda igual, solo más lenta en bases muy grandes.
DO $$
BEGIN
  BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'sin pg_trgm: el buscador anda sin índice de trigramas';
  END;
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
    BEGIN
      EXECUTE 'CREATE INDEX comprobantes_buscar_razon ON comprobantes USING gin (razon_social_emisor gin_trgm_ops) WHERE borrado_en IS NULL';
      EXECUTE 'CREATE INDEX lotes_buscar_nombre ON lotes USING gin (nombre gin_trgm_ops) WHERE borrado_en IS NULL';
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'pg_trgm está pero no en el camino de búsqueda: el buscador anda sin índice de trigramas';
    END;
  END IF;
END $$;
