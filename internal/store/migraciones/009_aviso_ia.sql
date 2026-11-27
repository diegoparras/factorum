-- 0.8.0: el aviso de la IA externa. Cada persona lo ve la primera vez que va a usar una IA que manda
-- los comprobantes fuera del servidor; se recuerda por persona, host y versión del texto del aviso,
-- así vuelve a aparecer si cambia el proveedor o lo que dice el aviso.
CREATE TABLE ia_avisos_vistos (
  usuario_id bigint NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
  host text NOT NULL,
  version int NOT NULL,
  visto timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (usuario_id, host, version)
);
