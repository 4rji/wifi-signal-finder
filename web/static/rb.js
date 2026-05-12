const elements = {
  ifname: document.getElementById("ifname"),
  freq: document.getElementById("freq"),
  signal: document.getElementById("signal-db"),
  quality: document.getElementById("quality"),
  ssid: document.getElementById("ssid"),
  bssid: document.getElementById("bssid"),
  bar: document.getElementById("quality-bar"),
  pulse: document.getElementById("pulse"),
};

const state = {
  lastUpdate: 0,
};

function normalizeSignal(signalDbm) {
  if (signalDbm === null || signalDbm === undefined || Number.isNaN(signalDbm)) {
    return 0;
  }
  const clamped = Math.max(-100, Math.min(-30, signalDbm));
  return Math.round(((clamped + 100) / 70) * 100);
}

function pickBestSample(samples) {
  if (!samples || samples.length === 0) {
    return null;
  }
  return samples.reduce((best, current) => {
    if (!best || current.signal_dbm > best.signal_dbm) {
      return current;
    }
    return best;
  }, null);
}

function signalTone(quality) {
  if (quality >= 72) {
    return { color: "#39d66f", rgb: "57, 214, 111" };
  }
  if (quality >= 38) {
    return { color: "#f6b84b", rgb: "246, 184, 75" };
  }
  return { color: "#ff5e57", rgb: "255, 94, 87" };
}

function qualityText(quality) {
  if (quality >= 72) {
    return "Fuerte";
  }
  if (quality >= 38) {
    return "Estable";
  }
  if (quality >= 12) {
    return "Debil";
  }
  return "Buscando";
}

function updateReadout(sample) {
  const signal = sample.signal_dbm;
  const quality = normalizeSignal(signal);
  const tone = signalTone(quality);

  state.lastUpdate = Date.now();
  document.documentElement.style.setProperty("--signal-color", tone.color);
  document.documentElement.style.setProperty("--signal-rgb", tone.rgb);

  elements.ifname.textContent = sample.ifname || "--";
  elements.freq.textContent = sample.freq_mhz ? `${sample.freq_mhz} MHz` : "--";
  elements.signal.textContent = signal === null || signal === undefined ? "--" : signal;
  elements.quality.textContent = qualityText(quality);
  elements.ssid.textContent = sample.ssid || "--";
  elements.bssid.textContent = sample.bssid || "--";
  elements.bar.style.width = `${quality}%`;

  const pulseSpeed = Math.max(0.9, 2.6 - quality / 48);
  elements.pulse.style.animationDuration = `${pulseSpeed}s`;
}

function handleStatus(status) {
  const sample = pickBestSample(status.interfaces);
  if (!sample) {
    elements.quality.textContent = "Sin datos";
    return;
  }
  updateReadout(sample);
}

function startStream() {
  fetch("/api/status")
    .then((res) => (res.ok ? res.json() : null))
    .then((data) => {
      if (data) {
        handleStatus(data);
      }
    })
    .catch(() => {
      elements.quality.textContent = "Esperando";
    });

  const source = new EventSource("/api/stream");
  source.onmessage = (event) => {
    try {
      handleStatus(JSON.parse(event.data));
    } catch (err) {
      console.warn("Bad stream payload", err);
    }
  };
  source.onerror = () => {
    elements.quality.textContent = "Pausado";
  };
}

setInterval(() => {
  if (state.lastUpdate !== 0 && Date.now() - state.lastUpdate > 6000) {
    elements.quality.textContent = "Esperando";
  }
}, 1000);

startStream();
