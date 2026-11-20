// Settings window logic. Two tabs (Appearance, Radio) over the shared
// Cancel/Save footer. Loads the persisted settings, previews the theme live,
// and saves everything through the backend, which validates the rigctld
// address before persisting.
import {
  GetSettings,
  SaveSettings,
  SaveWindowGeometryNow,
} from "../bindings/frequency-browser/app";
import { Events, Window } from "@wailsio/runtime";

const addressInput = document.getElementById("address");
const messageEl = document.getElementById("message");
const saveWindowPosInput = document.getElementById("save-window-pos");
const saveNowButton = document.getElementById("save-now");

// The settings as last loaded or saved. The dirty check compares the form
// against this snapshot, and saving sends it back with the edited fields
// overwritten so window geometry recorded by other paths is preserved.
let clean = null;

// Applies the theme to this window's document. The light palette is the
// html.theme-light override in theme.css; everything else stays dark.
function applyTheme(theme) {
  document.documentElement.classList.toggle("theme-light", theme !== "dark");
}

function show(text, isError) {
  messageEl.textContent = text;
  messageEl.classList.toggle("ok", !isError && text.length > 0);
}

// Reads the editable fields and merges them into the snapshot so untouched
// settings (window geometry) are sent back unchanged.
function collect() {
  const themeInput = document.querySelector('input[name="theme"]:checked');
  return {
    ...clean,
    theme: themeInput ? themeInput.value : "light",
    saveWindowPos: saveWindowPosInput.checked,
    radioAddress: addressInput.value.trim(),
  };
}

function isDirty() {
  if (!clean) return false;
  const current = collect();
  return (
    current.theme !== clean.theme ||
    current.saveWindowPos !== clean.saveWindowPos ||
    current.radioAddress !== clean.radioAddress
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

async function loadCurrent() {
  try {
    const settings = await GetSettings();
    clean = settings ?? {
      theme: "light",
      saveWindowPos: false,
      radioAddress: "localhost:4532",
    };

    const themeInput = document.querySelector(
      `input[name="theme"][value="${clean.theme}"]`,
    );
    if (themeInput) themeInput.checked = true;
    saveWindowPosInput.checked = Boolean(clean.saveWindowPos);
    addressInput.value = clean.radioAddress ?? "";

    applyTheme(clean.theme);
    show("", false);
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

// Live preview: the theme applies to this window as soon as the radio changes.
document.querySelectorAll('input[name="theme"]').forEach((input) => {
  input.addEventListener("change", () => {
    applyTheme(input.value);
    reportState();
  });
});

saveWindowPosInput.addEventListener("change", reportState);
addressInput.addEventListener("input", reportState);

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
  if (!settings.radioAddress) {
    show("Enter a rigctld address.", true);
    return;
  }
  try {
    // SaveSettings validates the address by testing the connection and
    // reverts everything on failure, so the error message can be shown as-is.
    await SaveSettings(settings);
    clean = settings;
    applyTheme(clean.theme);
    reportState();
    Window.Close();
  } catch (err) {
    show(err.message || String(err), true);
  }
});

// The backend emits this every time an already-open Settings window is raised
// again, so the fields reflect current settings rather than stale values.
// Unsaved edits are left alone: reloading them would silently discard the
// very changes the shutdown prompt is meant to protect. The initial call
// covers the very first open (page load).
Events.On("settingsOpened", () => {
  if (isDirty()) return;
  loadCurrent();
});
loadCurrent();
