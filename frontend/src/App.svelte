<script>
  import { onMount } from "svelte";
  import { TabulatorFull as Tabulator } from "tabulator-tables";
  import "tabulator-tables/dist/css/tabulator.min.css";
  import {
    ImportCSV,
    LoadEIBIList,
    SaveEIBIList,
  } from "../wailsjs/go/main/App";

  let table;
  let tableContainer;
  let loading = false;
  let errorMessage = "";
  let successMessage = "";
  let debugInfo = "";
  let currentTimeFilter = "now"; // Default to "now"
  let allData = []; // Store all data for filtering

  // Load data on startup
  onMount(async () => {
    try {
      console.log("Checking for existing eibi-list.json...");
      const existingData = await LoadEIBIList();

      if (existingData && existingData.length > 0) {
        console.log("Found existing data:", existingData.length, "records");
        allData = existingData;
        initTabulator(existingData);
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
  });

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
        debugInfo =
          "The CSV file may contain only headers or be improperly formatted";
        return;
      }

      // Debug the data structure
      debugDataStructure(data);

      // Debug: show first few rows
      debugInfo = `Parsed ${data.length} rows. First row khz: "${data[0].khz}"`;

      // Save to JSON file
      console.log("Saving data to eibi-list.json...");
      await SaveEIBIList(data);

      allData = data; // Store all data

      if (table) {
        table.setData(data);
        applyTimeFilter(currentTimeFilter); // Apply current filter to new data
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

  function debugDataStructure(data) {
    if (!data || data.length === 0) return;

    const firstRow = data[0];
    console.log("Data structure keys:", Object.keys(firstRow));
    console.log("Data structure values:", firstRow);

    // Check if khz field exists
    if ("khz" in firstRow) {
      console.log("khz field exists with value:", firstRow.khz);
    } else {
      console.log(
        "khz field does not exist. Available fields:",
        Object.keys(firstRow),
      );
    }
  }

  // Function to parse time string (HH:MM) to minutes since midnight
  function timeToMinutes(timeStr) {
    if (!timeStr) return 0;
    const [hours, minutes] = timeStr.split(":").map(Number);
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

    let filteredData = allData;

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
    }
    // For "all", we use allData without filtering

    table.setData(filteredData);
  }

  // Handle radio button change
  function handleTimeFilterChange(event) {
    applyTimeFilter(event.target.value);
  }

  function initTabulator(data) {
    table = new Tabulator(tableContainer, {
      data: data,
      layout: "fitColumns",
      responsiveLayout: "collapse",
      // pagination: 'local',
      // paginationSize: 10,
      // paginationSizeSelector: [5, 10, 20, 50],
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
      height: "100%",
    });

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
    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  });
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

  {#if errorMessage}
    <div class="error">{errorMessage}</div>
  {/if}

  {#if successMessage}
    <div class="success">{successMessage}</div>
  {/if}

  {#if debugInfo}
    <div class="debug">{debugInfo}</div>
  {/if}

  <div class="table-container" bind:this={tableContainer}></div>
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

  .table-container {
    flex: 1;
    border: 1px solid #ddd;
    border-radius: 4px;
    overflow: hidden;
  }
</style>
