// Deadlines are advisory. Only native command forms record session progress.
const instances = new Map();
const storagePrefix = 'pk-guided-v1:';
const elapsedOrigin = Date.now() - performance.now();
const now = () => elapsedOrigin + performance.now();
const clock = seconds => {
  seconds = Math.max(0, Math.ceil(seconds));
  const tail = String(seconds % 60).padStart(2, '0');
  const minutes = Math.floor(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}:${String(minutes % 60).padStart(2, '0')}:${tail}` : `${minutes}:${tail}`;
};
function timerStorage(prefix, scope, notice, words) {
  const memory = new Map();
  function store(key, value) {
    memory.set(key, value);
    try { sessionStorage.setItem(prefix + key, JSON.stringify(value)); }
    catch { notice(words('storage_unavailable')); }
  }
  function read(key, seconds) {
    try {
      const raw = sessionStorage.getItem(prefix + key);
      if (raw) memory.set(key, JSON.parse(raw));
    } catch { notice(words('storage_unavailable')); }
    const value = memory.get(key);
    if (!value || value.seconds !== seconds || !Number.isFinite(value.deadline) || value.deadline < now() - 86400000 || value.deadline > now() + 86400000) return null;
    return value;
  }
  function remove(key) {
    memory.delete(key);
    try { sessionStorage.removeItem(prefix + key); } catch { /* Memory still clears. */ }
  }
  function purge(wholeScope = false) {
    try {
      for (const key of Object.keys(sessionStorage)) {
        if (key.startsWith(wholeScope ? storagePrefix : prefix)) sessionStorage.removeItem(key);
      }
    } catch { /* Storage can be denied independently of navigation. */ }
    memory.clear();
  }
  try {
    for (const key of Object.keys(sessionStorage)) {
      if (key.startsWith(storagePrefix) && !key.startsWith(storagePrefix + scope + '/')) sessionStorage.removeItem(key);
    }
  } catch { notice(words('storage_unavailable')); }
  return {store, read, remove, purge};
}
function timerButton(signal, label, run) {
  const node = document.createElement('button'); node.type = 'button'; node.textContent = label;
  node.addEventListener('click', run, {signal}); return node;
}
function namedTimers(root, cfg, timers, list, enable, words, read, store, remove, prepareAudio, tick, signal) {
  cfg.session.steps.forEach((step, i) => step.sets.forEach((set, j) => {
    if (!step.timer) return;
    const key = `${step.id}/${set}/named`, seconds = step.timer.seconds;
    const line = document.createElement('div'), label = document.createElement('a');
    label.href = `${cfg.base}/steps/${step.id}/sets/${set}`;
    const timer = {key, seconds, name:step.timer.name, value:read(key, seconds), line, label};
    const start = () => {
      if (timer.value && timer.value.deadline > now()) return;
      Promise.resolve(prepareAudio()).catch(() => { enable.hidden = false; });
      timer.value = {seconds, deadline:now()+seconds*1000, notified:false};
      store(key, timer.value); tick();
    };
    const cancel = timerButton(signal, words('cancel_timer', {name:timer.name}), () => { timer.value = null; remove(key); tick(); });
    const restart = timerButton(signal, words('restart_timer', {name:timer.name}), start);
    line.append(label, cancel, restart); list.append(line);
    timers.set(key, timer);
    if (i === cfg.Step && j === cfg.Set) root.querySelector('[data-guided-start]')?.addEventListener('click', start, {signal});
  }));
}
function install(root) {
  if (instances.has(root)) return;
  const cfg = JSON.parse(root.dataset.guided);
  const scope = location.origin + '/' + cfg.scope;
  const prefix = storagePrefix + scope + '/' + cfg.session.id + '/';
  const timers = new Map();
  const events = new AbortController();
  let disposed = false, sentinel = null, audio = null;
  const status = root.querySelector('[data-guided-status]');
  const words = (key, values = {}) => cfg.labels[key].replace(/\{([^}]+)\}/g, (slot, key) => Object.hasOwn(values, key) ? values[key] : slot);
  const notice = text => { status.textContent = text; };
  const {store, read, remove, purge} = timerStorage(prefix, scope, notice, words);
  function prepareAudio() {
    const Audio = window.AudioContext || window.webkitAudioContext;
    if (!Audio) return;
    audio ||= new Audio();
    return audio.resume();
  }
  function sound() {
    if (!audio || audio.state !== 'running') { enable.hidden = false; return; }
    const oscillator = audio.createOscillator(), gain = audio.createGain();
    oscillator.connect(gain); gain.connect(audio.destination);
    gain.gain.setValueAtTime(0.12, audio.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.3);
    oscillator.start(); oscillator.stop(audio.currentTime + 0.3);
  }
  const list = root.querySelector('[data-guided-timers]');
  const enable = document.createElement('button');
  enable.type = 'button'; enable.textContent = words('enable_sound'); enable.hidden = true;
  enable.addEventListener('click', () => Promise.resolve(prepareAudio()).then(() => { enable.hidden = audio?.state !== 'running'; }).catch(() => { enable.hidden = false; }), {signal:events.signal});
  list.append(enable);
  namedTimers(root, cfg, timers, list, enable, words, read, store, remove, prepareAudio, tick, events.signal);
  const rest = root.querySelector('[data-guided-rest]');
  let restValue = null, restKey = '';
  if (rest) {
    const step = cfg.session.steps[cfg.Step], set = step.sets[cfg.Set];
    restKey = `${step.id}/${set}/rest`;
    const seconds = Number(rest.dataset.guidedSeconds), declared = cfg.Set+1 < step.sets.length ? step.restBetweenSeconds : step.restAfterSeconds;
    restValue = read(restKey, declared);
    if (!restValue) restValue = {seconds:declared, deadline:Number(rest.dataset.guidedRest) || now()+seconds*1000, notified:false};
    if (rest.dataset.guidedRest) restValue.deadline = Math.max(restValue.deadline,Number(rest.dataset.guidedRest));
    store(restKey,restValue);
    root.querySelector('[data-guided-add]')?.addEventListener('click', event => {
      event.preventDefault();
      restValue.deadline = Math.min(now()+86400000, Math.max(now(),restValue.deadline)+15000);
      restValue.notified = false; store(restKey,restValue); notice(words('rest_added')); tick();
    }, {signal:events.signal});
  }
  function tick() {
    if (disposed || document.hidden) return;
    for (const timer of timers.values()) {
      const value = timer.value;
      timer.line.hidden = !value;
      if (!value) continue;
      const remaining = Math.max(0,Math.ceil((value.deadline-now())/1000));
      timer.label.textContent = remaining ? words('running_timer',{name:timer.name,duration:clock(remaining)}) : words('timer_ended',{name:timer.name});
      if (!remaining && !value.notified) { value.notified = true; store(timer.key,value); notice(timer.label.textContent); sound(); }
    }
    if (restValue) {
      const remaining = Math.max(0, Math.ceil((restValue.deadline-now())/1000));
      rest.querySelector('[data-guided-value]').textContent = clock(remaining);
      if (!remaining && !restValue.notified) { restValue.notified = true; store(restKey,restValue); notice(words('rest_ended')); root.querySelector('[data-guided-skip]').textContent = words('continue'); }
    }
  }
  async function wake() {
    if (disposed || document.hidden || cfg.session.progress?.finished) return;
    try {
      if (!navigator.wakeLock) throw new Error('unavailable');
      const acquired = await navigator.wakeLock.request('screen');
      if (disposed || document.hidden) await acquired.release();
      else { sentinel = acquired; acquired.addEventListener('release', () => { if (sentinel === acquired) sentinel = null; }); }
    } catch { notice(words('wake_unavailable')); }
  }
  function release() { sentinel?.release().catch(() => {}); sentinel = null; }
  function dispose(purgeScope = false) {
    if (disposed) return;
    disposed = true; clearInterval(interval); events.abort(); release(); audio?.close().catch(() => {});
    if (purgeScope) purge(true);
    instances.delete(root);
  }
  root.addEventListener('keydown', event => {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || event.repeat || event.isComposing || event.target.closest('input,button,a,textarea,select,[contenteditable],audio,video,[role=button]')) return;
    if (event.key === ' ' && rest) { event.preventDefault(); root.querySelector('[data-guided-skip]').click(); }
    else if (event.key === ' ' && root.querySelector('[data-guided-start]')) { event.preventDefault(); root.querySelector('[data-guided-start]').click(); }
    else if (cfg.mode === 'step' && ['ArrowLeft','ArrowRight'].includes(event.key)) {
      const index = cfg.Step + (event.key === 'ArrowLeft' ? -1 : 1), step = cfg.session.steps[index];
      if (step) { event.preventDefault(); location.assign(`${cfg.base}/steps/${step.id}/sets/${step.sets[0]}`); }
    }
  }, {signal:events.signal});
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) release();
    else { root.hidden = true; location.reload(); }
  }, {signal:events.signal});
  window.addEventListener('pagehide', () => dispose(), {signal:events.signal});
  document.addEventListener('htmx:responseError', event => { if ([401,403,404].includes(event.detail.xhr.status)) dispose(true); }, {signal:events.signal});
  const interval = setInterval(tick,250);
  instances.set(root,dispose);
  if (cfg.session.progress?.finished) { purge(); for (const timer of timers.values()) timer.value = null; }
  root.querySelector('[data-guided-heading]')?.focus({preventScroll:true});
  tick(); wake();
}
function scan() {
  for (const [root,dispose] of instances) if (!root.isConnected) dispose();
  document.querySelectorAll('[data-guided]').forEach(install);
}
window.addEventListener('pageshow', event => {
  if (event.persisted && document.querySelector('[data-guided]')) {
    document.querySelector('[data-guided]').hidden = true; location.reload();
  }
});
scan();
new MutationObserver(scan).observe(document.body,{childList:true,subtree:true});
