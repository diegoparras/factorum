# Novedades

Cada versión publicada tiene su sección. La imagen Docker toma la versión de la primera.

## [0.2.0] - 2026-11-26

### Tickets de compra

- Lee tickets de controlador fiscal (tique y tique factura A, B, C y M) de supermercados, estaciones de servicio, farmacias y bares, en PDF o en foto. Saca comercio, CUIT, tipo, punto de venta y número, fecha y hora, neto, IVA por alícuota y total, y nunca inventa lo que no está escrito.
- Las fotos se preparan antes del OCR (grises, tamaño, luz pareja, contraste y Otsu) y se leen con dos segmentaciones de página: gana la que encuentra más datos. Tesseract corre con un hilo por proceso, que es cuatro veces más rápido.
- Nuevos tipos de comprobante: los tiques de ARCA (81, 82, 83 y del 109 al 120).
- Los tiques no se constatan en ARCA: se controla el CUIT y la base de apócrifos. Nuevo veredicto `tique_controlado`. No se pide CAE.
- Rubro de gasto (supermercado, combustible, farmacia, gastronomía, transporte, servicios u otros) en tres capas: palabras clave, actividad del padrón de ARCA y la corrección de una persona, que se recuerda por CUIT del emisor. Las facturas también toman el rubro del padrón.
- Filtros `clase` (facturas o tickets) y `rubro` en la API, MCP y la exportación. El Excel y el CSV suman las columnas Hora y Rubro, y el resumen trae los tickets y el total por rubro.

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
