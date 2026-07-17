let cpuChart, memChart, gpuChart, netChart, diskChart;
let coreCells = [], hasGPU = false;

// Sets the three labels of an auto-scaled chart's Y gutter. Fixed-scale charts
// (CPU, Memory, GPU) have static labels in the HTML instead.
function setRateGutter(id, ymax) {
  const g = $(id);
  g.children[0].textContent = fmtRate(ymax);
  g.children[1].textContent = fmtRate(ymax / 2);
  g.children[2].textContent = '0';
}

// Charts are created from the first snapshot, because the core count and
// whether a GPU exists are only knowable from real data.
function initCharts(s) {
  const cores = (s.cpu.cores || []).length;
  hasGPU = !!(s.gpu && s.gpu.length);
  // No GPU: drop the card entirely rather than render one full of dashes. The
  // remaining four cards take its space via flex:1.
  if (!hasGPU) { const c = $('card-gpu'); if (c) c.remove(); }

  cpuChart = new Chart($('cpuChart'), {
    series: Array.from({ length: cores }, (_, i) =>
      ({ color: `hsl(${(i * 360 / cores) | 0} 65% 55%)` })),
    yMax: 100,
  });
  memChart = new Chart($('memChart'), {
    series: [{ color: cssVar('--red') }, { color: cssVar('--green') }], yMax: 100, fill: true,
  });
  netChart = new Chart($('netChart'), {
    series: [{ color: cssVar('--blue') }, { color: cssVar('--amber') }], fill: true,
    onScale: m => setRateGutter('netGut', m),
  });
  diskChart = new Chart($('diskChart'), {
    series: [{ color: cssVar('--cyan') }, { color: cssVar('--purple') }], fill: true,
    onScale: m => setRateGutter('dskGut', m),
  });
  if (hasGPU) {
    gpuChart = new Chart($('gpuChart'), { series: [{ color: cssVar('--blue') }], yMax: 100, fill: true });
  }

  const grid = $('coreGrid');
  if (grid) {
    grid.innerHTML = ''; coreCells = [];
    for (let i = 0; i < cores; i++) {
      const cell = document.createElement('i');
      const bar = document.createElement('b');
      cell.appendChild(bar); grid.appendChild(cell); coreCells.push(bar);
    }
  }
}

function renderTopbar(s) {
  $('host').textContent = s.host.name;
  $('os').textContent = s.host.os || '';
  $('kernel').textContent = s.host.kernel || '';
  // Hide OS/kernel outright when the collector could not read them, rather
  // than leaving empty slots and stray dividers.
  $('meta').classList.toggle('no-os', !s.host.os && !s.host.kernel);
  $('uptime').textContent = fmtUptime(s.host.uptime);
  $('load').textContent = s.host.load.map(x => x.toFixed(2)).join(' ');
  $('clock').textContent = new Date(s.t * 1000).toLocaleTimeString();
}

function applySnap(s) {
  // Coalesce nil Go slices (JSON null) to [] so one missing collector can't
  // abort the whole render.
  s.cpu.cores = s.cpu.cores || [];
  s.disk.devs = s.disk.devs || [];
  s.net.ifaces = s.net.ifaces || [];
  s.gpu = s.gpu || [];
  s.fs = s.fs || [];
  s.proc = s.proc || [];
  if (!cpuChart) initCharts(s);
  renderTopbar(s);
}

function seedHistory(history) {
  if (!history.length) return;
  const first = history[0];
  first.cpu.cores = first.cpu.cores || [];
  first.gpu = first.gpu || [];
  if (!cpuChart) initCharts(first);
  applySnap(history[history.length - 1]);
}

function setConn(on) {
  const e = $('conn');
  e.className = on ? 'on' : 'off';
  e.lastElementChild.textContent = on ? 'Live' : 'Offline';
}

let ws, backoff = 500;
function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onopen = () => { backoff = 500; setConn(true); };
  ws.onmessage = ev => {
    const m = JSON.parse(ev.data);
    if (m.type === 'init') seedHistory(m.history || []);
    else if (m.type === 'tick') applySnap(m.snap);
  };
  ws.onclose = () => {
    setConn(false);
    setTimeout(connect, backoff); backoff = Math.min(backoff * 2, 5000);
  };
  ws.onerror = () => ws.close();
}
connect();
