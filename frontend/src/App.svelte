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
    // When manual mode is disabled, update with actual radio frequency
    updateFrequencyFromRadio();
  }

  async function updateFrequencyFromRadio() {
    try {
      const currentFreq = await GetCurrentFrequency();
      if (currentFreq !== undefined) {
        frequency = typeof currentFreq === "string" 
          ? parseFloat(currentFreq) 
          : currentFreq;
      }
    } catch (error) {
      console.error("Error getting current frequency:", error);
      statusMessage = "Failed to get current frequency from radio";
    }
  }