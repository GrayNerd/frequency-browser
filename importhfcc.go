package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	// "strings"

	// "time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)



func (a *App) ImportHFCC() ([]CSVRow, error) {
	// Open file dialog
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select HFCC File (userlist1.txt)",
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
	transformer := transform.NewReader(file, charmap.Windows1252.NewDecoder())

	scanner := bufio.NewScanner(transformer)

	// Convert to struct
	var data []CSVRow
	i := 0
	for scanner.Scan() {
		i++
		if i < 8 {
			continue
		}

		line := scanner.Text()
		if len(line) < 20 {
			break
		}

		khz := line[0:5]
		startTime := line[6:10]
		endTime := line[11:15]
		daysString := line[72:79]
		ituCode := line[113:116]
		countryName := GetCountryName(ituCode)
		stationCode := line[117:120]
		stationName := GetHfccStationName(stationCode)
		languageCode := line[102:105]
		languageName := getLanguageName(languageCode)

		siteCode := line[47:50] // substring(record[0], 47, 50107)) // This might be Location now
		siteName := GetHfccSiteName (siteCode)
		siteCountry := GetCountryName(GetHfccSiteADM(siteCode))

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
			Transmitter: siteName + ", " + siteCountry,
			Source:      "HFCC",
		}

		data = append(data, row)
	}

	fmt.Printf("Parsed %d data rows from HFCC file\n", len(data))
	return data, nil
}

// LoadHfccList loads data from hfcc-list.json if it exists
func (a *App) LoadHfccList() ([]CSVRow, error) {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "hfcc-list.json")

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

	fmt.Printf("Loaded %d records from hfcc-list.json\n", len(data))
	return data, nil
}

// SaveAHfccList saves data to hfcc-list.json
func (a *App) SaveHfccList(data []CSVRow) error {
	// Get the current directory
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error getting executable path: %w", err)
	}
	dir := filepath.Dir(exePath)
	jsonPath := filepath.Join(dir, "hfcc-list.json")

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
		Message: fmt.Sprintf("Saved %d records to hfcc-list.json", len(data)),
	})

	return nil
}

