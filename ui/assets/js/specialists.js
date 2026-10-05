// Optional calendar/map engines enhance a complete server-rendered agenda/list.
(function () {
  const base = new URL('.', document.currentScript.src);
  const loads = new Map();
  const instances = new Map();
  const pending = new WeakSet();
  function asset(path, css) {
    if (loads.has(path)) return loads.get(path);
    const promise = new Promise((resolve, reject) => {
      const node = document.createElement(css ? 'link' : 'script');
      if (css) { node.rel = 'stylesheet'; node.href = new URL(path, base); }
      else node.src = new URL(path, base);
      node.onload = resolve;
      node.onerror = reject;
      document.head.append(node);
    });
    loads.set(path, promise);
    return promise;
  }
  async function calendar(root) {
    const config = JSON.parse(root.dataset.calendarConfig);
    await Promise.all([asset('fullcalendar-7.1.0.min.js'), asset('../css/fullcalendar-7.1.0.css', true)]);
    await Promise.all([asset('fullcalendar-classic-7.1.0.min.js'), asset('fullcalendar-locales-7.1.0.min.js')]);
    if (!root.isConnected) return;
    const engine = new FullCalendar.Calendar(root, {
      themeSystem: 'classic', initialView: config.view === 'week' ? 'timeGridWeek' : 'timeGridDay',
      initialDate: config.date, timeZone: config.zone, locale: config.locale, firstDay: config.firstDay,
      now: config.now, headerToolbar: false, editable: false, selectable: false,
      eventStartEditable: false, eventDurationEditable: false, nowIndicator: false,
      height: 384, allDayText: config.allDay, events: config.events,
      validRange: { start: config.start, end: config.end },
      eventMinHeight: 44, eventClass: 'pk-calendar-event',
      eventContent: ({ event }) => ({ domNodes: [document.createTextNode(
        event.title + ' · ' + event.extendedProps.timeText + ' · ' + event.extendedProps.statusLabel)] }),
    });
    instances.set(root, () => engine.destroy());
    engine.render();
    root.dataset.engineReady = 'true';
    root.parentElement.querySelector('[data-calendar-fallback]').hidden = true;
    root.closest('details')?.addEventListener('toggle', () => { if (root.isConnected) engine.updateSize(); });
  }
  async function map(root) {
    const config = JSON.parse(root.dataset.mapConfig);
    const fallback = root.parentElement.querySelector('[data-map-fallback]');
    const polar = Math.abs(config.viewport.latitude) > 85.0511287798 || config.points.some(p => Math.abs(p.latitude) > 85.0511287798);
    if (!config.tiles || polar) return; // Keep exact polar locations in the list.
    await Promise.all([asset('leaflet-1.9.4.min.js'), asset('../css/leaflet-1.9.4.css', true)]);
    if (!root.isConnected) return;
    const engine = L.map(root, { zoomControl: false, attributionControl: false, zoomAnimation: false, fadeAnimation: false, markerZoomAnimation: false, keyboard: true });
    instances.set(root, () => engine.remove());
    engine.setView([config.viewport.latitude, config.viewport.longitude], config.viewport.zoom);
    L.control.zoom({ zoomInTitle: config.zoomIn, zoomOutTitle: config.zoomOut }).addTo(engine);
    const layer = L.tileLayer(config.tiles.urlTemplate, { minZoom: config.tiles.minZoom, maxZoom: config.tiles.maxZoom });
    let failed = false;
    layer.on('tileerror', () => { failed = true; fallback.hidden = false; });
    layer.on('load', () => { fallback.hidden = !failed; });
    layer.addTo(engine);
    for (const point of config.points) {
      const legend = config.legend.find(entry => entry.key === point.statusKey);
      // A point with no address is a place to read, not a place to go.
      const marker = document.createElement(point.href ? 'a' : 'span');
      if (point.href) { marker.href = point.href; } else { marker.setAttribute('role', 'img'); }
      marker.className = 'pk-map-marker';
      marker.dataset.tone = legend.tone || 'neutral';
      marker.setAttribute('aria-label', point.title + ' · ' + point.statusText);
      marker.textContent = { circle: '●', square: '■', triangle: '▲' }[legend.symbol];
      if (point.id === config.selected) marker.setAttribute('aria-current', 'true');
      // Leaflet owns projection; native anchors own each authorized destination.
      L.marker([point.latitude, point.longitude], { keyboard: false,
        icon: L.divIcon({ html: marker, className: 'pk-map-pin', iconSize: [44, 44] }) }).addTo(engine);
    }
    root.dataset.engineReady = 'true';
  }
  function cleanup(removed) {
    for (const [root, destroy] of instances) {
      if (!root.isConnected || removed === root || removed?.contains(root)) { destroy(); instances.delete(root); }
    }
  }
  function init() {
    cleanup();
    for (const root of document.querySelectorAll('[data-calendar-engine], [data-map-engine]')) {
      if (pending.has(root)) continue;
      pending.add(root);
      (root.hasAttribute('data-calendar-engine') ? calendar(root) : map(root)).catch(() => {
        const fallback = root.parentElement.querySelector('[data-calendar-fallback], [data-map-fallback]');
        if (fallback) fallback.hidden = false;
        const destroy = instances.get(root);
        if (destroy) { destroy(); instances.delete(root); }
      });
    }
  }
  document.addEventListener('htmx:beforeCleanupElement', event => cleanup(event.detail.elt));
  document.addEventListener('htmx:afterSwap', init);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init); else init();
})();
