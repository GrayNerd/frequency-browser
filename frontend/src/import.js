// Import window logic. Hosts the schedule-import buttons (and their last
// successful download dates) for the native "Import" window, plus an OK button
// that closes the window.

import {
  ImportEiBi,
  ImportAOKI,
  ImportHFCC,
  LoadImportDates,
  SaveEIBIList,
  SaveAokiList,
  SaveHfccList,
} from "../bindings/frequency-browser/app";
import { Events, Window } from "@wailsio/runtime";

const OK_BUTTON = "ok";

// Show the busy mouse cursor while an import is running, clear it when done.
function setBusy(busy) {
  const el = document.querySelector(".import-window") || document.body;
  el.style.cursor = busy ? "progress" : "";
}

function formatImportDate(dates, source) {
  const recorded = dates && dates[source];
  if (!recorded) return "never";

  const when = new Date(recorded);
  if (isNaN(when.getTime())) return "never";

  const day = String(when.getDate()).padStart(2, "0");
  const month = when.toLocaleString(undefined, { month: "short" });
  const year = when.getFullYear();
  const hours = String(when.getHours()).padStart(2, "0");
  const minutes = String(when.getMinutes()).padStart(2, "0");
  return `${day} ${month} ${year} ${hours}:${minutes}`;
}

async function refreshImportDates() {
  try {
    const dates = (await LoadImportDates()) || {};
    document.getElementById("import-eibi-date").textContent = formatImportDate(dates, "EiBi");
    document.getElementById("import-aoki-date").textContent = formatImportDate(dates, "AOKI");
    document.getElementById("import-hfcc-date").textContent = formatImportDate(dates, "HFCC");
  } catch (error) {
    console.error("Error loading import dates:", error);
  }
}

async function importEiBi() {
  // No file dialog any more: the backend downloads the newest EiBi schedule
  // database (sked-???.csv) plus README.TXT from http://www.eibispace.de/.
  setBusy(true);
  try {
    const data = await ImportEiBi();
    if (!data) return;
    if (data.length === 0) return;
    await SaveEIBIList(data);
    await refreshImportDates();
  } finally {
    setBusy(false);
  }
}

async function importAoki() {
  setBusy(true);
  try {
    const data = await ImportAOKI();
    if (!data) return;
    if (data.length === 0) return;
    await SaveAokiList(data);
    await refreshImportDates();
  } finally {
    setBusy(false);
  }
}

async function importHfcc() {
  setBusy(true);
  try {
    const data = await ImportHFCC();
    if (!data) return;
    if (data.length === 0) return;
    await SaveHfccList(data);
    await refreshImportDates();
  } finally {
    setBusy(false);
  }
}

document.getElementById("import-eibi").addEventListener("click", importEiBi);
document.getElementById("import-aoki").addEventListener("click", importAoki);
document.getElementById("import-hfcc").addEventListener("click", importHfcc);
document.getElementById(OK_BUTTON).addEventListener("click", () => {
  Window.Close();
});

refreshImportDates();
