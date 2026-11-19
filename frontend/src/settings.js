// Settings window logic. Loads the current radio config, saves it on demand,
// and closes the window on Cancel or successful Save.
import { GetRadioStatus, UpdateConfig } from "../bindings/frequency-browser/app";
import { Events, Window } from "@wailsio/runtime";

const addressInput = document.getElementById("address");
const messageEl = document.getElementById("message");

// The address the field was last known to match, used to tell whether the
// window has unsaved edits. The backend listens for these reports so it can
// prompt before quitting with unsaved changes.
let cleanAddress = "";

function reportSettingsState() {
  const address = addressInput.value.trim();
  Events.Emit("settingsState", {
    dirty: address !== cleanAddress,
    address,
  });
}

async function loadCurrent() {
  try {
    const status = await GetRadioStatus();
    addressInput.value = status?.address ?? "";
    cleanAddress = addressInput.value.trim();
    messageEl.textContent = "";
    reportSettingsState();
  } catch (err) {
    messageEl.textContent = `Failed to load current settings: ${err.message || err}`;
  }
}

// The backend emits this every time an already-open Settings window is raised
// again, so the field reflects the current address rather than stale values.
// Unsaved edits are left alone: reloading them would silently discard the very
// changes the shutdown prompt is meant to protect. The initial call covers the
// very first open (page load).
Events.On("settingsOpened", () => {
  if (addressInput.value.trim() !== cleanAddress) return;
  loadCurrent();
});
loadCurrent();

addressInput.addEventListener("input", reportSettingsState);

document.getElementById("cancel").addEventListener("click", () => {
  Window.Close();
});

document.getElementById("save").addEventListener("click", async () => {
  const address = addressInput.value.trim();
  if (!address) {
    messageEl.textContent = "Enter a rigctld address.";
    return;
  }
  try {
    // UpdateConfig validates the address by testing the connection and
    // reverts it itself on failure, so the error message can be shown as-is.
    await UpdateConfig(address);
    cleanAddress = address;
    reportSettingsState();
    Window.Close();
  } catch (err) {
    messageEl.textContent = err.message || String(err);
  }
});
