const $ = id => document.getElementById(id);
const css = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

function fmtBytes(b) {
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB']; let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return b.toFixed(b >= 100 || i === 0 ? 0 : 1) + ' ' + u[i];
}
const fmtRate = b => fmtBytes(b) + '/s';
function fmtUptime(s) {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return (d ? d + 'd ' : '') + h + 'h ' + m + 'm';
}

let cpuChart, netChart, diskChart, gpuChart, coreCells = [];

function initCharts(coreCount) {
  cpuChart = new Chart($('cpuChart'), {
    series: Array.from({ length: coreCount }, (_, i) =>
      ({ color: `hsl(${(i * 360 / coreCount) | 0} 70% 60%)` })), yMax: 100 });
  netChart = new Chart($('netChart'), { series: [{ color: css('--rx') }, { color: css('--tx') }], fill: true });
  diskChart = new Chart($('diskChart'), { series: [{ color: css('--read') }, { color: css('--write') }], fill: true });
  gpuChart = new Chart($('gpuChart'), { series: [{ color: css('--accent') }], yMax: 100 });

  const grid = $('coreGrid'); grid.innerHTML = '';
  coreCells = [];
  for (let i = 0; i < coreCount; i++) {
    const cell = document.createElement('div'); cell.className = 'core-cell';
    const bar = document.createElement('i'); cell.appendChild(bar);
    grid.appendChild(cell); coreCells.push(bar);
  }
}

function applySnap(s) {
  if (!cpuChart) initCharts(s.cpu.cores.length);
  // CPU
  $('cpuAgg').textContent = s.cpu.agg.toFixed(0) + '%';
  $('cpuTemp').textContent = s.cpu.temp ? s.cpu.temp.toFixed(0) + '°C' : '';
  cpuChart.push(s.cpu.cores);
  s.cpu.cores.forEach((v, i) => { if (coreCells[i]) {
    coreCells[i].style.height = v + '%';
    coreCells[i].style.background = v > 80 ? css('--util') : css('--accent');
  }});
  // Mem
  $('memBar').style.width = s.mem.pct + '%';
  $('swapBar').style.width = s.mem.swap_pct + '%';
  $('memText').innerHTML = `<b>${fmtBytes(s.mem.used)}</b> (${s.mem.pct.toFixed(0)}%) of ${fmtBytes(s.mem.total)} · cache ${fmtBytes(s.mem.cache)}`;
  $('swapText').innerHTML = `Swap <b>${fmtBytes(s.mem.swap_used)}</b> (${s.mem.swap_pct.toFixed(0)}%) of ${fmtBytes(s.mem.swap_total)}`;
  // Net
  netChart.push([s.net.rx, s.net.tx]);
  $('netText').innerHTML = `<b style="color:${css('--rx')}">↓ ${fmtRate(s.net.rx)}</b> · <b style="color:${css('--tx')}">↑ ${fmtRate(s.net.tx)}</b> — tot ${fmtBytes(s.net.rx_total)} / ${fmtBytes(s.net.tx_total)}`;
  // Disk
  diskChart.push([s.disk.read, s.disk.write]);
  $('diskText').innerHTML = `<b style="color:${css('--read')}">R ${fmtRate(s.disk.read)}</b> · <b style="color:${css('--write')}">W ${fmtRate(s.disk.write)}</b>`;
  $('diskUtil').innerHTML = s.disk.devs.map(d =>
    `<div class="util-row"><span class="name" title="${d.model || ''}">${d.name}</span>` +
    `<span class="track"><i style="width:${d.util.toFixed(0)}%"></i></span>` +
    `<span class="n">${d.util.toFixed(0)}%</span></div>`).join('');
  // GPU
  if (s.gpu && s.gpu.length) {
    const g = s.gpu[0];
    $('gpuName').textContent = g.name;
    $('gpuUtil').textContent = g.util + '%';
    gpuChart.push([g.util]);
    $('gpuStats').innerHTML =
      `<div class="kv">mem <b>${fmtBytes(g.mem_used)}</b>/${fmtBytes(g.mem_total)}</div>` +
      `<div class="kv">temp <b>${g.temp}°C</b></div>` +
      `<div class="kv">power <b>${g.power} W</b></div>` +
      `<div class="kv">clk <b>${g.clk_sm} MHz</b></div>` +
      (g.fan >= 0 ? `<div class="kv">fan <b>${g.fan}%</b></div>` : '');
  } else {
    $('tile-gpu').style.display = 'none';
  }
  // FS
  $('fsList').innerHTML = s.fs.map(f =>
    `<div class="row"><span class="name">${f.mount}</span>` +
    `<span class="n">${fmtBytes(f.used)}/${fmtBytes(f.total)}</span>` +
    `<span class="n">${f.pct.toFixed(0)}%</span></div>`).join('');
  // Proc
  $('procList').innerHTML = s.proc.map(p =>
    `<div class="row"><span class="name">${p.name}</span>` +
    `<span class="n">${p.cpu.toFixed(0)}%</span>` +
    `<span class="n">${fmtBytes(p.rss)}</span></div>`).join('');
  // Header
  $('host').textContent = s.host.name;
  $('uptime').textContent = 'up ' + fmtUptime(s.host.uptime);
  $('load').textContent = 'load ' + s.host.load.map(x => x.toFixed(2)).join(' ');
  $('clock').textContent = new Date(s.t * 1000).toLocaleTimeString();
}

function seedHistory(history) {
  if (!history.length) return;
  const first = history[0];
  if (!cpuChart) initCharts(first.cpu.cores.length);
  cpuChart.seed(history.map(s => s.cpu.cores));
  netChart.seed(history.map(s => [s.net.rx, s.net.tx]));
  diskChart.seed(history.map(s => [s.disk.read, s.disk.write]));
  gpuChart.seed(history.map(s => [s.gpu && s.gpu[0] ? s.gpu[0].util : 0]));
  applySnap(history[history.length - 1]);
}

let ws, backoff = 500;
function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onopen = () => { backoff = 500; $('conn').className = 'on'; $('conn').textContent = 'live'; };
  ws.onmessage = ev => {
    const m = JSON.parse(ev.data);
    if (m.type === 'init') seedHistory(m.history || []);
    else if (m.type === 'tick') applySnap(m.snap);
  };
  ws.onclose = () => {
    $('conn').className = 'off'; $('conn').textContent = 'offline';
    setTimeout(connect, backoff); backoff = Math.min(backoff * 2, 5000);
  };
  ws.onerror = () => ws.close();
}
connect();
