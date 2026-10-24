// Pointer and canvas lifecycle adapted from platformkit-frontend-kit's Apache-2.0
// signature controller. Stroke values, keyboard input and history are owned here.
const mounted = new Map();
const copy = value => value === null ? null : value.map(stroke => stroke.map(point => [...point]));

function readValue(raw, limits) {
  if (raw.trim() !== 'null' && /[^\s\[\],0-9-]/.test(raw)) throw new Error('shape');
  const value = JSON.parse(raw);
  if (value === null) return null;
  if (!Array.isArray(value) || value.length > limits.strokes) throw new Error('strokes');
  let count = 0;
  for (const stroke of value) {
    if (!Array.isArray(stroke) || !stroke.length) throw new Error('stroke');
    count += stroke.length;
    let previous = 0;
    for (let i = 0; i < stroke.length; i++) {
      const point = stroke[i];
      if (!Array.isArray(point) || point.length !== 3 || !point.every(Number.isSafeInteger)) throw new Error('point');
      const [x, y, ms] = point;
      if (x < 0 || y < 0 || x > limits.coordinate || y > limits.coordinate || ms < 0 || ms > limits.milliseconds || (i === 0 && ms !== 0) || ms < previous) throw new Error('range');
      previous = ms;
    }
  }
  if (count > limits.points) throw new Error('points');
  return value;
}

class SketchEditor {
  constructor(root, canvas, context) {
    this.root = root;
    this.canvas = canvas;
    this.context = context;
    this.input = root.querySelector('[data-sketch-value]');
    this.initial = this.input.value;
    this.limits = Object.fromEntries(['strokes', 'points', 'coordinate', 'milliseconds'].map(key => [key, Number(root.getAttribute('data-sketch-' + key))]));
    this.status = root.querySelector('[data-sketch-status]');
    this.undoButton = root.querySelector('[data-sketch-action="undo"]');
    this.clearButton = root.querySelector('[data-sketch-action="clear"]');
    this.cursor = [500, 500];
    this.backup = null;
    this.active = null;
    this.abort = new AbortController();
    this.load(this.initial);
    this.bind();
    canvas.hidden = false;
    root.querySelector('[data-sketch-actions]').hidden = false;
    root.querySelector('[data-sketch-fallback]').hidden = true;
    root.querySelector('[data-sketch-preview]').hidden = true;
    this.observer = new ResizeObserver(() => this.draw());
    this.observer.observe(canvas);
    this.draw();
    if (document.activeElement === root) canvas.focus();
  }

  load(raw) {
    try { this.value = readValue(raw, this.limits); this.invalid = false; }
    catch { this.value = null; this.invalid = true; this.say('unavailable'); }
    this.buttons();
  }

  on(target, name, action, options = {}) {
    target?.addEventListener(name, action, { ...options, signal: this.abort.signal });
  }

  bind() {
    this.on(this.canvas, 'pointerdown', event => this.down(event));
    this.on(this.canvas, 'pointermove', event => this.move(event));
    this.on(this.canvas, 'pointerup', event => this.up(event));
    this.on(this.canvas, 'pointercancel', event => { if (this.active?.pointer === event.pointerId) this.cancel(); });
    this.on(this.canvas, 'lostpointercapture', event => { if (this.active?.pointer === event.pointerId) this.cancel(); });
    this.on(this.canvas, 'blur', () => this.cancel());
    this.on(window, 'blur', () => this.cancel());
    this.on(this.canvas, 'focus', () => this.draw());
    this.on(this.canvas, 'keydown', event => this.key(event));
    this.on(this.root, 'keydown', event => {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'z' && !event.target.matches('input, textarea, [contenteditable]')) {
        event.preventDefault(); this.undo();
      }
    });
    this.on(this.undoButton, 'click', () => this.undo());
    this.on(this.clearButton, 'click', () => this.clear());
    this.on(this.input.form, 'reset', event => {
      const changed = this.input.value !== this.initial;
      queueMicrotask(() => {
        if (event.defaultPrevented || !this.root.isConnected) return;
        this.cancel(false); this.backup = null; this.input.value = this.initial;
        this.cursor = [500, 500]; this.load(this.initial); this.draw();
        if (changed) this.notify();
      });
    });
    this.on(this.input.form, 'submit', event => { if (!this.finish()) event.preventDefault(); }, { capture: true });
  }

  say(key) { this.status.textContent = this.root.getAttribute('data-sketch-' + key) || ''; }
  buttons() {
    this.undoButton.disabled = this.invalid || (!(this.value?.length) && !this.backup);
    this.clearButton.disabled = !this.invalid && !(this.value?.length);
    this.canvas.setAttribute('aria-disabled', String(this.invalid));
  }
  notify() {
    this.input.dispatchEvent(new Event('input', { bubbles: true }));
    this.input.dispatchEvent(new Event('change', { bubbles: true }));
  }
  change(key) {
    this.input.value = JSON.stringify(this.value);
    this.notify();
    this.buttons(); this.draw(); this.say(key);
  }

  point(event, initial = false) {
    const rect = this.canvas.getBoundingClientRect();
    if (!this.canvas.isConnected || rect.width <= 0 || rect.height <= 0 || ![event.clientX, event.clientY, event.timeStamp].every(Number.isFinite)) return null;
    const x = (event.clientX - rect.left) * this.limits.coordinate / rect.width;
    const y = (event.clientY - rect.top) * this.limits.coordinate / rect.height;
    if (initial && (x < 0 || y < 0 || x > this.limits.coordinate || y > this.limits.coordinate)) return null;
    return [x, y].map(n => Math.max(0, Math.min(this.limits.coordinate, Math.round(n))));
  }

  start(point, time, pointer = null) {
    if (this.invalid || this.active || !Number.isFinite(time) || !this.canvas.isConnected || this.canvas.getBoundingClientRect().width <= 0) return false;
    if ((this.value?.length || 0) >= this.limits.strokes) { this.say('stroke-limit'); return false; }
    const count = (this.value || []).reduce((n, stroke) => n + stroke.length, 0);
    if (count >= this.limits.points) { this.say('point-limit'); return false; }
    this.active = { points: [[...point, 0]], time, pointer, count };
    this.say('started'); this.draw(); return true;
  }

  append(point, time) {
    if (!this.active || !point || !Number.isFinite(time)) return false;
    const last = this.active.points.at(-1);
    const ms = Math.max(last[2], Math.floor(time - this.active.time), 0);
    if (ms > this.limits.milliseconds) { this.cancel(false); this.say('time-limit'); return false; }
    if (point[0] === last[0] && point[1] === last[1]) return true;
    if (this.active.count + this.active.points.length >= this.limits.points) { this.cancel(false); this.say('point-limit'); return false; }
    this.active.points.push([...point, ms]); this.draw(); return true;
  }

  down(event) {
    if (!event.isPrimary || event.button !== 0 || this.active) return;
    const point = this.point(event, true);
    if (!point || !this.start(point, event.timeStamp, event.pointerId)) return;
    event.preventDefault(); this.canvas.focus();
    try { this.canvas.setPointerCapture(event.pointerId); } catch { this.cancel(); }
  }
  move(event) {
    if (!this.active || this.active.pointer !== event.pointerId) return;
    event.preventDefault(); this.append(this.point(event), event.timeStamp);
  }
  up(event) {
    if (!this.active || this.active.pointer !== event.pointerId) return;
    event.preventDefault();
    if (this.append(this.point(event), event.timeStamp)) this.finish();
  }
  release() {
    const pointer = this.active?.pointer;
    this.active = null;
    if (pointer !== null && pointer !== undefined && this.canvas.hasPointerCapture(pointer)) this.canvas.releasePointerCapture(pointer);
  }
  cancel(announce = true) {
    if (!this.active) return;
    this.release(); this.draw(); if (announce) this.say('canceled');
  }
  finish() {
    if (!this.active) return true;
    if (!this.root.isConnected) { this.cancel(false); return false; }
    const stroke = this.active.points;
    this.value = [...(this.value || []), stroke]; this.backup = null;
    this.release(); this.change('finished'); return true;
  }
  undo() {
    this.cancel(false);
    if (this.invalid) return;
    if (this.value?.length) this.value = this.value.slice(0, -1);
    else if (this.backup) { this.value = this.backup; this.backup = null; }
    else return;
    this.change('undone');
  }
  clear() {
    this.cancel(false);
    if (!this.invalid && !this.value?.length) return;
    this.backup = this.invalid ? null : copy(this.value);
    this.invalid = false; this.value = []; this.change('cleared');
  }

  key(event) {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === 'Escape' || event.key === 'Tab') {
      if (event.key === 'Escape') event.preventDefault();
      this.cancel(); return;
    }
    if (this.invalid || (this.active && this.active.pointer !== null)) return;
    if (event.key === ' ' || event.key === 'Enter') {
      event.preventDefault();
      if (!event.repeat) { if (this.active) this.finish(); else this.start(this.cursor, event.timeStamp); }
      return;
    }
    const move = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[event.key];
    if (!move) return;
    event.preventDefault();
    this.cursor = this.cursor.map((n, i) => Math.max(0, Math.min(this.limits.coordinate, n + move[i] * (event.shiftKey ? 1 : 10))));
    if (this.active) this.append(this.cursor, event.timeStamp);
    this.draw();
  }

  draw() {
    const rect = this.canvas.getBoundingClientRect();
    const ratio = window.devicePixelRatio || 1;
    this.canvas.width = Math.max(1, Math.round(rect.width * ratio));
    this.canvas.height = Math.max(1, Math.round(rect.height * ratio));
    const ctx = this.context, square = this.limits.coordinate;
    ctx.setTransform(this.canvas.width / square, 0, 0, this.canvas.height / square, 0, 0);
    ctx.clearRect(0, 0, square, square);
    ctx.strokeStyle = ctx.fillStyle = getComputedStyle(this.canvas).color;
    ctx.lineWidth = 12; ctx.lineCap = ctx.lineJoin = 'round';
    for (const stroke of [...(this.value || []), ...(this.active ? [this.active.points] : [])]) {
      ctx.beginPath();
      if (stroke.length === 1) { ctx.arc(stroke[0][0], stroke[0][1], 6, 0, Math.PI * 2); ctx.fill(); }
      else { ctx.moveTo(stroke[0][0], stroke[0][1]); for (const point of stroke.slice(1)) ctx.lineTo(point[0], point[1]); ctx.stroke(); }
    }
    if (document.activeElement === this.canvas && !this.invalid) {
      ctx.beginPath(); ctx.lineWidth = 2;
      ctx.arc(this.cursor[0], this.cursor[1], 12, 0, Math.PI * 2); ctx.stroke();
    }
  }

  destroy() {
    this.cancel(false); this.abort.abort(); this.observer.disconnect();
    mounted.delete(this.root);
  }
}

export function mountSketch(root) {
  if (mounted.has(root)) return mounted.get(root);
  const canvas = root.querySelector('canvas');
  let context;
  try { context = canvas?.getContext?.('2d'); } catch { return null; }
  if (!context) return null;
  const editor = new SketchEditor(root, canvas, context);
  mounted.set(root, editor); return editor;
}

function scan(node) {
  if (node.nodeType !== 1 && node.nodeType !== 9) return;
  if (node.matches?.('[data-sketch]')) mountSketch(node);
  node.querySelectorAll('[data-sketch]').forEach(mountSketch);
}

scan(document);
new MutationObserver(records => {
  for (const record of records) for (const node of record.addedNodes) scan(node);
  for (const [root, editor] of mounted) if (!root.isConnected) editor.destroy();
}).observe(document.documentElement, { childList: true, subtree: true });
