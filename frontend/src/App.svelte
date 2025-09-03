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
  let tableContainer; // This will reference the DOM element
  let loading = false;
  let errorMessage = "";
  let successMessage = "";
  let debugInfo = "";
  let currentTimeFilter = "now"; // Default to "now"
  let allData = []; // Store all data for filtering
  let frequency = "Loading...";
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

      // Save to JSON file
      console.log("Saving data to eibi-list.json...");
      await SaveEIBIList(data);

      allData = data; // Store all data

      if (table) {
        table.setData(data);
        // applyTimeFilter(currentTimeFilter); // Apply current filter to new data
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

  // Function to parse time string (HHMM) to minutes since midnight
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

  // Function to apply time filter based on selected radio button
  function applyTimeFilter(filterType) {
    currentTimeFilter = filterType;

    if (!table || allData.length === 0) return;

    const now = new Date();
    const currentUTCHours = now.getUTCHours();
    const currentUTCMinutes = now.getUTCMinutes();
    const currentMinutes = currentUTCHours * 60 + currentUTCMinutes;

    let filteredData = [];

    if (filterType === "now") {
      // Filter for rows where current time is between startTime and endTime
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        // Handle cases where end time might be on the next day
        if (endMinutes < startMinutes) {
          // End time is on the next day
          return currentMinutes >= startMinutes || currentMinutes <= endMinutes;
        } else {
          // Normal case within the same day
          return currentMinutes >= startMinutes && currentMinutes <= endMinutes;
        }
      });
    } else if (filterType === "soon") {
      // Filter for rows where current time is between startTime and endTime OR within 30 minutes of start time
      filteredData = allData.filter((row) => {
        const startMinutes = timeToMinutes(row.startTime);
        const endMinutes = timeToMinutes(row.endTime);

        // Check if current time is between start and end
        let isNow = false;
        if (endMinutes < startMinutes) {
          // End time is on the next day
          isNow =
            currentMinutes >= startMinutes || currentMinutes <= endMinutes;
        } else {
          // Normal case within the same day
          isNow =
            currentMinutes >= startMinutes && currentMinutes <= endMinutes;
        }

        // Check if current time is within 30 minutes of start time
        const timeUntilStart = (startMinutes - currentMinutes + 1440) % 1440;
        const isSoon = timeUntilStart <= 30 && timeUntilStart > 0;

        return isNow || isSoon;
      });
    } else if (filterType === "all") {
      filteredData = allData;
    }
    // For "all", we use allData without filtering

    table.setData(filteredData);
  }

  // Handle radio button change
  function handleTimeFilterChange(event) {
    applyTimeFilter(event.target.value);
  }

  function initTabulator(data) {
    // Add guard to prevent double initialization
    if (tabulatorInitialized && table) {
      console.log("Tabulator already initialized, setting data only");
      console.log("Setting data with", data.length, "records");
      table.setData(data);
      return;
    }
    
    if (!tableContainer) {
      console.error("Table container not found!");
      return;
    }
    
    console.log("Initializing Tabulator with data:", data, "records");
    table = new Tabulator(tableContainer, {
      data: data,
      layout: "fitColumns",
      responsiveLayout: "collapse",
      height: "100%", // Ensure table takes full container height

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

    // Add debug event to see what data Tabulator is receiving
    table.on("dataLoaded", function (data) {
      console.log("Tabulator data loaded:", data);
      if (data && data.length > 0) {
        console.log("First row in tabulator:", data[0]);
        console.log("khz value in tabulator:", data[0].khz);
      }
    });

    // Add column resize event to debug
    table.on("columnResized", function (column) {
      console.log(
        "Column resized:",
        column.getField(),
        "width:",
        column.getWidth(),
      );
    });
  }

  function handleResize() {
    if (table) {
      table.redraw(true);
    }
  }

  onMount(() => {
    if (hasMounted) return;
    hasMounted = true;
    
    console.log("Component mounted, tableContainer:", tableContainer);

    (async () => {
      try {
        console.log("Checking for existing eibi-list.json...");
        const existingData = await LoadEIBIList();
        console.log("Existing data loaded:", existingData.length);
        if (existingData && existingData.length > 0) {
          console.log("Found existing data:", existingData.length, "records");
          allData = existingData;
          initTabulator(existingData);
          console.log("Tabulator initialized with existing data");
          successMessage = `Loaded ${existingData.length} records from eibi-list.json`;
        } else {
          console.log("No existing data found, initializing empty table");
          allData = [];
          initTabulator([]);
        }
      } catch (err) {
        console.error("Error loading existing data:", err);
        allData = [];
        initTabulator([]);
      }

      // Get initial status
      updateStatus();
    })();

    //Set up event listeners
    EventsOn("frequencyUpdate", (newFrequency) => {
      frequency = newFrequency;
      updateRadioInput(newFrequency);
    });

    EventsOn("modeUpdate", (newMode) => {
      mode = newMode;
    });

    EventsOn("connectionStatus", (isConnected) => {
      connected = isConnected;
      statusMessage = isConnected ? "Connected to radio" : "Disconnected";
    });

    // Add window resize listener
    window.addEventListener("resize", handleResize);

    // Cleanup function
    return () => {
      window.removeEventListener("resize", handleResize);
      // Clean up Tabulator
      if (table) {
        table.destroy();
        table = null;
        tabulatorInitialized = false;
      }
    };
  });

  function updateRadioInput(freq) {
    const radioInput = document.getElementById("radio-input");
    if (radioInput && radioInput instanceof HTMLInputElement) {
      radioInput.value = formatFrequencyDisplay(freq);
    }
  }

  async function updateStatus() {
    try {
      const status = await GetRadioStatus();
      connected = status.connected;
      if (status.frequency) frequency = status.frequency.toString();
      if (status.mode) mode = status.mode;
      statusMessage = connected ? "Connected to radio" : "Disconnected";
    } catch (error) {
      statusMessage = "Error getting status";
      console.error("Status error:", error);
    }
  }

  function formatFrequencyDisplay(freq) {
    if (!freq) return "0";
    const num = parseInt(freq) || 0;
    return formatFrequency(num);
  }

  function formatFrequency(freq) {
    const freqStr = freq.toString().padStart(8, "0");
    return freqStr.slice;
  }

  async function updateConfiguration() {
    try {
      await UpdateConfig(config.address);
      await updateStatus();
      statusMessage = "Configuration updated";
    } catch (error) {
      console.error("Config update error:", error);
      statusMessage = "Config update failed";
    }
  }

  async function setNewFrequency() {
    const input = document.getElementById("set-frequency");
    if (input) {
      const freq = parseInt(input.value.replace(/\./g, ""));
      if (!isNaN(freq)) {
        try {
          await SetFrequency(freq);
          statusMessage = "Frequency set successfully";
          // Refresh current frequency
          const newFreq = await GetCurrentFrequency();
          frequency = newFreq;
        } catch (error) {
          statusMessage = "Failed to set frequency";
          console.error("Set frequency error:", error);
        }
      }
    }
  }

  async function sendCustomCommand() {
    if (customCommand.trim()) {
      try {
        const response = await SendCustomCommand(customCommand);
        commandResponse = response;
        statusMessage = "Command executed";

        // Refresh data if frequency or mode might have changed
        if (
          customCommand.toLowerCase() === "f" ||
          customCommand.toLowerCase() === "m"
        ) {
          await updateStatus();
        }
      } catch (error) {
        commandResponse = `Error: ${error.message}`;
        statusMessage = "Command failed";
      }
    }
  }

  // Debug panel state and function already defined above, so remove duplicate below if present.
</script>

<main>
  <div class="header">
    <label for="radio-input" class="radio-label">
      <span>Radio</span>
      <span>Frequency:</span>
    </label>
    <input
      id="radio-input"
      type="text"
      class="radio-input"
      maxlength="8"
      placeholder="Frequency"
      readonly
      value={frequency}
    />

    <div class="radio-group">
      <div class="radio-group-title">Time</div>
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
      </div>
    </div>

    <button on:click={importCSV} disabled={loading}>
      {loading ? "Importing..." : "Import CSV"}
    </button>
  </div>

  <!-- ADD THIS TABLE CONTAINER -->
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
  /* Your existing styles remain unchanged */
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
    padding: 10px;
    flex-shrink: 0;
  }

  .radio-label {
    display: flex;
    flex-direction: column;
    font-weight: bold;
    color: #ffffff;
    min-width: 60px;
    background-color: transparent;
    text-align: left;
    line-height: 1.2;
    padding: 4px 8px;
  }

  .radio-label span:first-child {
    font-size: 14px;
  }

  .radio-label span:last-child {
    font-size: 14px;
  }

  .radio-input {
    padding: 8px 12px;
    border: 1px solid #ddd;
    border-radius: 4px;
    width: 100px;
    text-align: center;
    font-family: monospace;
    background-color: #f8f9fa;
    color: #495057;
    cursor: not-allowed;
  }

  .radio-group {
    border: 1px solid #ccc;
    border-radius: 6px;
    padding: 8px 12px;
    background-color: #fff;
  }

  .radio-group-title {
    font-weight: bold;
    font-size: 14px;
    color: black;
    margin-bottom: 4px;
    text-align: center;
  }

  .radio-buttons {
    display: flex;
    gap: 15px;
  }

  .radio-option {
    display: flex;
    align-items: center;
    gap: 5px;
    cursor: pointer;
    font-size: 14px;
  }

  .radio-option span {
    color: #000000 !important; /* Force black text */
    font-weight: normal;
  }

  .radio-option input[type="radio"] {
    margin: 0;
  }

  button {
    background-color: #3498db;
    color: black;
    border: none;
    padding: 10px 20px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 16px;
    transition: background-color 0.3s;
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

  /* ADD THESE STYLES FOR TABLE CONTAINER */
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

  /* Ensure Tabulator styles work properly */
  :global(.tabulator) {
    height: 100% !important;
  }

  :global(.tabulator .tabulator-tableholder) {
    height: calc(100% - 40px) !important;
  }
</style>