package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

	// Create a reader that converts from Shift-JIS to UTF-8 (common Japanese encoding)
	// transformer := transform.NewReader(file, japanese.ShiftJIS.NewDecoder())
	// transformer := transform.NewReader(file, charmap.ISO8859_1.NewDecoder())

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
		runes := []rune(line)
		if len(line) < 20 {
			break
		}
		// fmt.Printf("record: ***%v***\n", line)

		// khz := strings.TrimSpace(substring(record[0], 0, 4))
		// khz := substring(line, 0, 4)
		khz := string(runes[0:5])
		// fmt.Printf("khz: ***%v***\n", khz)
		timeUTC := string(runes[38:47]) // strings.TrimSpace(substring(line, 36, 45))
		// ituCode := strings.TrimSpace(substring(line, 109, 112))
		stationName := string(runes[6:38])
		daysString := string(runes[48:55])
		languageName := string(runes[56:76]) // strings.TrimSpace(substring(line, 56, 76)) // Lang. column

		var ituCode string
		var transmitter string
		if len(runes) > 109 {
			ituCode = string(runes[109:112])
			transmitter = string(runes[84:107]) // strings.TrimSpace(substring(line, 84, 107)) // This might be Location now
		}

		// fmt.Printf("line: %v\n", line)
		// fmt.Printf("khz: %v\n", khz)
		// fmt.Printf("timeUTC: %v\n", timeUTC)
		// fmt.Printf("ituCode: %v\n", ituCode)
		// fmt.Printf("station: %v\n", stationName)
		// fmt.Printf("langCode: ***%v***\n", languageName)

		// fmt.Printf("khz: %s, timeUTC: %s, days: %s, ituCode: %s, stationName: %s, language: %s, target: %s\n", khz,record[2], days, ituCode, stationName, language, target)
		// Parse time range into separate start and end times
		startTime, endTime := ParseTimeRange(timeUTC)

		// fmt.Printf("days: ***%s***\n", days)
		countryName := GetCountryName(ituCode)

		// fmt.Printf("khz: %s, startTime: %s, endTime: %s, days: %s, country: %s, station: %s, language: %s, transmitter: %s\n", khz, startTime, endTime, daysString, countryName, stationName, languageName, transmitter)
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

	// Check if file exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return nil, nil // File doesn't exist, return empty data
	}

	// Read the file
	fileData, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON file: %w", err)
	}

	// Parse JSON
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

	// Convert to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	// Write to file
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
