// Draws a count, number, or duration chart from the series the server embedded.
// Loaded only when a tracker page or the charts page has a chart. Logging does not use this file.

(function () {
  function bootCountChart() {
    const canvas = document.getElementById("trend-canvas");
    const countEl = document.getElementById("count-chart");
    const valueEl = document.getElementById("value-chart");
    if (!canvas || (!countEl && !valueEl)) return;
    const draw = function () {
      const next = document.getElementById("trend-canvas");
      const nextCount = document.getElementById("count-chart");
      const nextValue = document.getElementById("value-chart");
      if (!next) return;
      if (nextCount) drawCountChart(next, nextCount);
      else if (nextValue) drawValueChart(next, nextValue);
    };
    if (window.Chart) {
      draw();
      return;
    }
    if (!window.__chartJsLoading) {
      window.__chartJsLoading = new Promise(function (resolve, reject) {
        const script = document.createElement("script");
        script.src = "/static/chart.umd.min.js";
        script.onload = function () {
          resolve();
        };
        script.onerror = function () {
          reject(new Error("chart failed"));
        };
        document.head.appendChild(script);
      });
    }
    window.__chartJsLoading.then(draw).catch(function () {});
  }

  if (!window.__countChartBound) {
    window.__countChartBound = true;
    document.addEventListener("htmx:beforeSwap", function () {
      if (!window.Chart) return;
      const canvas = document.getElementById("trend-canvas");
      const existing = canvas && window.Chart.getChart(canvas);
      if (existing) existing.destroy();
    });
    document.addEventListener("htmx:afterSettle", bootCountChart);
  }

  function cssVar(name, fallback) {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return value || fallback;
  }

  // Bands mark event days. They are not a second series and not a duration.
  const overlayBands = {
    id: "overlayBands",
    beforeDatasetsDraw: function (chart, _args, opts) {
      const overlay = opts && opts.overlay;
      const series = opts && opts.series;
      if (!overlay || !series || !overlay.days || overlay.days.length === 0) return;
      const area = chart.chartArea;
      const scale = chart.scales.x;
      if (!area || !scale) return;

      const indexByDay = {};
      series.forEach(function (day, i) {
        indexByDay[day.day] = i;
      });
      const bands = [];
      for (const day of overlay.days) {
        const i = indexByDay[day.day];
        if (i === undefined) continue;
        const xValue = series[i].x === undefined || series[i].x === null ? i : series[i].x;
        const center = scale.getPixelForValue(xValue);
        const slot = series[i].x === undefined ? slotWidth(scale, i, series.length) : Math.abs(scale.getPixelForValue(xValue + 0.5) - scale.getPixelForValue(xValue - 0.5));
        const width = Math.max(3, Math.min(8, slot * 0.35));
        bands.push({ i: i, center: center, width: width, day: day });
      }
      if (bands.length === 0) return;

      const ctx = chart.ctx;
      const fill = mixAlpha(cssVar("--pico-primary", "#0f766e"), 28);
      const cap = mixAlpha(cssVar("--pico-primary", "#0f766e"), 70);
      ctx.save();
      ctx.beginPath();
      ctx.rect(area.left, area.top, area.right - area.left, area.bottom - area.top);
      ctx.clip();
      for (const band of bands) {
        // The first and last points sit on the plot edge. Keep the whole band inside it.
        let left = band.center - band.width / 2;
        if (left < area.left) left = area.left;
        if (left + band.width > area.right) left = area.right - band.width;
        ctx.fillStyle = fill;
        ctx.fillRect(left, area.top, band.width, area.bottom - area.top);
        ctx.fillStyle = cap;
        ctx.fillRect(left, area.top, band.width, 3);
      }
      ctx.restore();

      if (!overlay.mark || bands.length > 8) return;
      ctx.save();
      ctx.fillStyle = cssVar("--pico-muted-color", "#5c6b7a");
      ctx.font = "12px " + getComputedStyle(document.body).fontFamily;
      ctx.textAlign = "center";
      ctx.textBaseline = "bottom";
      for (const band of bands) {
        if (hasNeighbor(bands, band.i)) continue;
        if (!roomForMark(bands, band)) continue;
        ctx.fillText(overlay.mark, band.center, area.top - 1);
      }
      ctx.restore();
    },
    afterEvent: function (chart, args, opts) {
      const tip = chart.canvas.parentElement && chart.canvas.parentElement.querySelector(".chart-tip");
      if (!tip || !opts) return;
      const type = args.event.type;
      if (type === "mouseout" || type === "mouseleave") {
        tip.hidden = true;
        return;
      }
      if (type !== "mousemove" && type !== "click" && type !== "touchstart" && type !== "touchmove") return;
      if (opts.value) {
        showValueTip(chart, args, opts, tip);
        return;
      }
      const scale = chart.scales.x;
      const area = chart.chartArea;
      if (!scale || !area) return;
      const ev = args.event;
      if (ev.x < area.left || ev.x > area.right || ev.y < area.top || ev.y > area.bottom) {
        tip.hidden = true;
        return;
      }
      const series = opts.series || [];
      const index = dayUnderPointer(scale, ev.x, series, opts.overlay);
      if (index < 0) {
        tip.hidden = true;
        return;
      }
      const day = series[index];
      const hit = overlayHit(opts.overlay, day.day);
      if (day.count === null || day.count === undefined) {
        if (!hit) {
          tip.hidden = true;
          return;
        }
      }
      const lines = [];
      lines.push(day.label || day.day);
      if (day.count !== null && day.count !== undefined) lines.push(countText(day.count));
      if (hit) lines.push(overlayText(opts.overlay, hit));
      tip.textContent = lines.join("\n");
      tip.hidden = false;
      placeTip(chart, tip, ev);
    },
  };

  // Half of a 44px touch target. A year of days is about one pixel wide, so the
  // column under the pointer is not a target a person can hit.
  const fingerPx = 22;

  // A logged day or an overlay day within a finger's width wins. Otherwise the
  // day column under the pointer, which is wide enough to aim at on a short range.
  function dayUnderPointer(scale, x, series, overlay) {
    const n = series.length;
    if (!scale || n === 0) return -1;
    let best = -1;
    let bestD = fingerPx;
    for (let i = 0; i < n; i++) {
      const day = series[i];
      const logged = day.count !== null && day.count !== undefined;
      if (!logged && !overlayHit(overlay, day.day)) continue;
      const d = Math.abs(scale.getPixelForValue(i) - x);
      if (d <= bestD) {
        bestD = d;
        best = i;
      }
    }
    if (best >= 0) return best;
    const raw = scale.getValueForPixel(x);
    if (typeof raw !== "number" || !isFinite(raw)) return -1;
    const index = Math.round(raw);
    if (index < 0 || index >= n) return -1;
    return index;
  }

  function slotWidth(scale, index, n) {
    if (n < 2) return 16;
    const here = scale.getPixelForValue(index);
    const other = scale.getPixelForValue(index < n - 1 ? index + 1 : index - 1);
    return Math.abs(other - here);
  }

  function hasNeighbor(bands, index) {
    return bands.some(function (band) {
      return Math.abs(band.i - index) === 1;
    });
  }

  function roomForMark(bands, band) {
    return !bands.some(function (other) {
      return other !== band && Math.abs(other.center - band.center) < 18;
    });
  }

  function overlayHit(overlay, day) {
    if (!overlay || !overlay.days) return null;
    for (const hit of overlay.days) {
      if (hit.day === day) return hit;
    }
    return null;
  }

  function overlayText(overlay, hit) {
    if (!hit.times || hit.times.length === 0) return overlay.name + " — " + hit.label;
    if (hit.times.length === 1) return overlay.name + " — " + hit.label + ", " + hit.times[0];
    return overlay.name + " — " + hit.label + ", " + hit.times.join(", ");
  }

  function showValueTip(chart, args, opts, tip) {
    const ev = args.event;
    const area = chart.chartArea;
    if (!area || ev.x < area.left || ev.x > area.right || ev.y < area.top || ev.y > area.bottom) {
      tip.hidden = true;
      return;
    }
    const point = nearestPoint(chart, ev.x, ev.y);
    const band = bandAt(chart, opts, ev.x);
    if (!point && !band) {
      tip.hidden = true;
      return;
    }
    const lines = [];
    if (point) {
      lines.push(point.label || point.day);
      if (point.text) lines.push(point.text);
      const hit = overlayHit(opts.overlay, point.day);
      if (hit) lines.push(overlayText(opts.overlay, hit));
    } else {
      lines.push(overlayText(opts.overlay, band));
    }
    tip.textContent = lines.join("\n");
    tip.hidden = false;
    placeTip(chart, tip, ev);
  }

  function nearestPoint(chart, x, y) {
    const meta = chart.getDatasetMeta(0);
    const data = chart.data.datasets[0] && chart.data.datasets[0].data;
    if (!meta || !meta.data || !data) return null;
    let best = null;
    let bestD = 28;
    meta.data.forEach(function (pt, i) {
      const d = Math.hypot(pt.x - x, pt.y - y);
      if (d <= bestD) {
        bestD = d;
        best = data[i];
      }
    });
    return best;
  }

  // A band is only a few pixels wide. A tap within a finger's width of that day still counts.
  function bandAt(chart, opts, x) {
    const scale = chart.scales.x;
    const series = (opts && opts.series) || [];
    if (!scale || !opts || !opts.overlay || series.length === 0) return null;
    let best = null;
    let bestD = fingerPx;
    for (let i = 0; i < series.length; i++) {
      const hit = overlayHit(opts.overlay, series[i].day);
      if (!hit) continue;
      const at = series[i].x === undefined || series[i].x === null ? i : series[i].x;
      const d = Math.abs(scale.getPixelForValue(at) - x);
      if (d <= bestD) {
        bestD = d;
        best = hit;
      }
    }
    if (best) return best;
    const raw = scale.getValueForPixel(x);
    const index = series[0].x === undefined || series[0].x === null ? Math.round(raw) : Math.floor(raw);
    if (index < 0 || index >= series.length) return null;
    return overlayHit(opts.overlay, series[index].day);
  }

  function pointRadius(n) {
    if (n > 120) return 0;
    if (n > 40) return 2;
    return 4;
  }

  function placeTip(chart, tip, ev) {
    const parent = chart.canvas.parentElement;
    const maxLeft = Math.max(0, parent.clientWidth - tip.offsetWidth - 4);
    tip.style.left = Math.min(Math.max(0, ev.x + 10), maxLeft) + "px";
    tip.style.top = Math.min(Math.max(0, ev.y - tip.offsetHeight - 8), Math.max(0, parent.clientHeight - tip.offsetHeight)) + "px";
  }

  // Chart.js would otherwise pick a step in raw seconds, which formats as
  // "8 min 20 sec". A round step keeps the axis on 10 min, 15 min, 30 min, 1 hr.
  function durationStep(max) {
    const steps = [1, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 28800];
    if (!(max > 0)) return 60;
    const target = max / 4;
    let step = steps[0];
    for (let i = 0; i < steps.length; i++) {
      if (steps[i] <= target) step = steps[i];
      else break;
    }
    return step;
  }

  function formatDuration(sec) {
    sec = Math.round(sec);
    if (sec < 0) sec = 0;
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    const parts = [];
    if (h) parts.push(h + " hr");
    if (m) parts.push(m + " min");
    if (s) parts.push(s + " sec");
    if (!parts.length) parts.push("0 sec");
    return parts.join(" ");
  }

  function countText(n) {
    if (n === 0) return "None";
    if (n === 1) return "1 time";
    return n + " times";
  }

  // color-mix is not a canvas fill. Read the computed color instead.
  function mixAlpha(base, percent) {
    const probe = document.createElement("span");
    probe.style.color = "color-mix(in srgb, " + base + " " + percent + "%, transparent)";
    probe.style.position = "absolute";
    probe.style.left = "-9999px";
    document.body.appendChild(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    if (!color || color === "color-mix(in srgb, " + base + " " + percent + "%, transparent)") {
      return "rgba(1, 114, 173, " + percent / 100 + ")";
    }
    return color;
  }

  function readOverlay() {
    const el = document.getElementById("count-overlay");
    if (!el) return null;
    try {
      const data = JSON.parse(el.textContent);
      if (!data || !Array.isArray(data.days)) return null;
      return data;
    } catch {
      return null;
    }
  }

  function drawCountChart(canvas, dataEl) {
    if (!window.Chart) return;
    const existing = window.Chart.getChart(canvas);
    if (existing) existing.destroy();

    let series;
    try {
      series = JSON.parse(dataEl.textContent);
    } catch {
      return;
    }
    if (!Array.isArray(series) || series.length === 0) return;

    const labels = [];
    const counts = [];
    for (const day of series) {
      labels.push(day.label || day.day);
      counts.push(day.count === null || day.count === undefined ? null : day.count);
    }

    const overlay = readOverlay();
    const hasBands = !!(overlay && overlay.days.length > 0);
    const dayTip = !!(canvas.parentElement && canvas.parentElement.querySelector(".chart-tip"));
    const primary = cssVar("--pico-primary", "#0f766e");
    const muted = cssVar("--pico-muted-color", "#5c6b7a");
    const grid = cssVar("--pico-muted-border-color", "rgba(128, 128, 128, 0.35)");
    const font = getComputedStyle(document.body).fontFamily;
    const narrow = canvas.parentElement && canvas.parentElement.clientWidth < 520;
    const radius = pointRadius(series.length);
    const tooltip = {
      callbacks: {
        label: function (item) {
          return countText(item.parsed.y);
        },
      },
    };
    if (hasBands || dayTip) {
      // One tip covers the day's count and, when present, the band. A tap near that day is enough.
      tooltip.enabled = false;
    }

    new window.Chart(canvas, {
      type: "line",
      plugins: hasBands || dayTip ? [overlayBands] : [],
      data: {
        labels: labels,
        datasets: [
          {
            label: "Count",
            data: counts,
            spanGaps: false,
            tension: 0,
            borderColor: primary,
            backgroundColor: primary,
            pointBackgroundColor: primary,
            pointBorderColor: primary,
            pointRadius: radius,
            pointHoverRadius: radius === 0 ? 4 : 6,
            borderWidth: 2,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        layout: { padding: { top: hasBands && overlay.mark ? 18 : 12, right: 8, bottom: 4, left: 4 } },
        interaction: { mode: "nearest", intersect: true },
        plugins: {
          legend: { display: false },
          tooltip: tooltip,
          overlayBands: { overlay: overlay, series: series },
        },
        scales: {
          x: {
            grid: { display: false },
            ticks: {
              color: muted,
              font: { family: font },
              maxRotation: 0,
              autoSkip: true,
              maxTicksLimit: narrow ? 5 : 8,
            },
          },
          y: {
            beginAtZero: true,
            ticks: { color: muted, font: { family: font }, precision: 0 },
            grid: { color: grid },
            border: { display: false },
          },
        },
      },
    });
  }

  function drawValueChart(canvas, dataEl) {
    if (!window.Chart) return;
    const existing = window.Chart.getChart(canvas);
    if (existing) existing.destroy();

    let chartData;
    try {
      chartData = JSON.parse(dataEl.textContent);
    } catch {
      return;
    }
    const points = chartData && chartData.points;
    const days = chartData && chartData.days;
    if (!Array.isArray(points) || points.length === 0 || !Array.isArray(days)) return;

    const overlay = readOverlay();
    const hasBands = !!(overlay && overlay.days.length > 0);
    const dayTip = !!(canvas.parentElement && canvas.parentElement.querySelector(".chart-tip"));
    const duration = chartData.kind === "duration";
    const cumulative = chartData.kind === "cumulative";
    const primary = cssVar("--pico-primary", "#0f766e");
    const muted = cssVar("--pico-muted-color", "#5c6b7a");
    const grid = cssVar("--pico-muted-border-color", "rgba(128, 128, 128, 0.35)");
    const font = getComputedStyle(document.body).fontFamily;
    const narrow = canvas.parentElement && canvas.parentElement.clientWidth < 520;
    const tooltip = {
      callbacks: {
        title: function (items) {
          const raw = items[0] && items[0].raw;
          return (raw && raw.label) || "";
        },
        label: function (item) {
          return (item.raw && item.raw.text) || "";
        },
      },
    };
    if (hasBands || dayTip) tooltip.enabled = false;
    const radius = pointRadius(points.length);

    new window.Chart(canvas, {
      type: "line",
      plugins: hasBands || dayTip ? [overlayBands] : [],
      data: {
        datasets: [
          {
            label: duration ? "Duration" : cumulative ? "Total earned" : "Value",
            data: points,
            tension: 0,
            borderColor: primary,
            backgroundColor: primary,
            pointBackgroundColor: primary,
            pointBorderColor: primary,
            pointRadius: radius,
            pointHoverRadius: radius === 0 ? 4 : 6,
            borderWidth: 2,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        layout: { padding: { top: hasBands && overlay.mark ? 18 : 12, right: 8, bottom: 4, left: 4 } },
        interaction: { mode: "nearest", intersect: true },
        plugins: {
          legend: { display: false },
          tooltip: tooltip,
          overlayBands: { overlay: overlay, series: days, value: true },
        },
        scales: {
          x: {
            type: "linear",
            min: 0,
            max: days.length,
            grid: { display: false },
            ticks: {
              color: muted,
              font: { family: font },
              maxRotation: 0,
              autoSkip: true,
              maxTicksLimit: narrow ? 5 : 8,
              stepSize: 1,
              callback: function (value) {
                const i = Math.round(value);
                if (Math.abs(value - i) > 0.001 || i < 0 || i >= days.length) return "";
                return days[i].label;
              },
            },
          },
          y: {
            beginAtZero: duration,
            ticks: {
              color: muted,
              font: { family: font },
              stepSize: duration ? durationStep(Math.max.apply(null, points.map(function (p) { return p.y; }))) : undefined,
              callback: function (value) {
                return duration ? formatDuration(value) : value;
              },
            },
            grid: { color: grid },
            border: { display: false },
          },
        },
      },
    });
  }

  bootCountChart();
})();
