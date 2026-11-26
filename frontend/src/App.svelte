<script>
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import { MoveRowsModule, TabulatorFull as Tabulator } from "tabulator-tables";
  import "tabulator-tables/dist/css/tabulator.min.css";
  import {
    ImportEiBi,
    LoadEIBIList,
    SaveEIBIList,
    ImportAOKI,
    LoadAokiList,
    SaveAokiList,
    ImportHFCC,
    LoadHfccList,
    SaveHfccList,
    GetCurrentFrequency,
    SetFrequency,
    GetMode,
    GetRadioStatus,
    SendCustomCommand,
    GetFrequencyRange,
    GetSettings,
    LoadColumnWidths,
    SaveColumnWidths,
    LoadColumnOrder,
    SaveColumnOrder,
    SaveFilterOptions,
  } from "../bindings/frequency-browser/app";
  import app from "./main";

  // UI State and Components
  let table;
  let tableContainer;
  let tabulatorInitialized = false;
  let hasMounted = false;

  // Data State
  let allData = [];
  let loading = false;
  let loadingEiBi = false;
  let loadingAoki = false;
  let loadingHfcc = false;

  // Status and Messages
  let statusMessage = "Connecting to radio...";
  let errorMessage = "";
  let successMessage = "";
  let debugInfo = "";
  let connected = false;

  // How long a message shown at the bottom of the window stays there. Failures
  // are kept longer so an error is not missed.
  const MESSAGE_TIMEOUT_MS = 5000;
  const ERROR_TIMEOUT_MS = 10000;
  // How long typing in the manual frequency box waits before the grid is
  // re-filtered: table.setData() rebuilds every row, so doing it on every
  // keystroke is wasteful and visibly flickers.
  const MANUAL_INPUT_DEBOUNCE_MS = 150;
  let messageTimer = null;

  // Clears the messages and cancels the pending countdown. Called when a new
  // operation starts so an old message cannot be mistaken for its result, and
  // again by the countdown itself when the message expires.
  function clearMessages() {
    if (messageTimer !== null) {
      clearTimeout(messageTimer);
      messageTimer = null;
    }
    errorMessage = "";
    successMessage = "";
    debugInfo = "";
  }

  // Radio Configuration and State
  let config = {
    address: "localhost:4532",
  };
  let mode = "Unknown";
  let frequency = 0;

  // Frequency Filtering
  let frequencyFilterMode = "near"; // "exact", "near", "all", or "manual"
  let frequencyFilterTimeout;
  let exactFrequency = false;
  let manualFrequency = false;
  let editableFrequency = "";
  let showManualInput = false;
  // The manual frequency is debounced: the field shows each keystroke at once,
  // while the value the filters use follows a beat behind so typing does not
  // rebuild the grid for every character.
  let debouncedEditableFrequency = "";
  let manualInputDebounceTimer = null;

  // Time Filtering
  let currentTimeFilter = "now";
  let currentUTCTime = "0000";
  let manualTimeInput = "";
  let showManualTimeInput = false;
  let manualTimeMode = false;
  let timeInterval;

  // Command Handling
  let customCommand = "";
  let commandResponse = "";

  // Frequency of the last row the user clicked, kept visible across refilters
  let pinnedKhz = null;

  // Tuning range of the connected rig (kHz). Entries outside it are hidden
  // because the radio physically cannot receive them.
  let rigMinKhz = null;
  let rigMaxKhz = null;

  // Input filtering functions
  let filterSeq = 0;
  let lastFilteredRows = null;

  // Re-apply the pinned-row highlight without rebuilding the table.
  function refreshPinnedHighlight() {
    if (!table) return;
    table.getRows("display").forEach((row) => {
      const el = row.getElement();
      if (el) {
        const isPinned =
          pinnedKhz !== null && parseFloat(row.getData().khz) === pinnedKhz;
        el.classList.toggle("row-pinned", isPinned);
      }
    });
  }

  // True when two filter results contain the same row objects in the same order.
  function sameRows(a, b) {
    if (!a || !b || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (a[i] !== b[i]) return false;
    }
    return true;
  }

  // True when the entry falls inside the rig's tuning range. Entries outside
  // it are dropped from the list because the radio cannot receive them.
  function withinRigRange(row) {
    if (rigMinKhz === null || rigMaxKhz === null) return true;
    const khz = parseFloat(row.khz);
    if (isNaN(khz)) return false;
    return khz >= rigMinKhz && khz <= rigMaxKhz;
  }

  // Ask the backend for the connected rig's tuning range.
  async function loadRigRange() {
    try {
      const range = await GetFrequencyRange();
      if (range && isFinite(range.minKhz) && isFinite(range.maxKhz)) {
        rigMinKhz = range.minKhz;
        rigMaxKhz = range.maxKhz;
        // Invalidate the cached filter result so the new range is applied.
        lastFilteredRows = null;
        console.log(`Rig tuning range: ${rigMinKhz} kHz - ${rigMaxKhz} kHz`);
      }
    } catch (err) {
      // Leave the range unset so the list is shown unfiltered.
      console.warn("Could not determine rig tuning range:", err);
    }
  }

  // Computes the rows that pass the active filters. Kept separate from
  // applyAllFilters() so the startup path can filter before the grid exists.
  function computeFilteredRows() {
    if (allData.length === 0) {
      return [];
    }

    let filteredData = [...allData];

    // Drop entries the connected radio cannot receive at all.
    if (rigMinKhz !== null && rigMaxKhz !== null) {
      filteredData = filteredData.filter(withinRigRange);
    }

    // Apply frequency filtering
    if (frequencyFilterMode !== "all") {
      if (frequencyFilterMode === "exact") {
        filteredData = filteredData.filter((row) => {
          const rowFrequency = parseFloat(row.khz);
          return Math.abs(rowFrequency - frequency) < 0.001;
        });
      } else if (frequencyFilterMode === "near") {
        filteredData = filteredData.filter((row) => {
          const rowFrequency = parseFloat(row.khz);
          return Math.abs(rowFrequency - frequency) <= 2.5;
        });
      } else if (frequencyFilterMode === "manual") {
        // The debounced value, not the raw field, so every filter pass sees
        // the same settled input even when another trigger (say, a rig
        // frequency update) re-filters while the user is still typing.
        const manualFreq = parseFloat(debouncedEditableFrequency);
        if (!isNaN(manualFreq)) {
          filteredData = filteredData.filter((row) => {
            const rowFrequency = parseFloat(row.khz);
            return Math.abs(rowFrequency - manualFreq) <= 2.5;
          });
        }
      }
    }

    // Apply time filtering
    if (currentTimeFilter !== "all") {
      let timeToUse = currentUTCTime;

      if (currentTimeFilter === "manual" && manualTimeInput.length === 4) {
        timeToUse = manualTimeInput;
      }

      const filterMinutes = timeToMinutes(timeToUse);
      if (filterMinutes === null) {
        // Nothing sensible to filter against: leave the list untouched rather
        // than blanking the grid while the user is still typing a time.
        console.warn("Ignoring time filter: unusable time", timeToUse);
      } else if (currentTimeFilter === "now" || currentTimeFilter === "manual") {
        // Rows whose start or end time is malformed are skipped individually:
        // they must not prevent the remaining rows from being filtered.
        let skipped = 0;
        filteredData = filteredData.filter((row) => {
          if (timeToMinutes(row.startTime) === null || timeToMinutes(row.endTime) === null) {
            skipped++;
            return false;
          }
          return isOnAir(row.startTime, row.endTime, filterMinutes);
        });
        if (skipped > 0) {
          console.log(`Time filter skipped ${skipped} rows with invalid times`);
        }
      } else if (currentTimeFilter === "soon") {
        let skipped = 0;
        filteredData = filteredData.filter((row) => {
          const startMinutes = timeToMinutes(row.startTime);
          const endMinutes = timeToMinutes(row.endTime);
          if (startMinutes === null || endMinutes === null) {
            skipped++;
            return false;
          }

          // Already on air, or starting within the next half hour.
          const isSoon =
            ((startMinutes - filterMinutes + 1440) % 1440) <= 30;

          return isOnAir(row.startTime, row.endTime, filterMinutes) || isSoon;
        });
        if (skipped > 0) {
          console.log(`Time filter skipped ${skipped} rows with invalid times`);
        }
      }
    }

    console.log(`Filtered to ${filteredData.length} records`);
    return filteredData;
  }

  // Re-applies the active filters to the grid.
  async function applyAllFilters() {
    console.log(
      "Applying filters - Frequency mode:",
      frequencyFilterMode,
      "Time mode:",
      currentTimeFilter,
    );

    if (!table) {
      console.warn("Table not initialized yet");
      return;
    }

    const seq = ++filterSeq;
    const filteredData = computeFilteredRows();

    if (allData.length === 0) {
      console.warn("No data to filter");
      await table.setData([]);
      lastFilteredRows = null;
      return;
    }

    // table.setData() destroys and rebuilds every row, which resets the
    // scroll offset to the top and causes a visible flicker. Only rebuild
    // when the filter result actually changed; otherwise just refresh the
    // highlight and leave the grid exactly where it is.
    if (sameRows(lastFilteredRows, filteredData)) {
      refreshPinnedHighlight();
      return;
    }
    lastFilteredRows = filteredData;

    // Remember the scroll offset so the view stays put across the rebuild.
    const scrollEl = table.rowManager?.element;
    const savedScrollTop = scrollEl ? scrollEl.scrollTop : 0;

    await table.setData(filteredData);

    // Abandon this pass if a newer filter run has started meanwhile.
    if (seq !== filterSeq) return;

    // setData() resolves before the rows are painted, so let the renderer
    // finish a frame before restoring the view.
    await new Promise((resolve) => requestAnimationFrame(resolve));
    if (seq !== filterSeq) return;

    // Restore the offset we were at before the refilter. Never scroll to the
    // clicked row: the grid should stay exactly where the user left it.
    if (scrollEl && savedScrollTop > 0) {
      scrollEl.scrollTop = savedScrollTop;
    }
  }
  // Add this debug code to see which rows have invalid time formats
  function logInvalidTimeData(data) {
    data.forEach((row, index) => {
      if (!row.startTime || !/^\d{4}$/.test(row.startTime)) {
        console.warn(`Row ${index} has invalid startTime:`, row.startTime, row);
      }
      if (!row.endTime || !/^\d{4}$/.test(row.endTime)) {
        console.warn(`Row ${index} has invalid endTime:`, row.endTime, row);
      }
    });
  }

  function handleManualInput(event) {
    const input = event.target.value;
    const filteredInput = input.replace(/[^0-9.]/g, "");

    const decimalCount = (filteredInput.match(/\./g) || []).length;
    if (decimalCount > 1) {
      const firstDecimalIndex = filteredInput.indexOf(".");
      editableFrequency =
        filteredInput.substring(0, firstDecimalIndex + 1) +
        filteredInput.substring(firstDecimalIndex + 1).replace(/\./g, "");
    } else {
      editableFrequency = filteredInput;
    }

    event.target.value = editableFrequency;

    // Restart the debounce window; the consolidated reactive trigger picks the
    // value up once it settles.
    if (manualInputDebounceTimer !== null) {
      clearTimeout(manualInputDebounceTimer);
    }
    manualInputDebounceTimer = setTimeout(() => {
      manualInputDebounceTimer = null;
      debouncedEditableFrequency = editableFrequency;
    }, MANUAL_INPUT_DEBOUNCE_MS);
  }

  function handleManualKeypress(event) {
    // Allow only numbers, decimal point, and navigation keys
    if (
      !/[0-9.]/.test(event.key) &&
      event.key !== "Enter" &&
      event.key !== "Backspace" &&
      event.key !== "Delete" &&
      event.key !== "ArrowLeft" &&
      event.key !== "ArrowRight" &&
      event.key !== "Tab"
    ) {
      event.preventDefault();
    }

    // Handle Enter key to apply frequency
    if (event.key === "Enter") {
      applyManualFrequency();
    }
  }

  async function applyManualFrequency() {
    if (editableFrequency && editableFrequency.trim() !== "") {
      try {
        const freqValue = parseFloat(editableFrequency);
        if (!isNaN(freqValue)) {
          // Enter applies immediately: flush any pending debounce so the
          // filters use exactly the frequency being tuned to. The reactive
          // trigger then re-filters when the radio frequency updates.
          if (manualInputDebounceTimer !== null) {
            clearTimeout(manualInputDebounceTimer);
            manualInputDebounceTimer = null;
          }
          debouncedEditableFrequency = editableFrequency;
          await SetFrequency(freqValue);
          frequency = freqValue;
          statusMessage = "Manual frequency set successfully";
        }
      } catch (error) {
        statusMessage = "Failed to set manual frequency";
        console.error("Set frequency error:", error);
      }
    }
  }

  async function importEiBi() {
    loading = true;
    loadingEiBi = true; // Change text only for this button
    clearMessages();

    try {
      // No file dialog any more: the backend downloads the newest EiBi schedule
      // database (sked-???.csv) plus README.TXT from http://www.eibispace.de/.
      console.log("Downloading newest EiBi schedule from http://www.eibispace.de/...");
      const data = await ImportEiBi();
      console.log("EiBi data received:", data);

      if (!data) {
        showSuccess("EiBi import cancelled");
        return;
      }

      if (data.length === 0) {
        showSuccess("No data rows found in the newest EiBi schedule");
        return;
      }

      await SaveEIBIList(data);
      allData = await combineLists();
      console.log(`Combined to ${allData.length} records`);

      showSuccess(`Successfully imported ${data.length} records from the newest EiBi schedule (reference tables refreshed) and saved to eibi-list.json`);
    } catch (err) {
      showError(`Failed to import EiBi data: ${err.message || "Unknown error"}`);
      console.error("EiBi Import error details:", err);
      debugInfo = `EiBi Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingEiBi = false;
    }
  }

  async function importAoki() {
    loading = true;
    loadingAoki = true; // Change text only for this button
    clearMessages();

    try {
      // No file dialog any more: the backend downloads the newest AOKI shortwave
      // schedule archive from the Bi Newsletter page and unpacks the .txt inside.
      console.log("Downloading newest AOKI schedule from the Bi Newsletter page...");
      const data = await ImportAOKI();
      console.log("Aoki data received:", data);

      if (!data) {
        showSuccess("Aoki import cancelled");
        return;
      }

      if (data.length === 0) {
        showSuccess("No data rows found in the newest AOKI schedule");
        return;
      }

      await SaveAokiList(data);
      allData = await combineLists();

      showSuccess(`Successfully imported ${data.length} records from the newest AOKI schedule and saved to aoki-list.json`);
    } catch (err) {
      showError(`Failed to import AOKI data: ${err.message || "Unknown error"}`);
      console.error("Aoki Import error details:", err);
      debugInfo = `Aoki Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingAoki = false;
    }
  }

  async function importHFCC() {
    loading = true;
    loadingHfcc = true; // Change text only for this button
    clearMessages();

    try {
      // No file dialog any more: the backend downloads the newest operational
      // schedule archive from https://new.hfcc.org/data/ and parses it.
      console.log("Downloading newest HFCC schedule from https://new.hfcc.org/data/...");
      const data = await ImportHFCC();
      console.log("Hfcc data received:", data);

      if (!data) {
        showSuccess("Hfcc import cancelled");
        return;
      }

      if (data.length === 0) {
        showSuccess("No data rows found in the newest HFCC schedule");
        return;
      }

      await SaveHfccList(data);
      allData = await combineLists();

      showSuccess(`Successfully imported ${data.length} records from the newest HFCC schedule (reference tables refreshed) and saved to hfcc-list.json`);
    } catch (err) {
      showError(`Failed to import HFCC data: ${err.message || "Unknown error"}`);
      console.error("hfcc Import error details:", err);
      debugInfo = `Hfcc Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingHfcc = false;
    }
  }

  // Parses an HHMM string into minutes from midnight.
  //
  // Returns null for anything that is not a valid four-digit time, instead of
  // throwing: the imported lists contain a few malformed rows (AOKI has entries
  // such as "000" or "0."), and a throw inside Array.filter() would abort the
  // whole filter and leave the table unfiltered.
  //
  // "2400" is end of day, i.e. the same instant as "0000" of the next day, so it
  // is normalised to 0. Without that, a row such as 2300-2400 would be treated
  // as wrapping past midnight and disappear from the "now" list at 00:00.
  function timeToMinutes(timeString) {
    const value = String(timeString ?? "").trim();
    if (!/^\d{4}$/.test(value)) {
      return null;
    }

    const hours = parseInt(value.substring(0, 2), 10);
    const minutes = parseInt(value.substring(2, 4), 10);

    if (
      hours < 0 ||
      hours > 24 ||
      minutes < 0 ||
      minutes > 59 ||
      (hours === 24 && minutes !== 0)
    ) {
      return null;
    }

    // 2400 == 0000 of the following day.
    return (hours * 60 + minutes) % 1440;
  }

  // True when a broadcast from startTime to endTime is on air at filterMinutes.
  //
  // A range whose end is at or before its start runs past midnight
  // (e.g. 2300-0100), and an identical start and end means a full 24 hours
  // (e.g. 1200-1200).
  function isOnAir(startTime, endTime, filterMinutes) {
    const startMinutes = timeToMinutes(startTime);
    const endMinutes = timeToMinutes(endTime);
    if (startMinutes === null || endMinutes === null) {
      return false;
    }
    if (startMinutes === endMinutes) {
      return true; // full-day entry
    }
    if (endMinutes > startMinutes) {
      return filterMinutes >= startMinutes && filterMinutes <= endMinutes;
    }
    // Spans midnight: either after the start or before the end.
    return filterMinutes >= startMinutes || filterMinutes <= endMinutes;
  }

  function initTabulator(data) {
    console.log("Initializing Tabulator with data:", data.length, "records");
    if (tabulatorInitialized && table) {
      // Re-applying the filters is the caller's job; doing it here as well
      // would race the setData() above.
      table.setData(data);
      return;
    }

    if (!tableContainer) {
      console.error("Table container not found!");
      return;
    }

    table = new Tabulator(tableContainer, {
      data: data,
      layout: "fitColumns",
      // Hide columns instead of wrapping row content when the window is too
      // narrow ("collapse" mode puts cell content into a wrapped row).
      responsiveLayout: true,
      // Let the user drag column headers to reorder them; the new order is
      // saved and restored across sessions.
      movableColumns: true,
      modules: [MoveRowsModule],
      height: "100%",
      // Applied on every row render, so the pinned-row highlight survives the
      // full row rebuild that setData() performs when filters are re-applied.
      rowFormatter: (row) => {
        const el = row.getElement();
        const isPinned =
          pinnedKhz !== null && parseFloat(row.getData().khz) === pinnedKhz;
        el.classList.toggle("row-pinned", isPinned);
      },
      columns: [
        { title: "kHz", field: "khz", headerFilter: "input", width: 60 },
        {
          title: "Start",
          field: "startTime",
          headerFilter: "input",
          width: 75,
        },
        {
          title: "End",
          field: "endTime",
          headerFilter: "input",
          width: 75,
        },
        {
          title: "Days 1=Su",
          field: "days",
          headerFilter: "input",
          width: 105,
          formatter: (cell) => {
            // Create a span element with the desired font family
            const span = document.createElement("span");
            span.style.fontFamily = "monospace";
            span.style.fontSize = "13px";
            span.textContent = cell.getValue();
            return span; // Return the span element
          },
        },
        {
          title: "Country",
          field: "country",
          headerFilter: "input",
          widthShrink: 1,
          minWidth: 100,
        },
        {
          title: "Station",
          field: "station",
          headerFilter: "input",
          widthShrink: 2,
          minWidth: 100,
        },
        {
          title: "Language",
          field: "language",
          headerFilter: "input",
          widthShrink: 1,
          minWidth: 100,
        },
        {
          title: "Target",
          field: "target",
          headerFilter: "input",
          widthShrink: 1,
          minWidth: 100,
        },
        {
          title: "Transmitter",
          field: "transmitter",
          headerFilter: "input",
          widthShrink: 1,
          minWidth: 80,
        },
        {
          title: "Source",
          field: "source",
          headerFilter: "input",
          width: 79,
        },
      ],
    });

    // Restore persisted column widths, if any. Columns without a saved width
    // keep the default declared above.
    (async () => {
      try {
        const saved = await LoadColumnWidths();
        if (saved) {
          for (const col of table.getColumns()) {
            const field = col.getField();
            const width = saved[field];
            if (field && typeof width === "number" && width > 0) {
              col.setWidth(width);
            }
          }
        }
      } catch (err) {
        console.warn("Could not load column widths:", err);
      }
    })();

    // Restore persisted column order, if any. Columns are moved to match the
    // saved field order; any column not mentioned keeps its relative position.
    (async () => {
      try {
        const saved = await LoadColumnOrder();
        if (saved && saved.length > 0) {
          const fields = saved.filter((f) => typeof f === "string");
          // Walk the saved order left to right, moving each field to its
          // target position so the final order matches.
          let insertAfter = null;
          for (const field of fields) {
            const col = table.getColumn(field);
            if (col) {
              col.move(insertAfter);
              insertAfter = field;
            }
          }
        }
      } catch (err) {
        console.warn("Could not load column order:", err);
      }
    })();

    // Persist column widths whenever the user resizes a column, so the layout
    // survives between sessions.
    table.on("columnResized", async (column) => {
      try {
        const widths = {};
        for (const col of table.getColumns()) {
          const field = col.getField();
          if (field) {
            widths[field] = col.getWidth();
          }
        }
        await SaveColumnWidths(widths);
      } catch (err) {
        console.warn("Could not save column widths:", err);
      }
    });

    // Persist column order whenever the user drags a column to reorder it.
    table.on("columnMoved", async (column, columns) => {
      try {
        const order = table.getColumns().map((col) => col.getField());
        await SaveColumnOrder(order);
      } catch (err) {
        console.warn("Could not save column order:", err);
      }
    });

    // Tabulator 5.x removed option-based event callbacks (rowClick: fn is
    // silently ignored). Events must be subscribed with table.on().
    table.on("rowClick", async (e, row) => {
      const rowData = row.getData();
      const khz = parseFloat(rowData.khz);
      if (isNaN(khz)) {
        showError("Invalid frequency in row: " + rowData.khz);
        return;
      }
      // Anchor the view on this row immediately, even if tuning later fails,
      // so a rejected frequency doesn't throw the user back to the top.
      pinnedKhz = khz;
      try {
        // Tune the radio to the frequency in the clicked row's kHz column
        await SetFrequency(khz);
        // Re-read the frequency from the radio so the display reflects
        // what the rig actually accepted, not just what we asked for
        const actual = await GetCurrentFrequency();
        frequency = typeof actual === "string" ? parseFloat(actual) : actual;
        statusMessage = "Radio tuned to " + formatFrequency(frequency) + " kHz";
        errorMessage = "";
      } catch (err) {
        console.error("Set frequency error:", err);
        showError("Failed to set frequency: " + (err.message || String(err)));
      }
      // Re-apply the filters right away instead of waiting for the reactive
      // trigger: when tuning failed the radio frequency did not change, so
      // nothing else would refresh the pinned-row highlight. When tuning
      // succeeded the trigger fires as well; applyAllFilters() deduplicates.
      applyAllFilters();
    });

    tabulatorInitialized = true;
  }

  function handleResize() {
    if (table) {
      table.redraw(true);
    }
  }

  function formatFrequency(freq) {
    const num = typeof freq === "string" ? parseFloat(freq) : freq;
    if (isNaN(num)) return "0.000";
    return num.toFixed(3);
  }

  async function updateStatus() {
    try {
      const status = await GetRadioStatus();
      connected = status.connected;
      if (status.frequency !== undefined) {
        frequency =
          typeof status.frequency === "string"
            ? parseFloat(status.frequency)
            : status.frequency;
      }
      if (status.mode) mode = status.mode;
      statusMessage = connected ? "Connected to radio" : "Disconnected";
    } catch (error) {
      statusMessage = "Error getting status";
      console.error("Status error:", error);
    }
  }

  // Loads every saved list and returns the rows combined. Assigning the result
  // to allData is the caller's job, so exactly one assignment per import drives
  // the reactive re-filter.
  async function combineLists() {
    try {
      console.log("Loading existing data...");
      const [eibiData, aokiData, hfccData] = await Promise.all([
        LoadEIBIList().catch(() => []),
        LoadAokiList().catch(() => []),
        LoadHfccList().catch(() => []),
      ]);

      const combined = [
        ...(eibiData || []),
        ...(aokiData || []),
        ...(hfccData || []),
      ];
      // Sort by frequency so the grid reads in tuning order instead of in
      // source-block order (EiBi, then Aoki, then HFCC). kHz arrives as a
      // string, so compare numerically; rows with an unparseable frequency
      // sort last. Ties fall back to start time so the schedule order reads
      // naturally, and otherwise keep their combined order (the sort is
      // stable).
      combined.sort((a, b) => {
        const fa = parseFloat(a.khz);
        const fb = parseFloat(b.khz);
        const na = isNaN(fa);
        const nb = isNaN(fb);
        if (na !== nb) {
          return na ? 1 : -1;
        }
        if (!na && fa !== fb) {
          return fa - fb;
        }
        const ta = String(a.startTime ?? "");
        const tb = String(b.startTime ?? "");
        if (ta !== tb) {
          return ta < tb ? -1 : 1;
        }
        return 0;
      });
      console.log("Combined data loaded:", combined.length, "records");
      return combined;
    } catch (error) {
      console.error("Error combining lists:", error);
      showError("Failed to load combined data");
      return [];
    }
  }

  // Applies the persisted or newly saved theme by toggling the light
  // palette on the document root; the CSS variables in theme.css do the rest.
  function applyTheme(theme) {
    document.documentElement.classList.toggle("theme-light", theme !== "dark");
  }

  onMount(() => {
    console.log("Component mounting...");
    if (hasMounted) return;
    hasMounted = true;

    // Start UTC clock
    updateUTCTime();
    timeInterval = setInterval(updateUTCTime, 1000);

    (async () => {
      try {
        // Apply the persisted theme before anything else paints.
        try {
          const settings = await GetSettings();
          applyTheme(settings ? settings.theme : null);
          // Restore the persisted filter modes before the grid is built so
          // they take effect on the first render instead of defaulting.
          if (settings) {
            if (settings.frequencyFilterMode) {
              frequencyFilterMode = settings.frequencyFilterMode;
            }
            if (settings.currentTimeFilter) {
              currentTimeFilter = settings.currentTimeFilter;
            }
          }
        } catch (themeErr) {
          console.warn("Could not load theme:", themeErr);
        }
        await updateStatus();
        await updateFrequencyFromRadio();
        // Learn the rig's tuning range before filtering so unreadable
        // frequencies never appear in the table.
        await loadRigRange();
        allData = await combineLists();
        console.log("Existing data loaded:", allData.length, "records");
        if (allData.length > 0) {
          // Filter before building the grid: constructing Tabulator with the
          // full data and then calling setData() loses the race against its
          // own initial load, which would leave the selected filters inactive.
          const initialRows = computeFilteredRows();
          lastFilteredRows = initialRows;
          initTabulator(initialRows);
          console.log(
            `Startup filters (freq ${frequencyFilterMode}, time ${currentTimeFilter}): ` +
              `${allData.length} -> ${initialRows.length} records`,
          );
          showSuccess(`Loaded ${allData.length} records from all sources`);
        } else {
          allData = [];
          initTabulator([]);
        }
      } catch (err) {
        console.error("Error loading existing data:", err);
        allData = [];
        initTabulator([]);
      }
      await updateStatus();
    })();

    // In v3 the callback receives a WailsEvent object whose payload is on
    // .data (v2 passed the payload directly), and On() returns an unsubscribe
    // function so the listeners can be detached in the cleanup below.
    const unsubFrequency = Events.On("frequencyUpdate", (ev) => {
      const newFrequency = ev.data;
      if (!manualFrequency) {
        frequency =
          typeof newFrequency === "string"
            ? parseFloat(newFrequency)
            : newFrequency;
      }
    });

    const unsubMode = Events.On("modeUpdate", (ev) => {
      mode = ev.data;
    });

    const unsubConnection = Events.On("connectionStatus", (ev) => {
      const isConnected = ev.data;
      connected = isConnected;
      statusMessage = isConnected ? "Connected to radio" : "Disconnected";
    });

    const unsubTheme = Events.On("themeChanged", (ev) => {
      applyTheme(ev.data);
    });

    // The Settings window restarted the rigctld daemon with new options (a
    // different rig, serial parameters or polling interval), so re-read the
    // rig: its frequency, mode and tuning range may all have changed.
    const unsubRadio = Events.On("radioRestarted", async () => {
      await updateStatus();
      await updateFrequencyFromRadio();
      await loadRigRange();
      applyAllFilters();
    });

    window.addEventListener("resize", handleResize);

    return () => {
      clearInterval(timeInterval);
      window.removeEventListener("resize", handleResize);
      unsubFrequency();
      unsubMode();
      unsubConnection();
      unsubTheme();
      unsubRadio();
      if (manualInputDebounceTimer !== null) {
        clearTimeout(manualInputDebounceTimer);
        manualInputDebounceTimer = null;
      }
      if (table) {
        table.destroy();
        table = null;
        tabulatorInitialized = false;
      }
    };
  });

  function updateUTCTime() {
    // When the user is running a manual time filter, the live clock is
    // frozen so the chosen time stays in effect.
    if (manualTimeMode) return;
    const now = new Date();
    const hours = now.getUTCHours().toString().padStart(2, "0");
    const minutes = now.getUTCMinutes().toString().padStart(2, "0");
    currentUTCTime = hours + minutes;
  }

  function handleManualTimeInput(event) {
    const input = event.target.value;
    const filteredInput = input.replace(/[^0-9]/g, "");
    manualTimeInput = filteredInput.slice(0, 4);
    event.target.value = manualTimeInput;
    // The consolidated reactive trigger re-filters as this changes.
  }

  function handleManualTimeKeypress(event) {
    // Allow only numbers and navigation keys
    if (
      !/[0-9]/.test(event.key) &&
      event.key !== "Enter" &&
      event.key !== "Backspace" &&
      event.key !== "Delete" &&
      event.key !== "ArrowLeft" &&
      event.key !== "ArrowRight" &&
      event.key !== "Tab"
    ) {
      event.preventDefault();
    }

    // Handle Enter key to apply manual time
    if (event.key === "Enter") {
      applyManualTime();
    }
  }

  function applyManualTime() {
    if (manualTimeInput.length === 4) {
      try {
        const hours = parseInt(manualTimeInput.substring(0, 2), 10);
        const minutes = parseInt(manualTimeInput.substring(2, 4), 10);

        if (hours >= 0 && hours <= 23 && minutes >= 0 && minutes <= 59) {
          currentUTCTime = manualTimeInput;
          statusMessage = "Manual time applied successfully";
        } else {
          statusMessage = "Invalid time format (HHMM)";
        }
      } catch (error) {
        statusMessage = "Invalid time input";
        console.error("Time input error:", error);
      }
    }
  }

  function handleTimeFilterChange(event) {
    console.log("Time filter change event:", event);
    const filterType = event.target.value;
    const wasManual = currentTimeFilter === "manual";
    currentTimeFilter = filterType;
    void SaveFilterOptions(frequencyFilterMode, currentTimeFilter);

    if (filterType === "manual") {
      // Initialize with current time if manualTimeInput is empty
      if (!manualTimeInput) {
        manualTimeInput = currentUTCTime;
      }
      showManualTimeInput = true;
      manualTimeMode = true;
      
      // Use requestAnimationFrame for better timing
      requestAnimationFrame(() => {
        const manualTimeInputEl = document.getElementById("manual-time-input");
        if (manualTimeInputEl) {
          manualTimeInputEl.focus();
          manualTimeInputEl.select();
        }
      });
    } else {
      showManualTimeInput = false;
      manualTimeMode = false;
      // Leaving manual time mode: resync the clock to the real current time.
      if (wasManual) {
        updateUTCTime();
      }
    }
    
    console.log("Time filter changed to:", filterType, "showManualTimeInput:", showManualTimeInput);
  }

  async function handleFrequencyFilterChange(mode) {
    const wasManual = frequencyFilterMode === "manual";
    // The consolidated reactive trigger re-filters when the mode changes.
    frequencyFilterMode = mode;
    void SaveFilterOptions(frequencyFilterMode, currentTimeFilter);
    // Leaving manual frequency mode: sync the display to the radio's actual
    // frequency so the grid reflects the rig rather than the last manual entry.
    if (wasManual && mode !== "manual") {
      try {
        const currentFreq = await GetCurrentFrequency();
        if (currentFreq !== undefined) {
          frequency =
            typeof currentFreq === "string"
              ? parseFloat(currentFreq)
              : currentFreq;
        }
      } catch (error) {
        console.error("Failed to restore frequency from radio:", error);
      }
    }
  }
  
  async function updateFrequencyFromRadio() {
    try {
      const currentFreq = await GetCurrentFrequency();
      if (currentFreq !== undefined) {
        frequency =
          typeof currentFreq === "string"
            ? parseFloat(currentFreq)
            : currentFreq;
      }
    } catch (error) {
      console.error("Error getting current frequency:", error);
      statusMessage = "Failed to get current frequency from radio";
    }
  }

  $: if (frequencyFilterMode === "manual") {
    manualFrequency = true;
    editableFrequency = formatFrequency(frequency);
    // Publish the settled value immediately, without the typing debounce, so
    // switching into manual mode filters right away around the radio's
    // current frequency.
    debouncedEditableFrequency = editableFrequency;
    showManualInput = true;
    setTimeout(() => {
      const manualInput = document.getElementById("manual-frequency-input");
      if (manualInput) {
        manualInput.focus();
        manualInput.select();
      }
    }, 50);
  } else {
    manualFrequency = false;
    showManualInput = false;
  }

  // Any message shown at the bottom of the window disappears after a few
  // seconds, after which the grid reflows into the freed space. The countdown
  // is armed directly by the showError/showSuccess helpers at every assignment
  // instead of by a derived reactive statement watching the combined message
  // text; failures keep the longer countdown so an error is not missed.
  function showError(text) {
    errorMessage = text;
    armMessageTimeout(true);
  }

  function showSuccess(text) {
    successMessage = text;
    armMessageTimeout(false);
  }

  // (Re)starts the countdown that removes the current message.
  function armMessageTimeout(isError) {
    if (messageTimer !== null) {
      clearTimeout(messageTimer);
      messageTimer = null;
    }
    messageTimer = setTimeout(
      clearMessages,
      isError ? ERROR_TIMEOUT_MS : MESSAGE_TIMEOUT_MS,
    );
  }

  // The UTC clock updates every second, but the time filters only change their
  // result when the minute turns. Deriving a stable per-minute value keeps the
  // trigger below from re-filtering the grid once per second.
  $: currentMinute = currentUTCTime.substring(0, 3);

  // The single reactive trigger for re-filtering the grid. Every input the
  // filters read is referenced in the block so Svelte re-runs it when that
  // input changes; redundant firings (several inputs changing at once) are
  // cheap because applyAllFilters() skips the row rebuild when the filtered
  // result is unchanged.
  $: {
    allData;
    rigMinKhz;
    rigMaxKhz;
    frequencyFilterMode;
    frequency;
    debouncedEditableFrequency;
    currentTimeFilter;
    currentMinute;
    manualTimeInput;

    if (table) {
      applyAllFilters();
    }
  }
</script>

<main>
  <div class="header">
    <div class="frequency-box">
      <div class="frequency-label">Frequency</div>
      <div class="frequency-display-container">
        <input
          id="radio-input"
          type="text"
          class="radio-input"
          maxlength="10"
          placeholder="Frequency"
          readonly
          value={formatFrequency(frequency)}
        />
        {#if showManualInput}
          <input
            id="manual-frequency-input"
            type="text"
            class="manual-frequency-input"
            maxlength="10"
            value={editableFrequency}
            on:input={handleManualInput}
            on:keydown={handleManualKeypress}
          />
        {/if}
      </div>
      <div class="frequency-options">
        <div class="radio-buttons">
          <label class="radio-option">
            <input
              type="radio"
              name="frequencyFilter"
              value="exact"
              checked={frequencyFilterMode === "exact"}
              on:change={() => handleFrequencyFilterChange("exact")}
            />
            <span>Exact</span>
          </label>
          <label class="radio-option">
            <input
              type="radio"
              name="frequencyFilter"
              value="near"
              checked={frequencyFilterMode === "near"}
              on:change={() => handleFrequencyFilterChange("near")}
            />
            <span>Near</span>
          </label>
          <label class="radio-option">
            <input
              type="radio"
              name="frequencyFilter"
              value="all"
              checked={frequencyFilterMode === "all"}
              on:change={() => handleFrequencyFilterChange("all")}
            />
            <span>All</span>
          </label>
          <label class="radio-option">
            <input
              type="radio"
              name="frequencyFilter"
              value="manual"
              checked={frequencyFilterMode === "manual"}
              on:change={() => handleFrequencyFilterChange("manual")}
            />
            <span>Manual</span>
          </label>
        </div>
      </div>
    </div>

    <div class="frequency-box time-control-box">
      <div class="time-label">Time</div>
      <div class="time-display-container">
        <input
          id="utc-time-display"
          type="text"
          class="utc-time-display"
          maxlength="4"
          placeholder="0000"
          readonly
          value={currentUTCTime}
        />
        {#if showManualTimeInput}
          <input
            id="manual-time-input"
            type="text"
            class="manual-time-input"
            maxlength="4"
            value={manualTimeInput}
            on:input={handleManualTimeInput}
            on:keydown={handleManualTimeKeypress}
          />
        {/if}
      </div>
      <div class="radio-buttons">
        <label class="radio-option">
          <input
            type="radio"
            name="timeFilter"
            value="now"
            checked={currentTimeFilter === "now"}
            on:change={handleTimeFilterChange}
          />
          <span>Now</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="timeFilter"
            value="soon"
            checked={currentTimeFilter === "soon"}
            on:change={handleTimeFilterChange}
          />
          <span>Soon</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="timeFilter"
            value="all"
            checked={currentTimeFilter === "all"}
            on:change={handleTimeFilterChange}
          />
          <span>All</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="timeFilter"
            value="manual"
            checked={currentTimeFilter === "manual"}
            on:change={handleTimeFilterChange}
          />
          <span>Manual</span>
        </label>
      </div>
    </div>

  </div>

  <div class="table-container" bind:this={tableContainer}>
    {#if !tabulatorInitialized}
      <div class="loading-message">Loading table data...</div>
    {/if}
  </div>

  {#if errorMessage}
    <div class="error">{errorMessage}</div>
  {/if}

  {#if successMessage}
    <div class="success">{successMessage}</div>
  {/if}

  {#if debugInfo}
    <div class="debug">{debugInfo}</div>
  {/if}
</main>

<style>
  .debug {
    color: var(--debug-fg);
    padding: 10px;
    margin-bottom: 15px;
    background-color: var(--debug-bg);
    border-radius: 4px;
    border-left: 4px solid var(--debug-accent);
    font-family: monospace;
    font-size: 12px;
    overflow-x: auto;
  }

  .error {
    color: var(--danger);
    padding: 10px;
    margin-bottom: 15px;
    background-color: var(--error-bg);
    border-radius: 4px;
    border-left: 4px solid var(--danger);
  }

  .frequency-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 4px;
    background-color: var(--bg);
    color: var(--fg);
    min-width: 220px;
    position: relative;
  }

  .frequency-box .radio-buttons {
    display: flex !important;
    flex-wrap: nowrap;
    gap: 6px;
    justify-content: center;
  }

  .frequency-display-container {
    position: relative;
    width: 100%;
    display: flex;
    justify-content: center;
  }

  .frequency-label {
    font-weight: bold;
    font-size: 14px;
    color: var(--fg);
  }

  .frequency-options {
    display: flex;
    justify-content: center;
    width: 100%;
  }

  .frequency-options .radio-option {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    white-space: nowrap;
  }

  .header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0px;
    flex-shrink: 0;
  }

  .loading-message {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    color: var(--fg-dim);
    font-style: italic;
  }

  main {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 8px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen,
      Ubuntu, sans-serif;
  }

  .manual-frequency-input {
    position: absolute;
    top: 0;
    left: 50%;
    transform: translateX(-50%);
    padding: 6px 6px;
    border: 2px solid var(--accent);
    border-radius: 4px;
    width: 120px;
    text-align: center;
    font-family: monospace;
    background-color: var(--bg-input);
    color: var(--fg);
    font-size: 18px;
    font-weight: bold;
    z-index: 10;
    box-shadow: 0 0 5px rgba(74, 144, 217, 0.5);
  }

  .manual-time-input {
    position: absolute;
    top: 0;
    left: 50%;
    transform: translateX(-50%);
    padding: 6px 6px;
    border: 2px solid var(--accent);
    border-radius: 4px;
    width: 80px;
    text-align: center;
    font-family: monospace;
    background-color: var(--bg-input);
    color: var(--fg);
    font-size: 18px;
    font-weight: bold;
    z-index: 10;
    box-shadow: 0 0 5px rgba(74, 144, 217, 0.5);
  }

  .radio-buttons {
    display: flex !important;
    flex-wrap: nowrap;
    gap: 8px;
    justify-content: center;
  }

  .radio-input {
    padding: 6px 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    width: 120px;
    text-align: center;
    font-family: monospace;
    background-color: var(--bg-input);
    color: var(--fg);
    cursor: not-allowed;
    font-size: 18px;
    font-weight: bold;
  }

  .radio-option {
    white-space: nowrap;
  }

  .radio-option span {
    color: var(--fg);
    font-weight: normal;
  }

  .success {
    color: var(--success-fg);
    padding: 10px;
    margin-bottom: 15px;
    background-color: var(--success-bg);
    border-radius: 4px;
    border-left: 4px solid var(--success-accent);
  }

  .table-container {
    flex: 1;
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
    margin-top: 6px;
    min-height: 300px;
    position: relative;
  }

  .time-control-box .radio-buttons {
    display: flex !important;
    flex-wrap: nowrap !important;
    gap: 6px;
    justify-content: center;
  }

  .time-control-box .radio-option {
    white-space: nowrap;
    font-size: 12px;
  }

  .time-display-container {
    position: relative;
    width: 100%;
    display: flex;
    justify-content: center;
    margin-bottom: 2px;
  }

  .time-label {
    font-weight: bold;
    font-size: 14px;
    color: var(--fg);
  }

  .utc-time-display {
    padding: 6px 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    width: 80px;
    text-align: center;
    font-family: monospace;
    background-color: var(--bg-input);
    color: var(--fg);
    cursor: not-allowed;
    font-size: 18px;
    font-weight: bold;
  }

  :global(*) {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
  }

  :global(html, body) {
    height: 100%;
    overflow: hidden;
  }

  :global(.tabulator) {
    height: 100% !important;
    background-color: var(--bg);
    color: var(--fg);
    border-color: var(--border);
  }

  :global(.tabulator .tabulator-tableholder) {
    height: calc(100% - 40px) !important;
    background-color: var(--bg);
  }

  /* Header row: dark surface with lighter text. */
  :global(.tabulator .tabulator-header) {
    background-color: var(--bg-input);
    color: var(--fg);
    border-bottom: 1px solid var(--border);
  }

  :global(.tabulator .tabulator-header .tabulator-col) {
    background-color: var(--bg-input);
    color: var(--fg);
    border-right: 1px solid var(--border);
  }

  :global(.tabulator .tabulator-header .tabulator-col .tabulator-col-content) {
    color: var(--fg);
  }

  :global(.tabulator .tabulator-header .tabulator-col:hover),
  :global(.tabulator .tabulator-header .tabulator-col.tabulator-sortable:not(.tabulator-col-group):hover),
  :global(.tabulator .tabulator-header .tabulator-col.tabulator-sortable.tabulator-col-sorter-element:hover) {
    background-color: var(--row-hover) !important;
    color: var(--fg);
  }

  :global(.tabulator .tabulator-header .tabulator-col:hover .tabulator-col-content),
  :global(.tabulator .tabulator-header .tabulator-col.tabulator-sortable:not(.tabulator-col-group):hover .tabulator-col-content),
  :global(.tabulator .tabulator-header .tabulator-col.tabulator-sortable.tabulator-col-sorter-element:hover .tabulator-col-content) {
    color: var(--fg);
  }

  /* Column header filter boxes. */
  :global(.tabulator .tabulator-header .tabulator-col .tabulator-header-filter input) {
    background-color: var(--bg);
    color: var(--fg);
    border: 1px solid var(--border);
    border-radius: 3px;
  }

  :global(.tabulator .tabulator-header .tabulator-col .tabulator-header-filter input::placeholder) {
    color: var(--fg-dim);
  }

  /* Detail records. */
  :global(.tabulator .tabulator-row) {
    background-color: var(--bg);
    color: var(--fg);
    border-bottom: 1px solid var(--border);
  }

  :global(.tabulator .tabulator-row.tabulator-row-even) {
    background-color: var(--bg-input);
  }

  :global(.tabulator .tabulator-row .tabulator-cell) {
    background-color: transparent;
    color: var(--fg);
    border-right: 1px solid var(--border);
  }

  :global(.tabulator .tabulator-row.tabulator-selectable:hover) {
    background-color: var(--row-hover);
  }

  :global(.tabulator .tabulator-row.tabulator-selected) {
    background-color: var(--row-selected);
  }

  /* Row the user last clicked: keep it visually anchored across refilters */
  :global(.tabulator .tabulator-row.row-pinned) {
    background-color: var(--row-pinned-bg) !important;
    color: var(--fg) !important;
    box-shadow: inset 3px 0 0 var(--row-pinned-accent);
  }

  :global(.tabulator .tabulator-row.row-pinned .tabulator-cell) {
    background-color: transparent;
    color: var(--fg);
  }
</style>