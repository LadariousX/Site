"use strict";
pdfjsLib.GlobalWorkerOptions.workerSrc = "https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/pdf.worker.min.js";

const PAGE_W = 612, PAGE_H = 792;                  // US Letter (PDF points)
let MARGIN = 36, AREA_W = 0, AREA_H = 0;           // set from the header controls by readMargin()
const WHITE = 245;                                 // luminance >= this counts as blank
const TILE_ROWS = 4096;                            // max canvas height per render tile
const MAX_PIXELS = 60e6;                           // total render budget for very tall docs

const $ = (id) => document.getElementById(id);
const el = (tag, cls) => { const e = document.createElement(tag); if (cls) e.className = cls; return e; };

/* ---------- state ---------- */
// doc.pages: stacked vertically in "doc coordinates" (PDF points, y down, x from page's left edge)
let doc = null;          // { name, files: [bytes], pages: [{fileIdx, pageIdx, w, h, top, scale, tiles, rowMin, rowMax}], width, height }
let breaks = [];         // sorted doc-y positions
let undoStack = [];
let excluded = [];       // doc-y markers; a page whose slice contains one is left out of the output
let segments = [];       // computed output pages
let edScale = 1;         // editor px per pt
let pvScale = 1;         // preview css px per pt

/* ---------- loading ---------- */
async function loadFiles(fileList) {
  const files = [...fileList].filter((f) => f.type === "application/pdf" || /\.pdf$/i.test(f.name));
  if (!files.length) return setStatus("That isn't a PDF.");
  setStatus("Loading…");
  try {
    const bytes = await Promise.all(files.map((f) => f.arrayBuffer().then((b) => new Uint8Array(b))));
    let pages = [], top = 0, width = 0;
    for (let fi = 0; fi < bytes.length; fi++) {
      const pdf = await pdfjsLib.getDocument({ data: bytes[fi].slice() }).promise;  // copy: pdf.js detaches its buffer
      for (let pi = 1; pi <= pdf.numPages; pi++) {
        const page = await pdf.getPage(pi);
        const vp = page.getViewport({ scale: 1 });
        pages.push({ fileIdx: fi, pageIdx: pi - 1, pdfPage: page, w: vp.width, h: vp.height, top });
        top += vp.height;
        width = Math.max(width, vp.width);
      }
    }
    const total = pages.reduce((a, p) => a + p.w * p.h, 0);
    const scale = Math.max(0.75, Math.min(2, Math.sqrt(MAX_PIXELS / total)));
    for (let i = 0; i < pages.length; i++) {
      setStatus(`Rendering page ${i + 1} of ${pages.length}…`);
      await renderPage(pages[i], scale);
    }
    const base = files[0].name.replace(/\.pdf$/i, "").trim();
    doc = { name: base, files: bytes, pages, width, height: top };
    breaks = []; excluded = []; undoStack = [];
    setStatus(`${files.map((f) => f.name).join(", ")} · ${pages.length} source page${pages.length > 1 ? "s" : ""}`);
    buildEditor();
    update();
    $("editorScroll").scrollTop = 0; $("previewScroll").scrollTop = 0;
  } catch (err) {
    console.error(err);
    setStatus("Couldn't open that PDF: " + err.message);
  }
}

// Render a source page into vertical tiles and record, per pixel row, the leftmost/rightmost ink pixel.
async function renderPage(p, scale) {
  const vp = p.pdfPage.getViewport({ scale });
  const W = Math.ceil(vp.width), H = Math.ceil(vp.height);
  p.scale = scale; p.tiles = [];
  p.rowMin = new Int32Array(H).fill(-1);
  p.rowMax = new Int32Array(H).fill(-1);
  for (let y0 = 0; y0 < H; y0 += TILE_ROWS) {
    const rows = Math.min(TILE_ROWS, H - y0);
    const c = document.createElement("canvas");
    c.width = W; c.height = rows;
    const ctx = c.getContext("2d", { willReadFrequently: true });
    ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, W, rows);
    await p.pdfPage.render({ canvasContext: ctx, viewport: vp, transform: [1, 0, 0, 1, 0, -y0] }).promise;
    const data = ctx.getImageData(0, 0, W, rows).data;
    for (let r = 0; r < rows; r++) {
      let lo = -1, hi = -1, i = r * W * 4;
      for (let x = 0; x < W; x++, i += 4) {
        // luminance; anything darker than near-white is content
        if (data[i] * 0.299 + data[i + 1] * 0.587 + data[i + 2] * 0.114 < WHITE) { if (lo < 0) lo = x; hi = x; }
      }
      p.rowMin[y0 + r] = lo; p.rowMax[y0 + r] = hi;
    }
    p.tiles.push({ canvas: c, y0, rows });
  }
}

/* ---------- segments / layout ---------- */
// Bounding box (doc coords) of all ink between doc-y a and b, or null if blank.
function inkBox(a, b) {
  let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
  for (const p of doc.pages) {
    const lo = Math.max(a, p.top), hi = Math.min(b, p.top + p.h);
    if (hi <= lo) continue;
    const r0 = Math.floor((lo - p.top) * p.scale), r1 = Math.min(p.rowMin.length, Math.ceil((hi - p.top) * p.scale));
    for (let r = r0; r < r1; r++) {
      if (p.rowMin[r] < 0) continue;
      x0 = Math.min(x0, p.rowMin[r] / p.scale); x1 = Math.max(x1, (p.rowMax[r] + 1) / p.scale);
      y0 = Math.min(y0, p.top + r / p.scale);  y1 = Math.max(y1, p.top + (r + 1) / p.scale);
    }
  }
  if (x0 === Infinity) return null;
  // clamp to the slice so ink rows straddling a break don't leak across it
  return { x0, x1, y0: Math.max(y0, a), y1: Math.min(y1, b) };
}

const outputPages = () => segments.filter((s) => s.box && s.on);

function computeSegments() {
  const edges = [0, ...breaks, doc.height];
  segments = [];
  for (let i = 0; i < edges.length - 1; i++) {
    const a = edges[i], b = edges[i + 1];
    const box = inkBox(a, b);
    const seg = { a, b, box, scale: 0, on: !excluded.some((y) => y >= a && y < b) };
    if (box) seg.scale = Math.min(AREA_W / (box.x1 - box.x0), AREA_H / (box.y1 - box.y0));
    segments.push(seg);
  }
  const pages = outputPages();
  if ($("uniform").checked && pages.length) {
    const s = Math.min(...pages.map((p) => p.scale));
    pages.forEach((p) => (p.scale = s));
  }
  // placement on the Letter page: top-left of content at the margin; if narrower, left-aligned or centered
  const left = $("alignLeft").checked;
  for (const s of pages) {
    const w = (s.box.x1 - s.box.x0) * s.scale;
    s.ox = left ? MARGIN : MARGIN + (AREA_W - w) / 2;
    s.oy = MARGIN;
  }
}

/* ---------- editor ---------- */
function buildEditor() {
  const ed = $("editorDoc");
  ed.querySelectorAll("canvas, .page-sep").forEach((n) => n.remove());
  $("editorEmpty").style.display = doc ? "none" : "";
  ed.hidden = !doc;
  for (const p of doc.pages) {
    for (const t of p.tiles) {
      t.canvas.dataset.top = p.top + t.y0 / p.scale;
      t.canvas.dataset.h = t.rows / p.scale;
      t.canvas.dataset.w = t.canvas.width / p.scale;
      ed.insertBefore(t.canvas, $("ghost"));
    }
    if (p.top > 0) { const s = el("div", "page-sep"); s.dataset.top = p.top; ed.appendChild(s); }
  }
  layoutEditor();
}

function layoutEditor() {
  if (!doc) return;
  const avail = $("editorScroll").clientWidth - 90;
  edScale = Math.max(0.2, avail / doc.width);
  const ed = $("editorDoc");
  ed.style.width = doc.width * edScale + "px";
  ed.style.height = doc.height * edScale + "px";
  ed.querySelectorAll("canvas").forEach((c) => {
    c.style.top = c.dataset.top * edScale + "px";
    c.style.width = c.dataset.w * edScale + "px";
    c.style.height = c.dataset.h * edScale + "px";
  });
  ed.querySelectorAll(".page-sep").forEach((s) => (s.style.top = s.dataset.top * edScale + "px"));
  drawEditorOverlays();
}

function drawEditorOverlays() {
  const ed = $("editorDoc");
  ed.querySelectorAll(".brk, .bbox, .pick").forEach((n) => n.remove());
  let n = 0;
  segments.forEach((s, i) => {
    if (!s.box) return;
    const b = el("div", "bbox" + (s.on ? "" : " off"));
    Object.assign(b.style, {
      left: s.box.x0 * edScale + "px", top: s.box.y0 * edScale + "px",
      width: (s.box.x1 - s.box.x0) * edScale + "px", height: (s.box.y1 - s.box.y0) * edScale + "px",
    });
    ed.appendChild(b);
    // include/exclude toggle, pinned to the top-left of the page's content box
    const pick = el("label", "pick" + (s.on ? "" : " off"));
    const cb = el("input"); cb.type = "checkbox"; cb.checked = s.on; cb.dataset.seg = i;
    pick.append(cb, s.on ? `${++n}` : "off");
    pick.title = s.on ? `Page ${n} — uncheck to leave it out` : "Excluded — check to include";
    pick.style.left = "-38px";   // in the gutter, so it never covers content
    pick.style.top = s.box.y0 * edScale + "px";
    ed.appendChild(pick);
  });
  breaks.forEach((y, i) => {
    const b = el("div", "brk");
    b.style.top = y * edScale + "px";
    b.dataset.i = i;
    const x = el("div", "x"); x.textContent = "×"; x.title = "Remove break";
    const tag = el("div", "tag"); tag.textContent = `break ${i + 1}`;
    b.append(tag, x);
    ed.appendChild(b);
  });
}

function docYFromEvent(e) {
  const r = $("editorDoc").getBoundingClientRect();
  return Math.max(0, Math.min(doc.height, (e.clientY - r.top) / edScale));
}

function pushUndo() { undoStack.push({ breaks: breaks.slice(), excluded: excluded.slice() }); if (undoStack.length > 200) undoStack.shift(); }
function setBreaks(next) { breaks = next.filter((y) => y > 0.5 && y < doc.height - 0.5).sort((a, b) => a - b); update(); }

let drag = null;   // { i, moved }
$("editorDoc").addEventListener("pointerdown", (e) => {
  if (!doc || e.button !== 0 || e.target.closest(".pick")) return;
  const brk = e.target.closest(".brk");
  if (e.target.classList.contains("x")) {
    pushUndo();
    setBreaks(breaks.filter((_, j) => j !== +brk.dataset.i));
    return;
  }
  if (brk) {
    pushUndo();
    drag = { i: +brk.dataset.i, moved: false };
    brk.setPointerCapture(e.pointerId);
    e.preventDefault();
    return;
  }
  pushUndo();
  setBreaks([...breaks, docYFromEvent(e)]);
});
$("editorDoc").addEventListener("change", (e) => {
  const i = e.target.dataset.seg;
  if (i === undefined) return;
  const s = segments[+i];
  pushUndo();
  if (e.target.checked) excluded = excluded.filter((y) => y < s.a || y >= s.b);
  else excluded.push((s.box.y0 + s.box.y1) / 2);
  update();
});
$("editorDoc").addEventListener("pointermove", (e) => {
  if (!doc) return;
  const g = $("ghost");
  if (drag) {
    breaks[drag.i] = docYFromEvent(e);
    drag.moved = true;
    const b = $("editorDoc").querySelector(`.brk[data-i="${drag.i}"]`);
    if (b) b.style.top = breaks[drag.i] * edScale + "px";
    g.style.display = "none";
    return;
  }
  const overBreak = e.target.closest(".brk, .pick");
  g.style.display = overBreak ? "none" : "block";
  g.style.top = docYFromEvent(e) * edScale + "px";
});
$("editorDoc").addEventListener("pointerup", () => {
  if (!drag) return;
  if (!drag.moved) undoStack.pop();
  drag = null;
  setBreaks(breaks);
});
$("editorDoc").addEventListener("pointerleave", () => ($("ghost").style.display = "none"));

/* ---------- preview ---------- */
function buildPreview() {
  const pv = $("preview");
  pv.innerHTML = "";
  const pages = outputPages();
  const dispW = Math.min(700, $("previewScroll").clientWidth - 60);
  pvScale = dispW / PAGE_W;
  const dpr = window.devicePixelRatio || 1;
  pages.forEach((s, n) => {
    const wrap = el("div", "pv-page");
    const c = document.createElement("canvas");
    c.width = Math.round(PAGE_W * pvScale * dpr); c.height = Math.round(PAGE_H * pvScale * dpr);
    c.style.width = PAGE_W * pvScale + "px"; c.style.height = PAGE_H * pvScale + "px";
    const ctx = c.getContext("2d");
    ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, c.width, c.height);
    const k = pvScale * dpr;   // canvas px per output pt
    drawSegment(ctx, s, k);
    const lab = el("div", "pv-label");
    lab.textContent = `Page ${n + 1} · ${Math.round(s.scale * 100)}%`;
    wrap.append(c, lab);
    wrap.seg = s;
    s.el = wrap;
    pv.appendChild(wrap);
  });
  $("pageCount").textContent = `${pages.length} page${pages.length === 1 ? "" : "s"}`;
  $("downloadBtn").disabled = !pages.length;
  $("clearBtn").disabled = !breaks.length;
}

// Paint a segment's ink box from the rendered tiles onto a preview canvas.
function drawSegment(ctx, s, k) {
  const { box } = s;
  ctx.imageSmoothingQuality = "high";
  for (const p of doc.pages) {
    for (const t of p.tiles) {
      const tTop = p.top + t.y0 / p.scale, tBot = tTop + t.rows / p.scale;
      const y0 = Math.max(box.y0, tTop), y1 = Math.min(box.y1, tBot);
      if (y1 <= y0) continue;
      const x0 = box.x0, x1 = Math.min(box.x1, p.w);
      if (x1 <= x0) continue;
      ctx.drawImage(t.canvas,
        x0 * p.scale, (y0 - tTop) * p.scale, (x1 - x0) * p.scale, (y1 - y0) * p.scale,
        (s.ox + (x0 - box.x0) * s.scale) * k, (s.oy + (y0 - box.y0) * s.scale) * k,
        (x1 - x0) * s.scale * k, (y1 - y0) * s.scale * k);
    }
  }
}

/* ---------- sync scroll ---------- */
// Map the doc-y at the editor's viewport center to the matching spot in the preview, and back.
let activePane = "editor";
for (const [id, name] of [["editorScroll", "editor"], ["previewScroll", "preview"]]) {
  const n = $(id);
  for (const ev of ["pointerenter", "wheel", "touchstart", "keydown"]) n.addEventListener(ev, () => (activePane = name), { passive: true });
  n.addEventListener("scroll", () => {
    if (!doc || activePane !== name) return;
    const other = $(name === "editor" ? "previewScroll" : "editorScroll");
    const max = n.scrollHeight - n.clientHeight;
    if (n.scrollTop <= 0) other.scrollTop = 0;                      // pin the ends together
    else if (n.scrollTop >= max - 1) other.scrollTop = other.scrollHeight;
    else name === "editor" ? syncPreviewFromEditor() : syncEditorFromPreview();
  }, { passive: true });
}

function editorCenterY() {
  const sc = $("editorScroll"), ed = $("editorDoc");
  return (sc.scrollTop + sc.clientHeight / 2 - ed.offsetTop) / edScale;
}

function syncPreviewFromEditor() {
  const y = editorCenterY();
  const pages = outputPages();
  if (!pages.length) return;
  // the page whose slice contains y, else the nearest one
  let s = pages.find((p) => y >= p.a && y < p.b) || pages.reduce((best, p) =>
    Math.min(Math.abs(y - p.a), Math.abs(y - p.b)) < Math.min(Math.abs(y - best.a), Math.abs(y - best.b)) ? p : best);
  const yy = Math.max(s.box.y0, Math.min(s.box.y1, y));
  const off = (s.oy + (yy - s.box.y0) * s.scale) * pvScale;
  const sc = $("previewScroll");
  sc.scrollTop = s.el.offsetTop + off - sc.clientHeight / 2;
}

function syncEditorFromPreview() {
  const sc = $("previewScroll");
  const c = sc.scrollTop + sc.clientHeight / 2;
  const pages = outputPages();
  if (!pages.length) return;
  let s = pages[0];
  for (const p of pages) if (p.el.offsetTop <= c) s = p;
  const off = (c - s.el.offsetTop) / pvScale;
  const y = Math.max(s.box.y0, Math.min(s.box.y1, s.box.y0 + (off - s.oy) / s.scale));
  const es = $("editorScroll");
  es.scrollTop = $("editorDoc").offsetTop + y * edScale - es.clientHeight / 2;
}

/* ---------- export (vector, via pdf-lib) ---------- */
$("downloadBtn").addEventListener("click", async () => {
  const btn = $("downloadBtn");
  btn.disabled = true; btn.textContent = "Building…";
  try {
    const { PDFDocument, pushGraphicsState, popGraphicsState, rectangle, clip, endPath } = PDFLib;
    const out = await PDFDocument.create();
    const srcDocs = await Promise.all(doc.files.map((b) => PDFDocument.load(b, { ignoreEncryption: true })));
    const embedded = new Map();   // one embed per source page, reused by every output page
    const embedFor = async (p) => {
      const key = p.fileIdx + ":" + p.pageIdx;
      if (!embedded.has(key)) {
        const src = srcDocs[p.fileIdx].getPage(p.pageIdx);
        const mb = src.getMediaBox(), cb = src.getCropBox();
        const emb = await out.embedPage(src, { left: mb.x, bottom: mb.y, right: mb.x + mb.width, top: mb.y + mb.height });
        embedded.set(key, { emb, mb, cb });
      }
      return embedded.get(key);
    };
    for (const s of outputPages()) {
      const page = out.addPage([PAGE_W, PAGE_H]);
      const { box } = s;
      for (const p of doc.pages) {
        const ya = Math.max(box.y0, p.top), yb = Math.min(box.y1, p.top + p.h);
        const x1 = Math.min(box.x1, p.w);
        if (yb <= ya || x1 <= box.x0) continue;
        const { emb, mb, cb } = await embedFor(p);
        // clip rectangle in the source page's PDF coordinates (y up, from the crop box's top-left)
        const left = cb.x + box.x0, right = cb.x + x1;
        const top = cb.y + cb.height - (ya - p.top), bottom = cb.y + cb.height - (yb - p.top);
        // where that rectangle lands on the output page
        const ox = s.ox, oBottom = PAGE_H - (s.oy + (yb - box.y0) * s.scale);
        page.pushOperators(pushGraphicsState(), rectangle(ox, oBottom, (right - left) * s.scale, (top - bottom) * s.scale), clip(), endPath());
        page.drawPage(emb, { x: ox - (left - mb.x) * s.scale, y: oBottom - (bottom - mb.y) * s.scale, xScale: s.scale, yScale: s.scale });
        page.pushOperators(popGraphicsState());
      }
    }
    const bytes = await out.save();
    const url = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
    const a = el("a"); a.href = url; a.download = `${doc.name}_letter.pdf`;
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
  } catch (err) {
    console.error(err);
    setStatus("Export failed: " + err.message);
  } finally {
    btn.disabled = false; btn.textContent = "Download PDF";
  }
});

/* ---------- misc wiring ---------- */
function update() {
  if (!doc) return;
  computeSegments();
  drawEditorOverlays();
  buildPreview();
  syncPreviewFromEditor();   // rebuilt preview lost its scroll position
}
function setStatus(t) { $("status").textContent = t; }

$("fileInput").addEventListener("change", (e) => { loadFiles(e.target.files); e.target.value = ""; });
$("uniform").addEventListener("change", update);
$("alignLeft").addEventListener("change", update);

function readMargin() {
  const on = $("marginOn").checked;
  $("marginIn").disabled = !on;
  const inches = Math.max(0, Math.min(3, parseFloat($("marginIn").value) || 0));
  MARGIN = on ? inches * 72 : 0;
  AREA_W = PAGE_W - 2 * MARGIN; AREA_H = PAGE_H - 2 * MARGIN;
  try { localStorage.setItem("pdf-splitter.margin", JSON.stringify({ on, inches })); } catch {}
}
try {
  const m = JSON.parse(localStorage.getItem("pdf-splitter.margin"));
  if (m) { $("marginOn").checked = m.on; $("marginIn").value = m.inches; }
} catch {}
readMargin();
for (const id of ["marginOn", "marginIn"]) $(id).addEventListener("input", () => { readMargin(); update(); });
$("clearBtn").addEventListener("click", () => { pushUndo(); setBreaks([]); });
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "z" && undoStack.length && doc) {
    e.preventDefault();
    ({ breaks, excluded } = undoStack.pop());
    update();
  }
});

let dragDepth = 0;
window.addEventListener("dragenter", (e) => { e.preventDefault(); dragDepth++; document.body.classList.add("dragging"); });
window.addEventListener("dragleave", () => { if (--dragDepth <= 0) { dragDepth = 0; document.body.classList.remove("dragging"); } });
window.addEventListener("dragover", (e) => e.preventDefault());
window.addEventListener("drop", (e) => {
  e.preventDefault(); dragDepth = 0; document.body.classList.remove("dragging");
  if (e.dataTransfer.files.length) loadFiles(e.dataTransfer.files);
});

let resizeTimer;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => { if (doc) { layoutEditor(); buildPreview(); } }, 120);
});
