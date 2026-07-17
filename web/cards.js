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
