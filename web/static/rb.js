const elements = {
  ifname: document.getElementById("ifname"),
  freq: document.getElementById("freq"),
  channel: document.getElementById("channel"),
  signal: document.getElementById("signal-db"),
  quality: document.getElementById("quality"),
  qualityScore: document.getElementById("quality-score"),
  ssid: document.getElementById("ssid"),
  bssid: document.getElementById("bssid"),
  rx: document.getElementById("rx"),
  tx: document.getElementById("tx"),
  lastSeen: document.getElementById("last-seen"),
  bar: document.getElementById("quality-bar"),
  pulse: document.getElementById("pulse"),
  statusLine: document.getElementById("status-line"),
  scanCount: document.getElementById("scan-count"),
  networkList: document.getElementById("network-list"),
  refreshNetworks: document.getElementById("refresh-networks"),
  autoTarget: document.getElementById("auto-target"),
};

const state = {
  lastUpdate: 0,
  lastSample: null,
  networks: [],
  target: { ssid: "", bssid: "" },
  networkPickerAvailable: true,
  loadingNetworks: false,
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
    return { color: "#38d47a", rgb: "56, 212, 122" };
  }
  if (quality >= 38) {
    return { color: "#f6b84b", rgb: "246, 184, 75" };
  }
  return { color: "#ff5e57", rgb: "255, 94, 87" };
}

function qualityText(quality) {
  if (quality >= 72) {
    return "Strong";
  }
  if (quality >= 38) {
    return "Stable";
  }
  if (quality >= 12) {
    return "Weak";
  }
  return "Searching";
}

function networkName(sample) {
  return sample && sample.ssid ? sample.ssid : "Hidden network";
}

function formatFrequency(freqMHz) {
  return freqMHz ? `${freqMHz} MHz` : "--";
}

function formatMbps(value) {
  return value ? `${value.toFixed(1)} Mbps` : "--";
}

function channelFromFrequency(freqMHz) {
  if (!freqMHz) {
    return "--";
  }
  if (freqMHz === 2484) {
    return "14";
  }
  if (freqMHz >= 2412 && freqMHz <= 2472) {
    return String(Math.round((freqMHz - 2407) / 5));
  }
  if (freqMHz >= 5000 && freqMHz <= 5900) {
    return String(Math.round((freqMHz - 5000) / 5));
  }
  if (freqMHz >= 5955 && freqMHz <= 7115) {
    return String(Math.round((freqMHz - 5950) / 5));
  }
  return "--";
}

function ageText(timestampMs) {
  if (!timestampMs) {
    return "--";
  }
  const ageSeconds = Math.max(0, Math.round((Date.now() - timestampMs) / 1000));
  if (ageSeconds < 2) {
    return "Now";
  }
  return `${ageSeconds}s ago`;
}

function targetLabel() {
  if (!state.target.ssid && !state.target.bssid) {
    return "Auto strongest";
  }
  return state.target.ssid || state.target.bssid;
}

function isSelectedNetwork(sample) {
  const targetBSSID = (state.target.bssid || "").toLowerCase();
  const sampleBSSID = (sample.bssid || "").toLowerCase();
  if (targetBSSID) {
    return sampleBSSID === targetBSSID;
  }
  if (state.target.ssid) {
    return sample.ssid === state.target.ssid;
  }
  return false;
}

function updateReadout(sample) {
  const signal = sample.signal_dbm;
  const quality = normalizeSignal(signal);
  const tone = signalTone(quality);

  state.lastUpdate = Date.now();
  state.lastSample = sample;
  document.documentElement.style.setProperty("--signal-color", tone.color);
  document.documentElement.style.setProperty("--signal-rgb", tone.rgb);

  elements.ifname.textContent = sample.ifname || "--";
  elements.freq.textContent = formatFrequency(sample.freq_mhz);
  elements.channel.textContent = channelFromFrequency(sample.freq_mhz);
  elements.signal.textContent = signal === null || signal === undefined ? "--" : signal;
  elements.quality.textContent = qualityText(quality);
  elements.qualityScore.textContent = `${quality}%`;
  elements.ssid.textContent = networkName(sample);
  elements.bssid.textContent = sample.bssid || "--";
  elements.rx.textContent = formatMbps(sample.rx_mbps);
  elements.tx.textContent = formatMbps(sample.tx_mbps);
  elements.lastSeen.textContent = ageText(sample.ts_unix_ms);
  elements.bar.style.width = `${quality}%`;
  elements.statusLine.textContent = `Tracking ${targetLabel()}`;

  const pulseSpeed = Math.max(0.9, 2.6 - quality / 48);
  elements.pulse.style.animationDuration = `${pulseSpeed}s`;
}

function handleStatus(status) {
  const sample = pickBestSample(status.interfaces);
  if (!sample) {
    elements.quality.textContent = "No data";
    elements.statusLine.textContent = "Waiting for scan data";
    return;
  }
  updateReadout(sample);
}

function setEmptyList(message) {
  elements.networkList.replaceChildren();
  const empty = document.createElement("p");
  empty.className = "empty-state";
  empty.textContent = message;
  elements.networkList.append(empty);
}

function makeNetworkRow(network) {
  const quality = normalizeSignal(network.signal_dbm);
  const row = document.createElement("button");
  row.type = "button";
  row.className = "network-row";
  if (isSelectedNetwork(network)) {
    row.classList.add("selected");
  }

  const main = document.createElement("span");
  main.className = "network-main";

  const name = document.createElement("strong");
  name.textContent = networkName(network);

  const details = document.createElement("span");
  details.textContent = `${network.bssid || "--"}  ${formatFrequency(network.freq_mhz)}`;

  const signal = document.createElement("span");
  signal.className = "network-signal";
  signal.textContent = `${network.signal_dbm ?? "--"} dBm`;

  const meter = document.createElement("span");
  meter.className = "network-meter";

  const fill = document.createElement("span");
  fill.style.width = `${quality}%`;

  main.append(name, details, meter);
  meter.append(fill);
  row.append(main, signal);

  row.addEventListener("click", () => {
    selectNetwork(network);
  });

  return row;
}

function renderNetworks() {
  if (!state.networkPickerAvailable) {
    setEmptyList("Network selection is available in scan mode");
    elements.scanCount.textContent = "Link mode";
    return;
  }
  if (state.loadingNetworks && state.networks.length === 0) {
    setEmptyList("Scanning for nearby networks");
    elements.scanCount.textContent = "Scanning";
    return;
  }
  if (state.networks.length === 0) {
    setEmptyList("No networks found");
    elements.scanCount.textContent = "0 found";
    return;
  }

  const fragment = document.createDocumentFragment();
  state.networks.forEach((network) => {
    fragment.append(makeNetworkRow(network));
  });
  elements.networkList.replaceChildren(fragment);
  elements.scanCount.textContent = `${state.networks.length} found`;
}

async function loadTarget() {
  try {
    const res = await fetch("/api/target");
    if (res.status === 404) {
      state.networkPickerAvailable = false;
      renderNetworks();
      return;
    }
    if (!res.ok) {
      return;
    }
    const data = await res.json();
    state.target = data.target || { ssid: "", bssid: "" };
    elements.statusLine.textContent = `Tracking ${targetLabel()}`;
    renderNetworks();
  } catch (err) {
    console.warn("Target request failed", err);
  }
}

async function loadNetworks() {
  if (!state.networkPickerAvailable || state.loadingNetworks) {
    return;
  }
  state.loadingNetworks = true;
  renderNetworks();
  try {
    const res = await fetch("/api/networks");
    if (res.status === 404) {
      state.networkPickerAvailable = false;
      return;
    }
    if (!res.ok) {
      throw new Error(`Network scan failed: ${res.status}`);
    }
    const data = await res.json();
    state.networks = data.networks || [];
  } catch (err) {
    console.warn("Network scan failed", err);
    setEmptyList("Network scan failed");
  } finally {
    state.loadingNetworks = false;
    renderNetworks();
  }
}

async function setTarget(target) {
  const res = await fetch("/api/target", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(target),
  });
  if (!res.ok) {
    throw new Error(`Target update failed: ${res.status}`);
  }
  const data = await res.json();
  state.target = data.target || { ssid: "", bssid: "" };
  elements.statusLine.textContent = `Tracking ${targetLabel()}`;
  renderNetworks();
}

async function selectNetwork(network) {
  try {
    await setTarget({ ssid: network.ssid || "", bssid: network.bssid || "" });
  } catch (err) {
    console.warn("Target update failed", err);
    elements.statusLine.textContent = "Target update failed";
  }
}

async function selectAutoTarget() {
  try {
    await setTarget({ ssid: "", bssid: "" });
  } catch (err) {
    console.warn("Auto target failed", err);
    elements.statusLine.textContent = "Target update failed";
  }
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
      elements.quality.textContent = "Waiting";
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
    elements.quality.textContent = "Paused";
  };
}

setInterval(() => {
  if (state.lastSample) {
    elements.lastSeen.textContent = ageText(state.lastSample.ts_unix_ms);
  }
  if (state.lastUpdate !== 0 && Date.now() - state.lastUpdate > 6000) {
    elements.quality.textContent = "Waiting";
  }
}, 1000);

elements.refreshNetworks.addEventListener("click", loadNetworks);
elements.autoTarget.addEventListener("click", selectAutoTarget);

loadTarget();
loadNetworks();
setInterval(loadNetworks, 15000);
startStream();
