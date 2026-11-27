// Settings window logic. Two tabs (Appearance, Radio) over the shared
// Cancel/Save footer. Loads the persisted settings, previews the theme live,
// and saves everything through the backend. The Radio tab describes the rig
// completely; the backend starts or restarts the rigctld daemon from these
// values when Save is pressed, and reports hamlib's own error when it refuses.
import {
  GetSettings,
  SaveSettings,
  SaveWindowGeometryNow,
  GetRigModels,
  GetSerialPorts,
  GetDaemonStatus,
  GetRigctlVersion,
} from "../bindings/frequency-browser/app";
import { Events, Window } from "@wailsio/runtime";

const rigSelect = document.getElementById("rig");
const serialPortSelect = document.getElementById("serial-port");
const serialPortRow = document.getElementById("serial-port-row");
const networkAddressRow = document.getElementById("network-address-row");
const networkAddressInput = document.getElementById("network-address");
const serialParamsBox = document.getElementById("serial-params-box");
const baudRateSelect = document.getElementById("baud-rate");
const dtrSelect = document.getElementById("dtr");
const rtsSelect = document.getElementById("rts");
const pollIntervalSelect = document.getElementById("poll-interval");
const hamlibVersionEl = document.getElementById("hamlib-version");
const messageEl = document.getElementById("message");
const saveWindowPosInput = document.getElementById("save-window-pos");
const restrictFrequencyInput = document.getElementById("restrict-frequency");
const saveNowButton = document.getElementById("save-now");

// The settings as last loaded or saved. The dirty check compares the form
// against this snapshot, and saving sends it back with the edited fields
// overwritten so window geometry recorded by other paths is preserved.
let clean = null;

// Port type per rig model ("RS-232", "Network link", "None", ...), from the
// rig list. A backend with no serial port must not be given one: rigctld would
// start and then never answer, which shows up as an i/o timeout on every tune.
let rigPortTypes = new Map();

// Pre-filled Network Address for a network backend. "localhost" is used rather
// than this machine's IP: the daemon usually runs on the same host, and the name
// keeps working when the machine's address changes.
const DEFAULT_REMOTE_ADDRESS = "localhost:4532";

// Applies the theme to this window's document. The light palette is the
// html.theme-light override in theme.css; everything else stays dark.
function applyTheme(theme) {
  document.documentElement.classList.toggle("theme-light", theme !== "dark");
}

function show(text, isError) {
  messageEl.textContent = text;
  messageEl.classList.toggle("ok", !isError && text.length > 0);
}

// Adds (or replaces) one option, keeping the list sorted by label.
function upsertOption(select, value, label) {
  let option = select.querySelector(`option[value="${CSS.escape(String(value))}"]`);
  if (!option) {
    option = document.createElement("option");
    option.value = String(value);
    select.appendChild(option);
  }
  option.textContent = label;
  return option;
}

// Fills the Rig dropdown from Hamlib's own model list, grouped by
// manufacturer. The option value is hamlib's internal model number, which is
// exactly what rigctld's -m option takes.
async function loadRigModels(current) {
  let models = [];
  try {
    models = (await GetRigModels()) || [];
  } catch (err) {
    show(`Could not read the rig list from hamlib: ${err.message || err}`, true);
  }

  rigPortTypes = new Map();
  for (const model of models) {
    if (model && model.portType) {
      rigPortTypes.set(Number(model.id), model.portType);
    }
  }

  rigSelect.innerHTML = "";
  rigSelect.appendChild(new Option("Select a rig...", "0"));

  const groups = new Map();
  for (const model of models) {
    const mfg = model.mfg || "Other";
    if (!groups.has(mfg)) {
      const group = document.createElement("optgroup");
      group.label = mfg;
      rigSelect.appendChild(group);
      groups.set(mfg, group);
    }
    groups.get(mfg).appendChild(new Option(model.model, String(model.id)));
  }

  // Keep the saved rig selectable even when this hamlib build no longer
  // lists it, so saving another field cannot silently drop it.
  if (current && current > 0 && !rigSelect.querySelector(`option[value="${current}"]`)) {
    const group = document.createElement("optgroup");
    group.label = "Saved (not in this hamlib build)";
    group.appendChild(new Option(`Model ${current}`, String(current)));
    rigSelect.appendChild(group);
  }
  rigSelect.value = String(current || 0);
}

// Fills the Serial Port dropdown from the devices this machine exposes. Every
// /dev entry is listed, phantom ttyS ports included: the dropdown shows what
// the system has. A saved port that is currently unplugged is kept so the
// setting is not lost.
async function loadSerialPorts(current) {
  let ports = [];
  try {
    ports = (await GetSerialPorts()) || [];
  } catch (err) {
    console.warn("Could not list serial ports:", err);
  }

  serialPortSelect.innerHTML = "";
  serialPortSelect.appendChild(new Option("None (network or virtual rig)", ""));
  for (const port of ports) {
    serialPortSelect.appendChild(new Option(port.label || port.path, port.path));
  }
  if (current && !serialPortSelect.querySelector(`option[value="${CSS.escape(current)}"]`)) {
    serialPortSelect.appendChild(new Option(`${current} (not connected)`, current));
  }
  serialPortSelect.value = current || "";
}

function checkedValue(name, fallback) {
  const input = document.querySelector(`input[name="${name}"]:checked`);
  return input ? input.value : fallback;
}

function setCheckedRadio(name, value) {
  const inputs = document.querySelectorAll(`input[name="${name}"]`);
  let matched = false;
  inputs.forEach((input) => {
    const isMatch = input.value === (value ?? "");
    input.checked = isMatch;
    matched = matched || isMatch;
  });
  // Unknown stored value: fall back to "Default" rather than leaving none
  // selected.
  if (!matched && inputs.length > 0) {
    inputs[0].checked = true;
  }
}

// A network backend (hamlib's NET rigctl, or any other network link) is driven by
// the address of another rigctld rather than by a serial device, so the CAT
// control box swaps its serial controls for a host:port field.
function isNetworkBackend(rigModel) {
  const portType = rigPortTypes.get(Number(rigModel));
  return portType === "Network link";
}

// A backend with no port at all ("None") cannot take a serial port either, but
// it does not take a network address: it has nothing to configure.
function hasNoPort(rigModel) {
  return rigPortTypes.get(Number(rigModel)) === "None";
}

function refreshCatControl() {
  const network = isNetworkBackend(rigSelect.value);
  const noPort = hasNoPort(rigSelect.value);

  // One of the three shapes: serial, network, or nothing to configure.
  serialPortRow.hidden = network || noPort;
  networkAddressRow.hidden = !network;
  serialParamsBox.hidden = network || noPort;

  if (network) {
    serialPortSelect.value = "";
    // Offer the sensible default rather than leaving the field empty, but
    // never overwrite an address the user has already typed.
    if (networkAddressInput.value.trim() === "") {
      networkAddressInput.value = DEFAULT_REMOTE_ADDRESS;
    }
    networkAddressInput.focus();
  } else if (noPort) {
    serialPortSelect.value = "";
    networkAddressInput.value = "";
  }
}

// Reads the editable fields and merges them into the snapshot so untouched
// settings (window geometry) are sent back unchanged.
function collect() {
  const themeInput = document.querySelector('input[name="theme"]:checked');
  return {
    ...clean,
    theme: themeInput ? themeInput.value : "light",
    saveWindowPos: saveWindowPosInput.checked,
    restrictFrequency: restrictFrequencyInput.checked,
    rigModel: Number(rigSelect.value) || 0,
    serialPort: isNetworkBackend(rigSelect.value) || hasNoPort(rigSelect.value)
      ? ""
      : serialPortSelect.value,
    remoteAddress: isNetworkBackend(rigSelect.value)
      ? networkAddressInput.value.trim()
      : "",
    baudRate: Number(baudRateSelect.value) || 0,
    dataBits: checkedValue("data-bits", ""),
    stopBits: checkedValue("stop-bits", ""),
    handshake: checkedValue("handshake", ""),
    dtr: dtrSelect.value,
    rts: rtsSelect.value,
    pollIntervalMs: Number(pollIntervalSelect.value) || 0,
  };
}

function isDirty() {
  if (!clean) return false;
  const current = collect();
  return (
    current.theme !== clean.theme ||
    current.saveWindowPos !== clean.saveWindowPos ||
    current.restrictFrequency !== clean.restrictFrequency ||
    current.rigModel !== clean.rigModel ||
    current.serialPort !== clean.serialPort ||
    current.remoteAddress !== (clean.remoteAddress ?? "") ||
    current.baudRate !== clean.baudRate ||
    current.dataBits !== clean.dataBits ||
    current.stopBits !== clean.stopBits ||
    current.handshake !== clean.handshake ||
    current.dtr !== clean.dtr ||
    current.rts !== clean.rts ||
    current.pollIntervalMs !== clean.pollIntervalMs
  );
}

// The backend listens for these reports so a shutdown can prompt before
// discarding edits, and save the full pending state if the user chooses Save.
function reportState() {
  Events.Emit("settingsState", {
    dirty: isDirty(),
    settings: collect(),
  });
}

// Keeps the daemon state reachable now that the status line is gone: the Save
// button carries it as a tooltip, so the "external daemon is not restarted"
// warning is still visible where it matters.
async function refreshDaemonStatus() {
  const saveButton = document.getElementById("save");
  try {
    const status = await GetDaemonStatus();
    if (!status || !status.running) {
      saveButton.title = "No rigctld daemon is running. Save starts one with these settings.";
      return;
    }
    const owner = status.owned
      ? `started by this app (pid ${status.pid})`
      : "already running, not started by this app";
    saveButton.title =
      `rigctld is listening on ${status.address} - ${owner}.` +
      (status.note ? ` ${status.note}` : "");
  } catch (err) {
    saveButton.title = "";
    console.warn("Could not read the daemon status:", err);
  }
}

// Shows which Hamlib is installed, from `rigctl -V`. This is the hamlib the
// daemon will be started from, so it explains which rig models are available.
async function loadHamlibVersion() {
  try {
    const version = await GetRigctlVersion();
    hamlibVersionEl.textContent = version || "";
  } catch (err) {
    hamlibVersionEl.textContent = "Hamlib version unavailable";
    hamlibVersionEl.title = String((err && err.message) || err);
    console.warn("Could not read the rigctl version:", err);
  }
}

async function loadCurrent() {
  try {
    const settings = await GetSettings();
    clean = settings ?? {
      theme: "light",
      saveWindowPos: false,
      restrictFrequency: false,
      rigModel: 0,
      serialPort: "",
      baudRate: 19200,
      dataBits: "",
      stopBits: "",
      handshake: "",
      dtr: "",
      rts: "",
      pollIntervalMs: 500,
    };

    const themeInput = document.querySelector(
      `input[name="theme"][value="${clean.theme}"]`,
    );
    if (themeInput) themeInput.checked = true;
    saveWindowPosInput.checked = Boolean(clean.saveWindowPos);
    restrictFrequencyInput.checked = Boolean(clean.restrictFrequency);

    await loadRigModels(clean.rigModel);
    await loadSerialPorts(clean.serialPort);
    networkAddressInput.value = clean.remoteAddress ?? "";
    refreshCatControl();
    // A first-run config has no stored address; treat the pre-filled default as
    // the baseline so opening Settings does not immediately look dirty.
    if (clean.remoteAddress === undefined || clean.remoteAddress === "") {
      clean = { ...clean, remoteAddress: networkAddressInput.value };
    }

    baudRateSelect.value = String(clean.baudRate || 19200);
    setCheckedRadio("data-bits", clean.dataBits);
    setCheckedRadio("stop-bits", clean.stopBits);
    setCheckedRadio("handshake", clean.handshake);
    dtrSelect.value = clean.dtr ?? "";
    rtsSelect.value = clean.rts ?? "";
    pollIntervalSelect.value = String(clean.pollIntervalMs ?? 500);

    applyTheme(clean.theme);
    show("", false);
    refreshDaemonStatus();
    loadHamlibVersion();
    reportState();
  } catch (err) {
    show(`Failed to load current settings: ${err.message || err}`, true);
  }
}

// Tab switching: one panel visible at a time.
function selectTab(name) {
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.classList.toggle("active", tab.dataset.tab === name);
  });
  document.querySelectorAll(".panel").forEach((panel) => {
    panel.classList.toggle("active", panel.dataset.panel === name);
  });
}

document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => selectTab(tab.dataset.tab));
});

// Live preview removed: nothing takes effect until Save is pressed.
// Every control just reports the unsaved state.
document.querySelectorAll('input[name="theme"]').forEach((input) => {
  input.addEventListener("change", () => {
    reportState();
  });
});

// Every other control just reports the unsaved state.
saveWindowPosInput.addEventListener("change", reportState);
networkAddressInput.addEventListener("input", reportState);
for (const el of [
  rigSelect,
  serialPortSelect,
  baudRateSelect,
  dtrSelect,
  rtsSelect,
  pollIntervalSelect,
]) {
  el.addEventListener("change", reportState);
}
rigSelect.addEventListener("change", () => {
  refreshCatControl();
  reportState();
});
document
  .querySelectorAll('input[name="data-bits"], input[name="stop-bits"], input[name="handshake"]')
  .forEach((input) => input.addEventListener("change", reportState));

saveNowButton.addEventListener("click", async () => {
  try {
    const saved = await SaveWindowGeometryNow();
    show(
      `Saved ${saved.windowWidth}x${saved.windowHeight} at ${saved.windowX},${saved.windowY}.`,
      false,
    );
  } catch (err) {
    show(err.message || String(err), true);
  }
});

document.getElementById("cancel").addEventListener("click", () => {
  Window.Close();
});

document.getElementById("save").addEventListener("click", async () => {
  const settings = collect();
  if (!settings.rigModel) {
    show("Choose your rig before saving.", true);
    return;
  }
  try {
    // SaveSettings restarts the daemon and reverts everything when it cannot
    // start, so hamlib's own explanation can be shown as-is.
    await SaveSettings(settings);
    clean = settings;
    applyTheme(clean.theme);
    reportState();
    Window.Close();
  } catch (err) {
    show(err.message || String(err), true);
    refreshDaemonStatus();
  }
});

// The backend emits this every time an already-open Settings window is raised
// again, so the fields reflect current settings rather than stale values.
// Unsaved edits are left alone: reloading them would silently discard the
// very changes the shutdown prompt is meant to protect. The initial call
// covers the very first open (page load).
Events.On("settingsOpened", () => {
  refreshDaemonStatus();
  if (isDirty()) return;
  loadCurrent();
});
loadCurrent();
