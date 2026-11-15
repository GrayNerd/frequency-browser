// Settings window logic. Loads the current radio config, saves it on demand,
// and closes the window on Cancel or successful Save.
import { GetRadioStatus, UpdateConfig } from "../bindings/frequency-browser/app";
import { Events, Window } from "@wailsio/runtime";

const addressInput = document.getElementById("address");
const messageEl = document.getElementById("message");

async function loadCurrent() {
  try {
    const status = await GetRadioStatus();
    addressInput.value = status?.address ?? "";
    messageEl.textContent = "";
  } catch (err) {
    messageEl.textContent = `Failed to load current settings: ${err.message || err}`;
  }
}

// The backend emits this every time an already-open Settings window is raised
// again, so the field reflects the current address rather than stale values.
// The initial call covers the very first open (page load).
Events.On("settingsOpened", loadCurrent);
loadCurrent();

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
    Window.Close();
  } catch (err) {
    messageEl.textContent = err.message || String(err);
  }
});
