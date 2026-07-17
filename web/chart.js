// Minimal rolling line chart on a canvas. Fixed or auto Y scale.
class Chart {
  constructor(canvas, { series, maxPoints = 60, yMax = null, fill = false, onScale = null }) {
    this.c = canvas;
    this.ctx = canvas.getContext('2d');
    this.series = series;            // [{color}]
    this.maxPoints = maxPoints;
    this.yMax = yMax;                // null = auto
    this.fill = fill;
    this.onScale = onScale;          // called with the resolved Y max after each render
    this.data = series.map(() => []);// per-series array of values
    this._resize();
    if (typeof ResizeObserver !== 'undefined') {
      new ResizeObserver(() => this._resize()).observe(canvas);
    } else {
      window.addEventListener('resize', () => this._resize());
    }
  }
  _resize() {
    const r = this.c.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    this.c.width = Math.max(1, Math.floor(r.width * dpr));
    this.c.height = Math.max(1, Math.floor(r.height * dpr));
    this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    this.w = r.width; this.h = r.height;
    this.render();
  }
  push(values) {
    for (let i = 0; i < this.data.length; i++) {
      this.data[i].push(values[i] ?? 0);
      if (this.data[i].length > this.maxPoints) this.data[i].shift();
    }
    this.render();
  }
  seed(rows) { // rows: array of value-arrays, oldest first
    this.data = this.series.map(() => []);
    for (const row of rows) this.push(row);
  }
  render() {
    const { ctx, w, h } = this;
    ctx.clearRect(0, 0, w, h);
    let ymax = this.yMax;
    if (ymax == null) {
      ymax = 1;
      for (const s of this.data) for (const v of s) if (v > ymax) ymax = v;
      ymax *= 1.15;
    }
    // Gridline colour comes from the theme, so the chart is legible on any
    // background instead of assuming a dark one.
    ctx.strokeStyle = cssVar('--grid') || 'rgba(0,0,0,0.07)'; ctx.lineWidth = 1;
    for (let g = 1; g < 4; g++) {
      const y = (h * g) / 4; ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
    }
    const n = this.maxPoints;
    const x = i => (w * i) / (n - 1);
    const y = v => h - (v / ymax) * h;
    for (let s = 0; s < this.data.length; s++) {
      const d = this.data[s];
      if (d.length < 2) continue;
      const off = n - d.length;
      ctx.strokeStyle = this.series[s].color; ctx.lineWidth = 1.4;
      ctx.beginPath();
      for (let i = 0; i < d.length; i++) {
        const px = x(off + i), py = y(d[i]);
        i ? ctx.lineTo(px, py) : ctx.moveTo(px, py);
      }
      ctx.stroke();
      if (this.fill) {
        ctx.lineTo(x(off + d.length - 1), h); ctx.lineTo(x(off), h); ctx.closePath();
        ctx.fillStyle = this.series[s].color + '22'; ctx.fill();
      }
    }
    // Safe to call during render: the gutter is a fixed width, so relabelling
    // it cannot resize the canvas and re-enter here.
    if (this.onScale) this.onScale(ymax);
  }
}
