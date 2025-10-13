package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ImportAoki imports an Aoki format file and returns its contents
func (a *App) ImportAOKI() ([]CSVRow, error) {
	// Open file dialog
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Aoki File (userlist1.txt)",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Text Files (*.txt)",
				Pattern:     "*.txt",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error opening file dialog: %w", err)
	}

	if filePath == "" {
		return nil, nil // User cancelled
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Convert to struct
	var data []CSVRow
	i := 0
	for scanner.Scan() {
		i++
		if i < 3 {
			continue
		}

		line := scanner.Text()
		if strings.Contains(line, "?") {
			fmt.Printf("skipping line %4d: %v\n", i+1, line)
			continue
		}
		runes := []rune(line)
		if len(line) < 20 {
			break
		}
		khz := string(runes[0:5])
		timeUTC := string(runes[38:47])
		stationName := string(runes[6:38])
		daysString := string(runes[48:55])
		languageName := string(runes[56:76])

		var ituCode string
		var transmitter string
		if len(runes) > 109 {
			ituCode = string(runes[109:112])
			transmitter = string(runes[84:107]) 
		}

		// Parse time range into separate start and end times
		startTime, endTime := ParseTimeRange(timeUTC)

		countryName := GetCountryName(ituCode)

		row := CSVRow{
			Khz:       khz,
			StartTime: startTime,
			EndTime:   endTime,
			Days:      daysString,
			Country:   countryName,
			Station:   stationName,
			Language:  languageName,
			// Target:      targetName,
			Transmitter: transmitter,
			Source:      "Aoki",
		}

		data = append(data, row)
	}

	fmt.Printf("Parsed %d data rows from Aoki file\n", len(data))
	return data, nil
}

// LoadAokiList loads data from aoki-list.json if it exists
func (a *App) LoadAokiList() ([]CSVRow, error) {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "aoki-list.json")

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return nil, nil // File doesn't exist, return empty data
	}

	fileData, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON file: %w", err)
	}

	var data []CSVRow
	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	fmt.Printf("Loaded %d records from aoki-list.json\n", len(data))
	return data, nil
}

// SaveAokiList saves data to aoki-list.json
func (a *App) SaveAokiList(data []CSVRow) error {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "aoki-list.json")

	// Delete existing file if it exists
	if _, err := os.Stat(jsonPath); err == nil {
		err = os.Remove(jsonPath)
		if err != nil {
			return fmt.Errorf("error deleting existing JSON file: %w", err)
		}
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	err = os.WriteFile(jsonPath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing JSON file: %w", err)
	}

	// Show success message in dialog box with OK button
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "Success",
		Message: fmt.Sprintf("Saved %d records to aoki-list.json", len(data)),
	})

	return nil
}

// Helper function to safely extract substrings
func substring(s string, start, end int) string {
	if start >= len(s) {
		return ""
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
