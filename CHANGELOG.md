# Novedades

Cada versión publicada tiene su sección. La imagen Docker toma la versión de la primera.

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
