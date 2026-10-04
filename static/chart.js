// Draws the count trend from the series the server embedded.
// Loaded only on a tracker page that has a chart. Logging does not use this file.

(function () {
  function bootCountChart() {
    const dataEl = document.getElementById("count-chart");
    const canvas = document.getElementById("count-chart-canvas");
    if (!dataEl || !canvas) return;
    if (window.Chart) {
      drawCountChart(canvas, dataEl);
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
    window.__chartJsLoading.then(function () {
      const next = document.getElementById("count-chart-canvas");
      const nextData = document.getElementById("count-chart");
      if (next && nextData) drawCountChart(next, nextData);
    }).catch(function () {});
  }

  if (!window.__countChartBound) {
    window.__countChartBound = true;
    document.addEventListener("htmx:beforeSwap", function () {
      if (!window.Chart) return;
      const canvas = document.getElementById("count-chart-canvas");
      const existing = canvas && window.Chart.getChart(canvas);
      if (existing) existing.destroy();
    });
    document.addEventListener("htmx:afterSettle", bootCountChart);
  }
  bootCountChart();

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
        const center = scale.getPixelForValue(i);
        const slot = slotWidth(scale, i, series.length);
        const width = Math.max(3, Math.min(8, slot * 0.35));
        bands.push({ i: i, center: center, width: width, day: day });
      }
      if (bands.length === 0) return;

      const ctx = chart.ctx;
      const fill = mixAlpha(cssVar("--pico-primary", "#0172ad"), 28);
      const cap = mixAlpha(cssVar("--pico-primary", "#0172ad"), 70);
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
      if (!tip || !opts || !opts.overlay) return;
      const type = args.event.type;
      if (type === "mouseout" || type === "mouseleave") {
        tip.hidden = true;
        return;
      }
      if (type !== "mousemove" && type !== "click" && type !== "touchstart") return;
      const scale = chart.scales.x;
      const area = chart.chartArea;
      if (!scale || !area) return;
      const ev = args.event;
      if (ev.x < area.left || ev.x > area.right || ev.y < area.top || ev.y > area.bottom) {
        tip.hidden = true;
        return;
      }
      const raw = scale.getValueForPixel(ev.x);
      const index = Math.round(raw);
      const series = opts.series || [];
      if (index < 0 || index >= series.length) {
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
      const parent = chart.canvas.parentElement;
      const maxLeft = Math.max(0, parent.clientWidth - tip.offsetWidth - 4);
      tip.style.left = Math.min(Math.max(0, ev.x + 10), maxLeft) + "px";
      tip.style.top = Math.min(Math.max(0, ev.y - tip.offsetHeight - 8), Math.max(0, parent.clientHeight - tip.offsetHeight)) + "px";
    },
  };

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
    const primary = cssVar("--pico-primary", "#0172ad");
    const muted = cssVar("--pico-muted-color", "#5c6b7a");
    const grid = cssVar("--pico-muted-border-color", "rgba(128, 128, 128, 0.35)");
    const font = getComputedStyle(document.body).fontFamily;
    const narrow = canvas.parentElement && canvas.parentElement.clientWidth < 520;
    const tooltip = {
      callbacks: {
        label: function (item) {
          return countText(item.parsed.y);
        },
      },
    };
    if (hasBands) {
      // One tip covers the count and the band, including a band on a gap day.
      tooltip.enabled = false;
    }

    new window.Chart(canvas, {
      type: "line",
      plugins: hasBands ? [overlayBands] : [],
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
            pointRadius: 4,
            pointHoverRadius: 6,
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
})();
