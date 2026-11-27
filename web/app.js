'use strict';
// Factorum: interfaz sin frameworks ni build. Sin scripts ni handlers inline (CSP): todo por delegación.

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const soloDig = (s) => String(s || '').replace(/\D/g, '');

// ---------- Íconos (SVG line, sin emojis: regla de la suite) ----------
const svg = (d) => `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
const I = {
  verificar: svg('<path d="M12 3l7 3v6c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6z"/><path d="M9 12l2 2 4-4"/>'),
  subir: svg('<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 20h16"/>'),
  lotes: svg('<rect x="4" y="4" width="16" height="5" rx="1.5"/><rect x="4" y="11" width="16" height="9" rx="1.5"/>'),
  comprobantes: svg('<path d="M6 3h9l3 3v15l-3-2-3 2-3-2-3 2z"/><path d="M9 9h6M9 13h6"/>'),
  resumen: svg('<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>'),
  canales: svg('<path d="M21 12a8 8 0 0 1-11.5 7.2L4 21l1.8-5.5A8 8 0 1 1 21 12z"/>'),
  ojo: svg('<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>'),
  ojoNo: svg('<path d="M3 3l18 18"/><path d="M10.6 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3 3.9M6.6 6.6C3.8 8.4 2 12 2 12s3.5 7 10 7a9.8 9.8 0 0 0 5.4-1.6"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/>'),
  sol: svg('<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>'),
  luna: svg('<path d="M21 13A9 9 0 1 1 11 3a7 7 0 0 0 10 10z"/>'),
  salir: svg('<path d="M15 4h4v16h-4M10 8l-4 4 4 4M6 12h11"/>'),
  info: svg('<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>'),
  inicio: svg('<path d="M4 11l8-7 8 7"/><path d="M6 10v10h12V10"/><path d="M10 20v-6h4v6"/>'),
  revisar: svg('<path d="M4 13l3-8h10l3 8"/><path d="M4 13v6h16v-6"/><path d="M4 13h5l1 2h4l1-2h5"/>'),
  novedades: svg('<path d="M12 3l2.2 5.4L20 9l-4.4 3.8L17 19l-5-3-5 3 1.4-6.2L4 9l5.8-.6z"/>'),
  check: svg('<path d="M5 12.5l4.5 4.5L19 7.5"/>'),
  cruz: svg('<path d="M7 7l10 10M17 7L7 17"/>'),
  alerta: svg('<path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17h.01"/>'),
  doc: svg('<path d="M7 3h7l4 4v14H7z"/><path d="M14 3v4h4"/><path d="M10 12h5M10 16h5"/>'),
  configuracion: svg('<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>'),
  usuarios: svg('<circle cx="9" cy="8" r="3.2"/><path d="M3 20c.6-3.4 3-5.5 6-5.5s5.4 2.1 6 5.5"/><path d="M16 5.2a3 3 0 0 1 0 5.6M18 14.8c1.6.8 2.7 2.6 3 5.2"/>'),
  claves: svg('<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9M17 6l3 3M14 9l2 2"/>'),
  conectar: svg('<path d="M9 7V3M15 7V3"/><path d="M6 7h12v4a6 6 0 0 1-12 0z"/><path d="M12 17v4"/>'),
  auditoria: svg('<path d="M9 4h6M9 4a2 2 0 0 0-2 2H5v15h14V6h-2a2 2 0 0 0-2-2"/><path d="M8.5 12l2 2 4-4M8.5 17h7"/>'),
  llave: svg('<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>'),
  copiar: svg('<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/>'),
  persona: svg('<circle cx="12" cy="8" r="3.5"/><path d="M5 20c.8-3.7 3.6-6 7-6s6.2 2.3 7 6"/>'),
  externo: svg('<path d="M14 4h6v6M20 4l-9 9"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>'),
  correo: svg('<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3.5 6.5l8.5 6 8.5-6"/>'),
  retenido: svg('<path d="M20 12V7a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h8"/><path d="M3.5 6.5l8.5 6 8.5-6"/><path d="M17 15v5M21 15v5"/>'),
  conciliacion: svg('<path d="M4 8h13l-3-3"/><path d="M20 16H7l3 3"/><path d="M9 12h6"/>'),
  enviar: svg('<path d="M21 3L10 14"/><path d="M21 3l-7 18-4-7-7-4z"/>'),
  papelera: svg('<path d="M4 7h16"/><path d="M9 7V4.5h6V7"/><path d="M6 7l1 13h10l1-13"/><path d="M10 11v5.5M14 11v5.5"/>'),
  bajar: svg('<path d="M12 4v12M7 11l5 5 5-5"/><path d="M4 20h16"/>'),
  libro_iva: svg('<path d="M5 4.5A1.5 1.5 0 0 1 6.5 3H19v15H6.5A1.5 1.5 0 0 0 5 19.5z"/><path d="M5 19.5A1.5 1.5 0 0 0 6.5 21H19v-3"/><path d="M9 7.5h6M9 11h6M9 14.5h3"/>'),
  restaurar: svg('<path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3"/><path d="M4 4.5V9h4.5"/>'),
  nube: svg('<path d="M7 18.5a4.5 4.5 0 0 1-.7-8.95 6 6 0 0 1 11.6 1.2A3.9 3.9 0 0 1 17.5 18.5"/><path d="M12 20v-7.5M9 15.5l3-3 3 3"/>'),
  servidor: svg('<rect x="4" y="4" width="16" height="7" rx="1.6"/><rect x="4" y="13" width="16" height="7" rx="1.6"/><path d="M8 7.5h.01M8 16.5h.01M12 7.5h4M12 16.5h4"/>'),
  globo: svg('<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9S9.5 5.7 12 3z"/>'),
  reloj: svg('<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.2 2"/>'),
  chip: svg('<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M10 3.5V7M14 3.5V7M10 17v3.5M14 17v3.5M3.5 10H7M3.5 14H7M17 10h3.5M17 14h3.5"/><path d="M10.5 12h3"/>'),
};

// Cada sección dice qué permiso hace falta para verla (el servidor lo hace cumplir igual).
const NAV_TODO = [
  ['Trabajo', [['inicio', 'Inicio'], ['revisar', 'Revisar'], ['retenido', 'Correo retenido', 'retenido'], ['verificar', 'Verificar'], ['subir', 'Cargar facturas', 'cargar']]],
  ['Datos', [['lotes', 'Lotes'], ['comprobantes', 'Comprobantes'], ['resumen', 'Resumen'], ['conciliacion', 'Conciliación'], ['libro_iva', 'Libro IVA Digital', 'libro_iva'], ['papelera', 'Papelera', 'papelera']]],
  ['Configuración', [['configuracion', 'Configuración', 'clientes'], ['usuarios', 'Usuarios', 'usuarios'], ['claves', 'Claves de API', 'claves'],
    ['conectar', 'Conectar'], ['auditoria', 'Auditoría', 'auditoria']]],
];
const VISTAS = NAV_TODO.flatMap((g) => g[1]);
const puede = (p) => !p || !!(YO && YO.permisos && YO.permisos[p]);
// Sin clientes asignados (entró con Lockatus y todavía nadie le asignó clientes): solo Inicio, con el aviso.
const sinClientes = () => !!(YO && YO.permisos && YO.permisos.sin_clientes);
const navVisible = () => (sinClientes() ? [['Trabajo', [['inicio', 'Inicio']]]]
  : NAV_TODO.map(([g, items]) => [g, items.filter((it) => puede(it[2]))]).filter(([, items]) => items.length));
const vistaPermitida = (v) => (sinClientes() ? v === 'inicio' : VISTAS.some((it) => it[0] === v && puede(it[2])));
const PENDIENTES = { error: 0, revisar: 0, retenido: 0 };
let OLVIDE = false; // el login ofrece «Olvidé mi contraseña» (hay correo saliente)
let LOCKATUS = { listo: false, solo: false }; // el login ofrece «Entrar con Lockatus»; solo: no hay login con contraseña
// Lo que vuelve del inicio de sesión con Lockatus cuando no se pudo entrar (?lockatus=...).
const AVISOS_LOCKATUS = {
  estado: 'El inicio de sesión con Lockatus venció o no empezó en este navegador. Probá de nuevo.',
  token: 'Lockatus devolvió una respuesta que no se pudo verificar. Probá de nuevo; si sigue, avisale a un admin.',
  email: 'Lockatus no mandó un email verificado. Verificá tu email en Lockatus y probá de nuevo.',
  desactivado: 'Tu cuenta de Factorum está desactivada: pedile a un admin que la reactive.',
  sin_cuenta: 'Todavía no tenés cuenta en Factorum. Pedile a un admin que te la cree con tu email de Lockatus.',
  cancelado: 'No se completó el inicio de sesión en Lockatus.',
  caido: 'Lockatus no responde ahora. Probá en un rato.',
  no_config: 'El inicio de sesión con Lockatus no está configurado.',
  demasiados: 'Demasiados intentos. Probá de nuevo en unos minutos.',
  error: 'No se pudo terminar de entrar. Probá de nuevo en un rato.',
};
let AVISO_GOOGLE = null; // al volver de Google: { ok, detalle }

// ---------- Estado ----------
let YO = null; // { identidad, permisos, instalacion }
let SALUD = null;
let vista = 'inicio';
const filtro = { estado: '', clase: '', rubro: '', q: '', cliente: '', desde: '', hasta: '', lote: '', offset: 0, limite: 100 };
let listado = []; // comprobantes de la tabla, para Anterior / Siguiente
let rev = null; // { idx, cbte, original }
let timerLotes = null;
let timerCanales = null;

// ---------- API ----------
// La sesión va en una cookie HttpOnly; X-Factorum es la cabecera que el servidor pide en lo que
// modifica (CSRF).
async function api(ruta, opts = {}) {
  const h = Object.assign({ 'X-Factorum': '1' }, opts.headers || {});
  if (opts.json !== undefined) { h['Content-Type'] = 'application/json'; opts.body = JSON.stringify(opts.json); }
  const r = await fetch(ruta, { method: opts.method || 'GET', headers: h, body: opts.body, credentials: 'same-origin' });
  if (r.status === 401 && !opts.sinRedirigir) { sinSesion(); throw new Error('Tu sesión venció: entrá de nuevo'); }
  if (opts.binario) {
    if (!r.ok) throw new Error(`Error ${r.status}`);
    return r.blob();
  }
  const d = await r.json().catch(() => ({}));
  if (r.status === 403 && d.debe_cambiar) { pedirCambioClave(true); const e = new Error(d.error); e.silencioso = true; throw e; }
  if (!r.ok || d.ok === false) { const e = new Error(d.error || `Error ${r.status}`); e.status = r.status; e.datos = d; throw e; }
  return d;
}

function toast(msg, err) {
  if (msg && msg.silencioso) return;
  if (msg instanceof Error) msg = msg.message;
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'toast' + (err ? ' err' : '');
  clearTimeout(toast.t);
  toast.t = setTimeout(() => t.classList.add('hidden'), err ? 6000 : 3000);
}

// ---------- Formatos ----------
const pesos = (v) => {
  const n = Number(v || 0);
  return n.toLocaleString('es-AR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};
const fecha = (s) => (s ? String(s).slice(0, 10).split('-').reverse().join('/') : '');
const cuit = (s) => { const d = soloDig(s); return d.length === 11 ? `${d.slice(0, 2)}-${d.slice(2, 10)}-${d.slice(10)}` : (s || ''); };
const TIPOS = { 1: 'Factura A', 2: 'Nota de Débito A', 3: 'Nota de Crédito A', 6: 'Factura B', 7: 'Nota de Débito B', 8: 'Nota de Crédito B',
  11: 'Factura C', 12: 'Nota de Débito C', 13: 'Nota de Crédito C', 19: 'Factura E', 51: 'Factura M', 201: 'FCE MiPyME A', 206: 'FCE MiPyME B', 211: 'FCE MiPyME C',
  81: 'Tique Factura A', 82: 'Tique Factura B', 83: 'Tique', 109: 'Tique C', 110: 'Tique Nota de Crédito', 111: 'Tique Factura C',
  112: 'Tique Nota de Crédito A', 113: 'Tique Nota de Crédito B', 114: 'Tique Nota de Crédito C', 115: 'Tique Nota de Débito A',
  116: 'Tique Nota de Débito B', 117: 'Tique Nota de Débito C', 118: 'Tique Factura M', 119: 'Tique Nota de Crédito M', 120: 'Tique Nota de Débito M' };
const RUBROS = { supermercado: 'Supermercado', combustible: 'Combustible', farmacia: 'Farmacia', gastronomia: 'Gastronomía',
  transporte: 'Transporte', servicios: 'Servicios', otros: 'Otros', sin_rubro: 'Sin rubro' };
const rubroChip = (r) => (r ? `<span class="rubro r-${esc(r)}">${esc(RUBROS[r] || r)}</span>` : '');
const ticketChip = (c) => (c && c.es_tique ? '<span class="chip-ticket" title="Ticket de controlador fiscal">Ticket</span>' : '');
const nombreCbte = (c) => `${TIPOS[c.tipo] || 'Comprobante ' + (c.tipo || '')} ${String(c.punto_venta || 0).padStart(5, '0')}-${String(c.numero || 0).padStart(8, '0')}`;
const ETQ = { ok: 'Bien', revisar: 'Revisar', error: 'Con errores', aprobado: 'Aprobado',
  valida: 'Válida', invalida: 'No válida', apocrifa: 'Emisor apócrifo', con_errores: 'Con errores', no_verificable: 'No verificable',
  tique_controlado: 'Ticket controlado' };
const SELLO = { valida: 'check', tique_controlado: 'check', invalida: 'cruz', apocrifa: 'cruz', con_errores: 'alerta', no_verificable: 'alerta' };
const estado = (e) => `<span class="est ${esc(e)}">${esc(ETQ[e] || e)}</span>`;
const ORIGEN = { qr: 'QR', texto: 'texto', ocr: 'OCR', vision: 'IA', manual: 'manual', arcanum: 'ARCA', calculo: 'cálculo' };

function hallazgosHTML(h) {
  if (!h || !h.length) return '';
  return `<ul class="hall">${h.map((x) => `<li class="${esc(x.nivel)}">${esc(x.mensaje)}</li>`).join('')}</ul>`;
}

// ---------- Navegación ----------
function pintarNav() {
  const pend = PENDIENTES.error + PENDIENTES.revisar;
  $('#nav').innerHTML = navVisible().map(([grupo, items]) => `<div class="nav-g">${grupo}</div>` + items.map(([id, t]) => {
    let badge = id === 'revisar' && pend ? `<span class="nav-badge${PENDIENTES.error ? ' err' : ''}" aria-label="${pend} pendientes">${pend > 999 ? '999+' : pend}</span>` : '';
    if (id === 'retenido' && PENDIENTES.retenido) badge = `<span class="nav-badge" aria-label="${PENDIENTES.retenido} retenidos">${PENDIENTES.retenido > 999 ? '999+' : PENDIENTES.retenido}</span>`;
    return `<button type="button" class="nav-it${id === vista ? ' active' : ''}" data-accion="ir" data-vista="${id}" title="${t}" aria-label="${t}"${id === vista ? ' aria-current="page"' : ''}>${I[id]}<span>${t}</span>${badge}</button>`;
  }).join('')).join('');
}

async function actualizarPendientes() {
  try {
    const [e, r] = await Promise.all([api('/api/comprobantes?estado=error&limite=1'), api('/api/comprobantes?estado=revisar&limite=1')]);
    PENDIENTES.error = e.total; PENDIENTES.revisar = r.total;
    if (puede('retenido')) PENDIENTES.retenido = (await api('/api/correo/retenido?limite=1')).total;
    pintarNav();
    const ce = $('#cntError'); if (ce) ce.textContent = e.total;
    const cr = $('#cntRevisar'); if (cr) cr.textContent = r.total;
  } catch { /* sin conexión: se reintenta en la próxima acción */ }
}

function ir(v) {
  // Un link de un mail puede traer filtros: #comprobantes?lote=12
  const [base, qs] = String(v || '').split('?');
  v = base;
  if (qs && v === 'comprobantes') {
    const p = new URLSearchParams(qs);
    Object.assign(filtro, { estado: p.get('estado') || '', clase: '', rubro: '', q: '', cliente: '', desde: '', hasta: '', lote: soloDig(p.get('lote')), offset: 0 });
  }
  if (v === 'canales') v = 'configuracion'; // la pantalla Canales pasó a Configuración
  if (!vistaPermitida(v)) v = 'inicio';
  vista = v;
  $$('.vista').forEach((s) => s.classList.toggle('hidden', s.id !== 'v-' + v));
  pintarNav();
  document.documentElement.removeAttribute('data-nav-movil');
  clearInterval(timerLotes); clearInterval(timerCanales);
  if (v === 'lotes') { cargarLotes(); timerLotes = setInterval(cargarLotes, 3000); }
  if (v === 'comprobantes') cargarCbtes();
  if (v === 'resumen') cargarResumen();
  if (v === 'conciliacion') prepararConciliacion();
  if (v === 'libro_iva') prepararLibro();
  if (v === 'papelera') { papOffset = 0; PAP_SEL.clear(); cargarPapelera(); }
  if (v === 'configuracion') { cargarConfiguracion(); timerCanales = setInterval(refrescarWhatsApp, 5000); }
  if (v === 'usuarios') cargarUsuarios();
  if (v === 'claves') cargarClaves();
  if (v === 'conectar') pintarConectar();
  if (v === 'auditoria') { audOffset = 0; cargarAuditoria(); }
  if (v === 'inicio') cargarInicio();
  if (v === 'revisar') cargarBandeja();
  if (v === 'retenido') cargarRetenidos();
  const nom = (VISTAS.find(([id]) => id === v) || [])[1] || '';
  $('#crumb').textContent = v === 'inicio' ? '' : '/ ' + nom;
  document.title = v === 'inicio' ? 'Factorum' : `${nom} · Factorum`;
  try { history.replaceState(null, '', '#' + v); } catch { /* nada */ }
}

function toggleNav() {
  if (matchMedia('(max-width: 760px)').matches) {
    const el = document.documentElement;
    if (el.getAttribute('data-nav-movil') === 'abierto') el.removeAttribute('data-nav-movil'); else el.setAttribute('data-nav-movil', 'abierto');
    return;
  }
  const min = document.documentElement.getAttribute('data-nav') === 'min';
  if (min) document.documentElement.removeAttribute('data-nav'); else document.documentElement.setAttribute('data-nav', 'min');
  try { localStorage.setItem('factorum.nav', min ? '' : 'min'); } catch { /* nada */ }
}

const ROLES = { lectura: 'Lectura', usuario: 'Usuario', admin: 'Admin', superadmin: 'Superadmin' };
const ORDEN_ROL = ['lectura', 'usuario', 'admin', 'superadmin'];
const rolChip = (r) => `<span class="rol-chip r-${esc(r)}">${esc(ROLES[r] || r)}</span>`;

function pintarMenu() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';
  const s = SALUD || {};
  const id = (YO && YO.identidad) || {};
  const quien = id.nombre || id.email || (id.clave ? 'Clave ' + id.clave : 'Factorum');
  $('#menu').innerHTML = `
    <div class="menu-yo"><span class="avatar" aria-hidden="true">${esc(iniciales(quien))}</span><span><strong>${esc(quien)}</strong>${id.email && id.nombre ? `<br><span class="muted">${esc(id.email)}</span>` : ''}<br>${rolChip(id.rol)}${id.origen && id.origen.startsWith('suite:') ? ' <span class="muted">desde ' + esc(id.origen.slice(6)) + '</span>' : ''}${id.origen === 'lockatus' ? ' <span class="muted">con Lockatus</span>' : ''}</span></div>
    <div class="menu-sep"></div>
    ${id.tipo === 'sesion' && !(YO.cuenta && YO.cuenta.origen === 'lockatus') ? `<button class="menu-item" data-accion="miClave">${I.llave}<span>Cambiar mi contraseña</span></button>` : ''}
    <button class="menu-item" data-accion="tema">${dark ? I.sol : I.luna}<span>${dark ? 'Tema claro' : 'Tema oscuro'}</span></button>
    <button class="menu-item" data-accion="novedades">${I.novedades}<span>Novedades</span></button>
    <button class="menu-item" data-accion="acerca">${I.info}<span>Acerca de Factorum</span></button>
    <div class="menu-sep"></div>
    <button class="menu-item" data-accion="salir">${I.salir}<span>Cerrar sesión</span></button>`;
}

async function cargarSalud() {
  try {
    const r = await fetch('/healthz');
    SALUD = await r.json();
    const chip = $('#saludChip');
    chip.textContent = SALUD.base ? '' : 'Sin conexión con la base';
    chip.classList.toggle('mal', !SALUD.base);
    $('#loteConstatar').closest('label').classList.toggle('hidden', !SALUD.arcanum);
    $('#loteConstatar').checked = SALUD.constatar_por_defecto !== false;
    $('#loteIA').checked = $('#iaVerificar').checked = SALUD.ia_por_defecto !== false;
    if (SALUD.nombre) $$('.nombre-inst').forEach((e) => { e.textContent = SALUD.nombre; });
    $('#iaOpSubir').classList.toggle('hidden', !(SALUD.ia && SALUD.ia.habilitada));
    $('#iaOpVerificar').classList.toggle('hidden', !(SALUD.ia && SALUD.ia.habilitada));
  } catch { /* se reintenta en la próxima acción */ }
  await cargarAvisoIA();
}

// ---------- Sesión ----------
const iniciales = (t) => String(t || '?').replace(/@.*/, '').split(/[\s._-]+/).filter(Boolean).slice(0, 2).map((x) => x[0].toUpperCase()).join('') || '?';

function mostrarTarjeta(id) {
  $('#login').classList.remove('hidden');
  for (const f of ['#loginForm', '#arranqueForm', '#olvideForm', '#restablecerForm']) $(f).classList.toggle('hidden', f !== id);
}

function mostrarLogin(err, aviso) {
  mostrarTarjeta('#loginForm');
  const lk = LOCKATUS.listo;
  const solo = lk && LOCKATUS.solo;
  $('#lockatusBtn').classList.toggle('hidden', !lk);
  $('#loginO').classList.toggle('hidden', !lk || solo);
  $('#loginLocal').classList.toggle('hidden', solo);
  $('#loginSolo').classList.toggle('hidden', !solo);
  $('#loginEntrar').classList.toggle('ghost', lk);
  $('#loginEmail').required = $('#loginClave').required = !solo;
  $('#olvideBtn').classList.toggle('hidden', !OLVIDE || solo);
  $('#olvideNota').classList.toggle('hidden', OLVIDE || solo);
  $('#loginErr').textContent = err || '';
  $('#loginErr').classList.toggle('hidden', !err);
  $('#loginAviso').textContent = aviso || '';
  $('#loginAviso').classList.toggle('hidden', !aviso);
  setTimeout(() => (solo ? $('#lockatusBtn') : $('#loginEmail').value ? $('#loginClave') : $('#loginEmail')).focus(), 50);
}

function mostrarArranque() {
  mostrarTarjeta('#arranqueForm');
  setTimeout(() => $('#arrCodigo').focus(), 50);
}

function mostrarOlvide() {
  mostrarTarjeta('#olvideForm');
  $('#olvideOk').classList.add('hidden'); $('#olvideErr').classList.add('hidden');
  if (!$('#olvideEmail').value) $('#olvideEmail').value = $('#loginEmail').value;
  setTimeout(() => $('#olvideEmail').focus(), 50);
}

async function pedirLink() {
  const btn = $('#olvideForm button[type=submit]');
  btn.disabled = true;
  $('#olvideErr').classList.add('hidden');
  try {
    const r = await api('/api/sesion/olvide', { method: 'POST', json: { email: $('#olvideEmail').value.trim() }, sinRedirigir: true });
    $('#olvideOk').textContent = r.mensaje; $('#olvideOk').classList.remove('hidden');
  } catch (e) { $('#olvideErr').textContent = e.message; $('#olvideErr').classList.remove('hidden'); }
  finally { btn.disabled = false; }
}

// El link del mail (restablecer o invitación): #restablecer?t=...
let TOKEN_LINK = '';
async function mostrarRestablecer(token) {
  TOKEN_LINK = token || '';
  mostrarTarjeta('#restablecerForm');
  try { history.replaceState(null, '', location.pathname); } catch { /* nada */ } // el token no queda en la barra ni en el historial
  const err = $('#restErr');
  err.classList.add('hidden');
  $('#restBtn').disabled = true;
  try {
    const r = await api('/api/sesion/link', { method: 'POST', json: { token: TOKEN_LINK }, sinRedirigir: true });
    const inv = r.tipo === 'invitacion';
    $('#restTitulo').textContent = inv ? 'Te damos la bienvenida' : 'Elegí una contraseña nueva';
    $('#restSub').textContent = inv ? `Hola${r.nombre ? ' ' + r.nombre : ''}. Para entrar a Factorum como ${r.email}, elegí tu contraseña.`
      : `Para ${r.email}. Al guardarla, se cierran las sesiones abiertas en otros navegadores.`;
    $('#restBtn').disabled = false;
    setTimeout(() => $('#restClave').focus(), 50);
  } catch (e) {
    $('#restTitulo').textContent = 'El link no sirve';
    $('#restSub').textContent = '';
    err.textContent = e.message; err.classList.remove('hidden');
  }
}

async function usarLink() {
  const err = $('#restErr');
  err.classList.add('hidden');
  if ($('#restClave').value !== $('#restRepetir').value) { err.textContent = 'Las dos contraseñas no coinciden'; err.classList.remove('hidden'); return; }
  const btn = $('#restBtn');
  btn.disabled = true;
  try {
    await api('/api/sesion/restablecer', { method: 'POST', json: { token: TOKEN_LINK, clave: $('#restClave').value }, sinRedirigir: true });
    $('#restClave').value = ''; $('#restRepetir').value = ''; TOKEN_LINK = '';
    YO = await api('/api/sesion');
    iniciarApp();
    toast('Listo: ya tenés tu contraseña nueva.');
  } catch (e) { err.textContent = e.message; err.classList.remove('hidden'); btn.disabled = false; }
}

// sinSesion: la cookie venció o no hay. Si Factorum está por estrenar, la pantalla de arranque.
async function sinSesion(aviso, err) {
  YO = null;
  cerrarModal(true);
  $('#menu').classList.add('hidden');
  clearInterval(timerLotes); clearInterval(timerCanales);
  $('#suiteWrap').classList.add('hidden');
  try {
    const a = await fetch('/api/arranque').then((r) => r.json());
    OLVIDE = !!a.olvide;
    LOCKATUS = { listo: !!a.lockatus, solo: !!a.solo_lockatus };
    if (a.nombre) $$('.nombre-inst').forEach((e) => { e.textContent = a.nombre; });
    if (a.necesita) { mostrarArranque(); return; }
  } catch { /* sin conexión: login */ }
  mostrarLogin(err || '', aviso);
}

async function arrancar() {
  // Al volver de otra app con un pase que no sirvió, el servidor manda acá con ?aviso=pase.
  const q = new URLSearchParams(location.search);
  const aviso = q.get('aviso') === 'pase' ? 'El pase para entrar venció o ya se usó. Volvé a tocar el botón en la otra app.' : '';
  // Al volver de Lockatus sin poder entrar, el servidor manda acá con ?lockatus=<motivo> (nunca un texto).
  const errLockatus = q.has('lockatus') ? AVISOS_LOCKATUS[q.get('lockatus')] || AVISOS_LOCKATUS.error : '';
  if (q.has('google')) AVISO_GOOGLE = { ok: q.get('google') === 'ok', detalle: q.get('detalle') || '' };
  if (q.has('aviso') || q.has('google') || q.has('lockatus')) { try { history.replaceState(null, '', location.pathname + location.hash); } catch { /* nada */ } }
  // El link de un mail para elegir contraseña no necesita sesión.
  const h = location.hash.slice(1);
  if (h.startsWith('restablecer')) {
    try { OLVIDE = !!(await fetch('/api/arranque').then((r) => r.json())).olvide; } catch { /* nada */ }
    mostrarRestablecer(new URLSearchParams(h.split('?')[1] || '').get('t'));
    return;
  }
  try {
    YO = await api('/api/sesion', { sinRedirigir: true });
  } catch { sinSesion(aviso, errLockatus); return; }
  iniciarApp();
}

async function iniciarApp() {
  $('#login').classList.add('hidden');
  Object.assign(CONC, { clientes: null, datos: null, tab: '', cliente: '' }); // otra persona, otros clientes
  const id = YO.identidad || {};
  document.documentElement.setAttribute('data-puede-cargar', puede('cargar') && !sinClientes() ? 'si' : 'no');
  // Con clientes asignados, la carga sugiere los propios.
  const dl = $('#misCuits') || Object.assign(document.createElement('datalist'), { id: 'misCuits' });
  dl.innerHTML = (id.cuits || []).map((c) => `<option value="${esc(cuit(c))}">`).join('');
  document.body.append(dl);
  for (const sel of ['#loteCliente', '#cuitConsultante']) {
    const i = $(sel);
    i.setAttribute('list', 'misCuits');
    if (id.cuits && id.cuits.length) i.placeholder = id.cuits.length === 1 ? cuit(id.cuits[0]) : 'Elegí uno de tus clientes';
  }
  $('#loteCliente').closest('label').firstChild.textContent = id.cuits && id.cuits.length > 1 ? 'CUIT del cliente' : 'CUIT del cliente (opcional)';
  await cargarSalud();
  pintarNav();
  // Con la contraseña temporal no se carga nada más hasta que la cambie (el servidor tampoco deja).
  if (id.debe_cambiar) { pedirCambioClave(true); return; }
  ir(location.hash.slice(1) || 'inicio');
  if (sinClientes()) { verNovedades(false); return; } // no hay nada más que cargar
  if (AVISO_GOOGLE) {
    toast(AVISO_GOOGLE.ok ? `Gmail conectado: ${AVISO_GOOGLE.detalle}` : AVISO_GOOGLE.detalle || 'No se pudo conectar con Google', !AVISO_GOOGLE.ok);
    AVISO_GOOGLE = null;
  }
  actualizarPendientes();
  cargarSuite();
  verNovedades(false);
}

async function entrar(email, clave) {
  const btn = $('#loginForm button[type=submit]');
  btn.disabled = true;
  try {
    await api('/api/sesion', { method: 'POST', json: { email, clave }, sinRedirigir: true });
    $('#loginClave').value = '';
    YO = await api('/api/sesion');
    iniciarApp();
  } catch (e) {
    mostrarLogin(e.message);
  } finally { btn.disabled = false; }
}

async function crearPrimerSuperadmin() {
  const err = $('#arranqueErr');
  err.classList.add('hidden');
  try {
    await api('/api/arranque', { method: 'POST', sinRedirigir: true,
      json: { codigo: $('#arrCodigo').value.trim(), nombre: $('#arrNombre').value.trim(), email: $('#arrEmail').value.trim(), clave: $('#arrClave').value } });
    $('#arrClave').value = '';
    YO = await api('/api/sesion');
    iniciarApp();
    toast('Listo: sos el superadmin. Ahora completá la configuración.');
  } catch (e) {
    err.textContent = e.message; err.classList.remove('hidden');
  }
}

async function salir() {
  try { await api('/api/sesion', { method: 'DELETE', sinRedirigir: true }); } catch { /* ya no había sesión */ }
  sinSesion();
}

// campoClave arma un campo de contraseña o secreto con su ojito (regla de la suite).
function campoClave(id, attrs = '') {
  return `<div class="pass-wrap"><input id="${id}" type="password" ${attrs}><button type="button" class="pass-toggle" data-accion="ojo" data-para="${id}" aria-label="Mostrar u ocultar">${I.ojo}</button></div>`;
}

// pedirCambioClave: la contraseña temporal obliga (fijo: no se puede cerrar).
function pedirCambioClave(fijo) {
  if (fijo && !$('#modal').classList.contains('hidden') && $('#fClaveMia')) return;
  abrirModal(fijo ? 'Elegí tu contraseña' : 'Cambiar mi contraseña', `<form id="fClaveMia" class="form-modal">
      ${fijo ? '<p>Entraste con una contraseña temporal. Para seguir, elegí una tuya de 10 caracteres o más.</p>' : ''}
      <label>Contraseña actual${campoClave('cmActual', 'required autocomplete="current-password"')}</label>
      <label>Contraseña nueva${campoClave('cmNueva', 'required minlength="10" autocomplete="new-password"')}</label>
      <label>Repetila${campoClave('cmRepetir', 'required minlength="10" autocomplete="new-password"')}</label>
      <div class="login-err hidden" id="cmErr"></div>
    </form>`, { fijo, acciones: `${fijo ? '<button type="button" class="ghost" data-accion="salir">Salir</button>' : '<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button>'}<button type="submit" form="fClaveMia">Guardar</button>` });
  setTimeout(() => $('#cmActual') && $('#cmActual').focus(), 50);
}

async function cambiarMiClave() {
  const err = $('#cmErr');
  const nueva = $('#cmNueva').value;
  if (nueva !== $('#cmRepetir').value) { err.textContent = 'Las dos contraseñas nuevas no coinciden'; err.classList.remove('hidden'); return; }
  try {
    await api('/api/sesion/clave', { method: 'POST', json: { actual: $('#cmActual').value, nueva } });
    const eraFija = MODAL_FIJO;
    cerrarModal(true);
    toast('Contraseña cambiada. Se cerraron tus otras sesiones.');
    YO = await api('/api/sesion');
    if (eraFija) iniciarApp();
  } catch (e) { err.textContent = e.message; err.classList.remove('hidden'); }
}

// ---------- Suite Escriba ----------
let SUITE = [];
async function cargarSuite() {
  try {
    if (!YO || YO.identidad.tipo !== 'sesion') return;
    ({ apps: SUITE } = await api('/api/suite/apps'));
    $('#suiteWrap').classList.toggle('hidden', !SUITE.length);
  } catch { $('#suiteWrap').classList.add('hidden'); }
}
function pintarSuite() {
  $('#suiteMenu').innerHTML = `<div class="menu-user">Suite Escriba</div>` + SUITE.map((a) =>
    `<button class="menu-item" data-accion="abrirApp" data-app="${esc(a.app)}">${I.externo}<span>${esc(a.nombre)}</span><span class="mi-sub"><span class="punto ${a.disponible ? 'ok' : 'mal'}"></span>${a.disponible ? 'disponible' : 'no responde'}</span></button>`).join('');
}
async function abrirApp(app, btn) {
  if (btn) btn.disabled = true;
  try {
    const { url } = await api('/api/suite/abrir/' + encodeURIComponent(app), { method: 'POST' });
    location.href = url;
  } catch (e) { toast(e, true); if (btn) btn.disabled = false; }
}

// ---------- Verificar ----------
async function verificar(ev, reintento) {
  ev.preventDefault();
  const f = $('#archVerificar').files[0];
  if (!f) return;
  // Con una IA externa, la primera vez la persona ve qué pasa con los datos.
  if ($('#iaVerificar').checked && !(await confirmarIAPersona())) $('#iaVerificar').checked = false;
  const fd = new FormData();
  fd.append('archivo', f);
  if (soloDig($('#cuitConsultante').value)) fd.append('cuit_consultante', soloDig($('#cuitConsultante').value));
  fd.append('ia', $('#iaVerificar').checked ? 'true' : 'false');
  const btn = $('#fVerificar button[type=submit]');
  let otra = false;
  btn.disabled = true; btn.textContent = 'Verificando...';
  try {
    const r = await api('/api/verificar', { method: 'POST', body: fd });
    pintarVerificacion(r);
  } catch (e) {
    // Cambió la IA desde que se abrió la pantalla: se muestra el aviso nuevo y se prueba de nuevo.
    if (e.datos && e.datos.codigo === 'aviso_ia' && !reintento) { actualizarAvisoIA(e.datos.aviso_ia); otra = true; } else toast(e, true);
  } finally {
    btn.disabled = false; btn.textContent = 'Verificar';
  }
  if (otra) verificar(ev, true);
}

function pintarVerificacion(r) {
  const res = r.resultado || {};
  const cbs = res.comprobantes || [];
  const kons = r.constatacion || [];
  const filas = cbs.map((rc, i) => {
    const c = rc.comprobante;
    const k = kons[i];
    const v = (r.comprobantes || [])[i];
    return `<div class="card">
      <div class="fila" style="justify-content:space-between"><strong>${esc(nombreCbte(c))} ${ticketChip(c)}</strong>${v ? estado(v.veredicto) : estado(rc.estado)}</div>
      <dl class="kv">
        <dt>Emisor</dt><dd>${esc(c.razon_social_emisor || '')} <span class="mono">${esc(cuit(c.cuit_emisor))}</span></dd>
        <dt>Fecha</dt><dd>${fecha(c.fecha)}${c.hora ? ' ' + esc(c.hora.slice(0, 5)) : ''}</dd>
        ${c.rubro ? `<dt>Rubro</dt><dd>${rubroChip(c.rubro)}</dd>` : ''}
        <dt>Total</dt><dd class="mono">$ ${pesos(c.total)} ${c.moneda && c.moneda !== 'PES' ? esc(c.moneda) : ''}</dd>
        ${c.es_tique ? '<dt>CAE</dt><dd>No lleva: es un ticket de controlador fiscal. ARCA no lo constata; se controla el CUIT y la base de apócrifos.</dd>'
          : `<dt>CAE</dt><dd class="mono">${esc(c.cae || 'no encontrado')} ${c.vto_cae ? '(vence ' + fecha(c.vto_cae) + ')' : ''}</dd>`}
        ${k && k.resultado ? `<dt>ARCA</dt><dd>${k.valido ? 'Constatado: el comprobante existe con estos datos.' : 'Rechazado: ' + esc((k.observaciones || []).map((o) => o.message).join(' '))}</dd>` : ''}
        ${k && k.apoc && k.apoc.consultado ? `<dt>Apócrifos</dt><dd>${k.apoc.esApocrifo ? '<strong class="est error">El emisor figura como apócrifo</strong>' : 'El emisor no figura en la base'}</dd>` : ''}
        <dt>Leído con</dt><dd>${(res.extractores || []).map((x) => `<span class="origen ${esc(x)}">${esc(ORIGEN[x] || x)}</span>`).join(' ')} <span class="muted">${res.duracion_ms} ms</span></dd>
      </dl>
      ${hallazgosHTML(rc.hallazgos.filter((h) => h.nivel !== 'info' || h.codigo === 'constatado'))}
    </div>`;
  }).join('');
  $('#resVerificar').innerHTML = `<div class="veredicto ${esc(r.veredicto)}"><div class="sello">${I[SELLO[r.veredicto] || 'alerta']}</div><div><div class="titulo">${esc(ETQ[r.veredicto] || r.veredicto)}</div><div>${esc(r.mensaje)}</div></div></div>${filas}`;
}

// ---------- Subir ----------
async function subir(ev, reintento) {
  ev.preventDefault();
  const files = $('#archSubir').files;
  if (!files.length) return;
  if ($('#loteIA').checked && !(await confirmarIAPersona())) $('#loteIA').checked = false;
  const fd = new FormData();
  for (const f of files) fd.append('archivos', f);
  fd.append('nombre', $('#loteNombre').value.trim());
  fd.append('cliente_cuit', soloDig($('#loteCliente').value));
  fd.append('ia', $('#loteIA').checked ? 'true' : 'false');
  fd.append('constatar', $('#loteConstatar').checked ? 'true' : 'false');
  const prog = $('#progSubir');
  prog.classList.remove('hidden');
  const btn = $('#fSubir button[type=submit]');
  btn.disabled = true;
  // XHR para tener el progreso de la subida (fetch no lo da).
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/lotes');
  xhr.setRequestHeader('X-Factorum', '1');
  xhr.upload.onprogress = (e) => { if (e.lengthComputable) prog.firstElementChild.style.width = `${Math.round((e.loaded / e.total) * 100)}%`; };
  xhr.onload = () => {
    btn.disabled = false;
    prog.classList.add('hidden');
    prog.firstElementChild.style.width = '0';
    let d = {};
    try { d = JSON.parse(xhr.responseText); } catch { /* nada */ }
    if (xhr.status === 401) { sinSesion(); return; }
    if (xhr.status === 409 && d.codigo === 'aviso_ia' && !reintento) { actualizarAvisoIA(d.aviso_ia); subir(ev, true); return; }
    if (xhr.status >= 300 || !d.ok) { toast(d.error || `Error ${xhr.status}`, true); return; }
    let msg = `Lote ${d.lote.id}: ${d.recibidos} archivo(s) recibidos.`;
    if (d.repetidos) msg += ` ${d.repetidos} ya estaban cargados.`;
    if (d.descartados && d.descartados.length) msg += ` Se omitieron: ${d.descartados.join(', ')}.`;
    toast(msg);
    $('#fSubir').reset();
    ir('lotes');
  };
  xhr.onerror = () => { btn.disabled = false; prog.classList.add('hidden'); toast('No se pudo subir', true); };
  xhr.send(fd);
}

// ---------- Lotes ----------
async function cargarLotes() {
  try {
    const { lotes } = await api('/api/lotes?limite=50');
    if (!lotes.length) { $('#tablaLotes').innerHTML = vacio('doc', 'Todavía no hay lotes', 'Cada carga de facturas (desde acá, por chat, por MCP o desde Arcanum) aparece como un lote.', 'subir', 'Cargar facturas'); return; }
    $('#tablaLotes').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Lote</th><th>Origen</th><th>Cliente</th><th>Creado</th><th>Progreso</th><th>Bien</th><th>Revisar</th><th>Errores</th><th></th></tr></thead><tbody>
      ${lotes.map((l) => {
        const pct = l.documentos ? Math.round((l.procesados / l.documentos) * 100) : 100;
        const c = l.comprobantes || {};
        return `<tr class="clic" data-accion="verLote" data-id="${l.id}"><td><strong>${l.id}</strong> ${esc(l.nombre)}</td><td>${esc(l.origen)}</td><td class="mono">${esc(cuit(l.cliente_cuit))}</td>
          <td>${new Date(l.creado).toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' })}</td>
          <td><span class="prog mini-prog"><div style="width:${pct}%"></div></span> <span class="muted">${l.procesados}/${l.documentos}</span>${l.con_error ? ` <span class="est error">${l.con_error} ilegibles</span>` : ''}</td>
          <td class="num">${(c.ok || 0) + (c.aprobado || 0)}</td><td class="num">${c.revisar || 0}</td><td class="num">${c.error || 0}</td>
          <td class="acciones-fila">${accionesLote(l)}</td></tr>`;
      }).join('')}</tbody></table></div>`;
    tarjetas('#tablaLotes');
  } catch (e) { toast(e, true); }
}

// Excel, Cambiar cliente (usuario o más) y Borrar (admin o más) de un lote.
function accionesLote(l, largo) {
  return `<button class="ghost mini" data-accion="exportarLote" data-id="${l.id}">Excel</button>
    ${puede('cargar') ? `<button class="ghost mini" data-accion="cambiarCliente" data-id="${l.id}" title="Cambiar el cliente del lote">${largo ? 'Cambiar cliente' : 'Cliente'}</button>` : ''}
    ${puede('borrar_lotes') ? `<button class="ghost mini peligro" data-accion="borrarLote" data-id="${l.id}" title="Mandar el lote entero a la papelera">${I.papelera}<span>${largo ? 'Borrar el lote' : 'Borrar'}</span></button>` : ''}`;
}

const nombreLote = (l) => (l.nombre || '').trim() || `lote ${l.id}`;
// El lote recién leído (la tabla se refresca cada 3 segundos: los números pueden haber cambiado).
async function loteDe(id) {
  return (await api('/api/lotes/' + id)).lote;
}

// ---------- Papelera: borrar y cambiar el cliente ----------
let PAP_DIAS = 30;
async function pedirBorrarLote(id) {
  let l;
  try { l = await loteDe(id); } catch (e) { toast(e, true); return; }
  const c = l.comprobantes || {};
  const n = (c.ok || 0) + (c.revisar || 0) + (c.error || 0) + (c.aprobado || 0);
  const conf = nombreLote(l);
  abrirModal('Borrar el lote', `<p>El lote <strong>${esc(conf)}</strong>${l.cliente_cuit ? ` (cliente ${esc(cuit(l.cliente_cuit))})` : ''} se va a la papelera con sus <strong>${mil(l.documentos)} archivo(s)</strong> y <strong>${mil(n)} comprobante(s)</strong>.</p>
    ${l.estado !== 'terminado' ? '<p class="aviso-mini">Todavía se está procesando: lo que falta se cancela. Si lo restaurás, se termina de procesar.</p>' : ''}
    <p class="muted">Deja de aparecer en Comprobantes, Revisar, Resumen, Inicio y las exportaciones. Lo podés restaurar desde la Papelera, tal cual estaba, durante ${mil(PAP_DIAS)} días; después se borra para siempre. Si volvés a subir los mismos archivos, se procesan como nuevos.</p>
    <label class="campo-conf"><span class="et">Para confirmar, escribí «${esc(conf)}»</span><input id="borrarLoteConf" autocomplete="off" spellcheck="false" data-esperado="${esc(conf.toLowerCase())}"></label>
    <div class="login-err hidden" id="borrarLoteErr"></div>`,
  { acciones: `<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="button" class="peligro" data-accion="borrarLoteConfirmar" data-id="${l.id}" disabled>Borrar el lote</button>` });
  setTimeout(() => $('#borrarLoteConf')?.focus(), 50);
}

async function borrarLote(id, btn) {
  btn.disabled = true;
  try {
    const { borrado: b } = await api('/api/lotes/' + id, { method: 'DELETE' });
    cerrarModal();
    toast(`El lote se fue a la papelera (${b.documentos} archivo(s), ${b.comprobantes} comprobante(s)${b.trabajos_cancelados ? '; se canceló lo que faltaba procesar' : ''}).`);
    if (String(filtro.lote) === String(id)) filtro.lote = '';
    actualizarPendientes();
    if (vista === 'lotes') cargarLotes(); else if (vista === 'comprobantes') cargarCbtes(); else ir('lotes');
  } catch (e) {
    btn.disabled = false;
    const er = $('#borrarLoteErr');
    if (er) { er.textContent = e.message; er.classList.remove('hidden'); } else toast(e, true);
  }
}

// Dígito verificador del CUIT (el mismo control que hace el servidor).
function cuitValido(c) {
  const d = soloDig(c);
  if (d.length !== 11) return false;
  const pesos = [5, 4, 3, 2, 7, 6, 5, 4, 3, 2];
  let s = 0;
  for (let i = 0; i < 10; i++) s += Number(d[i]) * pesos[i];
  let v = 11 - (s % 11);
  if (v === 11) v = 0;
  if (v === 10) return false;
  return v === Number(d[10]);
}

async function pedirCambiarCliente(id) {
  let l;
  try { l = await loteDe(id); } catch (e) { toast(e, true); return; }
  const constatado = l.constatado || l.constatacion_pendiente;
  abrirModal('Cambiar el cliente del lote', `<form id="fCambioCliente" class="form-modal" data-id="${l.id}">
    <p>El lote <strong>${esc(nombreLote(l))}</strong> y todos sus comprobantes pasan al cliente nuevo. No se borra nada.</p>
    <label>Cliente actual<input value="${esc(l.cliente_cuit ? cuit(l.cliente_cuit) : 'Sin cliente')}" disabled></label>
    <label>CUIT del cliente nuevo<input id="clienteNuevo" required placeholder="20-12345678-9" inputmode="numeric" maxlength="13" list="misCuits" autocomplete="off"></label>
    ${constatado ? '<p class="aviso-mini">Lo que ya se constató con ARCA usó el CUIT del cliente de antes como consultante: se vuelve a constatar con el nuevo.</p>' : ''}
    <div class="login-err hidden" id="clienteErr"></div></form>`,
  { acciones: '<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="submit" form="fCambioCliente">Cambiar el cliente</button>' });
  setTimeout(() => $('#clienteNuevo')?.focus(), 50);
}

async function cambiarCliente(f) {
  const v = $('#clienteNuevo').value.trim();
  const er = $('#clienteErr');
  const mal = (m) => { er.textContent = m; er.classList.remove('hidden'); };
  if (!cuitValido(v)) { mal('Ese CUIT no es válido: revisá el dígito verificador.'); return; }
  try {
    const { cambio } = await api('/api/lotes/' + f.dataset.id, { method: 'PATCH', json: { cliente_cuit: soloDig(v) } });
    cerrarModal();
    let msg = cambio.antes === cambio.despues ? 'El lote ya era de ese cliente.' : `Listo: el lote y sus ${cambio.comprobantes} comprobante(s) son de ${cuit(cambio.despues)}.`;
    if (cambio.reconstatar) msg += cambio.constatacion_encolada ? ` ${cambio.reconstatar} se vuelven a constatar con el CUIT nuevo.` : ` ${cambio.reconstatar} quedan para volver a constatar.`;
    toast(msg);
    if (vista === 'lotes') cargarLotes(); else cargarCbtes();
  } catch (e) { mal(e.message); }
}

function pedirBorrarCbtes(ids, desdeRevision) {
  const n = ids.length;
  abrirModal(n === 1 ? 'Borrar el comprobante' : `Borrar ${mil(n)} comprobantes`, `<p>${n === 1 ? 'El comprobante se va' : `Los ${mil(n)} comprobantes se van`} a la papelera: dejan de aparecer en Comprobantes, Revisar, Resumen, Inicio y las exportaciones.</p>
    <p class="muted">Si a un archivo no le queda ningún comprobante, se va con ellos (y si lo volvés a subir, se procesa como nuevo). Se puede restaurar desde la Papelera, tal cual estaba, durante ${mil(PAP_DIAS)} días.</p>`,
  { acciones: `<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="button" class="peligro" data-accion="borrarCbtesConfirmar" data-ids="${ids.join(',')}"${desdeRevision ? ' data-rev="1"' : ''}>${n === 1 ? 'Borrar' : `Borrar (${mil(n)})`}</button>` });
}

async function borrarCbtes(btn) {
  const ids = btn.dataset.ids.split(',').map(Number);
  btn.disabled = true;
  try {
    const { borrado: b } = await api('/api/comprobantes/borrar', { method: 'POST', json: { ids } });
    cerrarModal();
    toast(`${b.comprobantes === 1 ? 'El comprobante se fue' : `${b.comprobantes} comprobantes se fueron`} a la papelera${b.documentos ? ` (y ${b.documentos} archivo(s) que quedaron vacíos)` : ''}.`);
    SEL.clear();
    actualizarPendientes();
    if (btn.dataset.rev && rev) {
      const sigue = rev.idx;
      listado.splice(sigue, 1);
      if (sigue < listado.length) abrirRevision(sigue); else cerrarRevision();
      if (vista === 'revisar') cargarBandeja();
      return;
    }
    cargarCbtes();
  } catch (e) { btn.disabled = false; toast(e, true); }
}

// Las casillas de Comprobantes: los elegidos de la página.
const SEL = new Set();
function pintarSeleccion() {
  const n = SEL.size;
  $('#barraSel').classList.toggle('hidden', !n);
  $('#selCuenta').textContent = n === 1 ? '1 elegido' : `${mil(n)} elegidos`;
  $('#btnBorrarSel').textContent = `Borrar (${mil(n)})`;
  const todos = $('#selTodos');
  if (todos) {
    todos.checked = n > 0 && listado.every((c) => SEL.has(c.id));
    todos.indeterminate = n > 0 && !todos.checked;
  }
  $$('#tablaCbtes tbody tr').forEach((tr) => tr.classList.toggle('elegido', SEL.has(Number(tr.dataset.id))));
}

// ---------- Comprobantes ----------
function qsFiltro(extra = {}) {
  const p = new URLSearchParams();
  const f = Object.assign({}, filtro, extra);
  for (const k of ['estado', 'clase', 'rubro', 'q', 'cliente', 'desde', 'hasta', 'lote', 'limite', 'offset']) if (f[k] !== '' && f[k] !== undefined && f[k] !== null) p.set(k, f[k]);
  return p.toString();
}

function leerFiltro() {
  filtro.estado = $('#fEstado').value;
  filtro.clase = $('#fClase').value;
  filtro.rubro = $('#fRubro').value;
  filtro.q = $('#fQ').value.trim();
  filtro.cliente = soloDig($('#fCliente').value);
  filtro.desde = $('#fDesde').value;
  filtro.hasta = $('#fHasta').value;
  filtro.lote = soloDig($('#fLote').value);
}

function pintarFiltro() {
  $('#fEstado').value = filtro.estado; $('#fClase').value = filtro.clase; $('#fRubro').value = filtro.rubro; $('#fQ').value = filtro.q; $('#fCliente').value = filtro.cliente;
  $('#fDesde').value = filtro.desde; $('#fHasta').value = filtro.hasta; $('#fLote').value = filtro.lote;
}

async function cargarCbtes() {
  pintarFiltro();
  try {
    pintarLoteBanner();
    const { comprobantes, total } = await api('/api/comprobantes?' + qsFiltro());
    listado = comprobantes;
    SEL.clear();
    pintarSeleccion();
    if (!comprobantes.length) { $('#tablaCbtes').innerHTML = vacio('doc', 'No hay comprobantes con estos filtros', 'Probá con otro estado, otras fechas o borrando la búsqueda.'); $('#pager').innerHTML = ''; return; }
    const sel = puede('borrar');
    $('#tablaCbtes').innerHTML = `<div class="tabla-scroll"><table class="t cbtes${sel ? ' con-sel' : ''}"><thead><tr>${sel ? '<th class="sel"><label class="selbox" data-accion="nada" title="Elegir todos los de esta página"><input type="checkbox" id="selTodos" data-accion="selTodos" aria-label="Elegir todos los de esta página"></label></th>' : ''}<th>Fecha</th><th>Comprobante</th><th>Emisor</th><th>Rubro</th><th class="num">Neto</th><th class="num">IVA</th><th class="num">Total</th><th>Estado</th><th>Observaciones</th></tr></thead><tbody>
      ${comprobantes.map((c, i) => {
        const iva = ['iva_27', 'iva_21', 'iva_10_5', 'iva_5', 'iva_2_5'].reduce((s, k) => s + Number(c[k] || 0), 0);
        const obs = (c.hallazgos || []).filter((h) => h.nivel !== 'info').map((h) => h.mensaje)[0] || '';
        return `<tr class="clic" data-accion="revisar" data-idx="${i}" data-id="${c.id}">${sel ? `<td class="sel"><label class="selbox" data-accion="nada"><input type="checkbox" data-accion="selCbte" data-id="${c.id}" aria-label="Elegir ${esc(nombreCbte(c))}"></label></td>` : ''}<td>${fecha(c.fecha)}${c.hora ? `<div class="muted mono">${esc(c.hora.slice(0, 5))}</div>` : ''}</td><td>${esc(nombreCbte(c))} ${ticketChip(c)}</td>
          <td>${esc(c.razon_social_emisor || '')}<div class="muted mono">${esc(cuit(c.cuit_emisor))}</div></td><td>${rubroChip(c.rubro)}</td>
          <td class="num">${pesos(c.neto_gravado)}</td><td class="num">${pesos(iva)}</td><td class="num">${pesos(c.total)}${c.moneda && c.moneda !== 'PES' ? ' ' + esc(c.moneda) : ''}</td>
          <td>${estado(c.estado)}</td><td class="muted">${esc(obs)}</td></tr>`;
      }).join('')}</tbody></table></div>`;
    const desde = filtro.offset + 1, hasta = filtro.offset + comprobantes.length;
    $('#pager').innerHTML = `<span>${desde} a ${hasta} de ${total}</span>
      ${filtro.offset > 0 ? '<button class="ghost mini" data-accion="pagina" data-dir="-1">Anteriores</button>' : ''}
      ${hasta < total ? '<button class="ghost mini" data-accion="pagina" data-dir="1">Siguientes</button>' : ''}`;
  } catch (e) { toast(e, true); }
}

// Con el filtro de un lote: de qué lote son, con Cambiar cliente y Borrar el lote (el detalle del lote).
async function pintarLoteBanner() {
  const b = $('#loteBanner');
  if (!filtro.lote) { b.innerHTML = ''; return; }
  try {
    const { lote: l } = await api('/api/lotes/' + filtro.lote);
    if (String(l.id) !== String(filtro.lote)) return;
    b.innerHTML = `<div class="lote-banner card"><div><strong>Lote ${l.id}${l.nombre ? ' · ' + esc(l.nombre) : ''}</strong><br>
      <span class="muted">${l.cliente_cuit ? 'Cliente ' + esc(cuit(l.cliente_cuit)) : 'Sin cliente'} · ${esc(l.origen)} · ${fechaHora(l.creado)} · ${mil(l.documentos)} archivo(s)${l.estado !== 'terminado' ? ` · procesando ${l.procesados}/${l.documentos}` : ''}</span></div>
      <div class="acciones-fila">${accionesLote(l, true)}<button type="button" class="ghost mini" data-accion="sinLote">Ver todos</button></div></div>`;
  } catch (e) {
    b.innerHTML = e.status === 404 ? `<div class="aviso-card">${I.info}<div>El lote ${esc(filtro.lote)} no existe o está en la <button type="button" class="link-btn" data-accion="ir" data-vista="papelera">Papelera</button>.</div><button type="button" class="ghost mini" data-accion="sinLote">Ver todos</button></div>` : '';
  }
}

// ---------- Papelera ----------
let papOffset = 0;
const PAP_LIM = 100;
let PAP = [];
const PAP_SEL = new Set(); // "tipo:id"
const claveItem = (it) => `${it.tipo}:${it.id}`;

function quedaHTML(it) {
  const d = it.dias_restantes;
  const cuando = fechaCorta(it.vence);
  if (d <= 0) return `<span class="txt-mal">Hoy</span><div class="muted">en la próxima pasada</div>`;
  return `<span class="${d <= 3 ? 'txt-mal' : ''}">${d === 1 ? 'En 1 día' : `En ${d} días`}</span><div class="muted">${esc(cuando)}</div>`;
}

function queHTML(it) {
  if (it.tipo === 'lote') {
    return `<span class="tipo-pap">${I.lotes}<span>Lote entero</span></span><strong>${esc(nombreLote({ id: it.id, nombre: it.lote_nombre }))}</strong>
      <div class="muted">${mil(it.documentos)} archivo(s), ${mil(it.comprobantes)} comprobante(s)</div>`;
  }
  const c = { tipo: it.cbte_tipo, punto_venta: it.punto_venta, numero: it.numero };
  return `<span class="tipo-pap">${I.comprobantes}<span>Comprobante</span></span><strong>${esc(nombreCbte(c))}</strong>
    <div class="muted">${esc(it.emisor || cuit(it.cuit_emisor))}${it.fecha ? ' · ' + fecha(it.fecha) : ''}${it.total ? ' · $ ' + pesos(it.total) : ''}</div>
    <div class="muted mono">${esc(it.archivo)}</div>`;
}

async function cargarPapelera() {
  const vaciar = puede('vaciar_papelera');
  try {
    const { items, total, dias } = await api(`/api/papelera?limite=${PAP_LIM}&offset=${papOffset}`);
    PAP = items;
    PAP_DIAS = dias;
    $('#papeleraSub').textContent = `Lo que se borró${YO.identidad.cuits ? ' de tus clientes' : ''}. Se puede restaurar tal cual estaba (con sus estados, correcciones y constatación) durante ${dias} días; después se borra para siempre, con sus archivos.`;
    $('#btnVaciar').classList.toggle('hidden', !vaciar || !total);
    for (const k of [...PAP_SEL]) if (!items.some((it) => claveItem(it) === k)) PAP_SEL.delete(k);
    pintarSelPapelera();
    if (!items.length) {
      $('#papelera').innerHTML = vacio('papelera', 'La papelera está vacía', 'Cuando borres un lote o comprobantes, van a aparecer acá hasta que se borren para siempre.');
      $('#papPager').innerHTML = '';
      return;
    }
    $('#papelera').innerHTML = `<div class="tabla-scroll"><table class="t pap con-sel"><thead><tr><th class="sel"><label class="selbox" data-accion="nada" title="Elegir todos los de esta página"><input type="checkbox" id="papTodos" data-accion="papTodos" aria-label="Elegir todos los de esta página"></label></th>
      <th>Qué</th><th>Lote y cliente</th><th>Se borró</th><th>Se borra para siempre</th><th></th></tr></thead><tbody>
      ${items.map((it) => `<tr data-clave="${claveItem(it)}"><td class="sel"><label class="selbox" data-accion="nada"><input type="checkbox" data-accion="papSel" data-clave="${claveItem(it)}" aria-label="Elegir"${PAP_SEL.has(claveItem(it)) ? ' checked' : ''}></label></td>
        <td>${queHTML(it)}</td>
        <td>${it.tipo === 'comprobante' ? `Lote ${it.lote_id}${it.lote_nombre ? ' · ' + esc(it.lote_nombre) : ''}${it.lote_borrado ? '<div class="aviso-mini">Su lote también está en la papelera</div>' : ''}` : `Lote ${it.id}`}
          <div class="muted mono">${it.cliente_cuit ? esc(cuit(it.cliente_cuit)) : 'Sin cliente'}</div></td>
        <td>${fechaHora(it.borrado_en)}<div class="muted">${esc(it.borrado_por)}</div></td>
        <td>${quedaHTML(it)}</td>
        <td class="acciones-fila"><button type="button" class="ghost mini" data-accion="restaurar" data-clave="${claveItem(it)}">${I.restaurar}<span>Restaurar</span></button>
          ${vaciar ? `<button type="button" class="ghost mini peligro" data-accion="borrarYa" data-clave="${claveItem(it)}">Borrar ya</button>` : ''}</td></tr>`).join('')}</tbody></table></div>`;
    tarjetas('#papelera');
    pintarSelPapelera();
    const hasta = papOffset + items.length;
    $('#papPager').innerHTML = `<span>${papOffset + 1} a ${hasta} de ${total}</span>
      ${papOffset > 0 ? '<button class="ghost mini" data-accion="papPagina" data-dir="-1">Anteriores</button>' : ''}
      ${hasta < total ? '<button class="ghost mini" data-accion="papPagina" data-dir="1">Siguientes</button>' : ''}`;
  } catch (e) { toast(e, true); }
}

function pintarSelPapelera() {
  const n = PAP_SEL.size;
  $('#barraPap').classList.toggle('hidden', !n);
  $('#papCuenta').textContent = n === 1 ? '1 elegido' : `${mil(n)} elegidos`;
  $('#btnRestaurarSel').textContent = `Restaurar (${mil(n)})`;
  $('#btnBorrarYaSel').textContent = `Borrar ya (${mil(n)})`;
  $('#btnBorrarYaSel').classList.toggle('hidden', !puede('vaciar_papelera'));
  const todos = $('#papTodos');
  if (todos) {
    todos.checked = n > 0 && PAP.every((it) => PAP_SEL.has(claveItem(it)));
    todos.indeterminate = n > 0 && !todos.checked;
  }
  $$('#papelera tbody tr').forEach((tr) => tr.classList.toggle('elegido', PAP_SEL.has(tr.dataset.clave)));
}

const refDe = (k) => { const [tipo, id] = k.split(':'); return { tipo, id: Number(id) }; };

async function restaurar(claves, btn) {
  if (btn) btn.disabled = true;
  try {
    const r = await api('/api/papelera/restaurar', { method: 'POST', json: { items: claves.map(refDe) } });
    PAP_SEL.clear();
    if (r.rechazados.length) {
      abrirModal('Algunas cosas no se pudieron restaurar', `<p>Se restauraron ${mil(r.restaurados)}. Estas no:</p><ul>${r.rechazados.map((x) => `<li><strong>${x.tipo === 'lote' ? 'Lote' : 'Comprobante'} ${x.id}</strong>: ${esc(x.error)}</li>`).join('')}</ul>`);
    } else toast(r.restaurados === 1 ? 'Restaurado: volvió como estaba.' : `Restaurados ${r.restaurados}: volvieron como estaban.`);
    actualizarPendientes();
  } catch (e) {
    if (e.status === 409 || e.status === 404) abrirModal('No se pudo restaurar', `<p>${esc(e.message)}</p>`);
    else toast(e, true);
  }
  if (btn) btn.disabled = false;
  cargarPapelera();
}

function pedirBorrarYa(claves) {
  const todo = !claves;
  const n = todo ? 0 : claves.length;
  const titulo = todo ? 'Vaciar la papelera' : n === 1 ? 'Borrar para siempre' : `Borrar para siempre ${mil(n)} cosas`;
  abrirModal(titulo, `<p>${todo ? 'Todo lo que está en la papelera' : n === 1 ? 'Esto' : 'Lo elegido'} se borra <strong>para siempre</strong>, con sus correcciones y los archivos que no use ningún otro lote. No se puede deshacer.</p>
    <label class="campo-conf"><span class="et">Para confirmar, escribí «borrar»</span><input id="borrarYaConf" autocomplete="off" spellcheck="false"></label>
    <div class="login-err hidden" id="borrarYaErr"></div>`,
  { acciones: `<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="button" class="peligro" data-accion="borrarYaConfirmar"${todo ? '' : ` data-claves="${claves.join(',')}"`} disabled>${todo ? 'Vaciar la papelera' : 'Borrar para siempre'}</button>` });
  setTimeout(() => $('#borrarYaConf')?.focus(), 50);
}

async function borrarYa(btn) {
  btn.disabled = true;
  const confirmacion = $('#borrarYaConf').value.trim();
  try {
    const r = btn.dataset.claves
      ? await api('/api/papelera/borrar', { method: 'POST', json: { items: btn.dataset.claves.split(',').map(refDe), confirmacion } })
      : await api('/api/papelera', { method: 'DELETE', json: { confirmacion } });
    const b = r.borrado;
    cerrarModal();
    PAP_SEL.clear();
    toast(`Borrado para siempre: ${b.lotes ? `${b.lotes} lote(s), ` : ''}${b.comprobantes} comprobante(s) y ${b.archivos} archivo(s).`);
    cargarPapelera();
  } catch (e) { btn.disabled = false; const er = $('#borrarYaErr'); er.textContent = e.message; er.classList.remove('hidden'); }
}

async function descargar(ruta, nombre) {
  try {
    const b = await api(ruta, { binario: true });
    const u = URL.createObjectURL(b);
    const a = document.createElement('a');
    a.href = u; a.download = nombre;
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(u), 10000);
  } catch (e) { toast(e, true); }
}

// ---------- Revisión ----------
const CAMPOS = [
  ['fecha', 'Fecha', 'date'], ['tipo', 'Código de tipo'], ['punto_venta', 'Punto de venta'], ['numero', 'Número'],
  ['cuit_emisor', 'CUIT emisor'], ['razon_social_emisor', 'Razón social emisor', 'texto', true], ['condicion_iva_emisor', 'Condición IVA emisor', 'texto', true],
  ['doc_nro_receptor', 'Doc. receptor'], ['razon_social_receptor', 'Razón social receptor'],
  ['neto_gravado', 'Neto gravado', 'm'], ['no_gravado', 'No gravado', 'm'], ['exento', 'Exento', 'm'],
  ['iva_21', 'IVA 21%', 'm'], ['iva_10_5', 'IVA 10,5%', 'm'], ['iva_27', 'IVA 27%', 'm'], ['iva_5', 'IVA 5%', 'm'], ['iva_2_5', 'IVA 2,5%', 'm'],
  ['otros_tributos', 'Otros tributos', 'm'], ['subtotal', 'Subtotal (B y C)', 'm'], ['total', 'Total', 'm'],
  ['moneda', 'Moneda'], ['cotizacion', 'Cotización'], ['cae', 'CAE'], ['vto_cae', 'Vto. CAE', 'date'],
];
const ORIGEN_RUBRO = { texto: 'Factorum lo dedujo del nombre y el texto.', ocr: 'Factorum lo dedujo del nombre y el texto.', arcanum: 'Sale de la actividad del emisor en el padrón de ARCA.', manual: 'Lo eligió una persona.' };

// Rubro y hora van arriba del formulario: son lo primero que se mira en un ticket.
function camposTicketHTML(c) {
  const o = (c.origenes || {}).rubro;
  const r = c.rubro || '';
  const ops = Object.keys(RUBROS).filter((k) => k !== 'sin_rubro')
    .map((k) => `<option value="${k}" ${k === r ? 'selected' : ''}>${esc(RUBROS[k])}</option>`).join('');
  const ayuda = r ? `${esc(ORIGEN_RUBRO[o] || '')} Si lo cambiás, se recuerda para este CUIT.` : 'Sin rubro. Si lo elegís, se recuerda para este CUIT.';
  const hora = c.es_tique || c.hora ? `<label><span class="et"><span>Hora</span>${(c.origenes || {}).hora ? `<span class="origen ${esc(c.origenes.hora)}">${esc(ORIGEN[c.origenes.hora] || c.origenes.hora)}</span>` : ''}</span>
      <input name="hora" value="${esc((c.hora || '').slice(0, 5))}" data-orig="${esc((c.hora || '').slice(0, 5))}" type="time"></label>` : '';
  return `<label class="${hora ? '' : 'ancho'}"><span class="et"><span>Rubro</span>${o ? `<span class="origen ${esc(o)}">${esc(ORIGEN[o] || o)}</span>` : ''}</span>
      <select name="rubro" data-orig="${esc(r)}"${puede('corregir') ? '' : ' disabled'}>${r ? '' : '<option value="" selected>Sin rubro</option>'}${ops}</select>
      <span class="ayuda">${ayuda}</span></label>${hora}`;
}

async function abrirRevision(idx) {
  if (idx < 0 || idx >= listado.length) return;
  const id = listado[idx].id;
  try {
    const { comprobante: c, correcciones } = await api('/api/comprobantes/' + id);
    if (rev && rev.url) URL.revokeObjectURL(rev.url);
    rev = { idx, cbte: c, url: null };
    $('#revision').classList.remove('hidden');
    $('#revTitulo').textContent = `${nombreCbte(c)}${c.es_tique ? ' (ticket)' : ''}  ·  ${c.archivo}`;
    $('#revPos').textContent = `${idx + 1} de ${listado.length}`;
    $('#revHallazgos').innerHTML = `<div class="fila" style="justify-content:space-between;margin-bottom:8px">${estado(c.estado)}<span class="muted">confianza ${Math.round((c.confianza || 0) * 100)}%</span></div>${hallazgosHTML(c.hallazgos)}`;
    $('#revForm').innerHTML = camposTicketHTML(c) + CAMPOS.filter(([k]) => !(c.es_tique && ['cae', 'vto_cae'].includes(k))).map(([k, t, tipo, ancho]) => {
      const o = (c.origenes || {})[k];
      const v = tipo === 'm' ? (Number(c[k] || 0) ? Number(c[k]).toFixed(2) : '') : (c[k] ?? '');
      return `<label class="${ancho ? 'ancho' : ''}"><span class="et"><span>${t}</span>${o ? `<span class="origen ${esc(o)}" title="Origen del dato">${esc(ORIGEN[o] || o)}</span>` : ''}</span>
        <input name="${k}" value="${esc(v === 0 ? '' : v)}" data-orig="${esc(v === 0 ? '' : v)}" ${tipo === 'date' ? 'type="date"' : ''} class="${tipo === 'm' ? 'num' : ''}" inputmode="${tipo === 'm' ? 'decimal' : 'text'}"${puede('corregir') ? '' : ' readonly'}></label>`;
    }).join('');
    $('#btnConstatar').classList.toggle('hidden', !(SALUD && SALUD.arcanum));
    const k = c.constatacion;
    $('#revConstatacion').innerHTML = k ? `<div class="card"><strong>${c.es_tique ? 'Control del emisor' : 'Constatación ARCA'}</strong><dl class="kv">
      <dt>Resultado</dt><dd>${c.es_tique ? (k.error ? esc(k.error) : 'Los tickets no se constatan en ARCA: se controló el CUIT y la base de apócrifos.') : k.valido === true ? 'Aprobado (el comprobante existe)' : k.valido === false ? 'Rechazado' : esc(k.error || 'sin respuesta')}</dd>
      ${(k.observaciones || []).length ? `<dt>Observaciones</dt><dd>${esc(k.observaciones.map((o) => o.message).join(' '))}</dd>` : ''}
      ${k.apoc && k.apoc.consultado ? `<dt>Apócrifos</dt><dd>${k.apoc.esApocrifo ? 'El emisor figura como apócrifo' : 'No figura'}</dd>` : ''}
      <dt>Fecha</dt><dd>${k.fecha ? new Date(k.fecha).toLocaleString('es-AR') : ''}</dd></dl></div>` : '';
    const det = $('#revCorrecciones');
    det.classList.toggle('hidden', !correcciones.length);
    det.lastElementChild.innerHTML = correcciones.length ? `<table class="t"><thead><tr><th>Campo</th><th>Antes</th><th>Después</th><th>Quién</th><th>Cuándo</th></tr></thead><tbody>${correcciones.map((x) =>
      `<tr><td>${esc(x.campo)}</td><td class="mono">${esc(x.antes)}</td><td class="mono">${esc(x.despues)}</td><td>${esc(x.usuario)}</td><td>${new Date(x.creado).toLocaleString('es-AR')}</td></tr>`).join('')}</tbody></table>` : '';
    // Original a la izquierda.
    $('#revDoc').innerHTML = '<p class="muted" style="padding:16px">Cargando el archivo...</p>';
    const pag = (c.paginas || [1])[0];
    const b = await api(`/api/documentos/${c.documento_id}/pagina/${pag}`, { binario: true });
    if (!rev || rev.cbte.id !== c.id) return; // se cerró o se pasó a otro mientras cargaba
    rev.url = URL.createObjectURL(b);
    $('#revDoc').innerHTML = `<div class="rev-doc-barra"><span class="muted">${esc(c.archivo)}${(c.paginas || []).length ? ', página ' + pag : ''}</span>
      <button class="ghost mini" data-accion="abrirOriginal" data-doc="${c.documento_id}">Abrir el original</button></div>
      <div class="rev-doc-img"><img src="${rev.url}" alt="Comprobante original"></div>`;
  } catch (e) { toast(e, true); }
}

function cerrarRevision() {
  $('#revision').classList.add('hidden');
  if (rev && rev.url) URL.revokeObjectURL(rev.url);
  rev = null;
  if (vista === 'conciliacion') cargarConciliacion(false); else if (vista === 'libro_iva') cargarLibro(); else cargarCbtes();
}

function cambiosRevision() {
  const campos = {};
  $$('#revForm input, #revForm select').forEach((i) => { if (i.value !== i.dataset.orig) campos[i.name] = i.value; });
  return campos;
}

async function guardarRevision(aprobar) {
  if (!rev || !puede('corregir')) return;
  const campos = cambiosRevision();
  if (!aprobar && !Object.keys(campos).length) { toast('No hay cambios'); return; }
  try {
    const { comprobante } = await api('/api/comprobantes/' + rev.cbte.id, { method: 'PATCH', json: { campos, aprobar } });
    listado[rev.idx] = comprobante;
    actualizarPendientes();
    if (aprobar) {
      toast('Aprobado');
      if (rev.idx + 1 < listado.length) abrirRevision(rev.idx + 1); else cerrarRevision();
    } else {
      toast('Guardado');
      abrirRevision(rev.idx);
    }
  } catch (e) { toast(e, true); }
}

async function constatarRevision() {
  if (!rev) return;
  try {
    await api(`/api/comprobantes/${rev.cbte.id}/constatar`, { method: 'POST' });
    toast('Constatado');
    abrirRevision(rev.idx);
  } catch (e) { toast(e, true); }
}

// ---------- Resumen ----------
function porRubroHTML(r) {
  const pr = Object.entries(r.por_rubro || {}).sort((a, b) => b[1] - a[1]);
  if (!pr.length) return '';
  const max = Math.max(1, ...pr.map(([, v]) => Math.abs(v)));
  return `<div><strong>Por rubro</strong>${filtro.clase === 'tickets' ? ' <span class="muted">(gastos con ticket)</span>' : ''}</div><div class="rubros">${pr.map(([k, v]) =>
    `<button type="button" class="rub" data-accion="verRubro" data-rubro="${esc(k)}"><span class="n">${rubroChip(k)}</span><span class="barra"><i class="r-${esc(k)}" style="width:${Math.max(1, Math.round((Math.abs(v) / max) * 100))}%"></i></span><span class="mono">$ ${pesos(v)}</span></button>`).join('')}</div>`;
}

async function cargarResumen() {
  try {
    const { resumen: r } = await api('/api/resumen?' + qsFiltro({ limite: '', offset: '' }));
    const fila = (t, v, fuerte) => `<tr><td>${fuerte ? `<strong>${t}</strong>` : t}</td><td class="num">${fuerte ? `<strong>$ ${pesos(v)}</strong>` : '$ ' + pesos(v)}</td></tr>`;
    $('#resumen').innerHTML = `<div class="fila" style="align-items:flex-start">
      <table class="t" style="max-width:460px"><tbody>
        <tr><td>Comprobantes</td><td class="num">${r.cantidad}</td></tr>
        ${r.tickets && r.tickets.cantidad ? `<tr><td>De esos, tickets</td><td class="num">${r.tickets.cantidad} · $ ${pesos(r.tickets.total)}</td></tr>` : ''}
        ${fila('Neto gravado', r.neto_gravado)}${fila('No gravado', r.no_gravado)}${fila('Exento', r.exento)}
        ${fila('IVA 21%', r.iva_21)}${fila('IVA 10,5%', r.iva_10_5)}${fila('IVA 27%', r.iva_27)}${fila('IVA 5%', r.iva_5)}${fila('IVA 2,5%', r.iva_2_5)}
        ${fila('IVA total', r.iva_total, true)}${fila('Otros tributos', r.otros_tributos)}${fila('Subtotal B y C', r.subtotal_b_c)}${fila('Total', r.total, true)}
      </tbody></table>
      <div class="card" style="flex:1;min-width:260px"><strong>Estados</strong><dl class="kv">
        ${Object.entries(r.por_estado || {}).map(([k, v]) => `<dt>${estado(k)}</dt><dd>${v}</dd>`).join('')}
        <dt>Moneda extranjera</dt><dd>${r.moneda_extranjera} (no sumados)</dd></dl>
        <strong>Tipos</strong><dl class="kv">${Object.entries(r.por_tipo || {}).map(([k, v]) => `<dt>${esc(k)}</dt><dd>${v}</dd>`).join('')}</dl>
        ${porRubroHTML(r)}
        <div class="fila"><button class="ghost" data-accion="exportar" data-formato="xlsx">Descargar Excel</button><button class="ghost" data-accion="exportar" data-formato="csv">Descargar CSV</button></div>
      </div></div>`;
  } catch (e) { toast(e, true); }
}

// ---------- Conciliación ----------
const GRUPOS_CONC = [['coinciden', 'Coinciden'], ['diferencias', 'Con diferencias'], ['falta_cargar', 'Falta cargar'], ['no_figura', 'No figura en ARCA']];
const CAUSAS = { tique: 'Tique de controlador fiscal', exterior: 'Moneda extranjera o exterior', otro_receptor: 'Otro receptor', no_sincronizado: 'Período sin sincronizar',
  nota: 'Nota de crédito o débito', incompleto: 'Faltan datos', sin_explicacion: 'Sin explicación' };
const CONSTATADO = { valido: ['ok', 'Válido en ARCA'], invalido: ['error', 'ARCA no lo reconoce'], apocrifo: ['error', 'Emisor apócrifo'], controlado: ['ok', 'Emisor controlado'], error: ['revisar', 'No se pudo constatar'] };
const CAMPO_DIF = { total: 'Total', neto_gravado: 'Neto', iva: 'IVA', fecha: 'Fecha', moneda: 'Moneda', punto_venta: 'Punto de venta', tipo: 'Tipo' };
const CONC = { clientes: null, datos: null, tab: '', orden: {}, cliente: '', desde: '', hasta: '', cargando: false };

const isoLocal = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
function periodoAtajo(a) {
  const hoy = new Date();
  const y = hoy.getFullYear(), m = hoy.getMonth();
  if (a === 'este_mes') return [isoLocal(new Date(y, m, 1)), isoLocal(hoy)];
  if (a === 'tres_meses') return [isoLocal(new Date(y, m - 3, 1)), isoLocal(new Date(y, m, 0))];
  return [isoLocal(new Date(y, m - 1, 1)), isoLocal(new Date(y, m, 0))]; // mes pasado
}
function aplicarAtajo(a) {
  const [d, h] = periodoAtajo(a);
  $('#concDesde').value = d; $('#concHasta').value = h;
  marcarAtajo();
}
function marcarAtajo() {
  $$('#fConciliacion [data-atajo]').forEach((b) => {
    const [d, h] = periodoAtajo(b.dataset.atajo);
    b.classList.toggle('on', $('#concDesde').value === d && $('#concHasta').value === h);
  });
}

async function prepararConciliacion() {
  if (!$('#concDesde').value) aplicarAtajo('mes_pasado');
  if (!CONC.clientes) {
    try {
      const r = await api('/api/conciliacion/clientes');
      CONC.clientes = r.clientes || [];
      CONC.arcanum = r.arcanum;
    } catch (e) { toast(e, true); return; }
  }
  const sel = $('#concCliente');
  const restringido = !!(YO && YO.identidad && YO.identidad.cuits);
  const cs = CONC.clientes;
  const previo = CONC.cliente || sel.value;
  sel.innerHTML = (cs.length === 1 ? '' : '<option value="">Elegí un cliente</option>')
    + cs.map((c) => `<option value="${esc(c.cuit)}">${esc(cuit(c.cuit))}${c.nombre ? ' · ' + esc(c.nombre) : ''}${c.comprobantes ? ` (${mil(c.comprobantes)})` : ' (sin comprobantes)'}</option>`).join('')
    + (restringido ? '' : '<option value="otro">Otro CUIT...</option>');
  if (cs.length === 1) sel.value = cs[0].cuit;
  else if (previo && [...sel.options].some((o) => o.value === previo)) sel.value = previo;
  $('#concOtroWrap').classList.toggle('hidden', sel.value !== 'otro');
  if (!CONC.datos) pintarConciliacionVacia();
  else pintarConciliacion();
}

function pintarConciliacionVacia() {
  if (CONC.arcanum === false) {
    $('#conciliacion').innerHTML = vacio('conciliacion', 'Factorum no está conectado con Arcanum',
      'La conciliación compara lo cargado con Mis Comprobantes de ARCA, que baja Arcanum. Un admin lo conecta en Configuración, Arcanum.', puede('configuracion') ? 'configuracion' : '', 'Ir a Configuración');
    return;
  }
  if (CONC.clientes && !CONC.clientes.length) {
    $('#conciliacion').innerHTML = vacio('conciliacion', 'Todavía no hay clientes para conciliar',
      'Cargá facturas con el CUIT del cliente (o elegí «Otro CUIT») y después conciliá un período contra lo que ARCA dice que recibió.', 'subir', 'Cargar facturas');
    return;
  }
  $('#conciliacion').innerHTML = `<div class="vacio"><div class="ico">${I.conciliacion}</div><h3>Elegí el cliente y el período</h3>
    <p>Factorum le pide a Arcanum lo que ARCA dice que el cliente recibió en esas fechas (por fecha de emisión) y lo cruza con lo que cargaste.</p>
    <div class="pasos">
      <div class="paso"><span class="n">Coinciden</span><span class="muted">Está en los dos con el mismo emisor, número, fecha y total (un centavo de tolerancia).</span></div>
      <div class="paso"><span class="n">Con diferencias</span><span class="muted">Está en los dos pero algo no cierra: te dice qué y cuánto.</span></div>
      <div class="paso"><span class="n">Falta cargar</span><span class="muted">ARCA lo tiene y vos no: copiá la lista y pedíselo al cliente.</span></div>
      <div class="paso"><span class="n">No figura en ARCA</span><span class="muted">Lo cargaste y ARCA no lo tiene: tiques, exterior y otras causas legítimas aparte del resto.</span></div>
    </div></div>`;
}

function clienteConc() {
  const v = $('#concCliente').value;
  return v === 'otro' ? soloDig($('#concOtro').value) : v;
}

async function conciliar(refrescar) {
  const cli = clienteConc();
  const desde = $('#concDesde').value, hasta = $('#concHasta').value;
  if (!cli) { toast('Elegí el cliente', true); return; }
  if (cli.length !== 11) { toast('El CUIT del cliente tiene que tener 11 dígitos', true); return; }
  if (!desde || !hasta) { toast('Elegí el período', true); return; }
  Object.assign(CONC, { cliente: cli, desde, hasta });
  await cargarConciliacion(refrescar);
}

async function cargarConciliacion(refrescar) {
  if (!CONC.cliente || CONC.cargando) return;
  CONC.cargando = true;
  const btn = $('#concBtn');
  btn.disabled = true; btn.textContent = 'Conciliando...';
  if (!CONC.datos) $('#conciliacion').innerHTML = '<p class="muted">Pidiendo a Arcanum lo que ARCA dice que recibió el cliente...</p>';
  try {
    const q = new URLSearchParams({ cliente: CONC.cliente, desde: CONC.desde, hasta: CONC.hasta });
    if (refrescar) q.set('refrescar', '1');
    const d = await api('/api/conciliacion?' + q);
    const cambio = !CONC.datos || CONC.datos.cliente !== d.cliente || CONC.datos.desde !== d.desde || CONC.datos.hasta !== d.hasta;
    CONC.datos = d;
    if (cambio || !CONC.tab) {
      const g = (d.resultado || {}).grupos || {};
      CONC.tab = ['diferencias', 'falta_cargar', 'no_figura', 'coinciden'].find((k) => (g[k] || []).length) || 'diferencias';
    }
    pintarConciliacion();
  } catch (e) { toast(e, true); if (!CONC.datos) pintarConciliacionVacia(); } finally {
    CONC.cargando = false;
    btn.disabled = false; btn.textContent = 'Conciliar';
  }
}

const diasEntre = (a, b) => Math.round((new Date(b + 'T00:00:00') - new Date(a + 'T00:00:00')) / 86400000) + 1;
const rangoTxt = (r) => (r.desde === r.hasta ? fecha(r.desde) : `${fecha(r.desde)} al ${fecha(r.hasta)}`);

function syncHTML(d) {
  const k = d.cobertura;
  const hace = d.consultado ? fechaHora(d.consultado) : '';
  const btnAct = `<button type="button" class="ghost mini" data-accion="concRefrescar" title="Volver a pedirle a Arcanum">Actualizar</button>`;
  if (!k) return '';
  const ult = k.ultimaSincronizacion;
  const partes = [];
  partes.push(ult ? `Última sincronización: <strong>${esc(fechaHora(ult.terminada))}</strong>` : 'Todavía no terminó ninguna sincronización');
  if ((k.rangos || []).length) partes.push(`bajado del ${k.rangos.map(rangoTxt).join(', ')}`);
  if (k.enCurso) partes.push('<span class="est revisar">sincronizando ahora</span>');
  else if (k.ultimaCorrida && k.ultimaCorrida.estado === 'error') partes.push(`<span class="txt-mal">la última corrida falló${k.ultimaCorrida.error ? ': ' + esc(k.ultimaCorrida.error) : ''}</span>`);
  let h = `<div class="conc-sync"><div class="conc-sync-ico">${I.conectar}</div><div class="conc-sync-txt"><strong>Mis Comprobantes en Arcanum</strong><br><span class="muted">${partes.join(' · ')}</span>
    <br><span class="muted mini-txt">Consultado ${esc(hace)}${d.de_cache ? ' (guardado unos minutos)' : ''}.</span></div>${btnAct}</div>`;
  const faltan = d.sin_sincronizar || [];
  if (faltan.length) {
    const dias = faltan.reduce((s, r) => s + diasEntre(r.desde, r.hasta), 0);
    const todo = dias >= diasEntre(d.desde, d.hasta);
    const app = SUITE.find((a) => a.app === 'arcanum');
    h += `<div class="aviso-mini${todo ? ' mal' : ''}">${I.alerta}<span><strong>${todo ? 'Arcanum no tiene sincronizado este período.' : 'El período no está completo en Arcanum.'}</strong>
      Falta${faltan.length > 1 || dias > 1 ? 'n' : ''} ${faltan.map(rangoTxt).join(', ')}. Lo que cae ahí puede aparecer como «Falta cargar» o «No figura en ARCA» sin serlo: sincronizá los recibidos de esas fechas en Arcanum y volvé a conciliar.</span>
      ${app ? '<button type="button" class="ghost mini" data-accion="abrirApp" data-app="arcanum">Abrir Arcanum</button>' : ''}</div>`;
  }
  if (d.truncado) h += `<div class="aviso-mini mal">${I.alerta}<span>${esc(d.mensaje || 'Arcanum tiene más recibidos de los que manda de una vez: conciliá un período más corto.')}</span></div>`;
  if (d.sin_fecha) h += `<div class="aviso-mini">${I.info}<span>Este cliente tiene ${mil(d.sin_fecha)} comprobante${d.sin_fecha > 1 ? 's' : ''} sin fecha: no entra${d.sin_fecha > 1 ? 'n' : ''} en ningún período. Completales la fecha en Revisar.</span></div>`;
  return h;
}

function estadoNoListo(d) {
  const ico = { sin_arcanum: 'conectar', sin_sincronizar: 'conciliacion', sin_permiso: 'llave' }[d.estado] || 'alerta';
  const tit = { sin_arcanum: 'Factorum no está conectado con Arcanum', sin_sincronizar: 'Arcanum todavía no bajó Mis Comprobantes de este cliente',
    sin_permiso: 'La clave de Arcanum no incluye este cliente', arcanum_viejo: 'Hay que actualizar Arcanum', arcanum_caido: 'Arcanum no responde',
    arcanum_error: 'Arcanum no pudo responder' }[d.estado] || 'No se pudo conciliar';
  const extra = d.en_factorum ? ` En Factorum hay ${mil(d.en_factorum)} comprobante${d.en_factorum > 1 ? 's' : ''} de este cliente en el período.` : '';
  const app = SUITE.find((a) => a.app === 'arcanum');
  const boton = d.estado === 'sin_arcanum' && puede('configuracion') ? '<button type="button" data-accion="irConf" data-sec="arcanum">Configurar Arcanum</button>'
    : ['sin_sincronizar', 'sin_permiso'].includes(d.estado) && app ? '<button type="button" data-accion="abrirApp" data-app="arcanum">Abrir Arcanum</button>'
    : ['arcanum_caido', 'arcanum_error'].includes(d.estado) ? '<button type="button" data-accion="concRefrescar">Probar de nuevo</button>' : '';
  return `${d.cobertura ? syncHTML(d) : ''}<div class="vacio"><div class="ico">${I[ico]}</div><h3>${esc(tit)}</h3><p>${esc(d.mensaje || '')}${esc(extra)}</p>${boton}</div>`;
}

// El período entero cae en días que Arcanum no bajó: los grupos vacíos no dicen nada.
const periodoSinArca = (d) => (d.sin_sincronizar || []).reduce((s, r) => s + diasEntre(r.desde, r.hasta), 0) >= diasEntre(d.desde, d.hasta);

function kpiConc(g, t, d) {
  const n = t.cantidad || 0;
  const ext = Object.entries(t.moneda_extranjera || {}).map(([m, v]) => `${esc(m)} ${pesos(v)}`).join(' · ');
  const monto = g === 'no_figura' ? t.pesos_factorum : g === 'diferencias' ? t.pesos_arca : t.pesos;
  const sinArca = periodoSinArca(d);
  let pie = n ? `$ ${pesos(monto)}${g === 'diferencias' ? ' en ARCA' : ''}${ext ? ' · ' + ext : ''}` : { coinciden: 'nada todavía', diferencias: 'todo cierra', falta_cargar: 'no falta nada', no_figura: 'todo figura' }[g];
  if (!n && sinArca && g !== 'no_figura') pie = 'sin datos de ARCA';
  if (g === 'diferencias' && n) pie += `<br>$ ${pesos(t.pesos_factorum)} en Factorum${t.casi_iguales ? ` · ${t.casi_iguales} casi igual${t.casi_iguales > 1 ? 'es' : ''}` : ''}`;
  if (g === 'no_figura' && n) pie += `<br>${t.legitimas || 0} con causa legítima${t.alarmas ? ` · <strong>${t.alarmas} con alarma</strong>` : ''}`;
  const cls = !n ? '' : g === 'coinciden' ? 'bien' : g === 'diferencias' ? 'aviso' : g === 'falta_cargar' ? 'alerta' : (t.alarmas ? 'alerta' : (n - (t.legitimas || 0)) ? 'aviso' : '');
  return `<button type="button" class="kpi conc-kpi ${cls}${CONC.tab === g ? ' on' : ''}" data-accion="concTab" data-grupo="${g}" aria-pressed="${CONC.tab === g}">
    <span class="et">${esc(GRUPOS_CONC.find((x) => x[0] === g)[1])}</span><span class="val">${mil(n)}</span><span class="kpi-pie">${pie}</span></button>`;
}

const nroCbte = (it) => `${String(it.punto_venta || 0).padStart(5, '0')}-${String(it.numero || 0).padStart(8, '0')}`;
const firmaDe = (it) => (it.clase === 'nota_credito' ? -1 : 1);

function difsHTML(it) {
  return (it.diferencias || []).map((x) => {
    const n = CAMPO_DIF[x.campo] || x.campo;
    if (['total', 'neto_gravado', 'iva'].includes(x.campo)) return `<li><strong>${n}</strong>: Factorum $ ${pesos(x.factorum)}, ARCA $ ${pesos(x.arca)} <span class="dif">${Number(x.diferencia) > 0 ? '+' : ''}${pesos(x.diferencia)}</span></li>`;
    if (x.campo === 'fecha') return `<li><strong>Fecha</strong>: Factorum ${fecha(x.factorum)}, ARCA ${fecha(x.arca)} <span class="dif">${x.dias > 0 ? '+' : ''}${x.dias} día${Math.abs(x.dias) === 1 ? '' : 's'}</span></li>`;
    return `<li><strong>${n}</strong>: Factorum ${esc(x.factorum)}, ARCA ${esc(x.arca)}</li>`;
  }).join('');
}

function dupHTML(it) {
  const p = [];
  if ((it.duplicados_factorum || []).length) p.push(`<span class="chip-dup" title="El mismo comprobante está cargado más de una vez">Cargado ${it.duplicados_factorum.length + 1} veces</span>`
    + it.duplicados_factorum.map((id) => ` <button type="button" class="link-btn" data-accion="concAbrir" data-id="${id}">ver ${id}</button>`).join(''));
  if (it.duplicados_arca) p.push(`<span class="chip-dup">Repetido en ARCA</span>`);
  return p.join(' ');
}

// Columnas de cada pestaña: [clave de orden, título, celda, clase].
function columnasConc(g) {
  const fechaC = ['fecha', 'Fecha', (it) => fecha(it.fecha)];
  const cbteC = ['comprobante', 'Comprobante', (it) => `${esc(it.nombre_tipo)}<div class="mono nowrap">${nroCbte(it)}</div>${it.casi_igual ? ' <span class="chip-casi" title="Mismo emisor y número con otro punto de venta o tipo: puede ser un error de lectura">Casi igual</span>' : ''}`];
  const emisorC = ['emisor', 'Emisor', (it) => `${esc(it.emisor || '')}<div class="muted mono">${esc(cuit(it.cuit_emisor))}</div>`];
  const totalC = ['total', 'Total', (it) => `$ ${pesos(firmaDe(it) * it.total)}${it.moneda_extranjera ? ' ' + esc(it.moneda) : ''}`, 'num'];
  const abrirC = ['', '', (it) => (it.factorum ? `<button type="button" class="ghost mini" data-accion="concAbrir" data-id="${it.factorum.id}">Abrir</button>` : '')];
  if (g === 'coinciden') return [fechaC, cbteC, emisorC, totalC, ['', '', (it) => [it.nota ? `<span class="muted">${esc(it.nota)}</span>` : '', dupHTML(it)].filter(Boolean).join('<br>')], abrirC];
  if (g === 'diferencias') {
    return [fechaC, cbteC, emisorC,
      ['arca', 'ARCA', (it) => `$ ${pesos(firmaDe(it) * it.arca.total)}${it.arca.moneda !== 'PES' ? ' ' + esc(it.arca.moneda) : ''}`, 'num'],
      ['factorum', 'Factorum', (it) => `$ ${pesos(firmaDe(it) * it.factorum.total)}${it.factorum.moneda !== 'PES' ? ' ' + esc(it.factorum.moneda) : ''}`, 'num'],
      ['diferencia', 'Qué no cierra', (it) => `<ul class="difs">${difsHTML(it)}</ul>${it.nota ? `<div class="muted">${esc(it.nota)}</div>` : ''}${dupHTML(it)}`], abrirC];
  }
  if (g === 'falta_cargar') {
    return [fechaC, cbteC, emisorC, totalC,
      ['', '', (it) => [it.arca && it.arca.emisor_apocrifo ? '<span class="est error">Emisor apócrifo</span>' : '', it.nota ? `<span class="muted">${esc(it.nota)}</span>` : '', dupHTML(it)].filter(Boolean).join('<br>')]];
  }
  return [fechaC, cbteC, emisorC, totalC,
    ['causa', 'Causa', (it) => `<span class="causa ${it.alarma ? 'alarma' : it.legitima ? 'legitima' : 'pendiente'}">${esc(it.alarma ? 'Alarma: no lo computes' : CAUSAS[it.causa] || it.causa)}</span><div class="muted">${esc(it.nota || '')}</div>${dupHTML(it)}`],
    ['constatacion', 'Constatación', (it) => {
      const k = CONSTATADO[(it.factorum || {}).constatacion];
      return k ? `<span class="est ${k[0]}">${k[1]}</span>` : '<span class="muted">Sin constatar</span>';
    }],
    ['', '', (it) => `<div class="acc-apiladas">${it.constatar && puede('corregir') && SALUD && SALUD.arcanum ? `<button type="button" class="ghost mini" data-accion="concConstatar" data-id="${it.factorum.id}">Constatar</button>` : ''}${it.factorum ? `<button type="button" class="ghost mini" data-accion="concAbrir" data-id="${it.factorum.id}">Abrir</button>` : ''}</div>`]];
}

const ORDEN_CONC = {
  fecha: (it) => it.fecha || '',
  comprobante: (it) => `${String(it.tipo).padStart(3, '0')}${nroCbte(it)}`,
  emisor: (it) => (it.emisor || it.cuit_emisor || '').toLowerCase(),
  total: (it) => firmaDe(it) * Number(it.total || 0),
  arca: (it) => firmaDe(it) * Number((it.arca || {}).total || 0),
  factorum: (it) => firmaDe(it) * Number((it.factorum || {}).total || 0),
  diferencia: (it) => Math.abs(Number(((it.diferencias || []).find((x) => x.campo === 'total') || {}).diferencia || 0)),
  causa: (it) => `${it.alarma ? 0 : it.legitima ? 2 : 1}${CAUSAS[it.causa] || ''}`,
  constatacion: (it) => (it.factorum || {}).constatacion || '',
};

function itemsConc(g) {
  const its = [...(((CONC.datos || {}).resultado || {}).grupos || {})[g] || []];
  const o = CONC.orden[g];
  if (o && ORDEN_CONC[o.col]) {
    const f = ORDEN_CONC[o.col];
    its.sort((a, b) => { const x = f(a), y = f(b); return (x < y ? -1 : x > y ? 1 : 0) * o.dir; });
  }
  return its;
}

const VACIO_CONC = {
  coinciden: ['Nada coincide todavía', 'Cuando un comprobante cargado sea igual al de ARCA (emisor, número, fecha y total) aparece acá.'],
  diferencias: ['No hay diferencias', 'Todo lo que está en los dos lados cierra: mismo total, fecha y moneda.'],
  falta_cargar: ['No falta cargar nada', 'Todo lo que ARCA tiene de este período está cargado en Factorum.'],
  no_figura: ['Todo lo cargado figura en ARCA', 'Ninguno de los comprobantes cargados de este período falta en Mis Comprobantes.'],
};

function pintarConciliacion() {
  const d = CONC.datos;
  if (!d) { pintarConciliacionVacia(); return; }
  if (d.estado !== 'lista' || !d.resultado) { $('#conciliacion').innerHTML = estadoNoListo(d); return; }
  const r = d.resultado;
  const g = CONC.tab;
  const its = itemsConc(g);
  const cols = columnasConc(g);
  const o = CONC.orden[g] || {};
  const nombreCli = ((CONC.clientes || []).find((c) => c.cuit === d.cliente) || {}).nombre || '';
  const accionesTab = g === 'falta_cargar' && its.length
    ? `<button type="button" class="ghost mini" data-accion="concCopiar">${I.copiar}<span>Copiar la lista</span></button><button type="button" class="ghost mini" data-accion="concExportar" data-formato="xlsx" data-grupo="falta_cargar">Excel para el cliente</button>`
    : '';
  const tabla = its.length ? `<div class="tabla-scroll"><table class="t conc-t"><thead><tr>${cols.map(([k, t, , cls]) => (k
    ? `<th class="${cls || ''}" aria-sort="${o.col === k ? (o.dir > 0 ? 'ascending' : 'descending') : 'none'}"><button type="button" class="orden${o.col === k ? ' on' : ''}" data-accion="concOrden" data-col="${k}">${t}<span class="flecha" aria-hidden="true">${o.col === k ? (o.dir > 0 ? '▲' : '▼') : ''}</span></button></th>`
    : `<th>${t}</th>`)).join('')}</tr></thead><tbody>
    ${its.map((it) => `<tr class="${it.alarma ? 'fila-alarma' : ''}">${cols.map(([, , f, cls]) => `<td class="${cls || ''}">${f(it)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`
    : periodoSinArca(d) && g !== 'no_figura' ? vacio('alerta', 'Sin datos de ARCA para este período', 'Arcanum no bajó Mis Comprobantes de estas fechas, así que no hay con qué comparar. Sincronizá los recibidos en Arcanum y volvé a conciliar.')
    : vacio(g === 'coinciden' ? 'doc' : 'check', VACIO_CONC[g][0], VACIO_CONC[g][1]);
  const ayudaTab = {
    diferencias: 'Están en los dos lados, pero algo no cierra. «Casi igual» es una sugerencia: mismo emisor y número con otro punto de venta o tipo, típico de un error de lectura; revisalo en Factorum.',
    falta_cargar: 'Figuran en ARCA como recibidas por el cliente y no están cargadas: pedíselas.',
    no_figura: 'Las cargaste y no están en los recibidos de ARCA. Las causas legítimas (tiques, exterior, otro receptor, período sin sincronizar, notas) van en gris; el resto, constatalo.',
    coinciden: 'Mismo emisor, tipo, punto de venta, número, fecha y total (y neto e IVA en las A cuando ARCA los trae), con un centavo de tolerancia.',
  }[g];
  $('#conciliacion').innerHTML = `${syncHTML(d)}
    <div class="conc-cab"><div><strong>${esc(cuit(d.cliente))}${nombreCli ? ' · ' + esc(nombreCli) : ''}</strong> <span class="muted">del ${fecha(d.desde)} al ${fecha(d.hasta)} · ${mil(d.en_factorum)} en Factorum · ${mil(d.en_arca)} en ARCA</span>
      ${r.duplicados.factorum || r.duplicados.arca ? `<br><span class="chip-dup">${r.duplicados.factorum ? `${r.duplicados.factorum} duplicado${r.duplicados.factorum > 1 ? 's' : ''} en Factorum` : ''}${r.duplicados.factorum && r.duplicados.arca ? ' · ' : ''}${r.duplicados.arca ? `${r.duplicados.arca} repetido${r.duplicados.arca > 1 ? 's' : ''} en ARCA` : ''}</span>` : ''}</div>
      <div class="acciones">${puede('libro_iva') ? `<button type="button" class="ghost mini" data-accion="concALibro" title="Las compras del mes en el formato del Libro IVA Digital">${I.libro_iva}<span>Exportar al Libro IVA</span></button>` : ''}<button type="button" class="ghost mini" data-accion="concExportar" data-formato="xlsx">Excel</button><button type="button" class="ghost mini" data-accion="concExportar" data-formato="csv">CSV</button></div></div>
    <div class="kpis conc-kpis">${GRUPOS_CONC.map(([k]) => kpiConc(k, r.totales[k], d)).join('')}</div>
    <div class="conc-barra"><div class="tabs" role="tablist">${GRUPOS_CONC.map(([k, t]) => `<button type="button" role="tab" aria-selected="${k === g}" class="${k === g ? 'on' : ''}" data-accion="concTab" data-grupo="${k}">${t} <span class="cnt">${r.grupos[k].length}</span></button>`).join('')}</div>
      <div class="acciones">${accionesTab}</div></div>
    <p class="muted conc-ayuda">${esc(ayudaTab)}</p>
    ${tabla}
    <p class="muted mini-txt">Las notas de crédito restan. Lo que está en moneda extranjera va aparte y no se suma con los pesos.</p>`;
  tarjetas('#conciliacion');
}

function ordenarConc(col) {
  const g = CONC.tab;
  const o = CONC.orden[g];
  CONC.orden[g] = o && o.col === col ? { col, dir: -o.dir } : { col, dir: ['total', 'arca', 'factorum', 'diferencia'].includes(col) ? -1 : 1 };
  pintarConciliacion();
}

function textoFaltaCargar() {
  const d = CONC.datos;
  const its = itemsConc('falta_cargar');
  const nom = ((CONC.clientes || []).find((c) => c.cuit === d.cliente) || {}).nombre || '';
  const lin = its.map((it) => `- ${it.nombre_tipo} ${nroCbte(it)} de ${it.emisor || 'emisor sin nombre'} (CUIT ${cuit(it.cuit_emisor)}), del ${fecha(it.fecha)}, por ${it.moneda_extranjera ? it.moneda : '$'} ${pesos(it.total)}`);
  return `Comprobantes de compra que figuran en ARCA a nombre de ${cuit(d.cliente)}${nom ? ' (' + nom + ')' : ''} entre el ${fecha(d.desde)} y el ${fecha(d.hasta)} y que todavía no recibimos:\n\n${lin.join('\n')}\n\nTotal: ${its.length} comprobante${its.length === 1 ? '' : 's'}.`;
}

function exportarConc(formato, grupo) {
  const d = CONC.datos;
  if (!d) return;
  const q = new URLSearchParams({ cliente: d.cliente, desde: d.desde, hasta: d.hasta });
  if (grupo) q.set('grupo', grupo);
  descargar(`/api/conciliacion.${formato}?${q}`, `conciliacion-${d.cliente}-${d.desde}-a-${d.hasta}${grupo ? '-' + grupo.replace('_', '-') : ''}.${formato}`);
}

// Revisar desde la conciliación: Anterior y Siguiente recorren lo que tiene Factorum en la pestaña.
function abrirDesdeConc(id) {
  const ids = itemsConc(CONC.tab).filter((it) => it.factorum).map((it) => it.factorum.id);
  if (!ids.includes(id)) ids.unshift(id);
  listado = ids.map((x) => ({ id: x }));
  abrirRevision(ids.indexOf(id));
}

async function constatarConc(id, btn) {
  if (btn) { btn.disabled = true; btn.textContent = 'Constatando...'; }
  try {
    const { constatacion: k } = await api(`/api/comprobantes/${id}/constatar`, { method: 'POST' });
    toast(k && k.apoc && k.apoc.esApocrifo ? 'El emisor figura como apócrifo' : k && k.valido === true ? 'ARCA lo reconoce: existe' : k && k.valido === false ? 'ARCA no lo reconoce' : 'Constatado');
    await cargarConciliacion(false);
  } catch (e) { toast(e, true); if (btn) { btn.disabled = false; btn.textContent = 'Constatar'; } }
}

// ---------- Libro IVA Digital ----------
// Las compras de un cliente en un mes, en el formato de importación de ARCA (RG 4597). El servidor
// arma los archivos y dice qué problemas hay; acá se muestran agrupados y se baja el ZIP.
const LIB = { datos: null, cliente: '', periodo: '', solo: false, cargando: false };
const NIVEL_LIB = {
  bloquea: ['error', 'No va al archivo', 'Hay que corregirlos en Revisar: con estos problemas no se pueden exportar.'],
  fuerte: ['error', 'Advertencia fuerte', 'Van solo si bajás todos. Su crédito fiscal no debería computarse.'],
  advertencia: ['revisar', 'Advertencias', 'Van al archivo. Conviene mirarlos antes de presentar.'],
  info: ['', 'Para tener en cuenta', 'Cómo quedaron en el archivo.'],
};
const mesDe = (d) => isoLocal(d).slice(0, 7);
function mesAtajo(a) {
  const hoy = new Date();
  return a === 'este_mes' ? mesDe(hoy) : mesDe(new Date(hoy.getFullYear(), hoy.getMonth() - 1, 1));
}
function libAtajo(a) { $('#libPeriodo').value = mesAtajo(a); marcarLibAtajo(); }
function marcarLibAtajo() {
  $$('#fLibro [data-atajo]').forEach((b) => b.classList.toggle('on', $('#libPeriodo').value === mesAtajo(b.dataset.atajo)));
}
const mesTxt = (p) => {
  const [y, m] = String(p || '').split('-').map(Number);
  if (!y || !m) return p || '';
  const t = new Date(y, m - 1, 1).toLocaleDateString('es-AR', { month: 'long', year: 'numeric' });
  return t.charAt(0).toUpperCase() + t.slice(1);
};

async function prepararLibro() {
  if (LIB.periodo) $('#libPeriodo').value = LIB.periodo;
  if (!$('#libPeriodo').value) libAtajo('mes_pasado');
  marcarLibAtajo();
  if (!CONC.clientes) {
    try {
      const r = await api('/api/conciliacion/clientes');
      CONC.clientes = r.clientes || [];
      CONC.arcanum = r.arcanum;
    } catch (e) { toast(e, true); return; }
  }
  const sel = $('#libCliente');
  const restringido = !!(YO && YO.identidad && YO.identidad.cuits);
  const cs = CONC.clientes;
  const previo = LIB.cliente || sel.value;
  sel.innerHTML = (cs.length === 1 ? '' : '<option value="">Elegí un cliente</option>')
    + cs.map((c) => `<option value="${esc(c.cuit)}">${esc(cuit(c.cuit))}${c.nombre ? ' · ' + esc(c.nombre) : ''}${c.comprobantes ? ` (${mil(c.comprobantes)})` : ' (sin comprobantes)'}</option>`).join('')
    + (restringido ? '' : '<option value="otro">Otro CUIT...</option>');
  if (cs.length === 1) sel.value = cs[0].cuit;
  else if (previo && [...sel.options].some((o) => o.value === previo)) sel.value = previo;
  else if (previo && !restringido) { sel.value = 'otro'; $('#libOtro').value = cuit(previo); }
  $('#libOtroWrap').classList.toggle('hidden', sel.value !== 'otro');
  // Desde Conciliación viene con el cliente y el mes: se arma solo.
  if (LIB.cliente && LIB.periodo && (!LIB.datos || LIB.datos.cliente !== LIB.cliente || LIB.datos.periodo !== LIB.periodo)) { await cargarLibro(); return; }
  if (!LIB.datos) pintarLibroVacio(); else pintarLibro();
}

function pintarLibroVacio() {
  if (CONC.clientes && !CONC.clientes.length) {
    $('#libro').innerHTML = vacio('libro_iva', 'Todavía no hay clientes con compras', 'Cargá las facturas de compra con el CUIT del cliente y después armá el libro del mes.', 'subir', 'Cargar facturas');
    return;
  }
  $('#libro').innerHTML = `<div class="vacio"><div class="ico">${I.libro_iva}</div><h3>Elegí el cliente y el mes</h3>
    <p>Factorum arma los dos archivos que pide ARCA con las compras del mes (por fecha de emisión) y te muestra antes qué hay que corregir.</p>
    <div class="pasos">
      <div class="paso"><span class="n">Comprobantes</span><span class="muted">Un registro de 325 posiciones por comprobante: LIBRO_IVA_DIGITAL_COMPRAS_CBTE.</span></div>
      <div class="paso"><span class="n">Alícuotas</span><span class="muted">Uno de 84 por cada alícuota de IVA de las A y M. B, C y tiques no llevan.</span></div>
      <div class="paso"><span class="n">Problemas</span><span class="muted">Lo que no puede ir, lo que no deberías computar y lo que conviene mirar.</span></div>
    </div></div>`;
}

function clienteLib() {
  const v = $('#libCliente').value;
  return v === 'otro' ? soloDig($('#libOtro').value) : v;
}

async function armarLibro() {
  const cli = clienteLib();
  const per = $('#libPeriodo').value.trim();
  if (!cli) { toast('Elegí el cliente', true); return; }
  if (cli.length !== 11) { toast('El CUIT del cliente tiene que tener 11 dígitos', true); return; }
  if (!/^\d{4}-\d{2}$/.test(per)) { toast('Elegí el mes (AAAA-MM)', true); return; }
  Object.assign(LIB, { cliente: cli, periodo: per });
  await cargarLibro();
}

async function cargarLibro() {
  if (!LIB.cliente || LIB.cargando) return;
  LIB.cargando = true;
  const btn = $('#libBtn');
  btn.disabled = true; btn.textContent = 'Armando...';
  if (!LIB.datos) $('#libro').innerHTML = '<p class="muted">Armando el libro y buscando problemas...</p>';
  try {
    const q = new URLSearchParams({ cliente: LIB.cliente, periodo: LIB.periodo });
    if (LIB.solo) q.set('solo_aprobados', '1');
    LIB.datos = await api('/api/libro-iva?' + q);
    pintarLibro();
  } catch (e) { toast(e, true); if (!LIB.datos) pintarLibroVacio(); } finally {
    LIB.cargando = false;
    btn.disabled = false; btn.textContent = 'Armar el libro';
  }
}

const nroItem = (it) => `${String(it.punto_venta || 0).padStart(5, '0')}-${String(it.numero || 0).padStart(8, '0')}`;
const montoMon = (v, mon) => (mon && mon !== 'PES' ? `${esc(mon)} ${pesos(v)}` : `$ ${pesos(v)}`);

function grupoLibHTML(g) {
  const [cls] = NIVEL_LIB[g.nivel] || [''];
  const n = g.items.length;
  const afuera = g.items.filter((it) => it.excluido).length;
  const filas = g.items.map((it) => `<tr class="${it.excluido ? 'lib-afuera' : ''}"><td>${esc(it.nombre_tipo)}<div class="mono nowrap">${nroItem(it)}</div></td>
      <td class="nowrap">${it.fecha ? fecha(it.fecha) : '<span class="muted">sin fecha</span>'}</td>
      <td>${esc(it.emisor || '')}<div class="muted mono">${esc(cuit(it.cuit_emisor))}</div></td>
      <td class="num nowrap">${montoMon(it.total, it.moneda)}</td>
      <td>${it.detalle ? esc(it.detalle) : '<span class="muted">-</span>'}</td>
      <td>${it.excluido ? '<span class="est error">No va</span>' : '<span class="est ok">Va</span>'}</td>
      <td><button type="button" class="ghost mini" data-accion="libAbrir" data-id="${it.id}" data-grupo="${esc(g.codigo)}">Abrir</button></td></tr>`).join('');
  return `<details class="lib-grupo ${cls}"${g.nivel === 'info' ? '' : ' open'}><summary><span class="lib-tit">${esc(g.titulo)}</span>
      <span class="cnt">${mil(n)}</span>${afuera && afuera < n ? `<span class="muted mini-txt">${mil(afuera)} no va${afuera > 1 ? 'n' : ''}</span>` : ''}</summary>
    <p class="muted lib-ayuda">${esc(g.ayuda)}</p>
    <div class="tabla-scroll"><table class="t lib-t"><thead><tr><th>Comprobante</th><th>Fecha</th><th>Emisor</th><th class="num">Total</th><th>Detalle</th><th>Archivo</th><th></th></tr></thead>
    <tbody>${filas}</tbody></table></div></details>`;
}

function problemasLibHTML(d) {
  const gs = d.problemas || [];
  const serios = gs.filter((g) => g.nivel !== 'info');
  let h = '';
  if (!serios.length) {
    h += `<div class="aviso-card lib-bien">${I.check}<div><strong>Sin problemas.</strong><br><span class="muted">${d.cantidad_todos ? 'Todo lo del mes se puede exportar tal cual.' : 'No hay comprobantes en el mes.'}</span></div></div>`;
  }
  for (const nivel of ['bloquea', 'fuerte', 'advertencia', 'info']) {
    const del = gs.filter((g) => g.nivel === nivel);
    if (!del.length) continue;
    const [cls, tit, ayuda] = NIVEL_LIB[nivel];
    const cuantos = new Set(del.flatMap((g) => g.items.map((it) => it.id))).size;
    h += `<section class="lib-nivel ${cls}"><h3>${esc(tit)} <span class="cnt">${mil(cuantos)} comprobante${cuantos === 1 ? '' : 's'}</span></h3><p class="muted lib-ayuda">${esc(ayuda)}</p>
      ${del.map(grupoLibHTML).join('')}</section>`;
  }
  return h;
}

function pintarLibro() {
  const d = LIB.datos;
  if (!d) { pintarLibroVacio(); return; }
  const r = d.resumen;
  const nombre = d.nombre || ((CONC.clientes || []).find((c) => c.cuit === d.cliente) || {}).nombre || '';
  const ext = r.moneda_extranjera || [];
  const extTxt = ext.map((m) => `${esc(m.moneda)} ${pesos(m.total)} (${m.cantidad})`).join(' · ');
  const conc = d.conciliacion || {};
  const concHTML = conc.estado === 'lista'
    ? `<div class="aviso-mini lib-ok">${I.check}<span>Conciliado con Mis Comprobantes: lo que no figura en ARCA aparece en las advertencias.${conc.mensaje ? ' ' + esc(conc.mensaje) : ''}</span></div>`
    : conc.estado && conc.estado !== 'no_pedida' ? `<div class="aviso-mini">${I.info}<span>Sin conciliación con ARCA: ${esc(conc.mensaje || 'no se pudo conciliar.')} Los archivos se arman igual.</span></div>` : '';
  const aliFilas = (r.por_alicuota || []).map((a) => `<tr><td>${esc(a.tasa)} %</td><td class="mono">${esc(a.codigo)}</td><td class="num">${mil(a.registros)}</td><td class="num">$ ${pesos(a.neto_gravado)}</td><td class="num">$ ${pesos(a.iva)}</td></tr>`).join('');
  const otros = [['No gravado', r.no_gravado], ['Exento', r.exento], ['Otros tributos', r.otros_tributos]].filter(([, v]) => Number(v))
    .map(([t, v]) => `<tr class="lib-extra"><td>${t}</td><td></td><td></td><td class="num">$ ${pesos(v)}</td><td></td></tr>`).join('');
  const n = r.comprobantes;
  const bajar = n
    ? `<button type="button" data-accion="libBajar">${I.bajar}<span>Bajar el ZIP (${mil(n)} comprobante${n === 1 ? '' : 's'})</span></button>`
    : '<button type="button" disabled>No hay nada para bajar</button>';
  const hayExt = ext.length > 0;
  $('#libro').innerHTML = `${concHTML}
    <div class="conc-cab"><div><strong>${esc(cuit(d.cliente))}${nombre ? ' · ' + esc(nombre) : ''}</strong> <span class="muted">${esc(mesTxt(d.periodo))} · ${mil(d.leidos)} comprobante${d.leidos === 1 ? '' : 's'} en Factorum · ${mil(d.excluidos)} no va${d.excluidos === 1 ? '' : 'n'}</span></div>
      <div class="acciones"><button type="button" class="ghost mini" data-accion="libRefrescar">Actualizar</button></div></div>
    <div class="kpis lib-kpis">
      <div class="kpi"><span class="et">Comprobantes</span><span class="val">${mil(n)}</span><span class="kpi-pie">${mil(r.registros_alicuotas)} registro${r.registros_alicuotas === 1 ? '' : 's'} de alícuotas${r.notas_credito ? ` · ${r.notas_credito} nota${r.notas_credito > 1 ? 's' : ''} de crédito` : ''}</span></div>
      <div class="kpi"><span class="et">IVA</span><span class="val">${pesos(r.iva)}</span><span class="kpi-pie">neto gravado $ ${pesos(r.neto_gravado)}</span></div>
      <div class="kpi lib-cf"><span class="et">Crédito fiscal computable</span><span class="val">${pesos(r.credito_fiscal)}</span><span class="kpi-pie">${r.sin_credito_fiscal ? `${r.sin_credito_fiscal} sin crédito fiscal (B, C y tiques)` : 'sin prorrateo: igual al IVA'}</span></div>
      <div class="kpi"><span class="et">Total</span><span class="val">${pesos(r.total)}</span><span class="kpi-pie">las notas de crédito restan${extTxt ? '<br>aparte: ' + extTxt : ''}</span></div>
    </div>
    <div class="dos parejo lib-dos">
      <div class="card"><div class="card-cab"><h3>Por alícuota</h3><span class="muted mini-txt">en pesos</span></div>
        ${aliFilas || otros ? `<div class="tabla-scroll"><table class="t lib-ali"><thead><tr><th>Alícuota</th><th>Código</th><th class="num">Registros</th><th class="num">Neto gravado</th><th class="num">IVA</th></tr></thead>
        <tbody>${aliFilas}${otros}</tbody></table></div>` : '<p class="muted">Sin IVA discriminado en el mes.</p>'}
      </div>
      <div class="card lib-bajar"><div class="card-cab"><h3>Bajar los archivos</h3></div>
        <div class="tabs" role="tablist" aria-label="Qué va al archivo">
          <button type="button" role="tab" aria-selected="${!d.solo_aprobados}" class="${d.solo_aprobados ? '' : 'on'}" data-accion="libSolo" data-solo="0">Todos <span class="cnt">${mil(d.cantidad_todos)}</span></button>
          <button type="button" role="tab" aria-selected="${!!d.solo_aprobados}" class="${d.solo_aprobados ? 'on' : ''}" data-accion="libSolo" data-solo="1">Solo los listos <span class="cnt">${mil(d.cantidad_listos)}</span></button>
        </div>
        <p class="muted mini-txt">${d.solo_aprobados ? 'Solo lo que está bien o aprobado y sin advertencias fuertes.' : 'Todo lo que se puede exportar, con sus advertencias (los apócrifos también: miralos antes).'}</p>
        ${bajar}
        <div class="aviso-mini${hayExt ? ' mal' : ''}">${I.info}<span>${hayExt ? '<strong>Hay comprobantes en moneda extranjera.</strong> ' : ''}${esc(d.importar || '')}</span></div>
        <p class="muted mini-txt">En el ZIP: ${esc('LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt')}, ${esc('LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS.txt')} (ISO 8859-1, una línea por registro) y resumen.csv. En ARCA: Libro IVA Compras, «Importar archivos».</p>
      </div>
    </div>
    <h3 class="lib-h">Problemas</h3>
    ${problemasLibHTML(d)}`;
  $$('#libro table.t').forEach(tarjetaDe);
}

function bajarLibro() {
  const d = LIB.datos;
  if (!d) return;
  const q = new URLSearchParams({ cliente: d.cliente, periodo: d.periodo });
  if (d.solo_aprobados) q.set('solo_aprobados', '1');
  descargar(`/api/libro-iva/archivos.zip?${q}`, `libro-iva-compras-${d.cliente}-${d.periodo}${d.solo_aprobados ? '-listos' : ''}.zip`);
}

// Revisar desde el libro: Anterior y Siguiente recorren el grupo de problemas.
function abrirDesdeLibro(id, codigo) {
  const g = ((LIB.datos || {}).problemas || []).find((x) => x.codigo === codigo);
  const ids = g ? [...new Set(g.items.map((it) => it.id))] : [id];
  if (!ids.includes(id)) ids.unshift(id);
  listado = ids.map((x) => ({ id: x }));
  abrirRevision(ids.indexOf(id));
}

function libroDesdeConc() {
  const d = CONC.datos;
  if (!d) return;
  Object.assign(LIB, { cliente: d.cliente, periodo: d.desde.slice(0, 7) });
  ir('libro_iva');
}

// ---------- Estado vacío ----------
function vacio(ico, titulo, texto, vistaBoton, textoBoton) {
  if (vistaBoton && !vistaPermitida(vistaBoton)) vistaBoton = '';
  return `<div class="vacio"><div class="ico">${I[ico] || I.doc}</div><h3>${esc(titulo)}</h3><p>${esc(texto)}</p>${vistaBoton ? `<button type="button" data-accion="ir" data-vista="${vistaBoton}">${esc(textoBoton)}</button>` : ''}</div>`;
}

// ---------- Inicio ----------
const mil = (n) => Number(n || 0).toLocaleString('es-AR');
async function cargarInicio() {
  const acciones = $('#v-inicio .cabecera .acciones');
  acciones.classList.toggle('hidden', sinClientes());
  if (sinClientes()) {
    $('#inicioSub').textContent = 'Todavía no ves ningún cliente.';
    $('#inicio').innerHTML = `<div class="aviso-card">${I.info}<div><strong>Pedile a un admin que te asigne clientes.</strong><br>
      <span class="muted">Entraste bien${YO.identidad.origen === 'lockatus' ? ' con Lockatus' : ''}, pero tu cuenta todavía no tiene clientes asignados. Cuando un admin te los asigne en Usuarios, volvé a entrar y vas a ver sus facturas.</span></div></div>`;
    return;
  }
  try {
    const { panel: p, copia_fallida: cf } = await api('/api/panel');
    const avisoCopia = cf ? `<div class="aviso-card mal">${I.alerta}<div><strong>Falló la última copia de la base</strong> (${fechaHora(cf.cuando)}).<br><span class="muted">${esc(cf.error)}</span></div><button type="button" class="ghost mini" data-accion="verCopias">Ver las copias</button></div>` : '';
    if (!p.total) {
      $('#inicioSub').textContent = 'Todavía no cargaste facturas. Así se empieza:';
      $('#inicio').innerHTML = `${avisoCopia}<div class="vacio"><div class="ico">${I.doc}</div><h3>Bienvenido a Factorum</h3>
        <p>Lee facturas de ARCA por QR y texto, controla que todo cierre y, con Arcanum, te dice si son legales.</p>
        <div class="pasos">
          <div class="paso"><span class="n">1</span><strong>Verificá una factura</strong><span class="muted">Subí un PDF o una foto y mirá qué lee y qué controla. No se guarda.</span><button type="button" class="ghost" data-accion="ir" data-vista="verificar">Verificar</button></div>
          ${puede('cargar') ? '<div class="paso"><span class="n">2</span><strong>Cargá un lote</strong><span class="muted">PDFs sueltos o un ZIP con cientos: se procesan en segundo plano.</span><button type="button" class="ghost" data-accion="ir" data-vista="subir">Cargar facturas</button></div>' : ''}
          ${puede('configuracion') ? '<div class="paso"><span class="n">3</span><strong>Conectá el chat</strong><span class="muted">WhatsApp o Telegram: tus clientes mandan la foto y queda cargada.</span><button type="button" class="ghost" data-accion="ir" data-vista="configuracion">Configuración</button></div>' : ''}
        </div></div>`;
      return;
    }
    $('#inicioSub').textContent = 'Cómo viene todo, de un vistazo.';
    const e = p.por_estado || {};
    const pend = (e.revisar || 0) + (e.error || 0);
    const sinIA = p.total ? Math.round((1 - p.con_ia / p.total) * 100) : 100;
    const kpi = (et, val, pie, cls, estado, clase) => clase
      ? `<button type="button" class="kpi ${cls || ''}" data-accion="verClase" data-clase="${clase}"><span class="et">${et}</span><span class="val">${val}</span><span class="kpi-pie">${pie}</span></button>`
      : estado
      ? `<button type="button" class="kpi ${cls || ''}" data-accion="verEstado" data-estado="${estado}"><span class="et">${et}</span><span class="val">${val}</span><span class="kpi-pie">${pie}</span></button>`
      : `<div class="kpi ${cls || ''}"><span class="et">${et}</span><span class="val">${val}</span><span class="kpi-pie">${pie}</span></div>`;
    const max = Math.max(1, ...p.por_dia.map((d) => d.ok + d.revisar + d.error));
    const alto = (n) => (n ? Math.max(2, Math.round((n / max) * 100)) : 0);
    const barras = p.por_dia.map((d) => `<div class="b" title="${fecha(d.dia)}: ${d.ok} bien, ${d.revisar} para revisar, ${d.error} con errores">
      <i class="ok" style="height:${alto(d.ok)}%"></i><i class="rv" style="height:${alto(d.revisar)}%"></i><i class="er" style="height:${alto(d.error)}%"></i></div>`).join('');
    const ejes = p.por_dia.map((d, i) => `<span>${i % 2 ? '' : `${d.dia.slice(8, 10)}<small>/${d.dia.slice(5, 7)}</small>`}</span>`).join('');
    const lotes = p.lotes.length ? p.lotes.map((l) => {
      const c = l.comprobantes || {};
      const pct = l.documentos ? Math.round((l.procesados / l.documentos) * 100) : 100;
      return `<button type="button" class="it" data-accion="verLote" data-id="${l.id}"><span><span class="t1">${esc(l.nombre || 'Lote ' + l.id)}</span><br><span class="t2">${esc(l.origen)} · ${new Date(l.creado).toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' })}</span></span>
        <span class="t2">${l.estado === 'terminado' ? `${(c.ok || 0) + (c.aprobado || 0)} bien · ${c.revisar || 0} revisar · ${c.error || 0} error` : `<span class="prog mini-prog"><div style="width:${pct}%"></div></span> ${l.procesados}/${l.documentos}`}</span></button>`;
    }).join('') : '<p class="muted">Sin lotes todavía.</p>';
    $('#inicio').innerHTML = `${avisoCopia}<div class="kpis">
        ${kpi('Para revisar', mil(pend), pend ? 'tocá para verlos' : 'todo al día', e.error ? 'alerta' : pend ? 'aviso' : '', e.error ? 'error' : 'revisar')}
        ${kpi('Este mes', mil(p.mes.comprobantes), `${mil(p.hoy.comprobantes)} hoy · ${mil(p.mes.documentos)} archivos en el mes`)}
        ${kpi('Emisores apócrifos', mil(p.apocrifos), 'detectados con ARCA', p.apocrifos ? 'alerta' : '')}
        ${kpi('Posibles adulterados', mil(p.adulterados), 'texto distinto del QR', p.adulterados ? 'alerta' : '')}
        ${kpi('Tickets este mes', mil(p.tickets_mes || 0), 'de supermercado, combustible y más', '', '', 'tickets')}
        ${kpi('Leído sin IA', sinIA + '%', `${Math.round(p.duracion_promedio_ms)} ms por archivo, en promedio`)}
      </div>
      <div class="dos">
        <div class="card"><div class="card-cab"><h3>Últimos 14 días</h3><span class="leyenda"><span><i style="background:var(--accent)"></i>Bien</span><span><i style="background:var(--warn)"></i>Revisar</span><span><i style="background:var(--err)"></i>Errores</span></span></div>
          <div><div class="barras">${barras}</div><div class="ejes">${ejes}</div></div></div>
        <div class="card"><div class="card-cab"><h3>Últimos lotes</h3><button type="button" class="ghost mini" data-accion="ir" data-vista="lotes">Ver todos</button></div><div class="lista">${lotes}</div></div>
      </div>`;
  } catch (e) { toast(e, true); }
}

// ---------- Bandeja de revisión ----------
let bandejaEstado = 'error';
async function cargarBandeja() {
  $$('#bandejaTabs button').forEach((b) => b.classList.toggle('on', b.dataset.estado === bandejaEstado));
  try {
    const { comprobantes, total } = await api(`/api/comprobantes?estado=${bandejaEstado}&limite=200`);
    actualizarPendientes();
    listado = comprobantes;
    if (!comprobantes.length) {
      $('#bandeja').innerHTML = vacio('check', bandejaEstado === 'error' ? 'No hay comprobantes con errores' : 'No hay nada para revisar',
        'Todo lo cargado cerró solo o ya lo aprobaste.');
      return;
    }
    $('#bandeja').innerHTML = `<div class="bandeja">${comprobantes.map((c, i) => {
      const motivo = (c.hallazgos || []).find((h) => h.nivel === 'error') || (c.hallazgos || []).find((h) => h.nivel === 'aviso');
      return `<button type="button" class="bitem ${esc(c.estado)}" data-accion="abrirBandeja" data-idx="${i}"><span class="marca-e"></span>
        <span><span class="t1">${esc(c.razon_social_emisor || cuit(c.cuit_emisor))}</span> <span class="muted">· ${esc(nombreCbte(c))} · ${fecha(c.fecha)}</span> ${ticketChip(c)}<br>
        <span class="t2">${esc(motivo ? motivo.mensaje : '')}</span></span>
        <span class="monto">$ ${pesos(c.total)}</span></button>`;
    }).join('')}</div>${total > comprobantes.length ? `<p class="muted">Se muestran ${comprobantes.length} de ${total}.</p>` : ''}`;
  } catch (e) { toast(e, true); }
}

// ---------- Aviso de la IA externa ----------
// Qué pasa con los datos cuando se usa una IA que no corre en el servidor. Lo arma el servidor
// (GET /api/ia/aviso) con los datos verificados de cada proveedor; acá solo se pinta.
let AVISO_IA = null;
let AIA = null; // el aviso abierto: { resolver, previo }
const ICONO_TARJETA = { donde: 'globo', tiempo: 'reloj', entrenan: 'chip', factorum: 'verificar' };
// Negrita mínima (**así**) sobre texto ya escapado.
const negrita = (t) => esc(t).replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
const iconoGrande = (k) => I[k].replace('width="18" height="18"', 'width="30" height="30"');
// Un host largo (generativelanguage.googleapis.com) se corta en los puntos, no en cualquier letra.
const hostCortable = (h) => esc(h).replace(/\./g, '.<wbr>');

async function cargarAvisoIA() {
  if (!YO) return;
  try { actualizarAvisoIA(await api('/api/ia/aviso')); } catch { /* sin aviso: los sellos quedan vacíos */ }
}
function actualizarAvisoIA(a) {
  AVISO_IA = a || null;
  $$('[data-ia-sello]').forEach((el) => { el.innerHTML = selloIA(AVISO_IA); });
}

// El sello que va al lado de «Usar IA si hace falta» (y en Configuración y Conectar).
function selloIA(a, conLink = true) {
  if (!a || !a.configurada) return '';
  if (a.local) return `<span class="sello-ia local" title="${esc(a.host)}">${I.servidor}<span>${esc(a.aviso.sello)}</span></span>`;
  const link = conLink ? '<button type="button" class="link-btn" data-accion="avisoIA">Qué pasa con los datos</button>' : '';
  return `<span class="sello-ia externa" title="${esc(a.host)}">${I.nube}<span>${esc(a.aviso.sello)}</span></span>${link}`;
}

function htmlAvisoIA(a, modo) {
  const av = a.aviso || {};
  const region = a.region_nombre ? `<span class="aia-region">${I.globo}Región: ${esc(a.region_nombre)}</span>` : '';
  const alerta = av.alerta ? `<div class="aia-alerta ${esc(av.alerta.nivel)}" role="note">${I.alerta}<div><strong>${esc(av.alerta.titulo)}</strong><p>${negrita(av.alerta.texto)}</p></div></div>` : '';
  const tarjetas = (av.tarjetas || []).map((t) => `<section class="aia-t" aria-labelledby="aia-t-${esc(t.id)}">
      <div class="aia-t-cab"><span class="aia-t-ico">${I[ICONO_TARJETA[t.id] || 'info']}</span><h4 id="aia-t-${esc(t.id)}">${esc(t.titulo)}</h4></div>
      <p class="aia-dest ${esc(t.tono || '')}">${esc(t.destacado)}</p>
      <ul>${t.puntos.map((p) => `<li>${negrita(p)}</li>`).join('')}</ul>
      ${(t.notas || []).length ? `<details class="aia-mas"><summary>Más detalle</summary>${t.notas.map((n) => `<p>${negrita(n)}</p>`).join('')}</details>` : ''}
    </section>`).join('');
  const fuentes = (av.fuentes || []).map((f) => `<li><a href="${esc(f.url)}" target="_blank" rel="noopener noreferrer">${esc(f.titulo)}${I.externo}</a>${f.nota ? `<span class="aia-nota">${esc(f.nota)}</span>` : ''}</li>`).join('');
  const quien = (YO && YO.identidad && (YO.identidad.email || YO.identidad.nombre)) || '';
  const registro = {
    admin: `Al aceptar queda registrado que ${quien ? 'lo aceptaste vos (' + esc(quien) + ')' : 'lo aceptaste'}, hoy, para <code>${hostCortable(a.host)}</code>. Si cambiás de proveedor o de host, se pide de nuevo.`,
    persona: 'Se recuerda para tu cuenta: no te lo vuelve a preguntar mientras no cambie el proveedor o este aviso.',
    prueba: `Para probar se manda solo el ticket de prueba (inventado), no comprobantes de tus clientes. Si después guardás esta IA, queda aceptado a tu nombre para <code>${hostCortable(a.host)}</code>.`,
  }[modo] || '';
  const botones = {
    admin: '<button type="button" class="ghost" data-accion="aiaCancelar">Cancelar</button><button type="button" data-accion="aiaOK">Entiendo y quiero usarla</button>',
    persona: '<button type="button" class="ghost" data-accion="aiaCancelar">Seguir sin IA</button><button type="button" data-accion="aiaOK">Entendido, usar IA</button>',
    prueba: '<button type="button" class="ghost" data-accion="aiaCancelar">Cancelar</button><button type="button" data-accion="aiaOK">Entiendo, probar</button>',
  }[modo] || '<button type="button" data-accion="aiaCancelar">Cerrar</button>';
  return `<header class="aia-cab">
      <div class="aia-ico" aria-hidden="true">${iconoGrande('nube')}</div>
      <div class="aia-cab-txt"><span class="aia-kicker">IA externa · Qué pasa con los datos</span>
        <h3 id="aiaTit">${a.proveedor === 'desconocido' ? 'Un proveedor que Factorum no conoce' : esc(av.titulo || a.nombre)}</h3>
        <div class="aia-host"><code>${hostCortable(a.host)}</code>${region}</div></div>
      <button type="button" class="modal-x" data-accion="aiaCancelar" aria-label="${modo === 'persona' ? 'Seguir sin IA' : modo === 'admin' || modo === 'prueba' ? 'Cancelar' : 'Cerrar'}">&times;</button>
    </header>
    <div class="aia-cuerpo">
      <p class="aia-intro" id="aiaIntro">${negrita(av.intro || '')}</p>
      ${alerta}
      <div class="aia-grid">${tarjetas}</div>
      <footer class="aia-fuentes"><p>${esc(av.pie || '')}</p>${fuentes ? `<ul>${fuentes}</ul>` : ''}</footer>
      ${registro ? `<p class="aia-registro cel">${registro}</p>` : ''}
    </div>
    <div class="aia-pie">${registro ? `<p class="aia-registro esc">${registro}</p>` : ''}<div class="aia-botones">${botones}</div></div>`;
}

// abrirAvisoIA muestra el aviso y resuelve true si se aceptó. modo: admin (aceptar para la
// instalación), prueba (mandarle el ticket de prueba), persona (la primera vez que la usa) o lectura. Esc, la X y el fondo cancelan.
function abrirAvisoIA(a, modo) {
  if (AIA) cerrarAvisoIA(false);
  return new Promise((resolver) => {
    const card = $('#aiaCard');
    card.innerHTML = htmlAvisoIA(a, modo);
    AIA = { resolver, previo: document.activeElement };
    $('#avisoIA').classList.remove('hidden');
    document.documentElement.classList.add('sin-scroll');
    card.querySelector('.aia-cuerpo').scrollTop = 0;
    card.focus();
  });
}
function cerrarAvisoIA(ok) {
  if (!AIA) return;
  const { resolver, previo } = AIA;
  AIA = null;
  $('#avisoIA').classList.add('hidden');
  document.documentElement.classList.remove('sin-scroll');
  if (previo && previo.focus && document.contains(previo)) previo.focus();
  resolver(ok);
}
// El foco no sale del aviso mientras está abierto.
function atraparFoco(ev, caja) {
  const f = $$('button, a[href], summary, [tabindex]:not([tabindex="-1"])', caja).filter((x) => !x.disabled && x.offsetParent !== null);
  if (!f.length) { ev.preventDefault(); return; }
  const primero = f[0], ultimo = f[f.length - 1];
  if (ev.shiftKey && (document.activeElement === primero || document.activeElement === caja)) { ev.preventDefault(); ultimo.focus(); }
  else if (!ev.shiftKey && document.activeElement === ultimo) { ev.preventDefault(); primero.focus(); }
  else if (!caja.contains(document.activeElement)) { ev.preventDefault(); primero.focus(); }
}

// confirmarIAPersona: la primera vez que esta persona va a usar la IA externa, el aviso. Devuelve
// true si puede usarla (local, ya lo vio o lo acaba de aceptar).
async function confirmarIAPersona() {
  const a = AVISO_IA;
  if (!a || !a.en_uso || !a.externa || !a.persona || a.visto) return true;
  if (!(await abrirAvisoIA(a, 'persona'))) return false;
  try {
    actualizarAvisoIA(await api('/api/ia/aviso/visto', { method: 'POST', json: { host: a.host, version: a.version } }));
    return true;
  } catch (e) {
    if (e.datos && e.datos.aviso_ia) actualizarAvisoIA(e.datos.aviso_ia);
    toast(e, true);
    return false;
  }
}

// Desde Configuración: aceptar el aviso de la IA que fija el entorno (no hay nada más que guardar).
async function aceptarAvisoIA(btn) {
  const a = CONF && CONF.ia_aviso;
  if (!a || !(await abrirAvisoIA(a, 'admin'))) return;
  if (btn) btn.disabled = true;
  try {
    CONF = await api('/api/configuracion', { method: 'PUT', json: { cambios: {}, aceptar_aviso_ia: { host: a.host, version: a.version } } });
    pintarConfiguracion();
    cargarSalud();
    toast(`Aceptado: Factorum ya usa ${a.nombre} cuando hace falta.`);
  } catch (e) { toast(e, true); } finally { if (btn) btn.disabled = false; }
}

// En Configuración, IA (visión): el sello, quién aceptó el aviso o el pedido de aceptarlo.
function estadoAvisoIA() {
  const a = CONF && CONF.ia_aviso;
  if (!a || !a.configurada) return '';
  const ahora = '<span class="ia-ahora-et hidden">Ahora se usa:</span>';
  if (a.local) return `<div class="ia-conf">${ahora}${selloIA(a)}</div>`;
  if (!a.aceptado) {
    return `<div class="aviso-mini mal ia-falta">${I.alerta}<span><strong>Factorum no está usando esta IA.</strong> Es externa (${esc(a.nombre)}, <code>${esc(a.host)}</code>) y ningún superadmin aceptó todavía el aviso de qué pasa con los datos. Hasta entonces no se usa: ni en la pantalla, ni en los chats, ni por MCP.</span>${a.puede_aceptar ? '<button type="button" class="mini" data-accion="aceptarAvisoIA">Ver el aviso y aceptarla</button>' : ''}</div>`;
  }
  const ac = a.aceptacion || {};
  return `<div class="ia-conf">${ahora}${selloIA(a)}<span class="muted">Aviso aceptado${ac.por ? ' por ' + esc(ac.por) : ''}${ac.cuando ? ' (' + fechaHora(ac.cuando) + ')' : ''}. Si cambiás de proveedor o de host, se pide de nuevo.</span></div>`;
}

// ---------- Configuración, IA (visión): elegir el proveedor y el modelo de una lista ----------
// El proveedor completa la URL; la clave, si hace falta; y el modelo sale de la lista que el
// servidor le pide al proveedor (POST /api/ia/modelos) con lo que está en pantalla, sin guardar.
const IA_OTRO = '__otro';
let IAUI = null; // { pedido, cache, lista, timer, aceptadoPrueba }

const iaCat = () => (CONF && CONF.ia) || { proveedores: [], recomendados: [] };
const iaProvDe = (id) => iaCat().proveedores.find((p) => p.id === id);
const iaCampo = (k) => (CONF.campos || []).find((c) => c.clave === k) || {};
const iaEl = (k) => document.getElementById(idCampo(k));
// Lo guardado (o del entorno): el punto de partida de cada proveedor.
const iaGuardado = () => ({ prov: iaCat().proveedor || '', url: iaCampo('ia.url').valor || '', modelo: iaCampo('ia.modelo').valor || '' });
const iaRecosDe = (prov) => {
  const r = (IAUI && IAUI.lista && IAUI.lista.proveedor === prov && IAUI.lista.recomendados) || null;
  if (r) return r;
  return iaCat().recomendados.flatMap((x) => (x.proveedor === prov ? [x] : prov === 'openrouter' && x.proveedor === 'openai'
    ? [{ ...x, modelo: 'openai/' + x.modelo, texto: x.texto + ' (medido con OpenAI directo)' }] : []));
};
const fijadoHTML = (c) => (c.bloqueado ? `<span class="fijado">${I.llave}Fijado por el servidor (variable ${esc(c.env)})</span>` : '');

function cuerpoIA() {
  IAUI = { pedido: 0, cache: new Map(), lista: null, timer: 0, aceptadoPrueba: (IAUI && IAUI.aceptadoPrueba) || null };
  const g = iaGuardado();
  const cp = iaCampo('ia.proveedor'), cu = iaCampo('ia.url'), cm = iaCampo('ia.modelo'), ck = iaCampo('ia.api_key');
  const bloqProv = cp.bloqueado || cu.bloqueado;
  const ops = [['', 'Sin IA'], ...iaCat().proveedores.map((p) => [p.id, p.nombre])];
  const sel = `<select id="${idCampo('ia.proveedor')}" data-orig="${esc(g.prov)}"${bloqProv ? ' disabled' : ''}>${ops.map(([v, t]) => `<option value="${esc(v)}"${v === g.prov ? ' selected' : ''}>${esc(t)}</option>`).join('')}</select>`;
  const regiones = (iaProvDe('openai') || {}).regiones || [];
  const region = `<select id="iaRegion"${cu.bloqueado ? ' disabled' : ''}>${regiones.map((r) => `<option value="${esc(r.codigo)}"${r.codigo === (iaCat().region || '') ? ' selected' : ''}>${esc(r.nombre)}</option>`).join('')}</select>`;
  const ayudaClave = ck.configurado ? `Configurada${ck.ultimos ? ', termina en ' + esc(ck.ultimos) : ''}. Escribí para reemplazarla.` : '';
  return `<div class="ia-elegir">
    <div class="conf-campos ia-campos">
      <label class="campo-conf"><span class="et">Proveedor</span>${sel}<span class="ayuda" id="iaProvAyuda"></span>${cp.bloqueado ? fijadoHTML(cp) : cu.bloqueado ? fijadoHTML(cu) : ''}</label>
      <div class="campo-conf" id="iaClaveWrap"><label class="et" for="${idCampo('ia.api_key')}">Clave de API</label>
        <div class="fila-secreto">${campoClave(idCampo('ia.api_key'), `autocomplete="off" spellcheck="false" placeholder="${ck.configurado ? 'Configurada' : 'Pegá la clave'}"${ck.bloqueado ? ' disabled' : ''}`)}</div>
        <span class="ayuda"><strong id="iaClaveGuardada" data-texto="${esc(ayudaClave)}">${ayudaClave}</strong> <span id="iaClaveDonde"></span></span>${fijadoHTML(ck)}</div>
      <label class="campo-conf" id="iaRegionWrap"><span class="et">Región de los datos</span>${region}
        <span class="ayuda">Con una región, OpenAI guarda los datos ahí (y en la mayoría, también los procesa). Hay que habilitar la residencia de datos en el proyecto de OpenAI; si no, el dominio regional rechaza la clave. Usa <code id="iaRegionUrl"></code>.</span></label>
      <label class="campo-conf" id="iaUrlWrap"><span class="et">URL</span>
        <input id="${idCampo('ia.url')}" data-orig="${esc(g.url)}" value="${esc(g.url)}" inputmode="url" spellcheck="false" autocomplete="off"${cu.bloqueado ? ' disabled' : ''}>
        <span class="ayuda" id="iaUrlAyuda"></span>${cu.bloqueado && !cp.bloqueado ? '' : ''}</label>
    </div>
    <div class="ia-modelo" id="iaModeloWrap">
      <div class="campo-conf"><label class="et" for="iaModeloSel">Modelo</label>
        <div class="fila-modelo"><select id="iaModeloSel"${cm.bloqueado ? ' disabled' : ''}></select>
          <button type="button" class="ghost mini" data-accion="iaRecargar" title="Volver a pedirle la lista al proveedor" aria-label="Actualizar la lista de modelos"${cm.bloqueado ? ' disabled' : ''}>${I.restaurar}<span>Actualizar</span></button></div>
        <input id="iaModeloMano" class="hidden" placeholder="El nombre exacto del modelo, como lo escribe el proveedor" spellcheck="false" autocomplete="off" aria-label="Otro modelo">
        <input type="hidden" id="${idCampo('ia.modelo')}" data-orig="${esc(g.modelo)}" value="${esc(g.modelo)}">
        <div id="iaModeloEstado" class="ia-estado" aria-live="polite"></div>${fijadoHTML(cm)}</div>
      <div id="iaRecos" class="ia-recos"></div>
    </div>
    <div id="iaSinIA" class="ia-sin hidden">${I.info}<span>Sin IA, Factorum lee con el QR, el texto del PDF y el OCR. Lo que no pueda leer queda para completar a mano.</span></div>
    <div id="iaDestino" class="ia-destino"></div>
  </div>`;
}

// iaIniciar arma la sección con lo guardado (sin tocar nada) y pide la lista de modelos.
function iaIniciar() {
  if (!$('#conf-ia') || !IAUI) return;
  iaAplicarProveedor(false);
}

// iaAplicarProveedor muestra lo que corresponde al proveedor elegido. Con nuevo, además completa la
// URL y vacía la clave y el modelo (salvo que se vuelva al proveedor guardado).
function iaAplicarProveedor(nuevo) {
  const prov = iaEl('ia.proveedor').value;
  const p = iaProvDe(prov);
  const g = iaGuardado();
  const url = iaEl('ia.url'), clave = iaEl('ia.api_key'), modelo = iaEl('ia.modelo');
  const vuelve = prov === g.prov;
  if (nuevo && !iaCampo('ia.url').bloqueado) {
    url.value = vuelve ? g.url : (p && p.url) || '';
    if (prov === 'openai') $('#iaRegion').value = vuelve ? (iaCat().region || '') : '';
  }
  if (nuevo) {
    if (clave && !clave.disabled) clave.value = '';
    modelo.value = vuelve ? g.modelo : '';
    IAUI.lista = null;
  }
  const hay = !!p;
  ['#iaModeloWrap', '#iaDestino'].forEach((s) => $(s).classList.toggle('hidden', !hay));
  $('#iaUrlWrap').classList.toggle('hidden', !hay || prov === 'openai'); // en OpenAI la URL sale de la región
  $('#iaRegionUrl').textContent = url.value;
  $('#iaSinIA').classList.toggle('hidden', hay);
  $('#iaRegionWrap').classList.toggle('hidden', prov !== 'openai');
  $('#iaClaveWrap').classList.toggle('hidden', !p || p.clave === 'no');
  $('#iaProvAyuda').textContent = p ? p.ayuda : 'Factorum no usa ninguna IA.';
  // La URL se escribe solo donde tiene sentido (Ollama en otra máquina, otro servidor).
  const editable = p && p.editable && !iaCampo('ia.url').bloqueado;
  url.readOnly = !editable;
  url.classList.toggle('solo-lectura', !editable);
  url.placeholder = prov === 'otro' ? 'Por ejemplo: http://lmstudio:1234/v1' : '';
  $('#iaUrlAyuda').textContent = !p ? '' : prov === 'ollama' ? 'Con el compose de Factorum es http://ollama:11434/v1. Si Ollama corre en otra máquina, cambiala.'
    : prov === 'otro' ? 'La de la API compatible con OpenAI; suele terminar en /v1.' : prov === 'openai' ? 'La completa la región.' : `La de ${p.corto}: no hace falta cambiarla.`;
  // La clave: dónde se saca y si la guardada sirve para este proveedor.
  const g2 = $('#iaClaveGuardada');
  if (g2) g2.innerHTML = vuelve ? g2.dataset.texto : (iaCampo('ia.api_key').configurado ? 'La clave guardada es de otro proveedor: escribí la nueva.' : '');
  if (clave && !clave.disabled) clave.placeholder = vuelve && iaCampo('ia.api_key').configurado ? 'Configurada' : 'Pegá la clave';
  $('#iaClaveDonde').innerHTML = p && p.clave_url ? `Se saca en <a class="link" href="${esc(p.clave_url)}" target="_blank" rel="noopener noreferrer">${esc(p.clave_donde)}${I.externo}</a>.`
    : p && p.clave === 'opcional' ? 'Solo si tu servidor la pide.' : '';
  iaPintarDestino();
  iaPintarRecos();
  iaPintarModelos();
  if (hay) iaPedirModelos(false);
}

// iaPintarDestino: a dónde irían los comprobantes con lo que está en pantalla (el sello de verdad
// lo calcula el servidor con el host; acá se muestra el que vino con la lista).
function iaPintarDestino() {
  const caja = $('#iaDestino');
  const l = IAUI.lista;
  if (!caja) return;
  const a = CONF.ia_aviso || {};
  // Si es lo mismo que ya se usa, el estado de abajo alcanza.
  const igual = !l || !l.sello || (a.configurada && a.host && iaHost(l.url) === a.host);
  $$('#conf-ia .ia-ahora-et').forEach((e) => e.classList.toggle('hidden', !!igual));
  if (igual) { caja.innerHTML = ''; return; }
  const yaAceptado = !l.local && a.aceptado && a.host && iaHost(l.url) === a.host;
  caja.innerHTML = '<span class="ia-destino-et">Con lo que elegiste:</span>' + (l.local
    ? `<span class="sello-ia local">${I.servidor}<span>${esc(l.sello)}</span></span>`
    : `<span class="sello-ia externa">${I.nube}<span>${esc(l.sello)}</span></span><span class="muted">${yaAceptado ? 'El aviso de qué pasa con los datos ya está aceptado para este host.' : 'Antes de guardarla (o de probarla) vas a ver qué pasa con los datos y tenés que aceptarlo.'}</span>`);
}
// conPunto: los mensajes del servidor no siempre terminan en punto.
const conPunto = (t) => (/[.!?…:)]$/.test(String(t || '').trim()) ? String(t).trim() : String(t || '').trim() + '.');
const iaHost = (u) => { try { return new URL(u).host.toLowerCase(); } catch { return ''; } };

// iaPedirModelos le pide la lista al servidor, que se la pide al proveedor. Lo último que se pidió
// gana; lo ya pedido con los mismos datos sale del cache (Actualizar lo vuelve a pedir).
async function iaPedirModelos(forzar) {
  const prov = iaEl('ia.proveedor').value;
  const p = iaProvDe(prov);
  if (!p || !IAUI) return;
  const url = iaEl('ia.url').value.trim();
  const ce = iaEl('ia.api_key');
  const clave = ce && !ce.disabled && p.clave !== 'no' ? ce.value.trim() : '';
  if (p.editable && !url) { IAUI.lista = { proveedor: prov, exito: false, motivo: 'url', mensaje: 'Escribí la URL para ver los modelos.' }; iaPintarModelos(); return; }
  const k = [prov, url, clave].join('\n');
  const n = ++IAUI.pedido;
  if (!forzar && IAUI.cache.has(k)) { IAUI.lista = IAUI.cache.get(k); iaPintarDestino(); iaPintarRecos(); iaPintarModelos(); return; }
  IAUI.lista = { proveedor: prov, cargando: true };
  iaPintarModelos();
  let r;
  try {
    r = await api('/api/ia/modelos', { method: 'POST', json: { proveedor: prov, url, clave } });
  } catch (e) { r = { proveedor: prov, exito: false, motivo: 'respuesta', mensaje: e.message }; }
  if (!IAUI || n !== IAUI.pedido) return; // ya se pidió otra cosa
  if (r.exito || r.motivo === 'clave' || r.motivo === 'vacia') IAUI.cache.set(k, r);
  IAUI.lista = r;
  iaPintarDestino();
  iaPintarRecos();
  iaPintarModelos();
}

// La etiqueta de cada opción: el nombre, el tamaño (Ollama) y, si Factorum lo probó, el resultado.
function iaEtiqueta(m) {
  let t = m.nombre && m.nombre !== m.id ? `${m.nombre} (${m.id})` : m.id;
  if (m.recomendado && m.imagenes) return `${t} · probado: ${m.recomendado.porcentaje} %`;
  if (m.detalle) t += ` · ${m.detalle}`;
  if (!m.imagenes) t += ' · no lee imágenes';
  return t;
}

function iaPintarModelos() {
  const sel = $('#iaModeloSel');
  if (!sel || !IAUI) return;
  const l = IAUI.lista || {};
  const actual = iaEl('ia.modelo').value;
  const est = $('#iaModeloEstado');
  const p = iaProvDe(iaEl('ia.proveedor').value) || {};
  const opt = (m, dis) => `<option value="${esc(m.id)}"${dis ? ' disabled' : ''}>${esc(iaEtiqueta(m))}</option>`;
  let h = '';
  const ms = l.exito ? l.modelos || [] : [];
  if (l.cargando) h = '<option value="">Pidiendo la lista…</option>';
  else if (ms.length) {
    // Los probados arriba, en el orden de la prueba (el mejor primero), y no se repiten abajo.
    const orden = iaRecosDe(p.id).map((r) => r.modelo);
    const recos = ms.filter((m) => m.recomendado && m.imagenes).sort((a, b) => orden.indexOf(a.id) - orden.indexOf(b.id));
    const lee = ms.filter((m) => m.imagenes && !recos.includes(m)), no = ms.filter((m) => !m.imagenes);
    const total = recos.length + lee.length;
    h = `<option value="">Elegí un modelo (${total} ${total === 1 ? 'lee' : 'leen'} imágenes)</option>`;
    if (recos.length) h += `<optgroup label="Probados por Factorum">${recos.map((m) => opt(m)).join('')}</optgroup>`;
    if (p.id === 'openrouter') {
      const grupos = [...new Set(lee.map((m) => m.grupo || 'Otros'))];
      h += grupos.map((g) => `<optgroup label="${esc(g)}">${lee.filter((m) => (m.grupo || 'Otros') === g).map((m) => opt(m)).join('')}</optgroup>`).join('');
    } else {
      if (lee.length) h += `<optgroup label="${recos.length ? 'Los demás' : 'Leen imágenes'}">${lee.map((m) => opt(m)).join('')}</optgroup>`;
    }
    if (no.length) h += `<optgroup label="No leen imágenes">${no.map((m) => opt(m, true)).join('')}</optgroup>`;
  } else {
    h = `<option value="">${actual ? 'Elegí un modelo' : 'Sin lista: escribí el modelo'}</option>`;
    if (actual) h += `<option value="${esc(actual)}">${esc(actual)}</option>`;
  }
  h += `<option value="${IA_OTRO}">Otro modelo…</option>`;
  sel.innerHTML = h;
  sel.disabled = !!l.cargando || iaCampo('ia.modelo').bloqueado;
  const enLista = actual && [...sel.options].some((o) => o.value === actual && !o.disabled);
  const mano = $('#iaModeloMano');
  if (l.cargando) { sel.value = ''; }
  else if (IAUI.aMano || (actual && !enLista)) { sel.value = IA_OTRO; mano.classList.remove('hidden'); if (document.activeElement !== mano) mano.value = actual; }
  else { sel.value = actual || ''; mano.classList.add('hidden'); }
  // Debajo: lo que pasa con la lista o el resultado de Factorum con el elegido.
  if (l.cargando) { est.className = 'ia-estado cargando'; est.innerHTML = `<span class="girando" aria-hidden="true"></span><span>Pidiendo los modelos a ${esc(p.corto || 'el proveedor')}…</span>`; return; }
  if (l.exito === false) {
    const masClave = (l.motivo === 'clave' || l.motivo === 'sin_clave') && p.clave_url ? ` <a class="link" href="${esc(p.clave_url)}" target="_blank" rel="noopener noreferrer">Sacar una clave${I.externo}</a>` : '';
    est.className = 'ia-estado ' + (l.motivo === 'sin_clave' || l.motivo === 'url' ? 'info' : 'mal');
    const aMano = ['red', 'respuesta', 'vacia'].includes(l.motivo) ? ' También podés elegir «Otro modelo…» y escribirlo.' : '';
    est.innerHTML = `${l.motivo === 'sin_clave' || l.motivo === 'url' ? I.info : I.alerta}<span>${esc(conPunto(l.mensaje))}${masClave}${aMano}</span>`;
    return;
  }
  const m = ms.find((x) => x.id === actual);
  const r = (m && m.recomendado) || iaRecosDe(p.id).find((x) => x.modelo === actual);
  if (r) {
    est.className = 'ia-estado ok';
    est.innerHTML = `<span class="sello-prueba">${I.check}<span>${esc(r.texto)}</span></span><span class="muted">${esc(r.nota)} Es una prueba chica: el detalle está en «Probados por Factorum».</span>`;
  } else if (ms.length && !ms.some((x) => x.imagenes)) {
    est.className = 'ia-estado mal';
    est.innerHTML = `${I.alerta}<span>Ninguno de tus modelos lee imágenes. Bajá uno, por ejemplo: <code>ollama pull qwen3-vl:4b-instruct</code></span>`;
  } else if (m && m.nota) {
    est.className = 'ia-estado'; est.innerHTML = `${I.info}<span>${esc(m.nota)}.</span>`;
  } else if (actual && sel.value === IA_OTRO) {
    est.className = 'ia-estado'; est.innerHTML = `${I.info}<span>Escrito a mano: tocá Probar para ver si lo lee.</span>`;
  } else {
    const n = ms.filter((x) => x.imagenes).length;
    const filtro = { openai: ' (sin los de audio, voz, imágenes, embeddings y los que no son de chat)', gemini: ' (sin los de audio, voz, imágenes y embeddings)', ollama: '' }[p.id] || '';
    est.className = 'ia-estado'; est.innerHTML = ms.length ? `<span class="muted">Lista de ${esc(p.corto)}: ${n} ${n === 1 ? 'modelo' : 'modelos'}${p.id === 'otro' ? '' : ' que leen imágenes'}${filtro}${l.clave_guardada ? ', con la clave guardada' : ''}.</span>` : '';
  }
}

// iaPintarRecos: los modelos que Factorum probó para este proveedor, con un botón para usarlos.
function iaPintarRecos() {
  const caja = $('#iaRecos');
  if (!caja || !IAUI) return;
  const prov = iaEl('ia.proveedor').value;
  const p = iaProvDe(prov);
  if (!p) { caja.innerHTML = ''; return; }
  const rs = iaRecosDe(prov);
  const l = IAUI.lista || {};
  const lista = l.exito ? l.modelos || [] : null;
  if (!rs.length) {
    caja.innerHTML = `<p class="muted ia-recos-nada">${I.info}<span>Factorum todavía no probó modelos ${esc(iaDel(p))}. Elegí uno que lea imágenes y tocá <strong>Probar</strong>: manda un ticket de prueba y te dice qué leyó y cuánto tardó.</span></p>`;
    return;
  }
  const filas = rs.map((r) => {
    const esta = !lista || lista.some((m) => m.id === r.modelo && m.imagenes);
    const elegido = iaEl('ia.modelo').value === r.modelo;
    const accion = elegido ? `<span class="ia-elegido">${I.check}Elegido</span>`
      : esta ? `<button type="button" class="ghost mini" data-accion="iaUsar" data-modelo="${esc(r.modelo)}">Usar</button>`
      : prov === 'ollama' ? `<span class="ia-falta-modelo"><span class="muted">No está en tu Ollama:</span> <code>ollama pull ${esc(r.modelo)}</code><button type="button" class="ghost mini" data-accion="copiar" data-texto="ollama pull ${esc(r.modelo)}">${I.copiar}<span>Copiar</span></button></span>`
        : '<span class="muted">No está en tu lista</span>';
    return `<li><span class="ia-reco-mod mono">${esc(r.modelo)}</span>
      <span class="ia-reco-pct" title="${esc(r.nota)}"><span class="prog mini-prog"><div style="width:${r.porcentaje}%"></div></span><strong>${r.porcentaje} %</strong></span>
      <span class="ia-reco-t">${esc(r.tiempo)} por imagen${r.cpu ? ' en 4 núcleos sin GPU' : ''}${r.mitad ? ' · en la mitad de la prueba' : ''}</span>
      <span class="ia-reco-acc">${accion}</span></li>`;
  }).join('');
  caja.innerHTML = `<h4>Probados por Factorum</h4><ul>${filas}</ul><p class="muted ia-recos-pie">${esc(iaCat().prueba || '')}</p>`;
}
const iaDel = (p) => (p.corto.startsWith('el ') ? 'del ' + p.corto.slice(3) : 'de ' + p.corto);

function iaElegirModelo(v) {
  const mano = $('#iaModeloMano');
  if (v === IA_OTRO) {
    IAUI.aMano = true;
    mano.classList.remove('hidden');
    mano.focus();
    iaEl('ia.modelo').value = mano.value.trim();
  } else {
    IAUI.aMano = false;
    mano.classList.add('hidden');
    iaEl('ia.modelo').value = v;
  }
  $('#conf-ia .prueba').classList.add('hidden');
  iaPintarModelos();
  iaPintarRecos();
}

function iaUsar(modelo) {
  const sel = $('#iaModeloSel');
  const esta = [...sel.options].some((o) => o.value === modelo && !o.disabled);
  IAUI.aMano = !esta;
  iaEl('ia.modelo').value = modelo;
  $('#iaModeloMano').value = modelo;
  iaPintarModelos();
  iaPintarRecos();
  sel.focus();
}

// Lo que cambió en la sección IA (o todo lo de la pantalla, para Probar).
function cambiosIA(todo) {
  const out = {};
  const prov = iaEl('ia.proveedor').value;
  const libre = (k) => !iaCampo(k).bloqueado;
  if (!prov) { // sin IA: vuelve a lo del servidor (o a nada)
    ['ia.proveedor', 'ia.url', 'ia.modelo', 'ia.api_key'].forEach((k) => { if (libre(k) && iaCampo(k).origen === 'pantalla') out[k] = null; });
    return out;
  }
  const val = { 'ia.proveedor': prov, 'ia.url': iaEl('ia.url').value.trim(), 'ia.modelo': iaEl('ia.modelo').value.trim() };
  for (const [k, v] of Object.entries(val)) {
    if (libre(k) && (todo || v !== iaEl(k).dataset.orig)) out[k] = v;
  }
  const ce = iaEl('ia.api_key');
  if (ce && !ce.disabled && (iaProvDe(prov) || {}).clave !== 'no' && ce.value.trim()) out['ia.api_key'] = ce.value.trim();
  return out;
}

// Probar la IA: lo que está en pantalla, con el ticket de prueba. Si es externa y no está
// aceptada, primero el aviso.
async function probarIA(btn) {
  const caja = $('#conf-ia .prueba');
  const cambios = cambiosIA(true);
  const prov = iaProvDe(iaEl('ia.proveedor').value);
  if (!prov) { caja.className = 'prueba mal'; caja.innerHTML = `${I.alerta}<span>Elegí un proveedor.</span>`; return; }
  btn.disabled = true; btn.textContent = 'Probando...';
  const lento = prov.id === 'ollama' ? ' Con Ollama sin GPU puede tardar uno o dos minutos.' : '';
  caja.className = 'prueba'; caja.innerHTML = `<span class="girando" aria-hidden="true"></span><span>Mandándole el ticket de prueba a ${esc(prov.corto)}…${lento}</span>`;
  const pedir = (acepta) => api('/api/configuracion/probar/ia', { method: 'POST', json: { cambios, aceptar_aviso_ia: acepta || undefined } });
  try {
    let r;
    const ac = IAUI.aceptadoPrueba;
    try {
      r = await pedir(ac);
    } catch (e) {
      if (!(e.datos && e.datos.codigo === 'aviso_ia')) throw e;
      const a = e.datos.aviso_ia;
      if (!(await abrirAvisoIA(a, 'prueba'))) { caja.className = 'prueba'; caja.innerHTML = `${I.info}<span>No se probó: es una IA externa y no aceptaste el aviso.</span>`; return; }
      IAUI.aceptadoPrueba = { host: a.host, version: a.version };
      r = await pedir(IAUI.aceptadoPrueba);
    }
    caja.className = 'prueba ' + (r.exito ? 'bien' : 'mal');
    const filas = (r.leido || []).map((f) => `<tr><th scope="row">${esc(f.campo)}</th><td class="mono">${esc(f.valor)}</td><td>${f.bien === true ? `<span class="ia-ok">${I.check}Bien</span>` : f.bien === false ? `<span class="ia-mal">${I.cruz}Era ${esc(f.esperado)}</span>` : ''}</td></tr>`).join('');
    caja.innerHTML = `${r.exito ? I.check : I.alerta}<div class="prueba-ia"><span>${esc(r.mensaje)}</span>${filas ? `<table class="ia-leido"><caption>Qué leyó del ticket de prueba</caption><tbody>${filas}</tbody></table>` : ''}</div>`;
  } catch (e) { caja.className = 'prueba mal'; caja.innerHTML = `${I.alerta}<span>${esc(e.message)}</span>`; }
  finally { btn.disabled = false; btn.textContent = 'Probar'; }
}

// ---------- Ventana: Novedades y Acerca de ----------
let MODAL_FIJO = false;
// abrirModal(titulo, html, { acciones, fijo, ancho }): fijo no se cierra con Esc, la X ni el fondo.
function abrirModal(titulo, html, opts = {}) {
  MODAL_FIJO = !!opts.fijo;
  $('#modalTit').textContent = titulo;
  $('#modalCuerpo').innerHTML = html;
  $('#modalAcciones').innerHTML = opts.acciones || '<button type="button" data-accion="modalCerrar">Entendido</button>';
  $('#modalX').classList.toggle('hidden', MODAL_FIJO);
  $('#modal').querySelector('.modal-card').classList.toggle('ancho', !!opts.ancho);
  $('#modal').classList.remove('hidden');
}
function cerrarModal(forzar) {
  if (MODAL_FIJO && !forzar) return;
  MODAL_FIJO = false;
  $('#modal').classList.add('hidden');
}

// Markdown mínimo del CHANGELOG: ### títulos, - viñetas (con continuación), **negrita** y `código`.
function mdMini(md) {
  const inl = (t) => esc(t).replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>').replace(/`(.+?)`/g, '<code>$1</code>');
  const out = []; let enLista = false; let item = null;
  const cerrarItem = () => { if (item !== null) { out.push(`<li>${inl(item)}</li>`); item = null; } };
  for (const l of md.split('\n')) {
    if (/^###\s/.test(l)) { cerrarItem(); if (enLista) { out.push('</ul>'); enLista = false; } out.push(`<h4>${inl(l.replace(/^###\s/, ''))}</h4>`); }
    else if (/^- /.test(l)) { cerrarItem(); if (!enLista) { out.push('<ul>'); enLista = true; } item = l.slice(2); }
    else if (/^\s+\S/.test(l) && item !== null) item += ' ' + l.trim();
    else if (l.trim()) { cerrarItem(); if (enLista) { out.push('</ul>'); enLista = false; } out.push(`<p>${inl(l)}</p>`); }
  }
  cerrarItem(); if (enLista) out.push('</ul>');
  return out.join('');
}

// Al entrar después de una actualización, muestra lo nuevo desde la última versión vista.
async function verNovedades(todas) {
  try {
    const { novedades } = await fetch('/api/novedades').then((r) => r.json());
    if (!novedades || !novedades.length) return;
    let vista = '';
    try { vista = localStorage.getItem('factorum.version_vista') || ''; } catch { /* nada */ }
    const actual = novedades[0].version;
    const cmp = (a, b) => { const x = a.split('.').map(Number), y = b.split('.').map(Number); for (let i = 0; i < 3; i++) { if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) - (y[i] || 0); } return 0; };
    let lista = novedades;
    if (!todas) {
      if (vista && cmp(actual, vista) <= 0) return;
      lista = vista ? novedades.filter((n) => cmp(n.version, vista) > 0) : novedades.slice(0, 1);
    }
    try { localStorage.setItem('factorum.version_vista', actual); } catch { /* nada */ }
    abrirModal(todas ? 'Novedades' : `Factorum se actualizó a la ${actual}`,
      lista.map((n) => `<p><span class="ver">v${esc(n.version)}</span> <span class="muted">${esc(n.fecha || '')}</span></p>${mdMini(n.cuerpo)}`).join('<hr style="border:0;border-top:1px solid var(--border);margin:14px 0">'));
  } catch { /* sin novedades: no pasa nada */ }
}

function acercaDe() {
  const s = SALUD || {};
  const ia = s.ia && s.ia.habilitada ? `${esc(s.ia.modelo)}${s.ia.local ? ' (local)' : ` (externa${AVISO_IA && AVISO_IA.nombre ? ': ' + esc(AVISO_IA.nombre) : ''}: los comprobantes que la necesiten salen del servidor)`}` : 'apagada';
  abrirModal('Acerca de Factorum', `<div class="acerca"><img src="logo.svg" alt="" width="56" height="56"><div><strong style="font-size:17px;color:var(--text)">Factorum ${esc(s.version || '')}</strong><br>Lector de facturas de ARCA. Parte de la suite Escriba.</div></div>
    <p>Lee los comprobantes por su QR y su texto, sin depender de una IA; controla que todo cierre y, con Arcanum, los constata con ARCA.</p>
    <dl class="kv" style="margin-top:12px">
      <dt>OCR</dt><dd>${s.ocr ? 'Tesseract' : 'no instalado'}</dd>
      <dt>IA</dt><dd>${ia}</dd>
      <dt>Arcanum</dt><dd>${s.arcanum ? 'conectado' : 'sin configurar'}</dd>
      <dt>Archivos</dt><dd>${esc(s.almacen || '')}${s.cifrado ? ', cifrados' : ', sin cifrar'}</dd>
      <dt>Base</dt><dd>${s.base ? 'conectada' : 'sin conexión'}</dd>
    </dl>
    <p style="margin-top:12px"><a class="link" href="https://github.com/diegoparras/factorum" target="_blank" rel="noopener">Código y documentación</a></p>`);
}

// ---------- Fechas en hora argentina ----------
const TZ = 'America/Argentina/Buenos_Aires';
const fechaHora = (s) => (s ? new Date(s).toLocaleString('es-AR', { timeZone: TZ, dateStyle: 'short', timeStyle: 'short' }) : '');
const fechaCorta = (s) => (s ? new Date(s).toLocaleDateString('es-AR', { timeZone: TZ }) : '');
const cuitsHTML = (l) => (l && l.length ? l.map((c) => `<span class="cuit-chip mono">${esc(cuit(c))}</span>`).join(' ')
  : Array.isArray(l) ? '<span class="chip-ninguno" title="No ve nada hasta que le asignes clientes">Ninguno todavía</span>' : '<span class="muted">Todos</span>');
const cuitsTexto = (l) => (l || []).map(cuit).join(', ');
const rolesHasta = (max, actual) => ORDEN_ROL.filter((r) => ORDEN_ROL.indexOf(r) <= ORDEN_ROL.indexOf(max))
  .map((r) => `<option value="${r}"${r === actual ? ' selected' : ''}>${ROLES[r]}</option>`).join('');
const miRol = () => (YO && YO.identidad && YO.identidad.rol) || 'lectura';

// copiar al portapapeles (con plan B para navegadores sin permiso).
async function copiar(texto) {
  try { await navigator.clipboard.writeText(texto); toast('Copiado'); return; } catch { /* plan B */ }
  const t = document.createElement('textarea');
  t.value = texto; t.setAttribute('readonly', ''); t.style.position = 'fixed'; t.style.opacity = '0';
  document.body.append(t); t.select();
  try { document.execCommand('copy'); toast('Copiado'); } catch { toast('No se pudo copiar: seleccionalo a mano', true); }
  t.remove();
}
const bloqueCodigo = (texto, etiqueta) => `<div class="codigo"><div class="codigo-cab"><span>${esc(etiqueta || '')}</span><button type="button" class="ghost mini" data-accion="copiar" data-texto="${esc(texto)}">${I.copiar}<span>Copiar</span></button></div><pre>${esc(texto)}</pre></div>`;

// secretoUnaVez: lo que se muestra una sola vez (contraseña temporal, clave de API).
function secretoUnaVez(titulo, intro, valor, pie) {
  abrirModal(titulo, `<p>${intro}</p>
    <div class="una-vez"><div class="pass-wrap"><input id="unaVez" class="mono" type="text" readonly value="${esc(valor)}" aria-label="${esc(titulo)}"><button type="button" class="pass-toggle" data-accion="ojo" data-para="unaVez" aria-label="Mostrar u ocultar">${I.ojoNo}</button></div><button type="button" class="ghost mini" data-accion="copiar" data-texto="${esc(valor)}">${I.copiar}<span>Copiar</span></button></div>
    <p class="muted">${pie}</p>`, { fijo: true, acciones: '<button type="button" data-accion="modalCerrarFijo">Ya la guardé</button>' });
}

// tarjetas: en el celular cada fila se ve como una tarjeta con la etiqueta de cada dato.
function tarjetas(sel) {
  const t = $(sel + ' table.t');
  if (t) tarjetaDe(t);
}
// tarjetaDe: en el celular, cada fila es una tarjeta con el título de la columna en cada dato.
function tarjetaDe(t) {
  t.classList.add('tarjetas');
  const et = $$('thead th', t).map((th) => th.textContent.trim());
  $$('tbody tr', t).forEach((tr) => [...tr.children].forEach((td, i) => { if (et[i]) td.dataset.et = et[i]; }));
}

// ---------- Configuración ----------
const SECCIONES = {
  general: { t: 'General', d: 'Nombre, URL pública y lo que viene marcado al cargar.', probar: true, clave: ['general.url_publica'],
    como: ['Poné la dirección con la que entrás desde el navegador, con https (por ejemplo https://factorum.estudio.com.ar), sin la barra final.',
      'Si Factorum está detrás de EasyPanel, Traefik o Caddy, es el dominio que configuraste ahí.',
      'Tocá Probar: Factorum se llama a sí mismo por esa URL desde el servidor.',
      'Con https, la cookie de sesión sale marcada como segura. La URL pública hace falta para entrar desde Arcanum sin volver a loguearte.'] },
  arcanum: { t: 'Arcanum', d: 'Constatación con ARCA (WSCDC), apócrifos y padrón; y el pase para abrir Arcanum desde acá.', probar: true, clave: ['arcanum.url', 'arcanum.api_key'],
    como: ['En Arcanum, entrá a Claves de API y creá una clave nueva. Para constatar alcanza con rol lectura; limitala a los CUITs que consultan.',
      'Para abrir Arcanum desde acá sin volver a loguearte, marcá en esa clave «Puede abrir sesiones de la suite».',
      'URL interna: cómo llega este servidor a Arcanum. En el mismo docker compose, http://arcanum:8094.',
      'URL pública: la que ves en el navegador cuando usás Arcanum.',
      'Pegá la clave, guardá y tocá Probar: Factorum consulta /api/health y una lista de solo lectura.',
      'Para el camino inverso (de Arcanum a Factorum), creá acá en Claves de API una clave con «Puede abrir sesiones de la suite» y cargala en Arcanum como FACTORUM_TOKEN.'] },
  lockatus: { t: 'Lockatus (inicio de sesión único)', d: 'Entrar con la cuenta de la suite Escriba: una sola contraseña para Factorum, Arcanum y las demás.', probar: true,
    clave: ['lockatus.emisor', 'lockatus.client_id', 'lockatus.client_secret'],
    como: ['En Lockatus, como administrador, registrá una aplicación nueva para Factorum: tipo confidencial (con client secret), flujo Authorization Code con PKCE (S256).',
      'Redirect URI: copiá la de arriba tal cual (tu URL pública más /api/auth/oidc/callback). Tiene que coincidir exacta, con https y sin barra al final. Si no aparece, completá primero la URL pública en General.',
      'Scopes: openid, profile y email. Factorum reconoce a cada persona por su email verificado: si Lockatus no lo manda, o lo manda con email_verified en false, no entra.',
      'El rol: en Lockatus, mandá el rol de cada persona para Factorum en el claim role del id_token (superadmin, admin, usuario u operador, o lectura). Si usás otro claim, poné su nombre en «Claim del rol» (con puntos si está anidado, como realm_access.roles). Solo se usa para crear la cuenta; después manda el rol que tiene en Factorum.',
      'Copiá acá el emisor (la dirección de Lockatus, tal como la publica en su discovery), el client ID y el client secret, guardá y tocá Probar: Factorum lee /.well-known/openid-configuration y las claves para firmar.',
      'Cerrá sesión y tocá «Entrar con Lockatus». Si tu email ya tiene cuenta acá, entrás con tu rol y tus clientes de Factorum.',
      'Quien entra por primera vez se crea con el rol que manda Lockatus (o el de «Rol para quien no trae uno conocido»). Admin y superadmin ven todos los clientes; usuario y lectura, ninguno hasta que les asignes clientes en Usuarios.',
      'Ya adentro con Lockatus, podés activar «Solo se entra con Lockatus». Si Lockatus se cae, en el servidor poné FACTORUM_LOGIN_LOCAL=1 y reiniciá: vuelve el login con contraseña. Sacala cuando Lockatus ande.'] },
  telegram: { t: 'Telegram', d: 'Un bot que recibe PDFs y fotos y contesta cuando termina.', probar: true, clave: ['telegram.token'],
    como: ['En Telegram, abrí @BotFather y mandale /newbot.', 'Elegí un nombre y un usuario que termine en «bot».',
      'Copiá el token que te da (algo como 123456789:AA...) y pegalo acá.', 'Guardá: el bot arranca solo, sin reiniciar. Probar confirma el token con getMe.',
      'Escribile al bot desde tu cuenta: te contesta tu id. Agregalo a «Usuarios o chats permitidos» y guardá.'] },
  whatsapp: { t: 'WhatsApp', d: 'Como dispositivo vinculado de un número: tus clientes mandan la foto y queda cargada.', clave: ['whatsapp.activo'],
    como: ['Usá un número que no sea el personal: no es la API oficial y WhatsApp lo puede bloquear.', 'Activá WhatsApp y guardá.',
      'En el teléfono: WhatsApp, Dispositivos vinculados, Vincular un dispositivo, y escaneá el código que aparece acá (o pedí un código de 8 letras).',
      'Agregá los números de quienes pueden mandar facturas, con código de país y sin el 0 ni el 15 (5491155550000).',
      'Para pedirle algo sin archivo, el mensaje empieza con «factorum» (por ejemplo «factorum estado»).'] },
  webhook: { t: 'Webhook', d: 'Para tu propio bot (la secretaria, Evolution API, n8n).', probar: true, clave: ['webhook.token'],
    como: ['Tocá Generar para crear el token de entrada y guardá.', 'Tu bot manda un POST a la URL de entrada (abajo) con la cabecera X-Factorum-Token.',
      'El cuerpo: {"chat", "remitente", "texto", "archivos": [{"nombre", "mime", "contenido_base64"}]}. La respuesta trae el texto para contestar.',
      'Si querés el aviso de fin de lote, poné la URL de salida: llega un POST {chat, texto} firmado con HMAC-SHA256 en X-Factorum-Firma.',
      'Probar manda un aviso de prueba firmado a la URL de salida.'] },
  ia: { t: 'IA (visión)', d: 'Último recurso: solo completa lo que el QR, el texto y el OCR no pudieron leer.', probar: true, clave: ['ia.url', 'ia.modelo'],
    como: ['Elegí el proveedor. La URL se completa sola; en Ollama y en «Otro» se puede cambiar.',
      'Local: Ollama en tu servidor (docker compose --profile ia up -d y después docker compose exec ollama ollama pull qwen3-vl:4b-instruct). Las imágenes no salen, pero sin GPU tarda cerca de un minuto por imagen.',
      'Externa: OpenAI, Anthropic, Google Gemini u OpenRouter, con su clave de API. Es rápida y lee mejor, pero los comprobantes que necesiten IA salen de tu servidor: antes de guardar, Factorum te muestra a dónde viajan, cuánto tiempo se guardan y si entrenan con ellos, y tenés que aceptarlo.',
      'Con el proveedor (y la clave, si la pide), Factorum le pide la lista de modelos y deja solo los que leen imágenes. Los «Probados por Factorum» salen de una prueba chica con tickets de compra. Si el tuyo no está, elegí «Otro modelo…» y escribilo.',
      'Probar manda un ticket de prueba (inventado) con lo que está en pantalla, aunque no lo hayas guardado, y te dice qué leyó y cuánto tardó.',
      'Cada persona, la primera vez que marca «Usar IA si hace falta», ve el mismo aviso. Si cambiás de proveedor o de host, se pide de nuevo.'] },
  r2: { t: 'Almacenamiento R2', d: 'Dónde se guardan los archivos. Sin R2, en el disco del servidor. Siempre comprimidos y cifrados.', probar: true, clave: ['r2.endpoint', 'r2.bucket', 'r2.access_key', 'r2.secret_key'],
    como: ['En Cloudflare, R2, creá un bucket (por ejemplo factorum).', 'En «Administrar tokens de la API de R2», creá un token de lectura y escritura sobre ese bucket.',
      'Copiá el endpoint de S3 (https://<cuenta>.r2.cloudflarestorage.com), la access key y la secret key.', 'Probar guarda y borra un objeto de prueba.',
      'Al guardar, lo nuevo va a R2 y lo que ya estaba en el disco se sigue leyendo de ahí.',
      'Para no depender del disco, tocá «Pasar lo del disco a R2»: sube cada objeto tal cual (ya está cifrado), lo verifica y se puede pausar. Si se corta o se reinicia el servidor, sigue desde donde quedó.',
      'Cuando termina, «Liberar el disco» borra del disco solo lo que quedó verificado en R2. Por consola: factorum r2 migrar y factorum r2 migrar --liberar.'] },
  copias: { t: 'Copias de seguridad', d: 'Una copia diaria de la base, comprimida y cifrada con la clave maestra, en R2 o en el disco.', clave: ['copias.activo'],
    como: ['Activá la copia diaria, elegí la hora (de Buenos Aires) y cuántas guardar, y guardá.',
      'Si R2 está activo, las copias van al bucket (carpeta copias); si no, al disco del servidor (FACTORUM_DATOS/copias). Conviene R2: una copia en el mismo disco no sirve si se rompe el disco.',
      'La copia es lógica y portable: no depende de la versión de Postgres. Se abre solo con la misma FACTORUM_MASTER_KEY: guardala fuera del servidor.',
      'Bajá de vez en cuando una copia con «Descargar» y guardala en otro lado.',
      'Restaurar se hace por consola (borra lo que hay en la base): mirá «Cómo restaurar», abajo de la lista.'] },
  papelera: { t: 'Papelera', d: 'Cuánto tiempo queda lo que se borra antes de borrarse para siempre.', clave: ['papelera.dias'],
    como: ['Borrar un lote (desde Lotes) o comprobantes sueltos (desde Comprobantes o la revisión) no borra nada de verdad: lo manda a la Papelera, donde se puede restaurar tal cual estaba.',
      'Pasados estos días (de 1 a 365), Factorum lo borra para siempre, con sus correcciones y los archivos del disco y de R2 que no use ningún otro lote. Lo revisa una vez por hora.',
      'Un superadmin puede vaciar la papelera o borrar algo ya, escribiendo «borrar».'] },
  clientes: { t: 'Clientes y canales', d: 'Qué cliente es cada remitente de chat o de mail: lo que mande se carga para ese CUIT.', clave: ['clientes.canales'],
    como: ['Una línea por remitente: el número de WhatsApp (con código de país), el id de Telegram, el chat del webhook, un email o @dominio, un igual y el CUIT del cliente.',
      'Ejemplos: 5491155550000 = 20-22222222-3, juan@cliente.com.ar = 30-71111111-1, @cliente.com.ar = 30-71111111-1.',
      'Lo que mande ese remitente se carga para ese cliente y se constata con su certificado en Arcanum. Por mail, una dirección facturas+CUIT@ le gana a esto.'] },
  correo_entrada: { t: 'Correo entrante', d: 'Tus clientes mandan las facturas por mail y quedan cargadas. Solo se toman los adjuntos (PDF, fotos y ZIP): el texto del mail no se lee.', probar: true, clave: [] },
  correo_salida: { t: 'Correo saliente', d: 'Para contestarle a quien manda facturas, los avisos y los links para elegir contraseña.', probar: true, clave: ['smtp.host', 'smtp.remitente'],
    grupos: [
      ['Gmail o Google Workspace', ['Activá la verificación en dos pasos de la cuenta y creá una contraseña de aplicación en myaccount.google.com/apppasswords (nombre: Factorum).',
        'Servidor smtp.gmail.com, puerto 587 con STARTTLS (o 465 con TLS implícito). Usuario: el email entero. Contraseña: la de aplicación, sin espacios.',
        'El remitente tiene que ser esa cuenta o un alias verificado en Gmail («Enviar como»). Gmail deja unos 500 mails por día.']],
      ['Microsoft 365', ['Servidor smtp.office365.com, puerto 587 con STARTTLS. Usuario: el email entero.',
        'En el centro de administración, en el buzón de ese usuario: Correo, Administrar aplicaciones de correo, marcá «SMTP autenticado». Con verificación en dos pasos, usá una contraseña de aplicación.',
        'Microsoft está retirando el envío con usuario y contraseña: si deja de andar, pasate a un servicio transaccional.']],
      ['Resend', ['Verificá tu dominio en Resend (los registros DNS que te muestra).', 'Servidor smtp.resend.com, puerto 465 con TLS implícito (o 587 con STARTTLS). Usuario: resend. Contraseña: una API key de Resend.']],
      ['Postmark', ['Creá un servidor y verificá el remitente o el dominio (Sender Signatures).', 'Servidor smtp.postmarkapp.com, puerto 587 con STARTTLS. Usuario y contraseña: el Server API Token.']],
      ['Mailgun', ['En el dominio, Configuración de SMTP: creá una credencial (por ejemplo postmaster@mg.tudominio.com.ar).', 'Servidor smtp.mailgun.org, puerto 587 con STARTTLS. Usuario y contraseña: los de esa credencial.']],
      ['Brevo', ['En SMTP y API, pestaña SMTP: copiá el login (algo@smtp-brevo.com) y generá una clave SMTP.', 'Servidor smtp-relay.brevo.com, puerto 587 con STARTTLS. Usuario: ese login. Contraseña: la clave SMTP. Verificá el dominio del remitente.']],
      ['Probar', ['Escribí tu email en «Mandar la prueba a» y tocá Probar: llega un mail de prueba. Si no llega, mirá en spam y revisá que el dominio tenga SPF y DKIM del servicio.']],
    ] },
  avisos: { t: 'Avisos', d: 'Mails cuando termina un lote, cuando aparece un emisor apócrifo o un PDF posiblemente adulterado, y un resumen los lunes.', clave: [],
    como: ['Prendé cada aviso y, si hace falta, poné a quién más mandarlo (uno por línea).',
      'Lote terminado: le llega a quien lo cargó desde la pantalla; si vino por chat, por la API o por mail, a la lista.',
      'Apócrifos y adulterados: a los admins y superadmins que ven ese cliente, y a la lista.',
      'Resumen semanal: los lunes a las 8 (hora de Buenos Aires), con lo de la semana anterior y lo pendiente de revisar. Sin lista, a los admins.',
      'Cada persona de Factorum recibe solo lo de los clientes que puede ver. Una dirección de la lista que no es de nadie en Factorum recibe todo lo que se le manda.',
      'Hace falta el correo saliente.'] },
};

// ---------- Correo entrante: las cuatro formas ----------
const FORMAS = [
  ['imap', 'IMAP', 'Factorum revisa una casilla cada tanto. La más fácil.'],
  ['cloudflare', 'Cloudflare', 'Email Routing y un Worker: llega al instante.'],
  ['proveedor', 'Proveedor', 'Mailgun, Postmark o SendGrid te pasan cada mail.'],
  ['gmail', 'Gmail', 'Con permiso de Google, lee una etiqueta.'],
  ['apagado', 'Apagado', 'No entra nada por mail.'],
];
const GRUPOS_CORREO = {
  imap: ['imap.host', 'imap.puerto', 'imap.tls', 'imap.usuario', 'imap.clave', 'imap.carpeta', 'imap.intervalo', 'imap.procesado', 'imap.carpeta_procesado'],
  cloudflare: ['cloudflare.secreto'],
  proveedor: ['correo.proveedor', 'mailgun.signing_key', 'correo.token'],
  gmail: ['google.client_id', 'google.client_secret', 'gmail.etiqueta', 'gmail.procesado', 'gmail.intervalo'],
};
const COMUNES_CORREO = ['correo.permitidos', 'correo.max_mb', 'correo.contestar'];
const PROV_DE = { 'mailgun.signing_key': 'mailgun', 'correo.token': 'postmark sendgrid' };
const COMO_CORREO = {
  imap: [
    ['Gmail', ['Mejor una cuenta solo para facturas (facturas@...). Activá la verificación en dos pasos.',
      'Creá una contraseña de aplicación en myaccount.google.com/apppasswords (nombre: Factorum) y copiala sin espacios.',
      'Servidor imap.gmail.com, puerto 993, TLS implícito. Usuario: el email entero. Contraseña: la de aplicación.',
      'Tocá Probar: Factorum entra y cuenta los mensajes de la carpeta.']],
    ['Outlook y Microsoft 365', ['Servidor outlook.office365.com, puerto 993, TLS implícito. Usuario: el email entero.',
      'Microsoft ya no deja entrar con usuario y contraseña en la mayoría de las cuentas (pide OAuth). Si Probar dice que no acepta la contraseña, poné una regla en Outlook que reenvíe las facturas a otra casilla, o usá Cloudflare Email Routing.']],
    ['Una casilla del dominio (cPanel, Hostinger, DonWeb...)', ['Creá una casilla para las facturas en el panel del hosting (facturas@tudominio.com.ar).',
      'Servidor: el que indica el hosting (suele ser mail.tudominio.com.ar), puerto 993 con TLS implícito o 143 con STARTTLS.',
      'Usuario: el email entero. Contraseña: la de la casilla.']],
    ['Qué hace Factorum', ['Mira los mails sin leer de la carpeta cada tanto (60 segundos por defecto).',
      'A los que procesó los marca como leídos o los mueve a la carpeta «Factorum» (la crea si no existe). Un mail que no se pudo guardar queda sin leer y se reintenta.']],
  ],
  cloudflare: [
    ['En Cloudflare', ['El dominio tiene que estar en Cloudflare. Entrá a Email, Email Routing y activalo: Cloudflare agrega los registros MX y SPF.',
      'Tocá Generar en «Secreto compartido» (más arriba), copialo y guardá.',
      'En Workers y Pages, creá un Worker (por ejemplo factorum-correo), editá el código, pegá el de más arriba y desplegalo.',
      'En el Worker, Configuración, Variables y secretos: agregá FACTORUM_URL (texto, con la dirección de más arriba) y FACTORUM_SECRETO (secreto, con el mismo secreto de acá).',
      'En Email Routing, Reglas de enrutamiento: creá la dirección facturas@tudominio con la acción «Enviar a un Worker» y elegí el Worker.',
      'Para el cliente por dirección (facturas+20222222223@...), activá el «subaddressing» en la configuración de Email Routing; si tu plan no lo tiene, usá la regla catch-all hacia el mismo Worker.',
      'Si Factorum contesta con error, el Worker rechaza el mail y quien lo mandó recibe el rebote con el motivo.']],
  ],
  proveedor: [
    ['Mailgun', ['Agregá un dominio o subdominio de recepción (por ejemplo mg.tudominio.com.ar) y apuntá sus MX a mxa.mailgun.org y mxb.mailgun.org.',
      'En Configuración, API Security, copiá la «HTTP webhook signing key» y pegala acá.',
      'En Receiving (Routes), creá una ruta: «Match Recipient» facturas@mg.tudominio.com.ar (o «Catch All»), acción Forward a la URL de más arriba, y guardá.',
      'Factorum controla la firma de cada pedido (y que no tenga más de 5 minutos).']],
    ['Postmark', ['Tocá Generar en «Token de la URL», guardá, y copiá la URL de más arriba (lleva el token).',
      'En el servidor de Postmark, Default Inbound Stream, Settings: pegala en «Webhook URL». Tildá «Include raw email content».',
      'Mandá las facturas a la dirección de entrada que te da Postmark, o configurá tu dominio (MX a inbound.postmarkapp.com) y reenviá a ella.']],
    ['SendGrid', ['Tocá Generar en «Token de la URL», guardá, y copiá la URL de más arriba.',
      'Apuntá el MX del subdominio de recepción a mx.sendgrid.net (prioridad 10).',
      'En Settings, Inbound Parse, Add Host & URL: elegí el subdominio, pegá la URL y tildá «POST the raw, full MIME message».']],
  ],
  gmail: [
    ['En la consola de Google Cloud', ['Entrá a console.cloud.google.com y creá un proyecto (por ejemplo «Factorum»).',
      'En APIs y servicios, Biblioteca: buscá Gmail API y habilitala.',
      'En la pantalla de consentimiento de OAuth (Google Auth Platform): tipo Externo (o Interno, si es Google Workspace), nombre «Factorum» y tu email. En Público, dejala «En prueba» y agregá tu cuenta como usuario de prueba.',
      'En Acceso a los datos, agregá el permiso https://www.googleapis.com/auth/gmail.modify (leer, etiquetar y marcar; no borra ni manda).',
      'En Clientes (Credenciales), creá un ID de cliente de OAuth de tipo «Aplicación web» y en «URI de redireccionamiento autorizados» pegá exacto el de más arriba.',
      'Copiá el ID de cliente y el secreto acá, guardá y tocá «Conectar con Google»: elegí la cuenta y aceptá el permiso.',
      'Ojo: en modo prueba, Google corta el permiso a los 7 días. Con Google Workspace y tipo Interno no vence; si no, cuando Factorum avise, tocá Conectar de nuevo.']],
    ['En Gmail', ['Creá la etiqueta «Facturas» (o la que pongas acá).', 'Opcional: un filtro que se la ponga sola (por ejemplo, a lo que llega a facturas@ o de tus proveedores).',
      'Factorum lee los mails con esa etiqueta cada 60 segundos y, al procesarlos, les saca la etiqueta o los marca como leídos.']],
  ],
};
const WORKER_CF = [
  '// Factorum: Email Worker de Cloudflare. Firma cada mail y se lo pasa a Factorum.',
  '// Variables del Worker: FACTORUM_URL (texto) y FACTORUM_SECRETO (secreto).',
  'export default {',
  '  async email(message, env, ctx) {',
  '    const crudo = new Uint8Array(await new Response(message.raw).arrayBuffer());',
  '    const t = Math.floor(Date.now() / 1000).toString();',
  '    const enc = new TextEncoder();',
  '    const prefijo = enc.encode(t + "." + message.to + ".");',
  '    const datos = new Uint8Array(prefijo.length + crudo.length);',
  '    datos.set(prefijo, 0);',
  '    datos.set(crudo, prefijo.length);',
  '    const clave = await crypto.subtle.importKey("raw", enc.encode(env.FACTORUM_SECRETO),',
  '      { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);',
  '    const firma = new Uint8Array(await crypto.subtle.sign("HMAC", clave, datos));',
  '    const hex = Array.from(firma, (b) => b.toString(16).padStart(2, "0")).join("");',
  '    let res;',
  '    try {',
  '      res = await fetch(env.FACTORUM_URL, {',
  '        method: "POST",',
  '        headers: {',
  '          "Content-Type": "message/rfc822",',
  '          "X-Factorum-Firma": "t=" + t + ",v1=" + hex,',
  '          "X-Factorum-Para": message.to,',
  '        },',
  '        body: crudo,',
  '      });',
  '    } catch (e) {',
  '      message.setReject("Factorum no responde: probá de nuevo más tarde.");',
  '      return;',
  '    }',
  '    if (!res.ok) {',
  '      let motivo = "Factorum no pudo recibir el mail (" + res.status + ")";',
  '      try { const j = await res.json(); if (j.error) motivo = j.error; } catch (e) {}',
  '      message.setReject(motivo);',
  '    }',
  '  },',
  '};',
].join('\n');

function comoHTML(cfg) {
  if (!cfg || (!cfg.como && !cfg.grupos)) return '';
  const ol = (l) => `<ol>${l.map((x) => `<li>${esc(x)}</li>`).join('')}</ol>`;
  // En el proveedor, cada grupo es de uno (Mailgun, Postmark, SendGrid): se ve el del elegido.
  const prov = (t) => (['Mailgun', 'Postmark', 'SendGrid'].includes(t) && cfg.porProveedor ? ` data-prov="${t.toLowerCase()}"` : '');
  return `<details class="como"><summary>Cómo se configura</summary>${cfg.como ? ol(cfg.como) : ''}${(cfg.grupos || []).map(([t, l]) => `<div${prov(t)}><h4>${esc(t)}</h4>${ol(l)}</div>`).join('')}</details>`;
}

const urlCorreo = (k) => ((CONF && CONF.correo && CONF.correo.urls) || {})[k] || '';
const sinURL = '<span class="muted">Completá la URL pública en General para ver la dirección.</span>';
function datoCopiable(et, valor, attrs = '') {
  return `<div class="dato-fijo"${attrs}><span class="et">${esc(et)}</span>${valor
    ? `<div class="fila-copia"><code class="mono">${esc(valor)}</code><button type="button" class="ghost mini" data-accion="copiar" data-texto="${esc(valor)}">${I.copiar}<span>Copiar</span></button></div>` : sinURL}</div>`;
}

// La URL de Postmark y SendGrid lleva el token: solo se puede armar entera cuando lo acabás de
// generar o escribir (después de guardarlo, Factorum no lo vuelve a mostrar).
function urlConToken(base, token) {
  try { const u = new URL(base); u.username = 'factorum'; u.password = token; return u.toString(); } catch { return base; }
}
function actualizarUrlsCorreo() {
  const i = document.getElementById(idCampo('correo.token'));
  const tok = i ? i.value.trim() : '';
  $$('.url-token').forEach((el) => {
    const base = el.dataset.base;
    if (!base || !el.querySelector('code')) return; // sin URL pública todavía
    const conf = (CONF.campos.find((c) => c.clave === 'correo.token') || {}).configurado;
    const txt = tok ? urlConToken(base, tok) : base.replace('://', '://factorum:' + (conf ? '<el token que guardaste>' : '<token>') + '@');
    el.querySelector('code').textContent = txt;
    const b = el.querySelector('button');
    b.dataset.texto = txt;
    b.disabled = !tok;
    b.title = tok ? '' : 'Generá o escribí el token para copiar la URL entera';
  });
}

function extraForma(f) {
  if (f === 'cloudflare') {
    return datoCopiable('FACTORUM_URL (la dirección para el Worker)', urlCorreo('cloudflare')) + bloqueCodigo(WORKER_CF, 'El código del Email Worker (worker.js)');
  }
  if (f === 'proveedor') {
    return datoCopiable('URL para Mailgun (acción Forward de la ruta)', urlCorreo('mailgun'), ' data-prov="mailgun"')
      + ['postmark', 'sendgrid'].map((p) => `<div class="dato-fijo url-token" data-prov="${p}" data-base="${esc(urlCorreo(p))}"><span class="et">URL para ${p === 'postmark' ? 'Postmark (Webhook URL)' : 'SendGrid (Inbound Parse)'}</span>${urlCorreo(p)
        ? `<div class="fila-copia"><code class="mono"></code><button type="button" class="ghost mini" data-accion="copiar" data-texto="">${I.copiar}<span>Copiar</span></button></div>` : sinURL}</div>`).join('');
  }
  if (f === 'gmail') {
    const g = (CONF.correo && CONF.correo.google) || {};
    const conectado = g.conectado
      ? `<span class="est ok">Conectado${g.cuenta ? ' como ' + esc(g.cuenta) : ''}</span>${g.fijado ? '' : '<button type="button" class="ghost mini" data-accion="desconectarGoogle">Desconectar</button>'}`
      : '<span class="est">Sin conectar</span>';
    return datoCopiable('URI de redireccionamiento autorizado (para Google Cloud)', urlCorreo('google_redirect'))
      + `<div class="fila google-fila">${conectado}<span class="sp"></span>${g.fijado ? '' : `<button type="button" data-accion="conectarGoogle">${g.conectado ? 'Conectar otra vez' : 'Conectar con Google'}</button>`}</div>`;
  }
  return '';
}

function estadoCorreoHTML() {
  const e = (CONF.correo && CONF.correo.estado) || {};
  if (!e.forma || e.forma === 'apagado') return '';
  const partes = [];
  if (e.ultima) partes.push('última revisión ' + fechaHora(e.ultima));
  if (e.recibidos || e.retenidos) partes.push(`${e.recibidos || 0} procesado(s) y ${e.retenidos || 0} retenido(s) desde que arrancó`);
  if (e.ultimo_mail) partes.push('último mail ' + fechaHora(e.ultimo_mail));
  return `<div class="estado-correo${e.error ? ' mal' : ''}">${e.error ? I.alerta : I.correo}<span>${esc(e.error || (e.activo ? 'Activo' : e.detalle || 'Sin configurar'))}${partes.length ? ' · ' + esc(partes.join(' · ')) : ''}</span></div>`;
}

function cuerpoCorreoEntrada(campos) {
  const pc = Object.fromEntries(campos.map((c) => [c.clave, c]));
  const cf = pc['correo.forma'] || {};
  const forma = cf.valor || 'imap';
  const bloq = !!cf.bloqueado;
  const radios = FORMAS.map(([v, t, d]) => `<label class="forma${v === forma ? ' on' : ''}"><input type="radio" name="correoForma" value="${v}"${v === forma ? ' checked' : ''}${bloq ? ' disabled' : ''}>
    <span class="forma-t">${v === 'apagado' ? '' : I.correo}${esc(t)}</span><span class="forma-d">${esc(d)}</span></label>`).join('');
  const grupo = (f) => {
    const cs = (GRUPOS_CORREO[f] || []).map((k) => pc[k]).filter(Boolean).map((c) => {
      const h = campoConfHTML(c);
      return PROV_DE[c.clave] ? `<div class="prov-campo" data-prov="${PROV_DE[c.clave]}">${h}</div>` : h;
    }).join('');
    const cuerpo = f === 'apagado' ? '<p class="muted">No entra nada por mail. Lo que ya se cargó sigue igual.</p>'
      : `<div class="conf-campos">${cs}</div>${extraForma(f)}${comoHTML({ grupos: COMO_CORREO[f], porProveedor: f === 'proveedor' })}`;
    return `<div class="grupo-forma${f === forma ? '' : ' hidden'}" data-forma="${f}">${cuerpo}</div>`;
  };
  return `<input type="hidden" id="${idCampo('correo.forma')}" data-clave="correo.forma" data-orig="${esc(forma)}" value="${esc(forma)}"${bloq ? ' disabled' : ''}>
    <div class="formas" role="radiogroup" aria-label="Cómo entra el correo">${radios}</div>
    ${bloq ? `<span class="fijado">${I.llave}Fijado por el servidor (variable ${esc(cf.env)})</span>` : ''}
    ${estadoCorreoHTML()}
    ${FORMAS.map(([f]) => grupo(f)).join('')}
    <h4 class="sub-conf">Para cualquier forma</h4>
    <div class="conf-campos">${COMUNES_CORREO.map((k) => pc[k]).filter(Boolean).map(campoConfHTML).join('')}</div>
    <p class="muted nota-conf">Para que un mail se cargue a un cliente, que lo manden a facturas+CUIT@tudominio (con el CUIT sin guiones) o asociá el remitente en Clientes y canales. Lo de remitentes fuera de la lista queda en <a class="link" href="#retenido" data-accion="ir" data-vista="retenido">Correo retenido</a>.</p>`;
}

function cambiarForma(f) {
  const h = document.getElementById(idCampo('correo.forma'));
  if (h) h.value = f;
  $$('#conf-correo_entrada .forma').forEach((l) => l.classList.toggle('on', l.querySelector('input').value === f));
  $$('#conf-correo_entrada .grupo-forma').forEach((g) => g.classList.toggle('hidden', g.dataset.forma !== f));
  const est = $('#conf-correo_entrada .estado-correo'); // el estado es de la forma guardada
  if (est) est.classList.toggle('hidden', f !== (h ? h.dataset.orig : f));
  $('#conf-correo_entrada .prueba').classList.add('hidden');
}

function mostrarProveedor() {
  const sel = document.getElementById(idCampo('correo.proveedor'));
  const p = sel ? sel.value : 'mailgun';
  $$('#conf-correo_entrada [data-prov]').forEach((el) => el.classList.toggle('hidden', !el.dataset.prov.split(' ').includes(p)));
}

function extraSeccion(sec) {
  if (sec === 'webhook') return datoCopiable('URL de entrada', CONF.webhook_entrada);
  if (sec === 'lockatus') {
    const lk = CONF.lockatus || {};
    let h = datoCopiable('Redirect URI (para registrar en Lockatus)', lk.redirect);
    if (lk.login_local) h += `<div class="aviso-mini mal">${I.alerta}<span><strong>Salida de emergencia activa:</strong> FACTORUM_LOGIN_LOCAL=1 deja entrar con email y contraseña aunque esté «Solo se entra con Lockatus». Sacala del servidor cuando Lockatus vuelva a andar.</span></div>`;
    else if (!lk.sesion_lockatus) h += `<div class="aviso-mini">${I.info}<span>Para activar «Solo se entra con Lockatus», primero cerrá sesión y entrá con Lockatus: así queda probado que tu cuenta entra.</span></div>`;
    return h;
  }
  if (sec === 'whatsapp') return '<div id="waEstado" class="wa-estado"></div>';
  if (sec === 'ia') return estadoAvisoIA();
  if (sec === 'r2') return '<div id="r2Pase" class="pase" aria-live="polite"></div>';
  if (sec === 'copias') return '<div id="copiasLista" class="copias"></div>';
  if (sec === 'correo_salida') return `<label class="campo-conf destino-prueba"><span class="et">Mandar la prueba a</span><input id="smtpDestino" type="email" placeholder="tu@email.com" autocomplete="email"><span class="ayuda">Probar manda un mail de prueba a esta dirección. Vacío: solo controla que el servidor acepte el usuario.</span></label>`;
  if (sec === 'avisos' && !(CONF.correo && CONF.correo.smtp)) return `<div class="aviso-mini">${I.info}<span>Los avisos salen por el correo saliente, que todavía no está configurado.</span><button type="button" class="ghost mini" data-accion="saltar" data-sec="correo_salida">Configurarlo</button></div>`;
  return '';
}

async function conectarGoogle(btn) {
  const cambios = cambiosDe('correo_entrada', false);
  btn.disabled = true;
  try {
    if (Object.keys(cambios).length) CONF = await api('/api/configuracion', { method: 'PUT', json: { cambios } });
    const { url } = await api('/api/correo/google/conectar', { method: 'POST' });
    location.href = url;
  } catch (e) { toast(e, true); btn.disabled = false; }
}

async function desconectarGoogle() {
  if (!confirm('¿Desconectar Gmail? Factorum deja de leer la etiqueta y le saca el permiso en tu cuenta de Google.')) return;
  try {
    const r = await api('/api/correo/google/desconectar', { method: 'POST' });
    toast(r.aviso || 'Gmail desconectado', !!r.aviso);
    cargarConfiguracion();
  } catch (e) { toast(e, true); }
}
let CONF = null; // respuesta de /api/configuracion

async function cargarConfiguracion() {
  try {
    CONF = await api('/api/configuracion');
    pintarConfiguracion();
  } catch (e) { $('#configuracion').innerHTML = vacio('configuracion', 'No se pudo cargar la configuración', e.message); }
}

const idCampo = (k) => 'cf-' + k.replace(/\./g, '-');

const GENERABLES = ['webhook.token', 'cloudflare.secreto', 'correo.token'];

function valorCampoTexto(c) {
  if (c.tipo === 'lista' || c.tipo === 'emails') return (c.valor || []).join('\n');
  if (c.tipo === 'entero') return c.valor ? String(c.valor) : '';
  if (c.tipo === 'clientes') return Object.entries(c.valor || {}).map(([k, v]) => `${k} = ${cuit(v)}`).join('\n');
  if (c.tipo === 'cuit') return c.valor ? cuit(c.valor) : '';
  return c.valor ?? '';
}

function campoConfHTML(c) {
  const id = idCampo(c.clave);
  const bloq = c.bloqueado ? ' disabled' : '';
  const nota = c.bloqueado ? `<span class="fijado">${I.llave}Fijado por el servidor (variable ${esc(c.env)})</span>`
    : c.origen === 'pantalla' && c.actualizado_por ? `<span class="muted">Cambiado por ${esc(c.actualizado_por)}, ${fechaHora(c.actualizado)}</span>` : '';
  let ayuda = c.ayuda ? `<span class="ayuda">${esc(c.ayuda)}</span>` : '';
  if (c.tipo === 'secreto' && c.configurado) ayuda = `<span class="ayuda"><strong>Configurado${c.ultimos ? ', termina en ' + esc(c.ultimos) : ''}.</strong> Escribí para reemplazarlo. ${esc(c.ayuda || '')}</span>`;
  if (c.tipo === 'bool') {
    return `<div class="campo-conf"><label class="chk"><input type="checkbox" id="${id}" data-clave="${c.clave}" data-orig="${c.valor ? '1' : ''}"${c.valor ? ' checked' : ''}${bloq}> ${esc(c.etiqueta)}</label>${ayuda}${nota}</div>`;
  }
  let control;
  if (c.tipo === 'secreto') {
    const ph = c.configurado ? 'Configurado' : 'Sin configurar';
    control = `<div class="fila-secreto">${campoClave(id, `data-clave="${c.clave}" data-orig="" autocomplete="off" spellcheck="false" placeholder="${esc(ph)}"${bloq}`)}
      ${GENERABLES.includes(c.clave) && !c.bloqueado ? `<button type="button" class="ghost mini" data-accion="generarToken" data-para="${id}">Generar</button>` : ''}
      ${c.configurado && !c.bloqueado && c.origen === 'pantalla' ? `<button type="button" class="ghost mini" data-accion="borrarSecreto" data-clave="${c.clave}">Borrar</button>` : ''}</div>`;
  } else if (c.tipo === 'lista' || c.tipo === 'clientes' || c.tipo === 'emails') {
    const v = valorCampoTexto(c);
    control = `<textarea id="${id}" data-clave="${c.clave}" data-orig="${esc(v)}" rows="${c.tipo === 'clientes' ? 4 : 3}" placeholder="${c.ejemplo ? 'Por ejemplo: ' + esc(c.ejemplo) : ''}" spellcheck="false"${bloq}>${esc(v)}</textarea>`;
  } else if (c.tipo === 'opcion') {
    const ops = c.opciones || [];
    const v = ops.some((o) => o.valor === c.valor) ? c.valor : (ops[0] || {}).valor || '';
    control = `<select id="${id}" data-clave="${c.clave}" data-orig="${esc(v)}"${bloq}>${ops.map((o) => `<option value="${esc(o.valor)}"${o.valor === v ? ' selected' : ''}>${esc(o.etiqueta)}</option>`).join('')}</select>`;
  } else if (c.tipo === 'entero') {
    const v = String(c.valor ?? '');
    control = `<input id="${id}" type="number" inputmode="numeric" min="${c.min || 0}" ${c.max ? `max="${c.max}"` : ''} step="1" data-clave="${c.clave}" data-orig="${esc(v)}" value="${esc(v)}"${bloq}>`;
  } else if (c.tipo === 'hora') {
    const v = String(c.valor || '');
    control = `<input id="${id}" type="time" step="60" data-clave="${c.clave}" data-orig="${esc(v)}" value="${esc(v)}"${bloq}>`;
  } else {
    const v = valorCampoTexto(c);
    control = `<input id="${id}" data-clave="${c.clave}" data-orig="${esc(v)}" value="${esc(v)}" placeholder="${c.ejemplo ? 'Por ejemplo: ' + esc(c.ejemplo) : ''}" ${c.tipo === 'url' ? 'inputmode="url"' : ''} spellcheck="false"${bloq}>`;
  }
  return `<label class="campo-conf${c.tipo === 'lista' || c.tipo === 'clientes' || c.clave === 'correo.permitidos' ? ' ancho' : ''}"><span class="et">${esc(c.etiqueta)}</span>${control}${ayuda}${nota}</label>`;
}

function estadoSeccion(sec, campos) {
  const cfg = SECCIONES[sec];
  if (sec === 'lockatus' && CONF.lockatus && CONF.lockatus.solo) return '<span class="est ok">Solo Lockatus</span>';
  if (sec === 'ia' && CONF.ia_aviso && CONF.ia_aviso.configurada && !CONF.ia_aviso.en_uso) return '<span class="est error">Sin usar: falta aceptar el aviso</span>';
  if (sec === 'correo_entrada') {
    const e = (CONF.correo && CONF.correo.estado) || {};
    if (e.forma === 'apagado') return '<span class="est">Apagado</span>';
    if (e.error) return '<span class="est revisar">Con problemas</span>';
    return e.activo ? '<span class="est ok">Activo</span>' : '<span class="est">Sin configurar</span>';
  }
  if (sec === 'avisos') {
    const on = campos.filter((c) => c.tipo === 'bool' && c.valor).length;
    if (!(CONF.correo && CONF.correo.smtp)) return '<span class="est">Falta el correo saliente</span>';
    return on ? `<span class="est ok">${on} prendido(s)</span>` : '<span class="est">Apagados</span>';
  }
  const porClave = Object.fromEntries(campos.map((c) => [c.clave, c]));
  const todos = campos.length && campos.every((c) => c.bloqueado);
  const ok = cfg.clave.every((k) => porClave[k] && porClave[k].configurado && (porClave[k].tipo !== 'bool' || porClave[k].valor));
  const canal = (CONF.canales || []).find((x) => x.canal === sec);
  if (canal && canal.habilitado) return canal.conectado ? '<span class="est ok">Conectado</span>' : `<span class="est revisar">${esc(canal.detalle || 'Desconectado')}</span>`;
  if (todos) return '<span class="est">Fijado por el servidor</span>';
  return ok ? '<span class="est ok">Configurado</span>' : '<span class="est">Sin configurar</span>';
}

function pintarConfiguracion() {
  const secs = CONF.secciones || [];
  $('#confSub').textContent = puede('configuracion')
    ? 'Todo lo que antes iba en variables de entorno. Lo que fija el servidor gana y aparece bloqueado. Al guardar, se aplica sin reiniciar.'
    : 'Como admin podés cambiar qué cliente es cada remitente de chat. El resto de la configuración lo maneja un superadmin.';
  $('#confSaltos').innerHTML = secs.length > 1 ? secs.map((s) => `<a class="salto" href="#conf-${s}" data-accion="saltar" data-sec="${s}">${esc(SECCIONES[s].t)}</a>`).join('') : '';
  $('#configuracion').innerHTML = secs.map((sec) => {
    const cfg = SECCIONES[sec] || { t: sec, d: '', clave: [] };
    const campos = CONF.campos.filter((c) => c.seccion === sec);
    const editables = campos.some((c) => !c.bloqueado);
    const cuerpo = sec === 'correo_entrada' ? cuerpoCorreoEntrada(campos)
      : sec === 'ia' && CONF.ia ? `${cuerpoIA()}${extraSeccion(sec)}${comoHTML(cfg)}`
        : `<div class="conf-campos">${campos.map(campoConfHTML).join('')}</div>${extraSeccion(sec)}${comoHTML(cfg)}`;
    return `<section class="card conf" id="conf-${sec}" data-sec="${sec}">
      <div class="card-cab"><div><h3>${esc(cfg.t)}</h3><p class="muted conf-d">${esc(cfg.d)}</p></div>${estadoSeccion(sec, campos)}</div>
      ${cuerpo}
      <div class="prueba hidden"></div>
      <div class="fila pie-conf">
        ${cfg.probar ? `<button type="button" class="ghost" data-accion="probarConf" data-sec="${sec}">Probar</button>` : ''}
        <span class="sp"></span>
        ${editables ? `<button type="button" data-accion="guardarConf" data-sec="${sec}">Guardar</button>` : ''}
      </div></section>`;
  }).join('');
  refrescarWhatsApp();
  if ($('#conf-correo_entrada')) { mostrarProveedor(); actualizarUrlsCorreo(); }
  iaIniciar();
  if (IR_SECCION) { const sec = IR_SECCION; IR_SECCION = ''; setTimeout(() => document.getElementById('conf-' + sec)?.scrollIntoView({ block: 'start' }), 30); }
  refrescarPase();
  cargarCopias();
}
let IR_SECCION = '';

// cambiosDe junta lo que cambió en una sección (o todo lo escrito, para Probar).
function cambiosDe(sec, todo) {
  const out = {};
  $$(`#conf-${sec} [data-clave]`).forEach((el) => {
    if (el.disabled) return;
    const c = CONF.campos.find((x) => x.clave === el.dataset.clave);
    if (!c) return;
    if (el.type === 'checkbox') {
      if (todo || (el.checked ? '1' : '') !== el.dataset.orig) out[c.clave] = el.checked;
      return;
    }
    const v = el.value;
    if (c.tipo === 'secreto') { if (v) out[c.clave] = v; return; }
    if (!todo && v === el.dataset.orig) return;
    out[c.clave] = v;
  });
  return out;
}

async function guardarConf(sec, cambios, btn) {
  cambios = cambios || (sec === 'ia' && IAUI && $('#conf-ia') ? cambiosIA(false) : cambiosDe(sec, false));
  if (!Object.keys(cambios).length) { toast('No hay cambios'); return; }
  if (btn) btn.disabled = true;
  try {
    try {
      CONF = await api('/api/configuracion', { method: 'PUT', json: { cambios } });
    } catch (e) {
      // Una IA externa nueva (o de otro host): antes de guardarla, el aviso de qué pasa con los datos.
      if (!(e.datos && e.datos.codigo === 'aviso_ia')) throw e;
      const a = e.datos.aviso_ia;
      // Si lo acaba de aceptar para probarla (el mismo host y el mismo aviso), no se lo pide de nuevo.
      const ap = IAUI && IAUI.aceptadoPrueba;
      const yaVisto = ap && ap.host === a.host && ap.version === a.version;
      if (!yaVisto && !(await abrirAvisoIA(a, 'admin'))) { toast('No se guardó. La IA externa no se usa hasta que aceptes el aviso.'); return; }
      CONF = await api('/api/configuracion', { method: 'PUT', json: { cambios, aceptar_aviso_ia: { host: a.host, version: a.version } } });
    }
    pintarConfiguracion();
    cargarSalud(); cargarSuite();
    api('/api/sesion').then((r) => { YO = r; }).catch(() => {}); // por si cambió el correo saliente (invitar por mail)
    toast((CONF.cambiadas || []).length ? 'Guardado: ya se está usando, sin reiniciar.' : 'No había cambios');
    document.getElementById('conf-' + sec)?.scrollIntoView({ block: 'nearest' });
  } catch (e) { toast(e, true); } finally { if (btn) btn.disabled = false; }
}

async function probarConf(sec, btn) {
  if (sec === 'ia' && IAUI && $('#conf-ia')) { probarIA(btn); return; }
  const caja = $(`#conf-${sec} .prueba`);
  btn.disabled = true; btn.textContent = 'Probando...';
  caja.className = 'prueba'; caja.textContent = 'Probando...';
  try {
    const destino = sec === 'correo_salida' && $('#smtpDestino') ? $('#smtpDestino').value.trim() : '';
    const r = await api('/api/configuracion/probar/' + sec, { method: 'POST', json: { cambios: cambiosDe(sec, true), destino } });
    caja.className = 'prueba ' + (r.exito ? 'bien' : 'mal');
    caja.innerHTML = `${r.exito ? I.check : I.alerta}<span>${esc(r.mensaje)}</span>`;
  } catch (e) { caja.className = 'prueba mal'; caja.innerHTML = `${I.alerta}<span>${esc(e.message)}</span>`; }
  finally { btn.disabled = false; btn.textContent = 'Probar'; }
}

// WhatsApp: estado, QR y vinculación (lo que antes estaba en Canales).
async function refrescarWhatsApp() {
  const caja = $('#waEstado');
  if (!caja || !puede('configuracion')) return;
  try {
    const { canales } = await api('/api/canales');
    const c = canales.find((x) => x.canal === 'whatsapp') || {};
    if (document.activeElement && document.activeElement.id === 'telVincular') return; // no repintar mientras se escribe
    let html;
    if (!c.habilitado) html = '<p class="muted">Activalo y guardá para vincular un teléfono.</p>';
    else if (c.conectado) html = `<div class="fila"><span class="est ok">${esc(c.detalle || 'Vinculado')}</span><span class="sp"></span><button type="button" class="ghost mini" data-accion="desvincularWa">Desvincular</button></div>`;
    else {
      html = `<div class="wa-vincular">${c.qr ? '<img class="qr" id="qrWa" alt="Código QR de vinculación">' : '<p class="muted">Esperando el código de vinculación...</p>'}
        <div><p>Escaneá el código desde WhatsApp: Dispositivos vinculados, Vincular un dispositivo.</p>
        <form class="fila" id="fVincular"><label>O vinculá con un código de 8 letras<input id="telVincular" placeholder="Teléfono con código de país, ej. 5491155550000" inputmode="numeric"></label><button class="ghost" type="submit">Pedir código</button></form></div></div>`;
    }
    caja.innerHTML = html;
    if ($('#qrWa')) {
      const b = await api('/api/canales/whatsapp/qr.png', { binario: true }).catch(() => null);
      if (b && $('#qrWa')) $('#qrWa').src = URL.createObjectURL(b);
    }
  } catch { /* se reintenta */ }
}

// ---------- Pasar lo del disco a R2 ----------
let PASE = null;
let timerPase = null;
const ESTADO_PASE = {
  corriendo: ['revisar', 'Pasando a R2'], pausada: ['', 'En pausa'], terminada: ['ok', 'Terminado'],
  liberando: ['revisar', 'Liberando el disco'], liberada: ['ok', 'Disco liberado'], error: ['error', 'Se frenó'],
};
const durTexto = (seg) => {
  if (seg < 60) return `${seg} s`;
  if (seg < 3600) return `${Math.round(seg / 60)} min`;
  return `${Math.floor(seg / 3600)} h ${Math.round((seg % 3600) / 60)} min`;
};

async function refrescarPase() {
  clearTimeout(timerPase);
  const caja = $('#r2Pase');
  if (!caja || !puede('configuracion')) return;
  try {
    ({ pase: PASE } = await api('/api/r2/pase'));
  } catch (e) { caja.innerHTML = `<p class="muted">${esc(e.message)}</p>`; return; }
  pintarPase();
  if (PASE.estado === 'corriendo' || PASE.estado === 'liberando' || PASE.trabajando) {
    timerPase = setTimeout(() => { if (vista === 'configuracion') refrescarPase(); }, 1500);
  }
}

function pintarPase() {
  const caja = $('#r2Pase');
  if (!caja || !PASE) return;
  const p = PASE;
  const [cls, etq] = ESTADO_PASE[p.estado] || ['', 'Sin empezar'];
  const lib = p.estado === 'liberando' || p.estado === 'liberada';
  const total = lib ? p.liberados.objetos + p.para_liberar.objetos : p.total.objetos;
  const hechos = lib ? p.liberados.objetos : p.hechos.objetos;
  const pct = total ? Math.floor((hechos / total) * 100) : (p.estado === 'terminada' ? 100 : 0);
  let detalle;
  if (!p.estado) detalle = p.r2_activo
    ? 'Lo que se guardó antes de activar R2 sigue en el disco del servidor (y se sigue leyendo de ahí). Pasalo a R2 para no depender del disco: se sube tal cual, ya cifrado, y se verifica cada objeto.'
    : 'Activá R2 y guardá. Después vas a poder pasar a R2 lo que quedó en el disco.';
  else if (p.estado === 'corriendo' && !p.total.objetos) detalle = 'Contando lo que hay en el disco...';
  else if (lib) detalle = `${mil(p.liberados.objetos)} de ${mil(total)} objetos borrados del disco (${tamanio(p.liberados.bytes)})`;
  else {
    detalle = `${mil(p.hechos.objetos)} de ${mil(p.total.objetos)} objetos · ${tamanio(p.hechos.bytes)} de ${tamanio(p.total.bytes)}`;
    if (p.errores.objetos) detalle += ` · <span class="txt-mal">${mil(p.errores.objetos)} con error</span>`;
    if (p.estimado_seg && p.estado === 'corriendo') detalle += ` · faltan unos ${durTexto(p.estimado_seg)}`;
  }
  const botones = [];
  if (p.r2_activo) {
    if (p.estado === 'corriendo') botones.push('<button type="button" class="ghost" data-accion="pase" data-que="pausar">Pausar</button>');
    else if (p.estado === 'pausada' || p.estado === 'error') botones.push('<button type="button" data-accion="pase" data-que="seguir">Seguir</button>');
    else if (!p.estado) botones.push('<button type="button" data-accion="pase" data-que="iniciar">Pasar lo del disco a R2</button>');
    else if (p.estado !== 'liberando') {
      if (p.errores.objetos || p.pendientes.objetos) botones.push('<button type="button" class="ghost" data-accion="pase" data-que="seguir">Reintentar lo que falta</button>');
      else botones.push('<button type="button" class="ghost" data-accion="pase" data-que="iniciar">Volver a revisar el disco</button>');
    }
    if (p.puede_liberar) botones.push(`<button type="button" class="peligro" data-accion="liberarDisco">Liberar el disco</button>`);
  }
  const errores = (p.ultimos_errores || []).length
    ? `<details class="como pase-errores"><summary>Los que fallaron (${mil(p.errores.objetos)})</summary><ul>${p.ultimos_errores.map((x) => `<li><code>${esc(x.clave)}</code> ${esc(x.error)}</li>`).join('')}</ul></details>` : '';
  const pie = p.estado ? `<span class="muted">${p.por ? `Lo empezó ${esc(p.por)}, ` : ''}${fechaHora(p.iniciada)}${p.terminada ? ` · terminó ${fechaHora(p.terminada)}` : ''}</span>` : '';
  caja.innerHTML = `<div class="pase-cab"><strong>Lo que quedó en el disco</strong>${p.estado ? `<span class="est ${cls}">${etq}</span>` : ''}</div>
    ${p.estado ? `<div class="prog pase-prog${p.estado === 'corriendo' || p.estado === 'liberando' ? ' vivo' : ''}" role="progressbar" aria-valuenow="${pct}" aria-valuemin="0" aria-valuemax="100"><div style="width:${pct}%"></div></div>` : ''}
    <p class="pase-det">${detalle}</p>
    ${p.mensaje ? `<p class="pase-msg${p.estado === 'error' ? ' txt-mal' : ''}">${esc(p.mensaje)}</p>` : ''}
    ${p.estado === 'liberada' && !p.r2_activo ? '<p class="pase-msg txt-mal">Ojo: R2 está apagado y hay objetos que están solo en R2. Volvé a activarlo para verlos.</p>' : ''}
    ${errores}
    <div class="fila pase-pie">${pie}<span class="sp"></span>${botones.join('')}</div>`;
}

async function accionPase(que, confirmacion, btn) {
  if (btn) btn.disabled = true;
  try {
    ({ pase: PASE } = await api('/api/r2/pase', { method: 'POST', json: { accion: que, confirmacion } }));
    pintarPase();
    toast({ iniciar: 'Empezó el pase a R2: podés seguir usando Factorum.', seguir: 'Sigue el pase a R2.', pausar: 'Pausado: lo verificado queda hecho.', liberar: 'Se está liberando el disco.' }[que]);
    setTimeout(refrescarPase, 600);
    return true;
  } catch (e) { toast(e, true); return false; } finally { if (btn) btn.disabled = false; }
}

function pedirLiberar() {
  const p = PASE;
  abrirModal('Liberar el disco', `<p>Se borran del disco del servidor <strong>${mil(p.para_liberar.objetos)} objetos (${tamanio(p.para_liberar.bytes)})</strong> que están verificados en R2. Antes de borrar cada uno, Factorum vuelve a mirar que siga en R2.</p>
    <p class="muted">Después, esos archivos se leen solo de R2: si apagás R2, no se van a poder ver. No se puede deshacer.</p>
    <label class="campo-conf"><span class="et">Para confirmar, escribí «liberar»</span><input id="liberarConf" autocomplete="off" spellcheck="false"></label>`,
  { acciones: '<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="button" class="peligro" data-accion="liberarConfirmar" disabled>Liberar el disco</button>' });
  setTimeout(() => $('#liberarConf')?.focus(), 50);
}

// ---------- Copias de seguridad ----------
let COPIAS = null;
let timerCopias = null;

async function cargarCopias() {
  clearTimeout(timerCopias);
  const caja = $('#copiasLista');
  if (!caja || !puede('configuracion')) return;
  try { COPIAS = await api('/api/copias'); } catch (e) { caja.innerHTML = `<p class="muted">${esc(e.message)}</p>`; return; }
  pintarCopias();
  if ((COPIAS.copias || []).some((c) => c.estado === 'haciendo') || cargarCopias.esperando) {
    timerCopias = setTimeout(() => { if (vista === 'configuracion') cargarCopias(); }, 1500);
  }
}

function pintarCopias() {
  const caja = $('#copiasLista');
  if (!caja || !COPIAS) return;
  const l = COPIAS.copias || [];
  if (cargarCopias.esperando && l.some((c) => c.estado !== 'haciendo' && new Date(c.creado) >= cargarCopias.esperando)) cargarCopias.esperando = null;
  const destino = COPIAS.destino === 'r2' ? 'a R2 (carpeta copias del bucket)' : 'al disco del servidor (FACTORUM_DATOS/copias)';
  const resumen = COPIAS.activas
    ? `Todos los días a las <strong>${esc(COPIAS.hora)}</strong> (Buenos Aires). Se guardan las últimas ${COPIAS.diarias} y la primera de cada uno de los últimos ${COPIAS.mensuales} meses. Van ${destino}.${COPIAS.proxima ? ` Próxima: ${fechaHora(COPIAS.proxima)}` : ''}`
    : `La copia diaria está apagada. Igual podés hacer una ahora: va a ${destino}.`;
  const filas = l.map((c) => {
    const tipo = c.programada ? 'Diaria' : c.por === 'antes de restaurar' ? 'Antes de restaurar' : c.manual ? 'A pedido'
      : /^estaba en el destino/.test(c.error || '') ? 'Encontrada en el destino' : 'Copia';
    const est = c.estado === 'ok' ? '<span class="est ok">Bien</span>' : c.estado === 'haciendo' ? '<span class="est revisar">Haciendo</span>' : `<span class="est error" title="${esc(c.error)}">Falló</span>`;
    const desc = c.estado === 'ok'
      ? (c.disponible ? `<a class="boton ghost mini" href="/api/copias/${c.id}/archivo" download>Descargar</a>` : '<span class="muted" title="Está en un R2 que ahora no está activo">No disponible</span>')
      : c.estado === 'error' ? `<span class="txt-mal copia-err">${esc(c.error)}</span>` : '';
    return `<tr><td class="nowrap">${fechaHora(c.creado)}</td><td>${tipo}${c.por && c.manual && c.por !== 'antes de restaurar' ? `<br><span class="muted">${esc(c.por)}</span>` : ''}</td>
      <td>${c.destino === 'r2' ? 'R2' : 'Disco'}</td><td class="num">${c.tamano != null ? tamanio(c.tamano) : ''}</td><td class="num">${c.filas != null ? mil(c.filas) : ''}</td><td>${est}</td><td>${desc}</td></tr>`;
  }).join('');
  const tabla = l.length
    ? `<div class="tabla-scroll"><table class="t"><thead><tr><th>Cuándo</th><th>Tipo</th><th>Dónde</th><th>Tamaño</th><th>Filas</th><th>Estado</th><th></th></tr></thead><tbody>${filas}</tbody></table></div>`
    : '<p class="muted">Todavía no hay copias.</p>';
  const ejemplo = (l.find((c) => c.estado === 'ok') || {}).clave || 'copias/factorum-20261127-033000.copia';
  const nombre = ejemplo.split('/').pop();
  caja.innerHTML = `<div class="pase-cab"><strong>Copias</strong><span class="sp"></span><button type="button" class="ghost mini" data-accion="hacerCopia"${cargarCopias.esperando ? ' disabled' : ''}>${cargarCopias.esperando ? 'Haciendo la copia...' : 'Hacer una copia ahora'}</button></div>
    <p class="muted copias-res">${resumen}</p>
    ${tabla}
    <details class="como"><summary>Cómo restaurar</summary>
      <p>Restaurar borra lo que hay en la base y carga la copia, así que no se hace desde acá: se hace por consola, en el servidor, con la misma FACTORUM_MASTER_KEY.</p>
      <ol><li>Abrí una consola en el contenedor de Factorum (en EasyPanel: el servicio, Consola; con Docker: docker compose exec factorum sh).</li>
        <li>Verificá la copia (no toca nada):</li></ol>
      ${bloqueCodigo(`factorum restaurar ${COPIAS.destino === 'r2' ? ejemplo : '/data/copias/' + nombre}`, 'Verificar')}
      <ol start="3"><li>Restaurala. Antes hace una copia de lo que hay, por las dudas, y todo va en una transacción: si algo falla, la base queda como estaba.</li></ol>
      ${bloqueCodigo(`factorum restaurar ${COPIAS.destino === 'r2' ? ejemplo : '/data/copias/' + nombre} --confirmar`, 'Restaurar')}
      <ol start="4"><li>Reiniciá Factorum para que tome la configuración restaurada.</li></ol>
      <p class="muted">Con una copia descargada: subila al servidor (por ejemplo a /data) y pasá la ruta del archivo. En una base nueva y vacía funciona igual.</p>
    </details>`;
  tarjetas('#copiasLista');
}

async function hacerCopia(btn) {
  btn.disabled = true;
  try {
    const r = await api('/api/copias', { method: 'POST' });
    cargarCopias.esperando = new Date(Date.now() - 2000);
    toast(r.mensaje);
    cargarCopias();
  } catch (e) { toast(e, true); btn.disabled = false; }
}

// ---------- Usuarios ----------
let USUARIOS = [];
async function cargarUsuarios() {
  try {
    ({ usuarios: USUARIOS } = await api('/api/usuarios'));
    const yo = YO.identidad;
    const sub = $('#usuariosSub');
    if (!sub.dataset.orig) sub.dataset.orig = sub.textContent;
    sub.textContent = YO.solo_lockatus
      ? 'Quién entra a Factorum, con qué rol y a qué clientes. Solo se entra con Lockatus: cada persona entra con su email de Lockatus y acá no tiene contraseña. Las que entran por primera vez como usuario o lectura no ven nada hasta que les asignes clientes.'
      : sub.dataset.orig + (YO.lockatus ? ' Quien entra con Lockatus no tiene contraseña acá: se reconoce por su email.' : '');
    const ORIGEN_U = { local: 'Factorum', arcanum: 'Arcanum', lockatus: 'Lockatus' };
    $('#usuarios').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Persona</th><th>Rol</th><th>Clientes</th><th>Origen</th><th>Último ingreso</th><th>Estado</th><th></th></tr></thead><tbody>
      ${USUARIOS.map((u) => {
        const tocable = ORDEN_ROL.indexOf(u.rol) <= ORDEN_ROL.indexOf(yo.rol);
        const conClave = u.origen !== 'lockatus' && !YO.solo_lockatus; // quien entra con Lockatus no tiene contraseña acá
        return `<tr class="${u.activo ? '' : 'apagado'}"><td><div class="persona"><span class="avatar chico" aria-hidden="true">${esc(iniciales(u.nombre || u.email))}</span><span><strong>${esc(u.nombre)}</strong>${u.id === yo.usuario_id ? ' <span class="muted">(vos)</span>' : ''}<br><span class="muted">${esc(u.email)}</span></span></div></td>
          <td>${rolChip(u.rol)}</td><td>${cuitsHTML(u.cuits)}</td><td>${u.origen === 'lockatus' ? `<span class="origen-u">${I.llave}Lockatus</span>` : esc(ORIGEN_U[u.origen] || u.origen)}</td>
          <td>${u.ultimo_ingreso ? fechaHora(u.ultimo_ingreso) : '<span class="muted">Nunca</span>'}${u.debe_cambiar ? '<br><span class="est revisar">Contraseña temporal</span>' : ''}${!u.tiene_clave && u.origen === 'local' && conClave ? '<br><span class="est revisar">Invitación pendiente</span>' : ''}</td>
          <td>${u.activo ? '<span class="est ok">Activa</span>' : '<span class="est">Desactivada</span>'}</td>
          <td class="acciones-fila">${tocable ? `<div class="acc-apiladas"><button type="button" class="ghost mini" data-accion="editarUsuario" data-id="${u.id}">Editar</button>
            ${YO.correo_saliente && u.activo && conClave ? `<button type="button" class="ghost mini" data-accion="linkUsuario" data-id="${u.id}" title="${u.tiene_clave ? 'Mandarle por mail un link para elegir una contraseña nueva' : 'Mandarle la invitación otra vez'}">${u.tiene_clave ? 'Link por mail' : 'Reenviar invitación'}</button>` : ''}
            ${conClave ? `<button type="button" class="ghost mini" data-accion="restablecer" data-id="${u.id}" title="Restablecer la contraseña">Nueva contraseña</button>` : ''}
            ${u.id !== yo.usuario_id ? `<button type="button" class="ghost mini" data-accion="activarUsuario" data-id="${u.id}" data-activo="${u.activo ? '' : '1'}">${u.activo ? 'Desactivar' : 'Reactivar'}</button>` : ''}</div>` : '<span class="muted">Solo un superadmin</span>'}</td></tr>`;
      }).join('')}</tbody></table></div>`;
    tarjetas('#usuarios');
  } catch (e) { toast(e, true); }
}

// alcanceDe: todos (null), ninguno todavía ([]) o algunos.
const alcanceDe = (l) => (l == null ? 'todos' : l.length ? 'estos' : 'ninguno');

function formUsuario(u) {
  const yo = YO.identidad;
  const restr = yo.cuits && yo.cuits.length;
  const alc = u ? alcanceDe(u.cuits) : (restr ? 'estos' : 'todos');
  const opsAlc = [['todos', 'Todos los clientes'], ['ninguno', 'Ninguno todavía (no ve nada)'], ['estos', 'Solo estos CUITs']]
    .filter(([v]) => !restr || v !== 'todos').map(([v, t]) => `<option value="${v}"${v === alc ? ' selected' : ''}>${t}</option>`).join('');
  const lk = !u && YO.lockatus;
  abrirModal(u ? 'Editar a ' + (u.nombre || u.email) : 'Nueva persona', `<form id="fUsuario" class="form-modal" data-id="${u ? u.id : ''}">
    ${u ? `<p class="muted">${esc(u.email)}</p>` : '<label>Email<input id="usEmail" type="email" required autocomplete="off"></label>'}
    <label>Nombre<input id="usNombre" value="${esc(u ? u.nombre : '')}" autocomplete="off"></label>
    <label>Rol<select id="usRol">${rolesHasta(yo.rol, u ? u.rol : 'usuario')}</select>
      <span class="ayuda">Lectura solo mira. Usuario carga, revisa, corrige y aprueba. Admin además maneja personas, claves y clientes. Superadmin, todo.</span></label>
    <label>Qué clientes ve<select id="usAlcance">${opsAlc}</select></label>
    <label id="usCuitsWrap" class="${alc === 'estos' ? '' : 'hidden'}">CUITs de los clientes<textarea id="usCuits" rows="2" placeholder="${restr ? 'Uno o más de tus clientes' : 'Por ejemplo: 20-22222222-3, 30-71111111-1'}">${esc(cuitsTexto(u ? u.cuits : (restr ? yo.cuits : [])))}</textarea>
      <span class="ayuda">Separados por coma. Solo ve, carga y exporta lo de esos clientes.</span></label>
    ${lk ? `<label class="chk"><input type="checkbox" id="usLockatus"${YO.solo_lockatus ? ' checked disabled' : ''}> Entra con Lockatus (sin contraseña de Factorum)</label>
      <span class="ayuda">${YO.solo_lockatus ? 'Solo se entra con Lockatus: ' : ''}la persona entra con «Entrar con Lockatus» usando este email, con el rol y los clientes que elijas acá.</span>` : ''}
    <div id="usClaveWrap" class="${YO.solo_lockatus ? 'hidden' : ''}">${u ? '' : YO.correo_saliente ? `<label class="chk"><input type="checkbox" id="usInvitar" checked> Mandarle una invitación por mail</label>
      <span class="ayuda">Le llega un link para elegir su contraseña (vence en 72 horas). Sin tildar, Factorum te muestra una contraseña temporal para pasársela vos.</span>`
      : '<span class="ayuda">Para invitar por mail (en lugar de la contraseña temporal), configurá el correo saliente en Configuración.</span>'}</div>
    <div class="login-err hidden" id="usErr"></div></form>`,
  { acciones: `<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="submit" form="fUsuario">${u ? 'Guardar' : 'Crear'}</button>` });
}

async function guardarUsuario(f) {
  const id = f.dataset.id;
  const alc = $('#usAlcance').value;
  const cuerpo = { nombre: $('#usNombre').value.trim(), rol: $('#usRol').value, cuits: alc === 'estos' ? $('#usCuits').value : null };
  if (alc === 'ninguno') cuerpo.sin_clientes = true;
  const er = $('#usErr');
  if (alc === 'estos' && !soloDig($('#usCuits').value)) { er.textContent = 'Escribí al menos un CUIT, o elegí «Todos» o «Ninguno todavía».'; er.classList.remove('hidden'); return; }
  try {
    if (id) {
      await api('/api/usuarios/' + id, { method: 'PATCH', json: cuerpo });
      cerrarModal(); toast('Guardado'); cargarUsuarios();
    } else {
      cuerpo.email = $('#usEmail').value.trim();
      cuerpo.lockatus = !!($('#usLockatus') && $('#usLockatus').checked);
      cuerpo.invitar = !cuerpo.lockatus && !!($('#usInvitar') && $('#usInvitar').checked);
      const r = await api('/api/usuarios', { method: 'POST', json: cuerpo });
      cargarUsuarios();
      if (r.lockatus) { cerrarModal(); toast(`Listo: ${r.usuario.email} entra con Lockatus.`); return; }
      if (cuerpo.invitar) {
        if (r.invitado) { cerrarModal(); toast(`Le mandamos la invitación a ${r.usuario.email}.`); }
        else abrirModal('Persona creada', `<p>${esc(r.aviso || 'No se pudo mandar la invitación.')}</p>`);
        return;
      }
      secretoUnaVez('Persona creada', `Pasale a <strong>${esc(r.usuario.email)}</strong> esta contraseña temporal. Al entrar, Factorum le pide que elija una propia.`,
        r.clave_temporal, 'Se muestra una sola vez: si la perdés, restablecela.');
    }
  } catch (e) { er.textContent = e.message; er.classList.remove('hidden'); }
}

async function restablecer(id) {
  const u = USUARIOS.find((x) => String(x.id) === String(id));
  if (!u || !confirm(`¿Restablecer la contraseña de ${u.nombre || u.email}? Se cierran sus sesiones y va a tener que entrar con una temporal.`)) return;
  try {
    const r = await api(`/api/usuarios/${id}/restablecer`, { method: 'POST' });
    cargarUsuarios();
    secretoUnaVez('Contraseña restablecida', `Pasale a <strong>${esc(u.email)}</strong> esta contraseña temporal.`, r.clave_temporal, 'Se muestra una sola vez.');
  } catch (e) { toast(e, true); }
}

async function linkUsuario(id, btn) {
  const u = USUARIOS.find((x) => String(x.id) === String(id));
  if (!u) return;
  btn.disabled = true;
  try {
    const r = await api(`/api/usuarios/${id}/link`, { method: 'POST' });
    toast(r.tipo === 'invitacion' ? `Le mandamos la invitación otra vez a ${u.email}.` : `Le mandamos a ${u.email} un link para elegir una contraseña nueva (vence en 30 minutos).`);
  } catch (e) { toast(e, true); } finally { btn.disabled = false; }
}

async function activarUsuario(id, activo) {
  const u = USUARIOS.find((x) => String(x.id) === String(id));
  if (!activo && !confirm(`¿Desactivar a ${u ? u.nombre || u.email : 'esta persona'}? No va a poder entrar y sus claves de API dejan de andar.`)) return;
  try { await api('/api/usuarios/' + id, { method: 'PATCH', json: { activo } }); toast(activo ? 'Reactivada' : 'Desactivada'); cargarUsuarios(); }
  catch (e) { toast(e, true); }
}

// ---------- Correo retenido ----------
let RETENIDOS = [];
async function cargarRetenidos() {
  try {
    const r = await api('/api/correo/retenido?limite=200');
    RETENIDOS = r.retenidos;
    PENDIENTES.retenido = r.total; pintarNav();
    if (!RETENIDOS.length) {
      $('#retenidos').innerHTML = vacio('retenido', 'No hay correo retenido', 'Lo que manden por mail remitentes que no están entre los permitidos aparece acá para que decidas qué hacer.');
      return;
    }
    const FORMA = { imap: 'IMAP', cloudflare: 'Cloudflare', mailgun: 'Mailgun', postmark: 'Postmark', sendgrid: 'SendGrid', gmail: 'Gmail' };
    $('#retenidos').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>De</th><th>Asunto</th><th>Llegó</th><th>Adjuntos</th><th>Cliente</th><th>Por qué quedó</th><th></th></tr></thead><tbody>
      ${RETENIDOS.map((m) => `<tr><td><div class="persona"><span class="avatar chico" aria-hidden="true">${esc(iniciales(m.nombre || m.remitente))}</span><span>${m.nombre ? `<strong>${esc(m.nombre)}</strong><br>` : ''}<span class="mono">${esc(m.remitente)}</span></span></div></td>
        <td>${esc(m.asunto || '(sin asunto)')}<div class="muted">a ${esc(m.destinatario || '')}</div></td>
        <td class="nowrap">${fechaHora(m.creado)}<div class="muted">por ${esc(FORMA[m.forma] || m.forma)}</div></td>
        <td><ul class="adj-lista">${m.adjuntos.map((a) => `<li>${I.doc}<span class="nom">${esc(a.nombre)}</span><span class="tam">${tamanio(a.tamano)}</span></li>`).join('')}</ul>${m.descartados.length ? `<div class="muted">No se leen: ${esc(m.descartados.join(', '))}</div>` : ''}</td>
        <td class="mono">${m.cliente_cuit ? esc(cuit(m.cliente_cuit)) : '<span class="muted">Sin cliente</span>'}</td>
        <td class="muted">${esc(m.motivo || '')}</td>
        <td class="acciones-fila"><div class="acc-apiladas">
          <button type="button" class="mini" data-accion="aceptarRetenido" data-id="${m.id}" data-permitir="1" title="Lo procesa y suma a ${esc(m.remitente)} a los permitidos">Aceptar y permitir</button>
          <button type="button" class="ghost mini" data-accion="aceptarRetenido" data-id="${m.id}" title="Lo procesa esta vez">Aceptar</button>
          <button type="button" class="ghost mini" data-accion="descartarRetenido" data-id="${m.id}">Descartar</button></div></td></tr>`).join('')}</tbody></table></div>
      ${r.total > RETENIDOS.length ? `<p class="muted">Se muestran ${RETENIDOS.length} de ${r.total}.</p>` : ''}
      ${r.permitidos_fijados ? '<p class="muted">La lista de remitentes permitidos la fija el servidor: «Aceptar y permitir» procesa el mail, pero no puede sumar al remitente.</p>' : ''}`;
    tarjetas('#retenidos');
  } catch (e) { toast(e, true); }
}

async function aceptarRetenido(id, permitir, btn) {
  const m = RETENIDOS.find((x) => String(x.id) === String(id));
  if (permitir && m && !confirm(`¿Aceptar el mail de ${m.remitente} y sumarlo a los remitentes permitidos? Lo próximo que mande se va a cargar solo.`)) return;
  btn.disabled = true;
  try {
    const r = await api(`/api/correo/retenido/${id}/aceptar`, { method: 'POST', json: { permitir } });
    toast(r.aviso || `Listo: se cargó el lote ${r.lote.id}${r.permitido ? ' y el remitente quedó permitido' : ''}.`, !!r.aviso);
    cargarRetenidos();
  } catch (e) { toast(e, true); btn.disabled = false; }
}

async function descartarRetenido(id) {
  const m = RETENIDOS.find((x) => String(x.id) === String(id));
  if (!confirm(`¿Descartar el mail de ${m ? m.remitente : 'este remitente'}? Se borran sus adjuntos y no se procesa.`)) return;
  try { await api(`/api/correo/retenido/${id}/descartar`, { method: 'POST' }); toast('Descartado'); cargarRetenidos(); } catch (e) { toast(e, true); }
}

// ---------- Claves de API ----------
let CLAVES = [];
async function cargarClaves() {
  try {
    ({ claves: CLAVES } = await api('/api/claves'));
    if (!CLAVES.length) {
      $('#claves').innerHTML = vacio('claves', 'Todavía no hay claves', 'Creá una para conectar un asistente por MCP, una integración por la API REST o Arcanum.');
      return;
    }
    const yo = YO.identidad;
    $('#claves').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Nombre</th><th>Clave</th><th>Rol</th><th>Clientes</th><th>Suite</th><th>Creada por</th><th>Último uso</th><th>Vence</th><th>Estado</th><th></th></tr></thead><tbody>
      ${CLAVES.map((k) => {
        const vencida = k.vence && new Date(k.vence) < new Date();
        const estadoK = k.revocada ? '<span class="est">Revocada</span>' : vencida ? '<span class="est revisar">Vencida</span>' : '<span class="est ok">Vigente</span>';
        const max = ORDEN_ROL.indexOf(k.suite ? (ORDEN_ROL.indexOf(k.suite_tope) > ORDEN_ROL.indexOf(k.rol) ? k.suite_tope : k.rol) : k.rol);
        return `<tr class="${k.revocada ? 'apagado' : ''}"><td><strong>${esc(k.nombre)}</strong><br><span class="muted">${fechaCorta(k.creado)}</span></td><td class="mono">${esc(k.prefijo)}...</td>
          <td>${rolChip(k.rol)}</td><td>${cuitsHTML(k.cuits)}</td><td>${k.suite ? 'Sí, hasta ' + rolChip(k.suite_tope) : '<span class="muted">No</span>'}</td>
          <td>${esc(k.dueno || '')}</td><td>${k.ultimo_uso ? fechaHora(k.ultimo_uso) : '<span class="muted">Nunca</span>'}</td><td>${k.vence ? fechaCorta(new Date(new Date(k.vence) - 1)) : '<span class="muted">No vence</span>'}</td>
          <td>${estadoK}</td><td>${!k.revocada && max <= ORDEN_ROL.indexOf(yo.rol) ? `<button type="button" class="ghost mini" data-accion="revocarClave" data-id="${k.id}">Revocar</button>` : ''}</td></tr>`;
      }).join('')}</tbody></table></div>`;
    tarjetas('#claves');
  } catch (e) { toast(e, true); }
}

function formClave() {
  const yo = YO.identidad;
  const restr = yo.cuits && yo.cuits.length;
  const topes = ORDEN_ROL.filter((r) => ORDEN_ROL.indexOf(r) <= ORDEN_ROL.indexOf(yo.rol) && (yo.rol === 'superadmin' || ORDEN_ROL.indexOf(r) <= ORDEN_ROL.indexOf('usuario')));
  abrirModal('Nueva clave de API', `<form id="fClave" class="form-modal">
    <label>Nombre<input id="ckNombre" required placeholder="Para qué es: «Claude de Diego», «Arcanum», «n8n»" autocomplete="off"></label>
    <label>Rol<select id="ckRol">${rolesHasta(yo.rol, 'lectura')}</select><span class="ayuda">Para un asistente (MCP), lectura: puede buscar y verificar, pero no cargar ni corregir.</span></label>
    <label>Clientes (CUITs)<textarea id="ckCuits" rows="2" placeholder="${restr ? 'Uno o más de tus clientes' : 'Vacío: todos los clientes'}">${esc(cuitsTexto(restr ? yo.cuits : []))}</textarea></label>
    <label>Vence<input id="ckVence" type="date"><span class="ayuda">Vacío: no vence. Vence al terminar ese día.</span></label>
    <label class="chk"><input type="checkbox" id="ckSuite"> Puede abrir sesiones de la suite</label>
    <label id="ckTopeWrap" class="hidden">Rol tope de la suite<select id="ckTope">${topes.map((r) => `<option value="${r}"${r === 'usuario' ? ' selected' : ''}>${ROLES[r]}</option>`).join('')}</select>
      <span class="ayuda">Quien entre desde otra app (Arcanum) con esta clave no pasa de este rol, aunque allá tenga uno mayor.${yo.rol === 'superadmin' ? '' : ' Admin y superadmin solo los pone un superadmin.'}</span></label>
    <div class="login-err hidden" id="ckErr"></div></form>`,
  { acciones: '<button type="button" class="ghost" data-accion="modalCerrar">Cancelar</button><button type="submit" form="fClave">Crear</button>' });
}

async function crearClave() {
  const suite = $('#ckSuite').checked;
  try {
    const r = await api('/api/claves', { method: 'POST', json: { nombre: $('#ckNombre').value.trim(), rol: $('#ckRol').value, cuits: $('#ckCuits').value,
      vence: $('#ckVence').value, suite, suite_tope: suite ? $('#ckTope').value : '' } });
    cargarClaves();
    secretoUnaVez('Clave creada', `Esta es la clave <strong>${esc(r.datos.nombre)}</strong>. Va en la cabecera <code>X-API-Key</code> o <code>Authorization: Bearer</code>.`,
      r.clave, 'Se muestra una sola vez: Factorum guarda solo su huella. Si la perdés, revocala y creá otra.');
  } catch (e) { const er = $('#ckErr'); er.textContent = e.message; er.classList.remove('hidden'); }
}

async function revocarClave(id) {
  const k = CLAVES.find((x) => String(x.id) === String(id));
  if (!confirm(`¿Revocar la clave «${k ? k.nombre : id}»? Lo que la use deja de andar en el momento.`)) return;
  try { await api('/api/claves/' + id, { method: 'DELETE' }); toast('Revocada'); cargarClaves(); } catch (e) { toast(e, true); }
}

// ---------- Conectar ----------
function pintarConectar() {
  const base = location.origin;
  const clave = 'fct_...';
  const desktop = JSON.stringify({ mcpServers: { factorum: { command: 'npx', args: ['-y', 'mcp-remote', base + '/mcp', '--header', 'Authorization:${FACTORUM_AUTH}'],
    env: { FACTORUM_AUTH: 'Bearer ' + clave } } } }, null, 2);
  const crearClaveBtn = puede('claves') ? '<button type="button" data-accion="ir" data-vista="claves">Crear una clave</button>' : '<span class="muted">Pedile a un admin que te cree una clave.</span>';
  $('#conectar').innerHTML = `
    <div class="aviso-card">${I.llave}<div><strong>Para un asistente, una clave de rol lectura.</strong> Puede buscar, resumir y verificar, pero no cargar, corregir ni aprobar. Limitala a los clientes que necesita.</div>${crearClaveBtn}</div>
    <div class="dos parejo">
      <section class="card"><div class="card-cab"><h3>API REST</h3><a class="link" href="https://github.com/diegoparras/factorum/blob/main/docs/API.md" target="_blank" rel="noopener">Documentación${I.externo}</a></div>
        <p class="muted">JSON sobre HTTPS. La clave va en <code>X-API-Key</code> o en <code>Authorization: Bearer</code>. Cada clave ve solo sus clientes.</p>
        ${bloqueCodigo(`curl -H "X-API-Key: ${clave}" "${base}/api/comprobantes?estado=revisar"`, 'Lo que hay para revisar')}
        ${bloqueCodigo(`curl -H "X-API-Key: ${clave}" -F archivo=@factura.pdf "${base}/api/verificar"`, '¿Es legal esta factura?')}
      </section>
      <section class="card"><div class="card-cab"><h3>MCP para asistentes</h3></div>
        <p class="muted">Streamable HTTP en <code>${esc(base)}/mcp</code>, con la misma clave. Herramientas: verificar_factura, buscar_comprobantes, comprobante, pendientes_revision, resumen, conciliar, estado_lote y, con rol usuario, procesar_factura.</p>
        ${bloqueCodigo(`claude mcp add --transport http factorum ${base}/mcp --header "Authorization: Bearer ${clave}"`, 'Claude Code')}
        ${bloqueCodigo(desktop, 'Claude Desktop: claude_desktop_config.json')}
        <p class="muted">En Claude Desktop: Configuración, Desarrollador, Editar configuración. Pegá el bloque, reemplazá <code>${clave}</code> por tu clave y reiniciá Claude. Hace falta Node (npx).</p>
      </section>
    </div>
    ${conectarIA()}`;
}

// En Conectar: qué IA se usa y cómo elegirla (local o externa), con lo que Factorum probó.
function conectarIA() {
  const a = AVISO_IA || {};
  const recos = a.recomendados || [];
  const usa = a.configurada && a.en_uso;
  const estado = usa ? `<div class="ia-conf">${selloIA(a)}${a.modelo ? `<span class="muted">Modelo <code>${esc(a.modelo)}</code></span>` : ''}</div>
      <p class="muted">${SALUD && SALUD.ia_por_defecto === false ? 'Por la API se puede pedir la IA con <code>ia=true</code>; los chats, el correo y MCP no la usan (está apagada en Configuración, General).' : 'Lo que llega por los chats, el correo, MCP y la API (salvo que mande <code>ia=false</code>) usa la IA cuando el QR, el texto y el OCR no alcanzan.'}</p>`
    : `<p class="muted">${a.configurada ? 'Hay una IA externa configurada, pero no se usa hasta que un superadmin acepte el aviso de qué pasa con los datos.' : 'Factorum no usa ninguna IA: lee con el QR, el texto del PDF y el OCR. La IA es opcional y es el último recurso.'}</p>`;
  const fila = (r) => `<li><code>${esc(r.modelo)}</code><span><strong>${esc(r.porcentaje)} %</strong> · ${esc(r.tiempo)} por imagen${r.cpu ? ' en 4 núcleos sin GPU' : ''}${r.mitad ? ' (en la mitad de la prueba)' : ''}</span></li>`;
  const de = (p) => recos.filter((r) => r.proveedor === p).map(fila).join('');
  const elegir = miRol() === 'superadmin' ? '<button type="button" class="ghost mini" data-accion="irConf" data-sec="ia">Elegir la IA</button>' : '';
  return `<section class="card ia-conectar"><div class="card-cab"><h3>IA (visión)</h3>${elegir}</div>
      ${estado}
      <div class="ia-como">
        <div class="ia-como-col"><h4>${I.servidor}Local: Ollama en tu servidor</h4><p class="muted">Las imágenes no salen de tu servidor. Sin GPU es lento y lee menos.</p>${de('ollama') ? `<ul>${de('ollama')}</ul>` : ''}</div>
        <div class="ia-como-col"><h4>${I.nube}Externa: OpenAI, Anthropic, Gemini, OpenRouter</h4><p class="muted">Rápida y lee mejor, pero los comprobantes que necesitan IA salen de tu servidor: antes de usarla hay que aceptar el aviso.</p>${de('openai') ? `<ul>${de('openai')}</ul>` : ''}</div>
      </div>
      ${a.prueba ? `<p class="muted ia-como-pie">${esc(a.prueba)}</p>` : ''}</section>`;
}

// ---------- Auditoría ----------
const ACCIONES = {
  'sesion.entrar': 'Entró', 'sesion.fallida': 'Intento de entrada fallido', 'sesion.salir': 'Salió',
  'sesion.entrar_lockatus': 'Entró con Lockatus', 'sesion.rechazo_lockatus': 'Lockatus: entrada rechazada',
  'usuario.crear': 'Alta de persona', 'usuario.editar': 'Cambio de persona', 'usuario.desactivar': 'Desactivó a una persona',
  'usuario.reactivar': 'Reactivó a una persona', 'usuario.restablecer': 'Restableció una contraseña', 'usuario.cambiar_clave': 'Cambió su contraseña',
  'clave.crear': 'Creó una clave de API', 'clave.revocar': 'Revocó una clave de API', 'config.cambiar': 'Cambió la configuración',
  'config.probar_ia': 'Probó la IA', 'ia.aviso_aceptado': 'Aceptó el aviso de la IA externa', 'ia.aviso_visto': 'Vio el aviso de la IA externa',
  'ia.aviso_aceptado_prueba': 'Aceptó el aviso de la IA para probarla',
  'config.whatsapp_desvincular': 'Desvinculó WhatsApp', 'comprobante.corregir': 'Corrigió un comprobante', 'comprobante.aprobar': 'Aprobó un comprobante',
  'exportar': 'Exportó', 'lote.crear': 'Cargó un lote', 'suite.pase_emitido': 'Pase de la suite emitido', 'suite.pase_usado': 'Entró con un pase de la suite',
  'suite.pase_rechazado': 'Pase de la suite rechazado', 'suite.abrir': 'Abrió otra app de la suite', 'suite.abrir_fallido': 'No pudo abrir otra app',
  'r2.migrar_inicio': 'Empezó a pasar el disco a R2', 'r2.migrar_seguir': 'Siguió el pase a R2', 'r2.migrar_pausa': 'Pausó el pase a R2',
  'r2.migrar_fin': 'Terminó el pase a R2', 'r2.liberar_inicio': 'Pidió liberar el disco', 'r2.liberar_fin': 'Se liberó el disco',
  'copia.ok': 'Copia de la base hecha', 'copia.error': 'Falló la copia de la base', 'copia.pedir': 'Pidió una copia de la base',
  'copia.descargar': 'Descargó una copia de la base', 'copia.restaurar': 'Restauró una copia de la base',
  'lote.borrar': 'Mandó un lote a la papelera', 'comprobante.borrar': 'Mandó comprobantes a la papelera', 'lote.cambiar_cliente': 'Cambió el cliente de un lote',
  'papelera.restaurar': 'Restauró de la papelera', 'papelera.vaciar': 'Vació la papelera', 'papelera.borrar_ya': 'Borró para siempre de la papelera',
  'papelera.purgar': 'Borrado definitivo de lo vencido',
  'libro_iva.exportar': 'Exportó el Libro IVA Digital',
};
let audOffset = 0;
const AUD_LIM = 50;
function detalleHTML(d) {
  if (!d) return '';
  return Object.entries(d).filter(([, v]) => v !== null && v !== '' && !(Array.isArray(v) && !v.length)).map(([k, v]) => {
    let t;
    if (Array.isArray(v)) t = (k === 'cuits' ? v.map(cuit) : v).join(', ');
    else if (v && typeof v === 'object') t = 'antes' in v ? `${v.antes} → ${v.despues}` : JSON.stringify(v);
    else if (typeof v === 'boolean') t = v ? 'sí' : 'no';
    else t = String(v);
    return `<span class="det"><span class="muted">${esc(k.replace(/_/g, ' '))}:</span> ${esc(t)}</span>`;
  }).join(' ');
}
async function cargarAuditoria() {
  const p = new URLSearchParams({ limite: AUD_LIM, offset: audOffset });
  for (const [k, sel] of [['desde', '#audDesde'], ['hasta', '#audHasta'], ['persona', '#audPersona'], ['accion', '#audAccion']]) if ($(sel).value.trim()) p.set(k, $(sel).value.trim());
  try {
    const { eventos, total, acciones } = await api('/api/auditoria?' + p);
    const sel = $('#audAccion');
    const elegida = sel.value;
    const grupos = [...new Set(acciones.map((a) => a.split('.')[0] + '.'))].filter((g) => acciones.filter((a) => a.startsWith(g)).length > 1);
    sel.innerHTML = '<option value="">Todas las acciones</option>' + grupos.map((g) => `<option value="${g}">${esc({ 'sesion.': 'Entradas y salidas', 'usuario.': 'Personas', 'clave.': 'Claves de API', 'config.': 'Configuración', 'comprobante.': 'Comprobantes', 'suite.': 'Suite', 'r2.': 'Almacenamiento R2', 'copia.': 'Copias de la base', 'lote.': 'Lotes', 'papelera.': 'Papelera', 'libro_iva.': 'Libro IVA Digital' }[g] || g)} (todas)</option>`).join('')
      + acciones.map((a) => `<option value="${esc(a)}">${esc(ACCIONES[a] || a)}</option>`).join('');
    sel.value = elegida;
    if (!eventos.length) { $('#auditoria').innerHTML = vacio('auditoria', 'No hay movimientos con estos filtros', 'Probá con otras fechas o sacando la persona.'); $('#audPager').innerHTML = ''; return; }
    $('#auditoria').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Cuándo</th><th>Quién</th><th>Qué</th><th>Sobre</th><th>Detalle</th><th>IP</th></tr></thead><tbody>
      ${eventos.map((ev) => `<tr><td class="nowrap">${fechaHora(ev.creado)}</td><td>${esc(ev.actor || '')}</td>
        <td><span class="accion ${esc(ev.accion.split('.')[0])}${ev.accion.endsWith('fallida') || ev.accion.endsWith('rechazado') || ev.accion.startsWith('sesion.rechazo') || ev.accion.endsWith('fallido') || ev.accion.endsWith('.error') ? ' mal' : ''}">${esc(ACCIONES[ev.accion] || ev.accion)}</span></td>
        <td>${esc(ev.objeto || '')}</td><td class="detalle">${detalleHTML(ev.detalle)}</td><td class="mono muted">${esc(ev.ip || '')}</td></tr>`).join('')}</tbody></table></div>`;
    tarjetas('#auditoria');
    const hasta = audOffset + eventos.length;
    $('#audPager').innerHTML = `<span>${audOffset + 1} a ${hasta} de ${total}</span>
      ${audOffset > 0 ? '<button class="ghost mini" data-accion="audPagina" data-dir="-1">Anteriores</button>' : ''}
      ${hasta < total ? '<button class="ghost mini" data-accion="audPagina" data-dir="1">Siguientes</button>' : ''}`;
  } catch (e) { toast(e, true); }
}

// ---------- Eventos ----------
document.addEventListener('click', (ev) => {
  const el = ev.target.closest('[data-accion]');
  if (!el || !el.closest('.menu-wrap')) {
    if (!ev.target.closest('.menu-wrap')) { $('#menu').classList.add('hidden'); $('#suiteMenu').classList.add('hidden'); }
    if (!el) return;
  }
  const a = el.dataset.accion;
  switch (a) {
    case 'ir': ir(el.dataset.vista); break;
    case 'nav': toggleNav(); break;
    case 'cerrarNav': document.documentElement.removeAttribute('data-nav-movil'); break;
    case 'menu': pintarMenu(); $('#suiteMenu').classList.add('hidden'); $('#menu').classList.toggle('hidden'); break;
    case 'suite': pintarSuite(); $('#menu').classList.add('hidden'); $('#suiteMenu').classList.toggle('hidden'); break;
    case 'abrirApp': abrirApp(el.dataset.app, el); break;
    case 'miClave': $('#menu').classList.add('hidden'); pedirCambioClave(false); break;
    case 'copiar': copiar(el.dataset.texto); break;
    case 'modalCerrarFijo': cerrarModal(true); break;
    case 'saltar': ev.preventDefault(); document.getElementById('conf-' + el.dataset.sec)?.scrollIntoView({ behavior: 'smooth', block: 'start' }); break;
    case 'guardarConf': guardarConf(el.dataset.sec, null, el); break;
    case 'pase': accionPase(el.dataset.que, '', el); break;
    case 'liberarDisco': pedirLiberar(); break;
    case 'liberarConfirmar': accionPase('liberar', $('#liberarConf').value.trim().toLowerCase(), el).then((ok) => { if (ok) cerrarModal(); }); break;
    case 'hacerCopia': hacerCopia(el); break;
    case 'verCopias': ir('configuracion'); setTimeout(() => document.getElementById('conf-copias')?.scrollIntoView({ block: 'start' }), 400); break;
    case 'probarConf': probarConf(el.dataset.sec, el); break;
    case 'iaRecargar': iaPedirModelos(true); break;
    case 'iaUsar': iaUsar(el.dataset.modelo); break;
    case 'generarToken': {
      const b = new Uint8Array(24); crypto.getRandomValues(b);
      const i = document.getElementById(el.dataset.para);
      i.value = [...b].map((x) => x.toString(16).padStart(2, '0')).join(''); i.type = 'text';
      const t = i.parentElement.querySelector('.pass-toggle'); if (t) t.innerHTML = I.ojoNo;
      i.dispatchEvent(new Event('input', { bubbles: true }));
      toast(i.dataset.clave === 'webhook.token' ? 'Token generado: guardá para usarlo y pasáselo a tu bot' : 'Generado: copialo donde corresponda y guardá');
      break;
    }
    case 'borrarSecreto': {
      const c = CONF.campos.find((x) => x.clave === el.dataset.clave);
      if (c && confirm(`¿Borrar «${c.etiqueta}»? Vuelve al valor del servidor, si hay uno.`)) guardarConf(c.seccion, { [c.clave]: null });
      break;
    }
    case 'nuevoUsuario': formUsuario(null); break;
    case 'linkUsuario': linkUsuario(el.dataset.id, el); break;
    case 'aceptarRetenido': aceptarRetenido(el.dataset.id, !!el.dataset.permitir, el); break;
    case 'descartarRetenido': descartarRetenido(el.dataset.id); break;
    case 'conectarGoogle': conectarGoogle(el); break;
    case 'desconectarGoogle': desconectarGoogle(); break;
    case 'olvide': mostrarOlvide(); break;
    case 'volverLogin': try { history.replaceState(null, '', location.pathname); } catch { /* nada */ } sinSesion(); break;
    case 'irConf': IR_SECCION = el.dataset.sec; ir('configuracion'); break;
    case 'editarUsuario': formUsuario(USUARIOS.find((u) => String(u.id) === el.dataset.id)); break;
    case 'restablecer': restablecer(el.dataset.id); break;
    case 'activarUsuario': activarUsuario(el.dataset.id, !!el.dataset.activo); break;
    case 'nuevaClave': formClave(); break;
    case 'revocarClave': revocarClave(el.dataset.id); break;
    case 'audPagina': audOffset = Math.max(0, audOffset + Number(el.dataset.dir) * AUD_LIM); cargarAuditoria(); break;
    case 'tema': {
      const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      try { localStorage.setItem('factorum.tema', next); } catch { /* nada */ }
      pintarMenu();
      break;
    }
    case 'salir': $('#menu').classList.add('hidden'); salir(); break;
    case 'ojo': {
      const i = document.getElementById(el.dataset.para);
      i.type = i.type === 'password' ? 'text' : 'password';
      el.innerHTML = i.type === 'password' ? I.ojo : I.ojoNo;
      break;
    }
    case 'verLote': if (!ev.target.closest('[data-accion="exportarLote"]')) { Object.assign(filtro, { lote: el.dataset.id, estado: '', q: '', offset: 0 }); ir('comprobantes'); } break;
    case 'exportarLote': ev.stopPropagation(); descargar(`/api/export.xlsx?lote=${el.dataset.id}`, `factorum-lote-${el.dataset.id}.xlsx`); break;
    case 'exportar': leerFiltro(); descargar(`/api/export.${el.dataset.formato}?` + qsFiltro({ limite: '', offset: '' }), `factorum.${el.dataset.formato}`); break;
    case 'pagina': filtro.offset = Math.max(0, filtro.offset + Number(el.dataset.dir) * filtro.limite); cargarCbtes(); break;
    case 'revisar': abrirRevision(Number(el.dataset.idx)); break;
    case 'concAtajo': aplicarAtajo(el.dataset.atajo); break;
    case 'concTab': CONC.tab = el.dataset.grupo; pintarConciliacion(); break;
    case 'concOrden': ordenarConc(el.dataset.col); break;
    case 'concAbrir': abrirDesdeConc(Number(el.dataset.id)); break;
    case 'concConstatar': constatarConc(Number(el.dataset.id), el); break;
    case 'concExportar': exportarConc(el.dataset.formato, el.dataset.grupo || ''); break;
    case 'concCopiar': copiar(textoFaltaCargar()); break;
    case 'concRefrescar': cargarConciliacion(true); break;
    case 'concALibro': libroDesdeConc(); break;
    case 'libAtajo': libAtajo(el.dataset.atajo); break;
    case 'libSolo': LIB.solo = el.dataset.solo === '1'; cargarLibro(); break;
    case 'libBajar': bajarLibro(); break;
    case 'libRefrescar': cargarLibro(); break;
    case 'libAbrir': abrirDesdeLibro(Number(el.dataset.id), el.dataset.grupo); break;
    case 'revAnterior': if (rev) abrirRevision(rev.idx - 1); break;
    case 'revSiguiente': if (rev) abrirRevision(rev.idx + 1); break;
    case 'revCerrar': cerrarRevision(); break;
    case 'bandeja': bandejaEstado = el.dataset.estado; cargarBandeja(); break;
    case 'abrirBandeja': abrirRevision(Number(el.dataset.idx)); break;
    case 'verEstado': Object.assign(filtro, { estado: el.dataset.estado, clase: '', rubro: '', lote: '', q: '', offset: 0 }); ir('comprobantes'); break;
    case 'verClase': Object.assign(filtro, { estado: '', clase: el.dataset.clase, rubro: '', lote: '', q: '', offset: 0 }); ir('comprobantes'); break;
    case 'verRubro': Object.assign(filtro, { rubro: el.dataset.rubro, offset: 0 }); ir('comprobantes'); break;
    case 'novedades': $('#menu').classList.add('hidden'); verNovedades(true); break;
    case 'acerca': $('#menu').classList.add('hidden'); acercaDe(); break;
    case 'modalCerrar': cerrarModal(); break;
    case 'modalFondo': if (ev.target === el) cerrarModal(); break;
    case 'avisoIA': if (AVISO_IA || (CONF && CONF.ia_aviso)) abrirAvisoIA(el.closest('#conf-ia') && CONF && CONF.ia_aviso ? CONF.ia_aviso : AVISO_IA, 'lectura'); break;
    case 'aiaOK': cerrarAvisoIA(true); break;
    case 'aiaCancelar': cerrarAvisoIA(false); break;
    case 'aiaFondo': if (ev.target === el) cerrarAvisoIA(false); break;
    case 'aceptarAvisoIA': aceptarAvisoIA(el); break;
    case 'abrirOriginal':
      api(`/api/documentos/${el.dataset.doc}/archivo`, { binario: true }).then((b) => {
        const u = URL.createObjectURL(b); window.open(u, '_blank', 'noopener'); setTimeout(() => URL.revokeObjectURL(u), 60000);
      }).catch((e) => toast(e, true));
      break;
    case 'revGuardar': guardarRevision(false); break;
    case 'revAprobar': guardarRevision(true); break;
    case 'revConstatar': constatarRevision(); break;
    case 'revBorrar': if (rev) pedirBorrarCbtes([rev.cbte.id], true); break;
    // Papelera: borrar lotes y comprobantes, cambiar el cliente, restaurar y borrar para siempre.
    case 'borrarLote': pedirBorrarLote(el.dataset.id); break;
    case 'borrarLoteConfirmar': borrarLote(el.dataset.id, el); break;
    case 'cambiarCliente': pedirCambiarCliente(el.dataset.id); break;
    case 'sinLote': Object.assign(filtro, { lote: '', offset: 0 }); cargarCbtes(); break;
    case 'selCbte': if (el.checked) SEL.add(Number(el.dataset.id)); else SEL.delete(Number(el.dataset.id)); pintarSeleccion(); break;
    case 'selTodos':
      SEL.clear();
      if (el.checked) listado.forEach((c) => SEL.add(c.id));
      $$('#tablaCbtes input[data-accion="selCbte"]').forEach((i) => { i.checked = el.checked; });
      pintarSeleccion();
      break;
    case 'selNinguno': SEL.clear(); $$('#tablaCbtes input[data-accion="selCbte"]').forEach((i) => { i.checked = false; }); pintarSeleccion(); break;
    case 'borrarElegidos': if (SEL.size) pedirBorrarCbtes([...SEL]); break;
    case 'borrarCbtesConfirmar': borrarCbtes(el); break;
    case 'papSel': if (el.checked) PAP_SEL.add(el.dataset.clave); else PAP_SEL.delete(el.dataset.clave); pintarSelPapelera(); break;
    case 'papTodos':
      PAP_SEL.clear();
      if (el.checked) PAP.forEach((it) => PAP_SEL.add(claveItem(it)));
      $$('#papelera input[data-accion="papSel"]').forEach((i) => { i.checked = el.checked; });
      pintarSelPapelera();
      break;
    case 'papNinguno': PAP_SEL.clear(); $$('#papelera input[data-accion="papSel"]').forEach((i) => { i.checked = false; }); pintarSelPapelera(); break;
    case 'papPagina': papOffset = Math.max(0, papOffset + Number(el.dataset.dir) * PAP_LIM); PAP_SEL.clear(); cargarPapelera(); break;
    case 'restaurar': restaurar([el.dataset.clave], el); break;
    case 'restaurarElegidos': if (PAP_SEL.size) restaurar([...PAP_SEL], el); break;
    case 'borrarYa': pedirBorrarYa([el.dataset.clave]); break;
    case 'borrarYaElegidos': if (PAP_SEL.size) pedirBorrarYa([...PAP_SEL]); break;
    case 'vaciarPapelera': pedirBorrarYa(null); break;
    case 'borrarYaConfirmar': borrarYa(el); break;
    case 'desvincularWa':
      if (confirm('¿Desvincular WhatsApp? Vas a tener que escanear el código otra vez.')) api('/api/canales/whatsapp/desvincular', { method: 'POST' }).then(refrescarWhatsApp).catch((e) => toast(e, true));
      break;
    default: break;
  }
});

document.addEventListener('submit', (ev) => {
  const f = ev.target;
  if (f.id === 'loginForm') { ev.preventDefault(); entrar($('#loginEmail').value.trim(), $('#loginClave').value); }
  else if (f.id === 'olvideForm') { ev.preventDefault(); pedirLink(); }
  else if (f.id === 'restablecerForm') { ev.preventDefault(); usarLink(); }
  else if (f.id === 'arranqueForm') { ev.preventDefault(); crearPrimerSuperadmin(); }
  else if (f.id === 'fClaveMia') { ev.preventDefault(); cambiarMiClave(); }
  else if (f.id === 'fUsuario') { ev.preventDefault(); guardarUsuario(f); }
  else if (f.id === 'fClave') { ev.preventDefault(); crearClave(); }
  else if (f.id === 'fCambioCliente') { ev.preventDefault(); cambiarCliente(f); }
  else if (f.id === 'fAuditoria') { ev.preventDefault(); audOffset = 0; cargarAuditoria(); }
  else if (f.id === 'fVerificar') verificar(ev);
  else if (f.id === 'fSubir') subir(ev);
  else if (f.id === 'fConciliacion') { ev.preventDefault(); conciliar(false); }
  else if (f.id === 'fLibro') { ev.preventDefault(); armarLibro(); }
  else if (f.id === 'fFiltro') { ev.preventDefault(); leerFiltro(); filtro.offset = 0; cargarCbtes(); }
  else if (f.id === 'revForm') { ev.preventDefault(); guardarRevision(false); }
  else if (f.id === 'fVincular') {
    ev.preventDefault();
    api('/api/canales/whatsapp/vincular', { method: 'POST', json: { telefono: soloDig($('#telVincular').value) } })
      .then((r) => toast(`Código: ${r.codigo}. Ingresalo en WhatsApp, Dispositivos vinculados, Vincular con número de teléfono.`))
      .catch((e) => toast(e, true));
  }
});

document.addEventListener('change', (ev) => {
  // Marcar «Usar IA si hace falta» con una IA externa: la primera vez, el aviso.
  if ((ev.target.id === 'iaVerificar' || ev.target.id === 'loteIA') && ev.target.checked) {
    const ck = ev.target;
    confirmarIAPersona().then((ok) => { if (!ok) ck.checked = false; });
  }
  if (ev.target.id === 'ckSuite') $('#ckTopeWrap').classList.toggle('hidden', !ev.target.checked);
  if (ev.target.name === 'correoForma') cambiarForma(ev.target.value);
  if (ev.target.id === idCampo('correo.proveedor')) mostrarProveedor();
  if (ev.target.id === idCampo('ia.proveedor')) { IAUI.aMano = false; $('#conf-ia .prueba').classList.add('hidden'); iaAplicarProveedor(true); }
  if (ev.target.id === 'iaRegion') { const r = ((iaProvDe('openai') || {}).regiones || []).find((x) => x.codigo === ev.target.value); if (r) { iaEl('ia.url').value = r.url; $('#iaRegionUrl').textContent = r.url; } iaPedirModelos(false); }
  if (ev.target.id === 'iaModeloSel') iaElegirModelo(ev.target.value);
  if (ev.target.id === 'concCliente') { $('#concOtroWrap').classList.toggle('hidden', ev.target.value !== 'otro'); if (ev.target.value === 'otro') $('#concOtro').focus(); }
  if (ev.target.id === 'concDesde' || ev.target.id === 'concHasta') marcarAtajo();
  if (ev.target.id === 'libCliente') { $('#libOtroWrap').classList.toggle('hidden', ev.target.value !== 'otro'); if (ev.target.value === 'otro') $('#libOtro').focus(); }
  if (ev.target.id === 'libPeriodo') marcarLibAtajo();
  if (ev.target.id === 'usAlcance') $('#usCuitsWrap').classList.toggle('hidden', ev.target.value !== 'estos');
  if (ev.target.id === 'usLockatus') $('#usClaveWrap').classList.toggle('hidden', ev.target.checked);
});

document.addEventListener('input', (ev) => {
  if (ev.target.id === 'liberarConf') { const b = $('[data-accion="liberarConfirmar"]'); if (b) b.disabled = ev.target.value.trim().toLowerCase() !== 'liberar'; }
  if (ev.target.id === 'borrarLoteConf') { const b = $('[data-accion="borrarLoteConfirmar"]'); if (b) b.disabled = ev.target.value.trim().toLowerCase() !== ev.target.dataset.esperado; }
  if (ev.target.id === 'borrarYaConf') { const b = $('[data-accion="borrarYaConfirmar"]'); if (b) b.disabled = ev.target.value.trim().toLowerCase() !== 'borrar'; }
  if (ev.target.closest('#revForm')) ev.target.classList.toggle('cambiado', ev.target.value !== ev.target.dataset.orig);
  if (ev.target.id === idCampo('correo.token')) actualizarUrlsCorreo();
  if (ev.target.id === 'iaModeloMano') { iaEl('ia.modelo').value = ev.target.value.trim(); $('#conf-ia .prueba').classList.add('hidden'); }
  // La clave o la URL nuevas: la lista se vuelve a pedir cuando se deja de escribir.
  if ((ev.target.id === idCampo('ia.api_key') || ev.target.id === idCampo('ia.url')) && IAUI) {
    clearTimeout(IAUI.timer);
    IAUI.timer = setTimeout(() => iaPedirModelos(false), 700);
  }
});

document.addEventListener('keydown', (ev) => {
  if (AIA) { // el aviso de la IA se queda con el teclado
    if (ev.key === 'Escape') { ev.preventDefault(); cerrarAvisoIA(false); } else if (ev.key === 'Tab') atraparFoco(ev, $('#aiaCard'));
    return;
  }
  if (ev.key === 'Escape' && !$('#modal').classList.contains('hidden')) { cerrarModal(); return; }
  if (!$('#modal').classList.contains('hidden')) return; // con una ventana abierta, el teclado es de la ventana
  if (ev.key === 'Escape') { $('#menu').classList.add('hidden'); $('#suiteMenu').classList.add('hidden'); }
  if (!rev || $('#revision').classList.contains('hidden')) return;
  if (ev.key === 'Escape') cerrarRevision();
  const escribiendo = ev.target.closest && ev.target.closest('input, textarea, select');
  if (!escribiendo && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
    const k = ev.key.toLowerCase();
    if (k === 'j') { ev.preventDefault(); abrirRevision(rev.idx + 1); return; }
    if (k === 'k') { ev.preventDefault(); abrirRevision(rev.idx - 1); return; }
    if (k === 'a' && puede('corregir')) { ev.preventDefault(); guardarRevision(true); return; }
    if (k === 'e') { ev.preventDefault(); const i = $('#revForm input'); if (i) i.focus(); return; }
  }
  if (ev.altKey && ev.key === 'ArrowRight') abrirRevision(rev.idx + 1);
  if (ev.altKey && ev.key === 'ArrowLeft') abrirRevision(rev.idx - 1);
  if ((ev.ctrlKey || ev.metaKey) && ev.key === 'Enter') guardarRevision(true);
});

// Arrastrar y soltar, y lo elegido a la vista.
function tamanio(b) {
  if (b < 1024) return `${b} B`;
  if (b < 1048576) return `${Math.round(b / 1024)} KB`;
  return `${(b / 1048576).toLocaleString('es-AR', { maximumFractionDigits: 1 })} MB`;
}
function mostrarArchivos(z, input) {
  const files = [...input.files];
  const txt = z.querySelector('.zona-txt');
  const btn = z.querySelector('.zona-btn');
  z.querySelector('.zona-lista')?.remove();
  z.classList.toggle('con-archivos', files.length > 0);
  if (!files.length) {
    txt.textContent = txt.dataset.vacio;
    btn.textContent = input.multiple ? 'Elegir archivos' : 'Elegir archivo';
    return;
  }
  const total = files.reduce((a, f) => a + f.size, 0);
  txt.textContent = files.length === 1 ? '1 archivo listo' : `${files.length} archivos listos (${tamanio(total)})`;
  btn.textContent = input.multiple ? 'Cambiar archivos' : 'Cambiar archivo';
  const ul = document.createElement('ul');
  ul.className = 'zona-lista';
  const MAX = 5;
  for (const f of files.slice(0, MAX)) {
    const li = document.createElement('li');
    li.innerHTML = `<span class="nom">${esc(f.name)}</span><span class="tam">${tamanio(f.size)}</span>`;
    ul.append(li);
  }
  if (files.length > MAX) {
    const li = document.createElement('li');
    li.className = 'mas';
    li.textContent = `y ${files.length - MAX} más`;
    ul.append(li);
  }
  txt.after(ul);
}
for (const [zona, sel] of [['#zonaVerificar', '#archVerificar'], ['#zonaSubir', '#archSubir']]) {
  const z = $(zona);
  const input = $(sel);
  z.addEventListener('dragover', (e) => { e.preventDefault(); z.classList.add('sobre'); });
  z.addEventListener('dragleave', () => z.classList.remove('sobre'));
  z.addEventListener('drop', (e) => {
    e.preventDefault(); z.classList.remove('sobre');
    input.files = e.dataTransfer.files;
    mostrarArchivos(z, input);
  });
  input.addEventListener('change', () => mostrarArchivos(z, input));
  // Después de un reset el input queda vacío, pero el change no se dispara.
  input.form.addEventListener('reset', () => setTimeout(() => mostrarArchivos(z, input)));
}

// ---------- Arranque ----------
$$('.pass-toggle').forEach((b) => { b.innerHTML = I.ojo; });
arrancar();

// Un link de «elegir contraseña» pegado en una pestaña que ya tenía Factorum abierto.
window.addEventListener('hashchange', () => {
  const h = location.hash.slice(1);
  if (h.startsWith('restablecer')) mostrarRestablecer(new URLSearchParams(h.split('?')[1] || '').get('t'));
});
