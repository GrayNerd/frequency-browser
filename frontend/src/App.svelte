<script>
  import { onMount } from "svelte";
  import { EventsOn } from "../wailsjs/runtime";
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
    LoadImportDates,
  } from "../wailsjs/go/main/App";
  import {
    GetCurrentFrequency,
    SetFrequency,
    GetMode,
    GetRadioStatus,
    UpdateConfig,
    SendCustomCommand,
    GetFrequencyRange,
  } from "../wailsjs/go/main/App";
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

  // When each source was last downloaded (RFC 3339, from import-dates.json).
  let importDates = {};

  // Status and Messages
  let statusMessage = "Connecting to radio...";
  let errorMessage = "";
  let successMessage = "";
  let debugInfo = "";
  let connected = false;

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

  async function applyAllFilters() {
    const seq = ++filterSeq;
    let debug = false
    console.log(
      "Applying all filters - Frequency mode:",
      frequencyFilterMode,
      "Time mode:",
      currentTimeFilter,
    );
    if (!table) {
      console.warn("Table not initialized yet");
      return;
    }
    if (allData.length === 0) {
      console.warn("No data to filter");
      table.setData([]);
      return;
    }
    // logInvalidTimeData(allData);
    let filteredData = [...allData];
    if (debug) console.log("Applying filters to", filteredData.length, "records");

    // Drop entries the connected radio cannot receive at all.
    if (rigMinKhz !== null && rigMaxKhz !== null) {
      const before = filteredData.length;
      filteredData = filteredData.filter(withinRigRange);
      if (debug)
        console.log(
          `Rig range filter (${rigMinKhz}-${rigMaxKhz} kHz): ${before} -> ${filteredData.length}`,
        );
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
        const manualFreq = parseFloat(editableFrequency);
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

      try {
        const filterMinutes = timeToMinutes(timeToUse);
        if (debug) console.log("Filter time:", timeToUse, "=", filterMinutes, "minutes");

        if (currentTimeFilter === "now" || currentTimeFilter === "manual") {
          filteredData = filteredData.filter((row) => {
            const startMinutes = timeToMinutes(row.startTime);
            const endMinutes = timeToMinutes(row.endTime);

            if (debug) console.log(`Row ${row.startTime}-${row.endTime}: start=${startMinutes}, end=${endMinutes}`);

            let isNow;
            if (endMinutes < startMinutes) {
              // Broadcast spans midnight
              isNow =
                filterMinutes >= startMinutes || filterMinutes <= endMinutes;
              if (debug)console.log(`  Spans midnight: ${isNow} (${filterMinutes} >= ${startMinutes} OR ${filterMinutes} <= ${endMinutes})`);
            } else {
              // Broadcast within same day
              isNow =
                filterMinutes >= startMinutes && filterMinutes <= endMinutes;
              if (debug) console.log(`  Same day: ${isNow} (${filterMinutes} >= ${startMinutes} AND ${filterMinutes} <= ${endMinutes})`);
            }

            return isNow;
          });
        } else if (currentTimeFilter === "soon") {
          filteredData = filteredData.filter((row) => {
            const startMinutes = timeToMinutes(row.startTime);
            const endMinutes = timeToMinutes(row.endTime);

            if (startMinutes === undefined || endMinutes === undefined) {
              console.log("Invalid time data:", row);
              return false;
            }

            if (debug) console.log(`Row ${row.startTime}-${row.endTime}: start=${startMinutes}, end=${endMinutes}`);

            // Check if currently active
            let isNow;
            if (endMinutes < startMinutes) {
              isNow =
                filterMinutes >= startMinutes || filterMinutes <= endMinutes;
              if (debug) console.log(`  Now (midnight): ${isNow}`);
            } else {
              isNow =
                filterMinutes >= startMinutes && filterMinutes <= endMinutes;
              if (debug) console.log(`  Now (same day): ${isNow}`);
            }

            // Calculate time until start
            let timeUntilStart;
            if (filterMinutes <= startMinutes) {
              // Start is later today
              timeUntilStart = startMinutes - filterMinutes;
            } else {
              // Start is tomorrow (or next occurrence)
              timeUntilStart = 1440 - filterMinutes + startMinutes;
            }
            if (debug)console.log(`  Time until start: ${timeUntilStart} minutes`);

            const isSoon = timeUntilStart <= 30 && timeUntilStart > 0;
            if (debug) console.log(`  Is soon: ${isSoon}`);

            return isNow || isSoon;
          });
        }
      } catch (error) {
        console.error("Error applying time filter:", error);
      }
    }

    console.log(`Filtered to ${filteredData.length} records`);

    if (table) {
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
    } else {
      initTabulator(filteredData);
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
    applyAllFilters(); // Apply filters as user types
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
          await SetFrequency(freqValue);
          frequency = freqValue;
          statusMessage = "Manual frequency set successfully";
          applyAllFilters(); // Apply filters after setting frequency
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
    errorMessage = "";
    successMessage = "";
    debugInfo = "";

    try {
      // No file dialog any more: the backend downloads the newest EiBi schedule
      // database (sked-???.csv) plus README.TXT from http://www.eibispace.de/.
      console.log("Downloading newest EiBi schedule from http://www.eibispace.de/...");
      const data = await ImportEiBi();
      console.log("EiBi data received:", data);

      if (!data) {
        successMessage = "EiBi import cancelled";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in the newest EiBi schedule";
        return;
      }

      await SaveEIBIList(data);
      await refreshImportDates();
      allData = await combineLists();
      console.log(`Combined to ${allData.length} records`);
      applyAllFilters(); // Apply filters after loading data

      successMessage = `Successfully imported ${data.length} records from the newest EiBi schedule (reference tables refreshed) and saved to eibi-list.json`;
    } catch (err) {
      errorMessage = `Failed to import EiBi data: ${err.message || "Unknown error"}`;
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
    errorMessage = "";
    successMessage = "";
    debugInfo = "";

    try {
      // No file dialog any more: the backend downloads the newest AOKI shortwave
      // schedule archive from the Bi Newsletter page and unpacks the .txt inside.
      console.log("Downloading newest AOKI schedule from the Bi Newsletter page...");
      const data = await ImportAOKI();
      console.log("Aoki data received:", data);

      if (!data) {
        successMessage = "Aoki import cancelled";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in the newest AOKI schedule";
        return;
      }

      await SaveAokiList(data);
      await refreshImportDates();
      allData = await combineLists();
      applyAllFilters(); // Apply filters after loading data

      successMessage = `Successfully imported ${data.length} records from the newest AOKI schedule and saved to aoki-list.json`;
    } catch (err) {
      errorMessage = `Failed to import AOKI data: ${err.message || "Unknown error"}`;
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
    errorMessage = "";
    successMessage = "";
    debugInfo = "";

    try {
      // No file dialog any more: the backend downloads the newest operational
      // schedule archive from https://new.hfcc.org/data/ and parses it.
      console.log("Downloading newest HFCC schedule from https://new.hfcc.org/data/...");
      const data = await ImportHFCC();
      console.log("Hfcc data received:", data);

      if (!data) {
        successMessage = "Hfcc import cancelled";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in the newest HFCC schedule";
        return;
      }

      await SaveHfccList(data);
      await refreshImportDates();
      allData = await combineLists();
      applyAllFilters(); // Apply filters after loading data

      successMessage = `Successfully imported ${data.length} records from the newest HFCC schedule (reference tables refreshed) and saved to hfcc-list.json`;
    } catch (err) {
      errorMessage = `Failed to import HFCC data: ${err.message || "Unknown error"}`;
      console.error("hfcc Import error details:", err);
      debugInfo = `Hfcc Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingHfcc = false;
    }
  }

  function timeToMinutes(timeString) {
    if (!/^\d{4}$/.test(timeString)) {
      throw new Error("Input must be a 4-digit string in HHMM format");
    }

    const [hours, minutes] = [
      parseInt(timeString.substring(0, 2), 10),
      parseInt(timeString.substring(2, 4), 10),
    ];

    if (
      hours < 0 ||
      hours > 24 ||
      minutes < 0 ||
      minutes > 59 ||
      (hours === 24 && minutes !== 0)
    ) {
      throw new Error(
        "Invalid time values. Hours must be 00-24, minutes must be 00-59",
      );
    }

    return hours * 60 + minutes;
  }

  function initTabulator(data) {
    console.log("Initializing Tabulator with data:", data.length, "records");
    if (tabulatorInitialized && table) {
      table.setData(data);
      applyAllFilters();
      return;
    }

    if (!tableContainer) {
      console.error("Table container not found!");
      return;
    }

    table = new Tabulator(tableContainer, {
      data: data,
      layout: "fitColumns",
      responsiveLayout: "collapse",
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
          title: "Start Time",
          field: "startTime",
          headerFilter: "input",
          width: 100,
        },
        {
          title: "End Time",
          field: "endTime",
          headerFilter: "input",
          width: 100,
        },
        {
          title: "Days",
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

    // Tabulator 5.x removed option-based event callbacks (rowClick: fn is
    // silently ignored). Events must be subscribed with table.on().
    table.on("rowClick", async (e, row) => {
      const rowData = row.getData();
      const khz = parseFloat(rowData.khz);
      if (isNaN(khz)) {
        errorMessage = "Invalid frequency in row: " + rowData.khz;
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
        errorMessage =
          "Failed to set frequency: " + (err.message || String(err));
      }
      // Re-apply filters (the radio frequency changed) without moving the grid.
      // The clicked row is highlighted by the rowFormatter.
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

  // Formats a recorded import time for the label beside each button.
  //
  // `dates` is passed explicitly rather than closed over: Svelte only re-renders
  // a call expression when the variables it references are passed as arguments.
  // Without the parameter the compiler emits no update branch and the label
  // keeps showing its initial value.
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

  // Reads the stored import times so the labels survive a restart.
  async function refreshImportDates() {
    try {
      const dates = (await LoadImportDates()) || {};
      // Always replace the object so the assignment is seen as a change.
      importDates = { ...dates };
    } catch (error) {
      console.error("Error loading import dates:", error);
      importDates = {};
    }
  }

  async function combineLists() {
    try {
      console.log("Loading existing data...");
      const [eibiData, aokiData, hfccData] = await Promise.all([
        LoadEIBIList().catch(() => []),
        LoadAokiList().catch(() => []),
        LoadHfccList().catch(() => []),
      ]);

      allData = [...(eibiData || []), ...(aokiData || []), ...(hfccData || [])];
      console.log("Combined data loaded:", allData.length, "records");
      return allData;
    } catch (error) {
      console.error("Error combining lists:", error);
      errorMessage = "Failed to load combined data";
    }
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
        await updateStatus();
        await updateFrequencyFromRadio();
        // Learn the rig's tuning range before filtering so unreadable
        // frequencies never appear in the table.
        await loadRigRange();
        await refreshImportDates();
        allData = await combineLists();
        console.log("Existing data loaded:", allData.length, "records");
        if (allData.length > 0) {
          // allData = existingData;
          initTabulator(allData);
          applyAllFilters(); // Apply filters after loading data
          successMessage = `Loaded ${allData.length} records from all sources`;
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

    EventsOn("frequencyUpdate", (newFrequency) => {
      if (!manualFrequency) {
        frequency =
          typeof newFrequency === "string"
            ? parseFloat(newFrequency)
            : newFrequency;
      }
    });

    EventsOn("modeUpdate", (newMode) => {
      mode = newMode;
    });

    EventsOn("connectionStatus", (isConnected) => {
      connected = isConnected;
      statusMessage = isConnected ? "Connected to radio" : "Disconnected";
    });

    window.addEventListener("resize", handleResize);

    return () => {
      clearInterval(timeInterval);
      window.removeEventListener("resize", handleResize);
      if (table) {
        table.destroy();
        table = null;
        tabulatorInitialized = false;
      }
    };
  });

  function updateUTCTime() {
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
    applyAllFilters(); // Apply filters as user types
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
          applyAllFilters(); // Apply filters after setting time
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
    currentTimeFilter = filterType;

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
    }
    
    console.log("Time filter changed to:", filterType, "showManualTimeInput:", showManualTimeInput);
    applyAllFilters();
  }

  function handleFrequencyFilterChange(mode) {
    frequencyFilterMode = mode;
    applyAllFilters();
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

  $: if (frequencyFilterMode === "all") {
    console.log("Frequency filter mode changed to 'all'");
    applyAllFilters();
  }

  $: if (frequencyFilterMode === "manual") {
    editableFrequency = formatFrequency(frequency);
    showManualInput = true;
    setTimeout(() => {
      const manualInput = document.getElementById("manual-frequency-input");
      if (manualInput) {
        manualInput.focus();
        manualInput.select();
      }
    }, 50);
  } else {
    showManualInput = false;
  }

  $: if (frequency && frequencyFilterMode !== "all" && !manualFrequency) {
    applyAllFilters();
  }

  let lastFilteredMinute = "";
  $: if (currentUTCTime && currentTimeFilter !== "all") {
    const minute = currentUTCTime.substring(0, 3); // HHM - changes once per minute
    if (minute !== lastFilteredMinute) {
      lastFilteredMinute = minute;
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

    <div class="import-buttons">
      <div class="import-row">
        <button id="import-eibi" on:click={importEiBi} disabled={loading}>
          {loadingEiBi ? "Downloading..." : "Import EiBi"}
        </button>
        <span class="import-date" title="Last successful download">
          {formatImportDate(importDates, "EiBi")}
        </span>
      </div>

      <div class="import-row">
        <button id="import-aoki" on:click={importAoki} disabled={loading}>
          {loadingAoki ? "Downloading..." : "Import AOKI"}
        </button>
        <span class="import-date" title="Last successful download">
          {formatImportDate(importDates, "AOKI")}
        </span>
      </div>

      <div class="import-row">
        <button id="import-hfcc" on:click={importHFCC} disabled={loading}>
          {loadingHfcc ? "Downloading..." : "Import HFCC"}
        </button>
        <span class="import-date" title="Last successful download">
          {formatImportDate(importDates, "HFCC")}
        </span>
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
    color: #7d3c98;
    padding: 10px;
    margin-bottom: 15px;
    background-color: #e8daef;
    border-radius: 4px;
    border-left: 4px solid #7d3c98;
    font-family: monospace;
    font-size: 12px;
    overflow-x: auto;
  }

  .error {
    color: #e74c3c;
    padding: 10px;
    margin-bottom: 15px;
    background-color: #fadbd8;
    border-radius: 4px;
    border-left: 4px solid #e74c3c;
  }

  .frequency-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 4px;
    background-color: #f8f9fa;
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
    color: #000000;
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

  /* Import buttons stacked vertically, to the right of the time box. */
  .import-buttons {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 4px;
    flex-shrink: 0;
    align-self: stretch;
    padding: 4px 0;
  }

  .import-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .import-row button {
    width: 130px;
  }

  /* Date the source was last downloaded, shown beside its button. */
  .import-date {
    font-family: monospace;
    font-size: 12px;
    color: #555555;
    white-space: nowrap;
    min-width: 132px;
  }

  .loading-message {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    color: #666;
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
    border: 2px solid #3498db;
    border-radius: 4px;
    width: 120px;
    text-align: center;
    font-family: monospace;
    background-color: #ffffff;
    color: #000000;
    font-size: 18px;
    font-weight: bold;
    z-index: 10;
    box-shadow: 0 0 5px rgba(52, 152, 219, 0.5);
  }

  .manual-time-input {
    position: absolute;
    top: 0;
    left: 50%;
    transform: translateX(-50%);
    padding: 6px 6px;
    border: 2px solid #3498db;
    border-radius: 4px;
    width: 80px;
    text-align: center;
    font-family: monospace;
    background-color: #ffffff;
    color: #000000;
    font-size: 18px;
    font-weight: bold;
    z-index: 10;
    box-shadow: 0 0 5px rgba(52, 152, 219, 0.5);
  }

  .radio-buttons {
    display: flex !important;
    flex-wrap: nowrap;
    gap: 8px;
    justify-content: center;
  }

  .radio-input {
    padding: 6px 6px;
    border: 1px solid #ccc;
    border-radius: 4px;
    width: 120px;
    text-align: center;
    font-family: monospace;
    background-color: #ffffff;
    color: #000000;
    cursor: not-allowed;
    font-size: 18px;
    font-weight: bold;
  }

  .radio-option {
    white-space: nowrap;
  }

  .radio-option span {
    color: #000000;
    font-weight: normal;
  }

  .success {
    color: #27ae60;
    padding: 10px;
    margin-bottom: 15px;
    background-color: #d5f4e6;
    border-radius: 4px;
    border-left: 4px solid #27ae60;
  }

  .table-container {
    flex: 1;
    border: 1px solid #ddd;
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
    color: #000000;
  }

  .utc-time-display {
    padding: 6px 6px;
    border: 1px solid #ccc;
    border-radius: 4px;
    width: 80px;
    text-align: center;
    font-family: monospace;
    background-color: #ffffff;
    color: #000000;
    cursor: not-allowed;
    font-size: 18px;
    font-weight: bold;
  }

  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background-color: #3498db;
    color: black;
    border: none;
    padding: 6px 14px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
    height: 30px;
    white-space: nowrap;
  }

  button:disabled {
    background-color: #95a5a6;
    cursor: not-allowed;
  }

  button:hover:not(:disabled) {
    background-color: #2980b9;
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
  }

  :global(.tabulator .tabulator-tableholder) {
    height: calc(100% - 40px) !important;
  }

  /* Row the user last clicked: keep it visually anchored across refilters */
  :global(.tabulator .tabulator-row.row-pinned) {
    background-color: #ffe9a8 !important;
    box-shadow: inset 3px 0 0 #e67e22;
  }
</style>