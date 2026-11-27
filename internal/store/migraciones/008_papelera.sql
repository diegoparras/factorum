-- 0.7.0: la papelera. Borrar un lote o comprobantes sueltos no borra filas: las marca (borrado
-- lógico) y todo lo que lista, cuenta, resume o exporta las ignora. Lo marcado se puede restaurar tal
-- cual estaba hasta que vence (Configuración, Papelera) y el trabajo de la cola lo borra de verdad,
-- con los archivos del almacén que ya no use nadie.

-- borrado_por es quién lo mandó a la papelera (como aparece en la auditoría). borrado_con_lote
-- dice que se fue con su lote: vuelve cuando se restaura el lote y no aparece suelto en la papelera.
ALTER TABLE lotes ADD COLUMN borrado_en timestamptz, ADD COLUMN borrado_por text;
ALTER TABLE documentos ADD COLUMN borrado_en timestamptz, ADD COLUMN borrado_por text,
  ADD COLUMN borrado_con_lote boolean NOT NULL DEFAULT false;
ALTER TABLE comprobantes ADD COLUMN borrado_en timestamptz, ADD COLUMN borrado_por text,
  ADD COLUMN borrado_con_lote boolean NOT NULL DEFAULT false;

CREATE INDEX lotes_papelera ON lotes (borrado_en) WHERE borrado_en IS NOT NULL;
CREATE INDEX documentos_papelera ON documentos (borrado_en) WHERE borrado_en IS NOT NULL;
CREATE INDEX comprobantes_papelera ON comprobantes (borrado_en) WHERE borrado_en IS NOT NULL;

-- Un repetido apunta al documento que se procesó primero: si ese se borra de verdad, el repetido
-- queda sin referencia en lugar de frenar el borrado.
ALTER TABLE documentos DROP CONSTRAINT documentos_repetido_de_fkey,
  ADD CONSTRAINT documentos_repetido_de_fkey FOREIGN KEY (repetido_de) REFERENCES documentos(id) ON DELETE SET NULL;

-- Archivos del almacén (por el SHA-256 del documento) que quedaron sin ningún documento después de
-- un borrado definitivo y hay que sacar del disco y de R2. Se anotan en la misma transacción que
-- borra las filas, así un corte no deja archivos huérfanos: el trabajo de la cola los vuelve a mirar
-- y solo los borra si sigue sin haber un documento con ese SHA-256 (vivo o en la papelera).
CREATE TABLE almacen_por_borrar (
  sha256 text PRIMARY KEY,
  creado timestamptz NOT NULL DEFAULT now()
);
