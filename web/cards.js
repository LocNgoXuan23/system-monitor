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
  $('swapV').textContent = fmtBytes(s.mem.swap_used) + ' / ' + fmtBytes(s.mem.swap_total);

  const total = s.mem.total || 1;
  const free = Math.max(0, s.mem.total - s.mem.used - s.mem.cache);
  $('stkUsed').style.width = (s.mem.used / total) * 100 + '%';
  $('stkCache').style.width = (s.mem.cache / total) * 100 + '%';
  // Value only — no "used/cache/free" word. The three items with words need
  // 258px but the stats column is 210px, so the words forced a second line.
  // Each legend chip is the same colour as its stacked-bar segment right above
  // (red=used, amber=cache, track=free), which carries the labelling instead.
  $('keyUsed').textContent = fmtBytes(s.mem.used);
  $('keyCache').textContent = fmtBytes(s.mem.cache);
  $('keyFree').textContent = fmtBytes(free);
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

function renderNet(s) {
  // Real interface names — the design's "enp5s0 - 1 Gb/s" invented a link
  // speed the app does not collect.
  $('subNet').textContent = s.net.ifaces.length ? s.net.ifaces.join(' + ') : 'no interface';
  netChart.push([s.net.rx, s.net.tx]);
  $('netRx').textContent = fmtRate(s.net.rx);
  $('netTx').textContent = fmtRate(s.net.tx);
  $('netRxTot').textContent = fmtBytes(s.net.rx_total);
  $('netTxTot').textContent = fmtBytes(s.net.tx_total);
}

function renderDisk(s) {
  const devs = s.disk.devs.slice().sort((a, b) => b.util - a.util);
  // Cumulative bytes since boot (the running totals gnome-system-monitor shows)
  // ride in the subtitle now — folding them out of the stats column is what lets
  // the full per-device list fit as a list without clipping at the 1100x780 floor.
  $('subDisk').textContent = devs.length + (devs.length === 1 ? ' device' : ' devices')
    + ' · read ' + fmtBytes(s.disk.read_total) + ' · written ' + fmtBytes(s.disk.write_total);
  diskChart.push([s.disk.read, s.disk.write]);
  $('dskR').textContent = fmtRate(s.disk.read);
  $('dskW').textContent = fmtRate(s.disk.write);
  // Sorted descending: the device that matters is always the top row, which is
  // also what makes this degrade gracefully with many devices.
  $('devList').innerHTML = devs.map(d =>
    `<div class="dev"><span class="nm" title="${esc(d.model || d.name)}">${esc(d.name)}</span>` +
    `<span class="bar"><i class="${d.util > 75 ? 'hot' : ''}" style="width:${d.util.toFixed(0)}%"></i></span>` +
    `<span class="pc">${d.util.toFixed(0)}%</span></div>`).join('');
}

function renderProc(s) {
  // Already sorted by CPU descending server-side. No per-process icons: they
  // carried no information the name doesn't.
  $('procBody').innerHTML = s.proc.map(p =>
    `<tr><td class="pidl">${p.pid}</td>` +
    `<td class="nm" title="${esc(p.name)}">${esc(p.name)}</td>` +
    `<td class="n">${p.cpu.toFixed(0)}%</td>` +
    `<td class="n">${fmtBytes(p.rss)}</td></tr>`).join('');
}

// Only called when hasGPU; the card is removed from the DOM otherwise. Rows are
// already merged across GPUs and sorted by VRAM descending server-side.
function renderGPUProc(s) {
  const used = s.gpu.reduce((a, g) => a + g.mem_used, 0);
  const total = s.gpu.reduce((a, g) => a + g.mem_total, 0);
  $('subGpuProc').textContent = 'by VRAM · ' + fmtBytes(used) + ' / ' + fmtBytes(total);
  // A process that exits mid-tick still holds the VRAM NVML reported, so the
  // row stays and only its name falls back to a dash.
  $('gpuProcBody').innerHTML = s.gpu_proc.length
    ? s.gpu_proc.map(p =>
        `<tr><td class="pidl">${p.pid}</td>` +
        `<td class="nm" title="${esc(p.name)}">${esc(p.name || '—')}</td>` +
        `<td class="n tag">${esc(p.type)}</td>` +
        `<td class="n">${fmtBytes(p.vram)}</td></tr>`).join('')
    : '<tr><td class="empty" colspan="4">không có tiến trình dùng GPU</td></tr>';
  $('gpuProcBody').dataset.total = s.gpu_proc.length;
}

function renderFS(s) {
  // Sorted by % used descending — the full mount is the one worth seeing.
  const fs = s.fs.slice().sort((a, b) => b.pct - a.pct);
  // Show the backing device (/dev/nvme1n1p1, /dev/sda1); the mount point moves
  // to the row's tooltip so it is still one hover away.
  $('fsBody').innerHTML = fs.map(f =>
    `<tr><td class="nm" title="${esc(f.mount)}">${esc(f.dev || f.mount)}</td>` +
    `<td class="n">${fmtBytes(f.used)} / ${fmtBytes(f.total)}</td>` +
    `<td class="n"><span class="fsbar"><i class="${f.pct > 90 ? 'hot' : ''}" style="width:${f.pct.toFixed(0)}%"></i></span></td>` +
    `<td class="n">${f.pct.toFixed(0)}%</td></tr>`).join('');
  $('fsBody').dataset.total = fs.length;
}

// Trim rows from the bottom until each table fits its wrapper, so the right
// column adapts to the window height without ever scrolling. The loop only
// ever deletes, so it terminates. Both lists are pre-sorted, so the rows that
// survive are the ones worth keeping.
function autoFit() {
  document.querySelectorAll('[data-fit]').forEach(wrap => {
    const tb = wrap.querySelector('tbody');
    if (!tb) return;
    // The renderer records how many rows the data had. tb.rows is only what
    // survived the last trim, and autoFit also runs on resize with no re-render
    // in between — reading it here would report "nothing hidden" and wipe the note.
    const total = tb.dataset.total !== undefined ? +tb.dataset.total : tb.rows.length;
    while (tb.rows.length > 1 && wrap.scrollHeight > wrap.clientHeight) {
      tb.deleteRow(tb.rows.length - 1);
    }
    // Never truncate silently. A .note reserves its height even when empty, so
    // writing into it cannot re-overflow the table just trimmed to fit.
    const note = wrap.dataset.note && $(wrap.dataset.note);
    if (note) {
      const hidden = total - tb.rows.length;
      note.textContent = hidden > 0 ? `+${hidden} ${note.dataset.unit} khác` : '';
    }
  });
}
