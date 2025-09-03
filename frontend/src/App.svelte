<script>
  import { onMount } from "svelte";
  import { EventsOn } from "../wailsjs/runtime";
  import { TabulatorFull as Tabulator } from "tabulator-tables";
  import "tabulator-tables/dist/css/tabulator.min.css";
  import {
    ImportCSV,
    LoadEIBIList,
    SaveEIBIList,
  } from "../wailsjs/go/main/App";
  import {
    GetCurrentFrequency,
    SetFrequency,
    GetMode,
    GetRadioStatus,
    UpdateConfig,
    SendCustomCommand,
  } from "../wailsjs/go/main/App";

  let table;
  let tableContainer;
  let loading = false;
  let errorMessage = "";
  let successMessage = "";
  let debugInfo = "";
  let currentTimeFilter = "now";
  let allData = [];
  let mode = "Unknown";
  let connected = false;
  let statusMessage = "Connecting to radio...";
  let config = {
    address: "localhost:4532",
  };
  let customCommand = "";
  let commandResponse = "";
  let tabulatorInitialized = false;
  let hasMounted = false;
  let frequency = 0;
  let exactFrequency = false;
  let manualFrequency = false;
  let editableFrequency = "";
  let showManualInput = false;
  let currentUTCTime = "0000";
  let manualTimeInput = "";
  let showManualTimeInput = false;
  let timeInterval;
  let manualTimeMode = false;

  // Input filtering functions
  function handleManualInput(event) {
    const input = event.target.value;
    // Only allow digits and decimal point
    const filteredInput = input.replace(/[^0-9.]/g, "");

    // Ensure only one decimal point
    const decimalCount = (filteredInput.match(/\./g) || []).length;
    if (decimalCount > 1) {
      const firstDecimalIndex = filteredInput.indexOf(".");
      editableFrequency =
        filteredInput.substring(0, firstDecimalIndex + 1) +
        filteredInput.substring(firstDecimalIndex + 1).replace(/\./g, "");
    } else {
      editableFrequency = filteredInput;
    }

    // Update the input value
    event.target.value = editableFrequency;
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
          showManualInput = false;
        }
      } catch (error) {
        statusMessage = "Failed to set manual frequency";
        console.error("Set frequency error:", error);
      }
    }
  }

  // Watch for manual mode changes
  $: if (manualFrequency) {
    editableFrequency = formatFrequency(frequency);
    showManualInput = true;
    // Focus the input after a short delay
    setTimeout(() => {
      const manualInput = document.getElementById("manual-frequency-input");
      if (manualInput) manualInput.focus();
    }, 50);
  } else {
    showManualInput = false;
  }

  async function importCSV() {
    loading = true;
    errorMessage = "";
    successMessage = "";
    debugInfo = "";

    try {
      console.log("Starting CSV import...");
      const data = await ImportCSV();
      console.log("CSV data received:", data);

      if (!data) {
        successMessage = "Import cancelled by user";
        return;
      }

      if (data.length === 0) {
        successMessage = "No data rows found in CSV file";
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
      errorMessage = `Failed to import CSV: ${err.message || "Unknown error"}`;
      console.error("Import error details:", err);
      debugInfo = `Error details: ${err.toString()}`;
    } finally {
      loading = false;
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

  function applyTimeFilter(filterType) {
    currentTimeFilter = filterType;
    if (!table || allData.length === 0) return;

    const now = new Date();
    const currentUTCHours = now.getUTCHours();
    const currentUTCMinutes = now.getUTCMinutes();
    const currentMinutes = currentUTCHours * 60 + currentUTCMinutes;

    let filteredData = [];

    if (filterType === "now") {
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        if (endMinutes < startMinutes) {
          return currentMinutes >= startMinutes || currentMinutes <= endMinutes;
        } else {
          return currentMinutes >= startMinutes && currentMinutes <= endMinutes;
        }
      });
    } else if (filterType === "soon") {
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        let isNow = false;
        if (endMinutes < startMinutes) {
          isNow =
            currentMinutes >= startMinutes || currentMinutes <= endMinutes;
        } else {
          isNow =
            currentMinutes >= startMinutes && currentMinutes <= endMinutes;
        }

        const timeUntilStart = (startMinutes - currentMinutes + 1440) % 1440;
        const isSoon = timeUntilStart <= 30 && timeUntilStart > 0;

        return isNow || isSoon;
      });
    } else if (filterType === "all") {
      filteredData = allData;
    }

    table.setData(filteredData);
  }

  function initTabulator(data) {
    if (tabulatorInitialized && table) {
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
        { title: "Days", field: "days", headerFilter: "input", width: 100 },
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
          minWidth: 150,
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
    // Only allow digits
    const filteredInput = input.replace(/[^0-9]/g, "");

    // Limit to 4 digits
    manualTimeInput = filteredInput.slice(0, 4);

    // Update the input value
    event.target.value = manualTimeInput;
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
        // Validate the time input
        const hours = parseInt(manualTimeInput.substring(0, 2), 10);
        const minutes = parseInt(manualTimeInput.substring(2, 4), 10);

        if (hours >= 0 && hours <= 23 && minutes >= 0 && minutes <= 59) {
          currentUTCTime = manualTimeInput;
          showManualTimeInput = false;
          // Apply filter with manual time
          applyTimeFilterWithManualTime(currentTimeFilter, currentUTCTime);
        } else {
          statusMessage = "Invalid time format (HHMM)";
        }
      } catch (error) {
        statusMessage = "Invalid time input";
        console.error("Time input error:", error);
      }
    }
  }

  function applyTimeFilterWithManualTime(filterType, manualTime) {
    if (!table || allData.length === 0) return;

    const manualMinutes = timeToMinutes(manualTime);
    let filteredData = [];

    if (filterType === "now") {
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        if (endMinutes < startMinutes) {
          return manualMinutes >= startMinutes || manualMinutes <= endMinutes;
        } else {
          return manualMinutes >= startMinutes && manualMinutes <= endMinutes;
        }
      });
    } else if (filterType === "soon") {
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        let isNow = false;
        if (endMinutes < startMinutes) {
          isNow = manualMinutes >= startMinutes || manualMinutes <= endMinutes;
        } else {
          isNow = manualMinutes >= startMinutes && manualMinutes <= endMinutes;
        }

        const timeUntilStart = (startMinutes - manualMinutes + 1440) % 1440;
        const isSoon = timeUntilStart <= 30 && timeUntilStart > 0;

        return isNow || isSoon;
      });
    } else if (filterType === "all") {
      filteredData = allData;
    } else if (filterType === "manual") {
      // For manual mode, we'll use the manual time but show all data
      // or you can implement custom filtering logic here
      filteredData = allData;
    }

    table.setData(filteredData);
  }

  function handleTimeFilterChange(event) {
    const filterType = event.target.value;
    currentTimeFilter = filterType;

    if (filterType === "manual") {
      manualTimeInput = currentUTCTime;
      showManualTimeInput = true;
      manualTimeMode = true;
      // Focus the input after a short delay
      setTimeout(() => {
        const manualTimeInputEl = document.getElementById("manual-time-input");
        if (manualTimeInputEl) manualTimeInputEl.focus();
      }, 50);
    } else {
      showManualTimeInput = false;
      manualTimeMode = false;
      applyTimeFilterWithManualTime(filterType, currentUTCTime);
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
        <label class="checkbox-option">
          <input type="checkbox" bind:checked={exactFrequency} />
          <span>Exact</span>
        </label>
        <label class="checkbox-option">
          <input type="checkbox" bind:checked={manualFrequency} />
          <span>Manual</span>
        </label>
      </div>
    </div>

    <div class="frequency-box time-control-box" >
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
            checked
            on:change={handleTimeFilterChange}
          />
          <span>Now</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="soon"
            on:change={handleTimeFilterChange}
          />
          <span>Soon</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="all"
            on:change={handleTimeFilterChange}
          />
          <span>All</span>
        </label>
        <label class="radio-option">
          <input
            type="radio"
            name="time"
            value="manual"
            on:change={handleTimeFilterChange}
          />
          <span>Manual</span>
        </label>
      </div>
    </div>
  </div>

  <button on:click={importCSV} disabled={loading}>
    {loading ? "Importing..." : "Import CSV"}
  </button>

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
  :global(*) {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
  }

  :global(html, body) {
    height: 100%;
    overflow: hidden;
  }

  main {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 20px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen,
      Ubuntu, sans-serif;
  }

  .header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px;
    flex-shrink: 0;
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
    min-width: 140px;
    position: relative;
  }

  .frequency-label {
    font-weight: bold;
    font-size: 14px;
    color: #000000;
  }

  .time-label {
    font-weight: bold;
    font-size: 14px;
    color: #000000;
  }

  .frequency-display-container {
    position: relative;
    width: 100%;
    display: flex;
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

  .frequency-options {
    display: flex;
    gap: 15px;
    justify-content: center;
  }

  .checkbox-option {
    display: flex;
    align-items: center;
    gap: 5px;
    cursor: pointer;
    font-size: 12px;
    color: #000000;
  }

  .radio-group {
    border: 1px solid #ccc;
    border-radius: 6px;
    padding: 6px 6px;
    background-color: #fff;
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
  .radio-option span {
    color: #000000;
    font-weight: normal;
  }

  button {
    background-color: #3498db;
    color: black;
    border: none;
    padding: 10px 20px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 16px;
  }

  button:hover:not(:disabled) {
    background-color: #2980b9;
  }

  button:disabled {
    background-color: #95a5a6;
    cursor: not-allowed;
  }

  .error {
    color: #e74c3c;
    padding: 10px;
    margin-bottom: 15px;
    background-color: #fadbd8;
    border-radius: 4px;
    border-left: 4px solid #e74c3c;
  }

  .success {
    color: #27ae60;
    padding: 10px;
    margin-bottom: 15px;
    background-color: #d5f4e6;
    border-radius: 4px;
    border-left: 4px solid #27ae60;
  }

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

  .table-container {
    flex: 1;
    border: 1px solid #ddd;
    border-radius: 4px;
    overflow: hidden;
    margin-top: 15px;
    min-height: 300px;
    position: relative;
  }

  .loading-message {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    color: #666;
    font-style: italic;
  }

  :global(.tabulator) {
    height: 100% !important;
  }

  :global(.tabulator .tabulator-tableholder) {
    height: calc(100% - 40px) !important;
  }

  .time-group {
    min-width: 160px;
  }

  .time-display-container {
    position: relative;
    width: 100%;
    display: flex;
    justify-content: center;
    margin-bottom: 2px;
  }

  .utc-time-display {
    padding: 4px 4px;
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

  /* Adjust radio buttons layout for the new option */
  .radio-buttons {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }

  .radio-option {
    white-space: nowrap;
  }
</style>
