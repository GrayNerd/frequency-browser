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
  } from "../wailsjs/go/main/App";
  import {
    GetCurrentFrequency,
    SetFrequency,
    GetMode,
    GetRadioStatus,
    UpdateConfig,
    SendCustomCommand,
  } from "../wailsjs/go/main/App";

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

  // Input filtering functions
  function applyAllFilters() {
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
    let filteredData = [...allData];

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

        if (currentTimeFilter === "now" || currentTimeFilter === "manual") {
          filteredData = filteredData.filter((row) => {
            const startMinutes = timeToMinutes(row.startTime);
            const endMinutes = timeToMinutes(row.endTime);

            if (endMinutes < startMinutes) {
              return (
                filterMinutes >= startMinutes || filterMinutes <= endMinutes
              );
            } else {
              return (
                filterMinutes >= startMinutes && filterMinutes <= endMinutes
              );
            }
          });
        } else if (currentTimeFilter === "soon") {
          filteredData = filteredData.filter((row) => {
            const startMinutes = timeToMinutes(row.startTime);
            const endMinutes = timeToMinutes(row.endTime);

            let isNow = false;
            if (endMinutes < startMinutes) {
              isNow =
                filterMinutes >= startMinutes || filterMinutes <= endMinutes;
            } else {
              isNow =
                filterMinutes >= startMinutes && filterMinutes <= endMinutes;
            }

            let timeUntilStart;
            if (startMinutes >= filterMinutes) {
              timeUntilStart = startMinutes - filterMinutes;
            } else {
              timeUntilStart = 1440 - filterMinutes + startMinutes;
            }

            const isSoon = timeUntilStart <= 30 && timeUntilStart > 0;

            return isNow || isSoon;
          });
          console.log(`Filtered to ${filteredData.length} records`);
          table.setData(filteredData);
        }
      } catch (error) {
        console.error("Error applying time filter:", error);
      }
    }

    console.log(`Filtered to ${filteredData.length} records`);
    table.setData(filteredData);
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
      console.log("Starting EiBi CSV import...");
      const data = await ImportEiBi();
      console.log("EiBi CSV data received:", data);

      if (!data) {
        successMessage = "EiBi CSV Import cancelled by user";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in EiBi CSV file";
        return;
      }

      await SaveEIBIList(data);
      allData = data;

      if (table) {
        table.setData(data);
      } else {
        initTabulator(data);
      }

      successMessage = `Successfully imported ${data.length} records and saved to eibi-list.json`;
    } catch (err) {
      errorMessage = `Failed to import EiBi CSV: ${err.message || "Unknown error"}`;
      console.error("EiBi CSV Import error details:", err);
      debugInfo = `EiBi CSV Import Error details: ${err.toString()}`;
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
      console.log("Starting Aoki CSV import...");
      const data = await ImportAOKI();
      console.log("Aoki CSV data received:", data);

      if (!data) {
        successMessage = "Aoki CSV Import cancelled by user";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in Aoki CSV file";
        return;
      }

      await SaveAokiList(data);
      allData = data;

      if (table) {
        table.setData(data);
      } else {
        initTabulator(data);
      }

      successMessage = `Successfully imported ${data.length} records and saved to aoki-list.json`;
    } catch (err) {
      errorMessage = `Failed to import Aoki CSV: ${err.message || "Unknown error"}`;
      console.error("Aoki CSV Import error details:", err);
      debugInfo = `Aoki CSV Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingAoki = false;
    }
  }

  async function importHFCC() {
    loading = true;
    errorMessage = "";
    successMessage = "";
    debugInfo = "";

    try {
      console.log("Starting Hfcc CSV import...");
      const data = await ImportHFCC();
      console.log("Hfcc CSV data received:", data);

      if (!data) {
        successMessage = "Hfcc CSV Import cancelled by user";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in Hfcc CSV file";
        return;
      }

      await SaveHfccList(data);
      allData = data;

      if (table) {
        table.setData(data);
      } else {
        initTabulator(data);
      }

      successMessage = `Successfully imported ${data.length} records and saved to hfcc-list.json`;
    } catch (err) {
      errorMessage = `Failed to import hfcc CSV: ${err.message || "Unknown error"}`;
      console.error("hfcc CSV Import error details:", err);
      debugInfo = `Hfcc CSV Import Error details: ${err.toString()}`;
    } finally {
      loading = false;
      loadingHFCC = false;
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

  onMount(() => {
    console.log("Component mounting...");
    if (hasMounted) return;
    hasMounted = true;

    // Start UTC clock
    updateUTCTime();
    timeInterval = setInterval(updateUTCTime, 1000);

    (async () => {
      try {
        const existingData = await LoadEIBIList();
        if (existingData && existingData.length > 0) {
          allData = existingData;
          initTabulator(existingData);
          applyAllFilters(); // Apply filters after loading data
          successMessage = `Loaded ${existingData.length} records from eibi-list.json`;
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
    const filterType = event.target.value;
    currentTimeFilter = filterType;

    if (filterType === "manual") {
      manualTimeInput = currentUTCTime;
      showManualTimeInput = true;
      manualTimeMode = true;
      setTimeout(() => {
        const manualTimeInputEl = document.getElementById("manual-time-input");
        if (manualTimeInputEl) manualTimeInputEl.focus();
      }, 50);
    } else {
      showManualTimeInput = false;
      manualTimeMode = false;
    }

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
      if (manualInput) manualInput.focus();
    }, 50);
  } else {
    showManualInput = false;
  }

  $: if (frequency && frequencyFilterMode !== "all" && !manualFrequency) {
    applyAllFilters();
  }

  $: if (currentUTCTime && currentTimeFilter !== "all") {
    applyAllFilters();
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
            name="time"
            value="now"
            checked={currentTimeFilter === "now"}
            on:change={handleTimeFilterChange}
          />
          <span>Now</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="soon"
            checked={currentTimeFilter === "soon"}
            on:change={handleTimeFilterChange}
          />
          <span>Soon</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="all"
            checked={currentTimeFilter === "all"}
            on:change={handleTimeFilterChange}
          />
          <span>All</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="manual"
            checked={currentTimeFilter === "manual"}
            on:change={handleTimeFilterChange}
          />
          <span>Manual</span>
        </label>
      </div>
    </div>

    <button id="import-eibi" on:click={importEiBi} disabled={loading}>
      {loadingEiBi ? "Importing..." : "Import EiBi"}
    </button>

    <button id="import-aoki" on:click={importAoki} disabled={loading}>
      {loadingAoki ? "Importing..." : "Import AOKI"}
    </button>

    <button id="import-hfcc" on:click={importHFCC} disabled={loading}>
      {loadingHfcc ? "Importing..." : "Import HFCC"}
    </button>
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
    background-color: #3498db;
    color: black;
    border: none;
    padding: 10px 20px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 14px;
    height: 30px;
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
</style>
