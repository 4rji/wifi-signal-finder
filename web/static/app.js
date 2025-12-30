const state = {
  targetQuality: 0,
  displayQuality: 0,
  lastSignal: null,
};

const elements = {
  ssid: document.getElementById("ssid"),
  bssid: document.getElementById("bssid"),
  freq: document.getElementById("freq"),
  signal: document.getElementById("signal-db"),
  quality: document.getElementById("quality"),
  rx: document.getElementById("rx"),
  tx: document.getElementById("tx"),
  gaugeFill: document.getElementById("gauge-fill"),
  needle: document.getElementById("needle"),
  pulse: document.getElementById("pulse"),
};

const GAUGE_LENGTH = 410;

function normalizeSignal(signalDbm) {
  if (signalDbm === null || Number.isNaN(signalDbm)) return 0;
  const clamped = Math.max(-100, Math.min(-30, signalDbm));
  return Math.round(((clamped + 100) / 70) * 100);
}

function pickBestSample(samples) {
  if (!samples || samples.length === 0) return null;
  return samples.reduce((best, current) => {
    if (!best) return current;
    if (current.signal_dbm > best.signal_dbm) return current;
    if (current.signal_dbm === best.signal_dbm) {
      const bestRate = best.rx_mbps + best.tx_mbps;
      const currentRate = current.rx_mbps + current.tx_mbps;
      if (currentRate > bestRate) return current;
    }
    return best;
  }, null);
}

function updateReadout(sample) {
  elements.ssid.textContent = sample.ssid || "—";
  elements.bssid.textContent = sample.bssid || "—";
  elements.freq.textContent = sample.freq_mhz ? `${sample.freq_mhz} MHz` : "—";
  elements.signal.textContent = sample.signal_dbm ?? "—";
  elements.rx.textContent = sample.rx_mbps ? `${sample.rx_mbps.toFixed(1)} Mbps` : "—";
  elements.tx.textContent = sample.tx_mbps ? `${sample.tx_mbps.toFixed(1)} Mbps` : "—";

  const quality = normalizeSignal(sample.signal_dbm);
  state.targetQuality = quality;

  if (quality >= 75) {
    elements.quality.textContent = "Strong signal";
  } else if (quality >= 45) {
    elements.quality.textContent = "Stable signal";
  } else if (quality >= 20) {
    elements.quality.textContent = "Weak signal";
  } else {
    elements.quality.textContent = "Searching for signal";
  }

  const pulseSpeed = Math.max(1.2, 2.8 - quality / 50);
  elements.pulse.style.animationDuration = `${pulseSpeed}s`;
  elements.pulse.style.opacity = `${0.2 + quality / 140}`;
}

function renderGauge() {
  state.displayQuality += (state.targetQuality - state.displayQuality) * 0.08;
  const quality = state.displayQuality;
  const offset = GAUGE_LENGTH - (GAUGE_LENGTH * quality) / 100;
  elements.gaugeFill.style.strokeDashoffset = `${offset}`;

  const angle = -90 + (quality / 100) * 180;
  elements.needle.style.transform = `translateX(-50%) rotate(${angle}deg)`;

  requestAnimationFrame(renderGauge);
}

function handleStatus(status) {
  const sample = pickBestSample(status.interfaces);
  if (!sample) return;
  updateReadout(sample);
}

function startStream() {
  fetch("/api/status")
    .then((res) => (res.ok ? res.json() : null))
    .then((data) => {
      if (data) handleStatus(data);
    })
    .catch(() => {
      elements.quality.textContent = "Waiting for data";
    });

  const source = new EventSource("/api/stream");
  source.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      handleStatus(data);
    } catch (err) {
      console.warn("Bad stream payload", err);
    }
  };

  source.onerror = () => {
    elements.quality.textContent = "Stream paused";
  };
}

renderGauge();
startStream();
