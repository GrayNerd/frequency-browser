package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// importDatesFile records when each source was last imported, so the UI can show
// how old the loaded schedule data is. It lives in the per-user data directory
// resolved by go-app-paths.
const importDatesFile = "import-dates.json"

// ImportDates maps a source name ("EiBi", "AOKI", "HFCC") to the RFC 3339 time
// of its last successful download. It is a plain map so the generated frontend
// binding is a simple object rather than a named class.
type ImportDates = map[string]string

// LoadImportDates returns the recorded import times, or an empty map when no
// import has been made yet.
func (a *App) LoadImportDates() (ImportDates, error) {
	path, err := importDatesPath()
	if err != nil {
		return ImportDates{}, err
	}
	return readImportDatesFrom(path)
}

// importDatesPath returns the path of the import date record, inside the
// per-user data directory resolved by go-app-paths.
func importDatesPath() (string, error) {
	return appDataPath(importDatesFile)
}

// recordImportDate stores the time a source was successfully imported. A failure
// to record the date must never fail the import itself, so it is only logged.
func recordImportDate(source string) {
	cachePath, err := importDatesPath()
	if err != nil {
		fmt.Printf("Warning: could not record the %s import date: %v\n", source, err)
		return
	}
	if err := recordImportDateTo(cachePath, source); err != nil {
		fmt.Printf("Warning: could not record the %s import date: %v\n", source, err)
	}
}

// recordImportDateTo records one source in the file at path, keeping the other
// entries intact.
func recordImportDateTo(path, source string) error {
	dates, err := readImportDatesFrom(path)
	if err != nil {
		// A corrupt record must not block recording a fresh date.
		dates = ImportDates{}
	}
	dates[source] = time.Now().UTC().Format(time.RFC3339)
	return writeImportDatesTo(path, dates)
}

// readImportDatesFrom loads the imported times recorded in path.
func readImportDatesFrom(path string) (ImportDates, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ImportDates{}, nil
	}
	if err != nil {
		return ImportDates{}, fmt.Errorf("error reading %s: %w", path, err)
	}

	var dates ImportDates
	if err := json.Unmarshal(data, &dates); err != nil {
		return ImportDates{}, fmt.Errorf("error parsing %s: %w", path, err)
	}
	if dates == nil {
		dates = ImportDates{}
	}
	return dates, nil
}

// writeImportDatesTo persists the recorded import times.
func writeImportDatesTo(path string, dates ImportDates) error {
	data, err := json.MarshalIndent(dates, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling import dates: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("error creating import dates directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("error writing %s: %w", path, err)
	}
	return nil
}
