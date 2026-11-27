# Novedades

Cada versión publicada tiene su sección. La imagen Docker toma la versión de la primera.

## [0.4.0] - 2026-11-27

### Correo

- Las facturas pueden llegar por mail. En Configuración, Correo entrante, elegís una de cuatro formas: una casilla por IMAP (Gmail, Microsoft 365 o la del dominio), Cloudflare Email Routing con un Worker (el código viene listo para copiar), un proveedor (Mailgun, Postmark o SendGrid) o Gmail con permiso de Google (botón «Conectar con Google»). Cada una con su paso a paso y un botón Probar.
- Cada mail con PDFs, fotos o un ZIP adjuntos genera un lote, igual que un mensaje de Telegram. Solo se toman los adjuntos: el texto del mail no se lee ni se guarda. Un mail repetido no se carga dos veces.
- Remitentes permitidos (un email o un @dominio): lo de los demás queda en la pantalla nueva **Correo retenido**, donde un admin lo acepta (y, si quiere, suma al remitente) o lo descarta. Los retenidos se borran solos a los 30 días. Sin lista, todo queda retenido.
- Para cargar a un cliente, que manden a facturas+CUIT@tudominio, o asociá el email o el @dominio del remitente en Clientes y canales.
- Cuando termina el lote, Factorum le contesta al remitente en el mismo hilo con el resumen: cuántos comprobantes, cuántos bien, para revisar y con errores, los apócrifos y el link. Nunca a un retenido.
- Correo saliente por SMTP (Gmail, Microsoft 365, Resend, Postmark, Mailgun, Brevo...), con un mail de prueba.
- Avisos por mail, cada uno con su interruptor y sus destinatarios: lote terminado, emisor apócrifo o PDF posiblemente adulterado (a los admins) y un resumen los lunes a las 8. Cada persona recibe solo lo de sus clientes.
- «Olvidé mi contraseña» en el login: llega un link que vale una vez y vence en 30 minutos, y al usarlo se cierran las demás sesiones.
- En Usuarios, al crear a alguien, «Mandarle una invitación por mail» para que elija su contraseña (vence en 72 horas), en lugar de la temporal. También «Reenviar invitación» y «Link por mail».

## [0.3.0] - 2026-11-27

### Usuarios y roles

- Se entra con email y contraseña. La primera vez, Factorum pide crear el primer superadmin con un código de un solo uso que aparece en el log del servidor (o con `factorum salud`).
- Cuatro roles: lectura (solo mira), usuario (carga, revisa, corrige y aprueba), admin (además personas, claves de API, clientes y auditoría) y superadmin (todo, también la configuración).
- A cada persona se le pueden asignar los CUITs de los clientes que atiende: solo ve, carga y exporta lo de esos clientes, en la pantalla, la API y MCP.
- Pantalla Usuarios: alta con una contraseña temporal que se muestra una sola vez y obliga a cambiarla al entrar, cambio de rol y de clientes, desactivar, reactivar y restablecer la contraseña.
- Menú de usuario arriba a la derecha con tu nombre, tu rol, «Cambiar mi contraseña» y «Cerrar sesión». La sesión vence a las 12 horas sin uso y, después de cinco intentos fallidos, hay que esperar.
- Lectura no ve los botones de cargar, corregir ni aprobar.

### Configuración desde la pantalla

- Casi todo lo que antes iba en variables de entorno se configura en Configuración: general (nombre y URL pública), Arcanum, Telegram, WhatsApp, webhook, el modelo de visión, el almacenamiento en R2 y qué cliente es cada remitente de chat.
- Cada sección trae «Cómo se configura» con el paso a paso y un botón «Probar» que hace una prueba de verdad y dice qué falló.
- Al guardar se aplica en el momento, sin reiniciar.
- Las claves y tokens se guardan cifrados y nunca se vuelven a mostrar: solo «configurado» y los últimos cuatro caracteres. Lo que fija el servidor con una variable de entorno gana y aparece bloqueado.
- La vinculación de WhatsApp (el QR) pasó de Canales a Configuración.
- Ahora hacen falta solo `FACTORUM_DATABASE_URL`, `FACTORUM_MASTER_KEY` (obligatoria) y, si querés, `FACTORUM_PUERTO`.

### Claves de API y Conectar

- Pantalla Claves de API: cada una con nombre, rol, clientes, vencimiento y último uso; se muestra una sola vez y se puede revocar.
- Pantalla Conectar: cómo usar la API REST y MCP, con los bloques listos para pegar en Claude Code y Claude Desktop.

### Auditoría

- Quién entró (y quién lo intentó), quién salió, altas y cambios de personas y claves, cambios de configuración (qué cambió, nunca el valor), correcciones, aprobaciones, exportaciones y pases entre apps. Con filtros por fecha, persona y acción.

### Suite Escriba

- Con Arcanum conectado, el ícono de grilla de arriba abre Arcanum sin volver a loguearte, y desde Arcanum se entra a Factorum igual. Cada uno entra con su rol en la app de destino, nunca con uno mayor.

## [0.2.0] - 2026-11-26

### Tickets de compra

- Lee tickets de controlador fiscal (tique y tique factura A, B, C y M) de supermercados, estaciones de servicio, farmacias y bares, en PDF o en foto. Saca comercio, CUIT, tipo, punto de venta y número, fecha y hora, neto, IVA por alícuota y total, y nunca inventa lo que no está escrito.
- Las fotos se preparan antes del OCR (grises, tamaño, luz pareja, contraste y Otsu) y se leen con dos segmentaciones de página: gana la que encuentra más datos. Tesseract corre con un hilo por proceso, que es cuatro veces más rápido.
- Nuevos tipos de comprobante: los tiques de ARCA (81, 82, 83 y del 109 al 120).
- Los tiques no se constatan en ARCA: se controla el CUIT y la base de apócrifos. Nuevo veredicto `tique_controlado`. No se pide CAE.
- Rubro de gasto (supermercado, combustible, farmacia, gastronomía, transporte, servicios u otros) en tres capas: palabras clave, actividad del padrón de ARCA y la corrección de una persona, que se recuerda por CUIT del emisor. Las facturas también toman el rubro del padrón.
- Filtros `clase` (facturas o tickets) y `rubro` en la API, MCP y la exportación. El Excel y el CSV suman las columnas Hora y Rubro, y el resumen trae los tickets y el total por rubro.
- En pantalla: marca «Ticket», hora y rubro en Comprobantes, Revisar y Verificar; filtros por clase y rubro; Resumen con barras por rubro (tocá una para ver esos comprobantes); el rubro se cambia desde la revisión y se recuerda para el CUIT; los tickets del mes en Inicio.

### Diseño nuevo

- Identidad propia dentro de la suite Escriba: logo (una hoja con el cuadrado petróleo y la F blanca), color petróleo y las mismas fuentes que Arcanum (Inter y JetBrains Mono, servidas por Factorum).
- Inicio: lo de hoy y del mes, apócrifos, posibles adulteraciones, cuánto se leyó con IA, los últimos 14 días en barras y los últimos lotes. Si todavía no hay nada, los pasos para empezar.
- Revisar: una bandeja con lo que tiene errores y lo que hay que mirar, con contador en el menú. Con el teclado: A aprueba y pasa al siguiente, J siguiente, K anterior, Esc cierra.
- Menú agrupado (Trabajo, Datos, Configuración) y la sección actual arriba, al lado del logo.
- Novedades: al entrar después de una actualización, Factorum muestra qué cambió. También desde el menú, con Acerca de (versión y estado de cada componente).
- Pantallas vacías que explican qué hacer, veredicto con sello e ícono y estados con punto de color.
- Carga de archivos con botón propio (sin el «Choose File» del navegador): muestra los elegidos con su tamaño y el total.

## [0.1.0] - 2026-11-25

Primera versión de Factorum.

- Lee comprobantes de ARCA sin IA: el QR (RG 4892) es la fuente oficial, el texto del PDF completa razones sociales, neto e IVA por alícuota.
- Cascada: QR, texto, OCR (Tesseract) y, solo si hace falta, un modelo de visión local (Ollama) o cualquier API compatible con OpenAI. Cada dato dice de dónde salió.
- Controles: sumas, IVA contra el neto, CUIT, letra contra tipo, CAE, fechas, moneda extranjera, duplicados y PDFs cuyo texto no coincide con el QR (posible adulteración).
- Verificar si una factura es legal: constatación WSCDC y base de apócrifos vía Arcanum.
- Lotes de miles de facturas (PDF, fotos, ZIP) en una cola sobre Postgres: mil facturas en menos de 30 segundos en 4 núcleos.
- Revisión en pantalla con el original al lado, correcciones registradas (papel de trabajo) y aprobación.
- Exportación a Excel y CSV con resumen por alícuota.
- Archivos guardados en disco o R2, comprimidos (zstd) y cifrados (AES-256-GCM).
- MCP para asistentes, Telegram, WhatsApp y webhook genérico.
- Módulo de Arcanum.
