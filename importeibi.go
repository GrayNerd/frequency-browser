package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CSVRow represents a row in the CSV file
type CSVRow struct {
	Khz         string `json:"khz"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Days        string `json:"days"`
	Country     string `json:"country"`
	Station     string `json:"station"`
	Language    string `json:"language"`
	Target      string `json:"target"`
	Transmitter string `json:"transmitter"` // New field
	Source      string `json:"source"`      // New field
}

// parseTimeRange splits a time range like "0000-2400" into start and end times

func ParseTimeRange(timeRange string) (string, string) {
	if timeRange == "" {
		return "", ""
	}

	parts := strings.Split(timeRange, "-")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return timeRange, "" // Return original if no hyphen found
}

// isHeaderRow checks if a row is a header/metadata row
func isHeaderRow(record []string) bool {
	if len(record) == 0 {
		return false
	}

	// Check if first field looks like a header (contains "kHz" or ":")
	firstField := strings.ToLower(strings.TrimSpace(record[0]))
	return strings.Contains(firstField, "khz:") || strings.Contains(firstField, ":") ||
		strings.Contains(firstField, "frequency") || strings.Contains(firstField, "freq")
}

// ImportEiBi imports a CSV file and returns its contents
func (a *App) ImportEiBi() ([]CSVRow, error) {
	// Open file dialog
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select CSV File",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "CSV Files (*.csv)",
				Pattern:     "*.csv",
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

	// Create a reader that converts from Windows-1252 to UTF-8 and removes BOM
	decodedReader := transform.NewReader(file, charmap.Windows1252.NewDecoder())

	// Parse CSV
	reader := csv.NewReader(decodedReader)
	reader.Comma = ';'          
	reader.FieldsPerRecord = -1 // Allow variable number of fields
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error parsing CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	// Convert to struct
	var data []CSVRow
	for i, record := range records {
		i=i
		// Skip header/metadata rows
		if isHeaderRow(record) {
			continue
		}

		// Skip empty rows
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue
		}

		// Ensure we have enough fields (we need at least 8 for existing fields)
		if len(record) < 8 {
			// Pad with empty strings if needed
			for len(record) < 7 {
				record = append(record, "")
			}
		}

		// Parse time range into separate start and end times
		timeUTC := strings.TrimSpace(record[1])
		startTime, endTime := ParseTimeRange(timeUTC)

		daysString := ConvertDays(strings.TrimSpace(record[2]))	
		ituCode := strings.TrimSpace(record[3])
		countryName := GetCountryName(ituCode)

		stationName := strings.TrimSpace(record[4])

		// Convert language code to full language name
		langCode := strings.TrimSpace(record[5])
		languageName := getLanguageName(langCode)

		targetCode := strings.TrimSpace(record[6]) // Extract target code from record[6]
		targetName := GetTargetDescription(targetCode)
		if targetName == "" {
			targetName = GetCountryName(targetCode) // Use country name if target code not found (e.g. "code")
		} else {
			targetName = targetCode
		}

		// fmt.Printf("looking up transmitter for %s\n", ituCode)
		transmitter := GetSiteDescription(ituCode, strings.TrimSpace(record[7]))

		row := CSVRow{
			Khz:         strings.TrimSpace(record[0]),
			StartTime:   startTime,
			EndTime:     endTime,
			Days:        daysString, //strings.TrimSpace(record[2]),
			Country:     countryName,
			Station:     stationName,
			Language:    languageName,
			Target:      targetName,
			Transmitter: transmitter,
			Source:      "EiBi",
		}

		data = append(data, row)
	}

	return data, nil
}

// LoadEIBIList loads data from eibi-list.json if it exists
func (a *App) LoadEIBIList() ([]CSVRow, error) {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "eibi-list.json")

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

	fmt.Printf("Loaded %d records from eibi-list.json\n", len(data))
	return data, nil
}

// SaveEIBIList saves data to eibi-list.json
func (a *App) SaveEIBIList(data []CSVRow) error {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "eibi-list.json")

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
		Message: fmt.Sprintf("Saved %d records to eibi-list.json", len(data)),
	})

	return nil
}

