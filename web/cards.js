// Per-card renderers. Each takes the snapshot and updates one card's DOM.
// Chart handles (cpuChart, memChart, ...) and coreCells are globals created by
// initCharts() in app.js.

function renderCPU(s) {
  const n = s.cpu.cores.length;
  $('subCpu').textContent = n + ' cores' + (s.cpu.model ? ' · ' + s.cpu.model : '');
  cpuChart.push(s.cpu.cores);
  $('cpuAvg').textContent = pct(s.cpu.agg);
  $('cpuMax').textContent = pct(peak(s.cpu.cores));
  // temp === 0 is the collector's "unknown"; hide the row rather than show 0°C.
  if (s.cpu.temp > 0) {
    $('cpuTempRow').style.display = '';
    $('cpuTemp').textContent = s.cpu.temp.toFixed(0) + '°C';
  } else {
    $('cpuTempRow').style.display = 'none';
  }
  const hot = cssVar('--red'), norm = cssVar('--blue');
  s.cpu.cores.forEach((v, i) => {
    const bar = coreCells[i];
    if (!bar) return;
    bar.style.height = v + '%';
    bar.style.background = v > 80 ? hot : norm;
  });
}

function renderMem(s) {
  $('subMem').textContent = fmtBytes(s.mem.total) + ' RAM · ' + fmtBytes(s.mem.swap_total) + ' swap';
  memChart.push([s.mem.pct, s.mem.swap_pct]);
  $('memV').textContent = fmtBytes(s.mem.used) + ' / ' + fmtBytes(s.mem.total);
  $('memDim').textContent = pct(s.mem.pct) + ' · cache ' + fmtBytes(s.mem.cache);
  $('swapV').textContent = fmtBytes(s.mem.swap_used) + ' / ' + fmtBytes(s.mem.swap_total);
  $('swapDim').textContent = pct(s.mem.swap_pct) + ' used';

  const total = s.mem.total || 1;
  const free = Math.max(0, s.mem.total - s.mem.used - s.mem.cache);
  $('stkUsed').style.width = (s.mem.used / total) * 100 + '%';
  $('stkCache').style.width = (s.mem.cache / total) * 100 + '%';
  $('keyUsed').textContent = 'used ' + fmtBytes(s.mem.used);
  $('keyCache').textContent = 'cache ' + fmtBytes(s.mem.cache);
  $('keyFree').textContent = 'free ' + fmtBytes(free);
}

// Only called when hasGPU; the card is removed from the DOM otherwise.
function renderGPU(s) {
  const g = s.gpu[0];
  $('subGpu').textContent = g.name;
  gpuChart.push([g.util]);
  $('gpuUtil').textContent = g.util + '%';
  $('gpuTemp').textContent = g.temp + '°C';
  $('gpuPower').textContent = g.power + ' W';
  $('gpuClk').textContent = g.clk_sm + ' MHz';
  // NVML reports -1 when the card exposes no fan (e.g. passively cooled).
  if (g.fan >= 0) {
    $('gpuFanRow').style.display = '';
    $('gpuFan').textContent = g.fan + '%';
  } else {
    $('gpuFanRow').style.display = 'none';
  }
  $('gpuVramCap').textContent = 'VRAM · ' + fmtBytes(g.mem_used) + ' / ' + fmtBytes(g.mem_total);
  $('gpuVramBar').style.width = (g.mem_total ? (g.mem_used / g.mem_total) * 100 : 0) + '%';
}
