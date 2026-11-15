package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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

const (
	// eibiSiteURL is the EiBi home page; it links the current season's CSV
	// database and the README with the reference tables.
	eibiSiteURL = "http://www.eibispace.de/"
	eibiBaseURL = "http://www.eibispace.de/"

	// eibiHTTPTimeout bounds each HTTP request made while importing.
	eibiHTTPTimeout = 2 * time.Minute
)

var eibiScheduleLinkRE = regexp.MustCompile(`(?i)HREF="([^"]*sked-[a-z0-9]+\.csv)"`)

// eibiSeasonRE extracts the season from a file name, e.g. "sked-b26.csv" -> "B26".
var eibiSeasonRE = regexp.MustCompile(`(?i)sked-([ab])(\d{2})\.csv`)

// eibiSeason describes the EiBi schedule currently published on the web site.
type eibiSeason struct {
	Season string // "B26"
	URL    string // absolute URL of the CSV database
	Name   string // file name, e.g. "sked-b26.csv"
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

// ImportEiBi downloads the newest EiBi schedule database (sked-???.csv) and its
// README.TXT reference tables, refreshes the lookups and returns the rows.
func (a *App) ImportEiBi() ([]CSVRow, error) {
	season, err := eibiFetchCurrentSeason()
	if err != nil {
		return nil, err
	}

	fmt.Printf("Downloading EiBi schedule %s from %s\n", season.Season, season.URL)

	sked, err := eibiDownload(season.URL)
	if err != nil {
		return nil, err
	}

	// Refresh the reference tables from README.TXT before parsing, so new
	// language, country, target and transmitter-site codes resolve.
	tables, err := eibiFetchReferenceTables(season.Season)
	if err != nil {
		fmt.Printf("Warning: could not refresh EiBi reference tables: %v\n", err)
	} else {
		if len(tables.Missing) > 0 {
			fmt.Printf("EiBi README.TXT is missing %s; keeping existing entries\n",
				strings.Join(tables.Missing, ", "))
		}
		stats := eibiApplyReferenceTables(tables)
		fmt.Printf("Updated EiBi %s\n", stats)
		if err := eibiSaveReferenceTables(tables); err != nil {
			fmt.Printf("Warning: could not cache EiBi reference tables: %v\n", err)
		}
	}

	data, err := eibiParseSchedule(sked)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Parsed %d data rows from EiBi %s\n", len(data), season.Name)
	recordImportDate("EiBi")
	return data, nil
}

// eibiDownload fetches rawURL and returns the response body.
func eibiDownload(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "frequency-browser/1.0")

	client := &http.Client{Timeout: eibiHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: unexpected status %s", rawURL, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	return body, nil
}

// eibiFetchCurrentSeason reads the EiBi home page and returns the schedule
// database for the current season.
func eibiFetchCurrentSeason() (eibiSeason, error) {
	page, err := eibiDownload(eibiSiteURL)
	if err != nil {
		return eibiSeason{}, err
	}
	seasons := eibiParseSeasonLinks(page, eibiBaseURL)
	if len(seasons) == 0 {
		return eibiSeason{}, fmt.Errorf("no schedule database (sked-???.csv) found on %s", eibiSiteURL)
	}
	return seasons[len(seasons)-1], nil
}

// eibiParseSeasonLinks extracts every schedule database reference from the EiBi
// home page, resolving relative links against baseURL and ordering the list by
// season so the last entry is the current one.
func eibiParseSeasonLinks(page []byte, baseURL string) []eibiSeason {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	matches := eibiScheduleLinkRE.FindAllSubmatchIndex(page, -1)
	seasons := make([]eibiSeason, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for _, match := range matches {
		href := string(page[match[2]:match[3]])
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}

		name := path.Base(ref.Path)
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		s := eibiSeason{
			Season: eibiSeasonCode(name),
			Name:   name,
			URL:    base.ResolveReference(ref).String(),
		}
		if s.Season == "" {
			continue
		}
		seasons = append(seasons, s)
	}

	sort.SliceStable(seasons, func(i, j int) bool {
		iy, io := eibiSeasonRank(seasons[i].Season)
		jy, jo := eibiSeasonRank(seasons[j].Season)
		if iy != jy {
			return iy < jy
		}
		return io < jo
	})
	return seasons
}

// eibiSeasonCode returns the season code of a schedule file name
// ("sked-b26.csv" -> "B26"), or an empty string when it has none.
func eibiSeasonCode(name string) string {
	match := eibiSeasonRE.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return strings.ToUpper(match[1] + match[2])
}

// eibiSeasonRank orders season codes chronologically: A25, B25, A26, B26.
func eibiSeasonRank(season string) (year int, order int) {
	if len(season) < 3 {
		return 0, 0
	}
	year, err := strconv.Atoi(season[1:])
	if err != nil {
		return 0, 0
	}
	if strings.EqualFold(season[:1], "A") {
		return year, 0
	}
	return year, 1
}

// eibiFetchReferenceTables downloads README.TXT and parses its reference tables.
func eibiFetchReferenceTables(season string) (eibiReferenceTables, error) {
	readme, err := eibiDownload(eibiBaseURL + "dx/README.TXT")
	if err != nil {
		return eibiReferenceTables{}, err
	}

	tables := eibiParseReadme(readme)
	tables.Season = season
	return tables, nil
}

// eibiParseSchedule converts the EiBi CSV database into rows.
func eibiParseSchedule(content []byte) ([]CSVRow, error) {
	// Create a reader that converts from Windows-1252 to UTF-8
	decodedReader := strings.NewReader(eibiDecodeText(content))

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
	for _, record := range records {
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
			for len(record) < 8 {
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
		}

		// Resolve the transmitter site from the refreshed README table, falling
		// back to the embedded sites.json for countries it does not cover.
		transmitter := GetEiBiSiteDescription(ituCode, strings.TrimSpace(record[7]))

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
