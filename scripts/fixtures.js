'use strict';
// Genera facturas de prueba con el modelo "ARCA" de Arcanum (copia de Comprobantes en linea).
// Uso: ARCANUM_DIR=../arcanum node scripts/fixtures.js testdata/pdf
// Los CUIT y CAE son de fantasia (con digito verificador valido).
const fs = require('fs');
const path = require('path');
const dir = process.env.ARCANUM_DIR || path.join(__dirname, '..', '..', 'arcanum');
process.env.ARCANUM_API_KEY = process.env.ARCANUM_API_KEY || 'x'.repeat(48);
process.env.ARCANUM_MASTER_KEY = process.env.ARCANUM_MASTER_KEY || '0'.repeat(64);
// Para simular un PDF adulterado: el QR conserva el importe original (raw.qrImporte) y el texto
// muestra otro. Se parchea qrUrl antes de cargar pdfArca, que lo toma al cargarse.
const comun = require(path.join(dir, 'src/services/pdfComun.js'));
const qrOriginal = comun.qrUrl;
comun.qrUrl = (cmp) => qrOriginal(cmp.raw && cmp.raw.qrImporte ? { ...cmp, importe_total: cmp.raw.qrImporte } : cmp);
const { generar } = require(path.join(dir, 'src/services/pdfArca.js'));

const out = process.argv[2] || 'testdata/pdf';
fs.mkdirSync(out, { recursive: true });

const emisorA = { razonSocial: 'DISTRIBUIDORA EJEMPLO S.R.L.', domicilio: 'Av. Siempreviva 742 - CABA', condicionIva: 'RI', iibb: '901-123456-7', inicioActividades: '2015-03-01' };
const emisorC = { razonSocial: 'PEREZ JUAN CARLOS', domicilio: 'Calle Falsa 123 - Rosario, Santa Fe', condicionIva: 'MONOTRIBUTO', iibb: '20-12345678-6', inicioActividades: '2019-08-01' };

const casos = [
  ['01_FA_21.pdf', emisorA, { cuit: '30711111111', punto_venta: 2, tipo_cbte: 1, numero: 1543, fecha: '2026-11-03', importe_total: 121000, doc_tipo: 80, doc_nro: '20222222223', cae: '76451234567890', cae_vto: '2026-11-13',
    raw: { concepto: 1, importeNeto: 100000, importeIva: 21000, alicuotasIva: [{ id: 5, baseImp: 100000, importe: 21000 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1, receptorDomicilio: 'Mitre 100 - CABA' } }],
  ['02_FA_mixta.pdf', emisorA, { cuit: '30711111111', punto_venta: 2, tipo_cbte: 1, numero: 1544, fecha: '2026-11-05', importe_total: 238345.5, doc_tipo: 80, doc_nro: '20222222223', cae: '76451234567891', cae_vto: '2026-11-15',
    raw: { concepto: 1, importeNeto: 200000, importeIva: 31845.5, importeTributos: 6500, alicuotasIva: [{ id: 5, baseImp: 120000, importe: 25200 }, { id: 4, baseImp: 63290, importe: 6645.5 }], items: [{ descripcion: 'Harina 000 x 25kg', cantidad: 10, precioUnitario: 12000, subtotal: 120000, alicuotaIva: 5 }, { descripcion: 'Libros tecnicos', cantidad: 2, precioUnitario: 40000, subtotal: 80000, alicuotaIva: 4 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1 } }],
  ['03_FC_servicios.pdf', emisorC, { cuit: '20123456786', punto_venta: 5, tipo_cbte: 11, numero: 91, fecha: '2026-11-15', importe_total: 112100, doc_tipo: 80, doc_nro: '30711111111', cae: '76459876543210', cae_vto: '2026-11-25',
    raw: { concepto: 2, fechaServicioDesde: '2026-10-01', fechaServicioHasta: '2026-10-31', fechaVtoPago: '2026-11-30', receptorNombre: 'DISTRIBUIDORA EJEMPLO S.R.L.', condicionIvaReceptor: 1 } }],
  ['04_FB_consumidor.pdf', emisorA, { cuit: '30711111111', punto_venta: 3, tipo_cbte: 6, numero: 20077, fecha: '2026-11-20', importe_total: 60500, doc_tipo: 99, doc_nro: '0', cae: '76451111122223', cae_vto: '2026-11-30',
    raw: { concepto: 1, importeNeto: 50000, importeIva: 10500, alicuotasIva: [{ id: 5, baseImp: 50000, importe: 10500 }], condicionIvaReceptor: 5 } }],
  ['05_NCA.pdf', emisorA, { cuit: '30711111111', punto_venta: 2, tipo_cbte: 3, numero: 88, fecha: '2026-11-21', importe_total: 12100, doc_tipo: 80, doc_nro: '20222222223', cae: '76452222233334', cae_vto: '2026-12-01',
    raw: { concepto: 1, importeNeto: 10000, importeIva: 2100, alicuotasIva: [{ id: 5, baseImp: 10000, importe: 2100 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1 } }],
  // Total del QR distinto de la suma: el control tiene que saltar.
  ['06_FA_no_cierra.pdf', emisorA, { cuit: '30711111111', punto_venta: 5, tipo_cbte: 1, numero: 91, fecha: '2026-11-15', importe_total: 112100, doc_tipo: 80, doc_nro: '20222222223', cae: '76453333344445', cae_vto: '2026-11-25',
    raw: { concepto: 1, importeNeto: 100000, importeIva: 21000, alicuotasIva: [{ id: 5, baseImp: 100000, importe: 21000 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1 } }],
  ['07_FA_dolares.pdf', emisorA, { cuit: '30711111111', punto_venta: 2, tipo_cbte: 1, numero: 1545, fecha: '2026-11-22', importe_total: 1210, doc_tipo: 80, doc_nro: '20222222223', cae: '76454444455556', cae_vto: '2026-12-02',
    raw: { concepto: 1, moneda: 'DOL', cotizacion: 1180.5, importeNeto: 1000, importeIva: 210, alicuotasIva: [{ id: 5, baseImp: 1000, importe: 210 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1 } }],
  // Adulterado: el texto dice 1.210.000 y el QR (el dato que firmo ARCA) dice 121.000.
  ['09_FA_adulterada.pdf', emisorA, { cuit: '30711111111', punto_venta: 2, tipo_cbte: 1, numero: 1546, fecha: '2026-11-23', importe_total: 1210000, doc_tipo: 80, doc_nro: '20222222223', cae: '76455555566667', cae_vto: '2026-12-03',
    raw: { concepto: 1, qrImporte: 121000, importeNeto: 1000000, importeIva: 210000, alicuotasIva: [{ id: 5, baseImp: 1000000, importe: 210000 }], receptorNombre: 'GOMEZ MARIA LAURA', condicionIvaReceptor: 1 } }],
];

(async () => {
  for (const [nombre, emisor, cmp] of casos) {
    const pdf = await generar(cmp, emisor, { copias: nombre.startsWith('01') ? 3 : 1 });
    fs.writeFileSync(path.join(out, nombre), pdf);
  }
  // Un PDF con dos facturas distintas (lote unido en un solo archivo): se hace con pdfunite si esta.
  console.log(`${casos.length} PDFs en ${out}`);
})().catch((e) => { console.error(e); process.exit(1); });
