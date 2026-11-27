-- 0.5.0: inicio de sesión único con Lockatus.
--
-- Una persona puede no tener ningún cliente asignado todavía (quien entra por primera vez con
-- Lockatus con rol usuario o lectura): eso es cuits = '{}' (no ve nada hasta que un admin le asigne
-- clientes). NULL sigue siendo «todos». Antes un '{}' también quería decir todos: se pasa a NULL para
-- que nadie pierda lo que veía.
UPDATE usuarios SET cuits = NULL WHERE cuits = '{}';

-- Las sesiones abiertas con Lockatus llevan origen 'lockatus' (las de siempre, 'local'; las del pase
-- de la suite, 'suite:arcanum').
COMMENT ON COLUMN usuarios.cuits IS 'CUITs de cliente permitidos; NULL = todos; {} = ninguno todavía';
