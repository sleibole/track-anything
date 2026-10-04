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

    const primary = cssVar("--pico-primary", "#0172ad");
    const muted = cssVar("--pico-muted-color", "#5c6b7a");
    const grid = cssVar("--pico-muted-border-color", "rgba(128, 128, 128, 0.35)");
    const font = getComputedStyle(document.body).fontFamily;
    const narrow = canvas.parentElement && canvas.parentElement.clientWidth < 520;

    new window.Chart(canvas, {
      type: "line",
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
        layout: { padding: { top: 12, right: 8, bottom: 4, left: 4 } },
        interaction: { mode: "nearest", intersect: true },
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: function (item) {
                const n = item.parsed.y;
                if (n === 0) return "None";
                if (n === 1) return "1 time";
                return n + " times";
              },
            },
          },
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
