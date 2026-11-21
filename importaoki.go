package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// ImportAOKI downloads the newest AOKI shortwave schedule published on the Bi
// Newsletter page and returns the parsed rows.
func (a *App) ImportAOKI() ([]CSVRow, error) {
	page, pageURL, err := aokiFetchLinkPage()
	if err != nil {
		return nil, err
	}

	schedules := aokiParseScheduleLinks(page, pageURL)
	schedule, fileName, content, err := aokiNewestArchive(schedules)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Downloading AOKI schedule %s (%s) from %s\n", schedule.Season, schedule.Name, schedule.URL)
	if !schedule.Revised.IsZero() {
		fmt.Printf("Schedule %s is dated %s\n", fileName, schedule.Revised.Format("02-Jan-2006"))
	}

	data, err := aokiParseSchedule(content)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Parsed %d data rows from AOKI %s\n", len(data), fileName)
	recordImportDate("AOKI")
	return data, nil
}

// aokiParseSchedule converts an AOKI schedule file into rows.
//
// The file is Shift_JIS (CP932) with fixed-width columns measured in bytes: a
// double-byte character occupies exactly two columns, so the columns have to be
// sliced out of the raw bytes and each field decoded separately. Splitting the
// whole file into UTF-8 first would shift every column after a Japanese station
// or power name.
func aokiParseSchedule(content []byte) ([]CSVRow, error) {
	var data []CSVRow
	skipped := 0

	for i, raw := range bytes.Split(content, []byte("\n")) {
		raw = bytes.TrimSuffix(raw, []byte("\r"))
		// The first two lines are the title and the column headings.
		if i < 2 || len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		// Some rows stop before the trailing columns (no ADM/L/L/remarks), and
		// the file ends with a free-text note; keep the former, drop the latter.
		if len(raw) < 38 {
			skipped++
			continue
		}

		khz := aokiField(raw, 0, 6)
		stationName := aokiField(raw, 6, 38)
		ituCode, transmitter := aokiLocationAndCountry(raw)

		// The station name column is 32 bytes, but a longer name (or a name
		// holding double-byte characters, whose padding is two columns wide)
		// pushes every later column to the right, so the fixed offset would
		// read "0000-2400" as "00-2400 1". The time column is unambiguous, so
		// the layout is re-synchronised on it instead.
		timeStart, timeUTC, ok := aokiTimeField(raw)
		if !ok {
			skipped++
			continue
		}

		// Columns after the time shift by however far the name overflowed.
		shift := timeStart - aokiTimeColumn
		daysString := aokiField(raw, 48+shift, 55+shift)
		languageName := aokiField(raw, 56+shift, 76+shift)

		if khz == "" || timeUTC == "" {
			skipped++
			continue
		}

		// Parse time range into separate start and end times
		startTime, endTime := ParseTimeRange(timeUTC)
		countryName := GetCountryName(ituCode)

		data = append(data, CSVRow{
			Khz:         khz,
			StartTime:   startTime,
			EndTime:     endTime,
			Days:        daysString,
			Country:     countryName,
			Station:     stationName,
			Language:    languageName,
			Transmitter: transmitter,
			Source:      "Aoki",
		})
	}

	if skipped > 0 {
		fmt.Printf("Skipped %d unparseable AOKI lines\n", skipped)
	}
	return data, nil
}

// aokiField returns raw[start:end] decoded from Shift_JIS and trimmed of
// surrounding spaces, tolerating short rows.
func aokiField(raw []byte, start, end int) string {
	if start >= len(raw) {
		return ""
	}
	if end > len(raw) {
		end = len(raw)
	}
	return strings.TrimSpace(aokiDecode(raw[start:end]))
}

// aokiLocationAndCountry splits the transmitter location from the administration
// code that follows it.
//
// The columns are normally Location 85-107 and ADM 109-111, but a location
// longer than its 23 bytes pushes the code to the right, which would otherwise
// be read as text: "Ceske Bud?jovice/HusovaCZE" yields the fragment "ZE". The
// position string is fixed width, so when the fixed columns do not produce a
// known administration code the row is re-aligned on that position instead.
func aokiLocationAndCountry(raw []byte) (country, location string) {
	fixedCountry := aokiField(raw, 109, 112)
	fixedLocation := aokiField(raw, 85, 108)

	// A code the country table knows is trusted as-is.
	if isKnownAokiCountry(fixedCountry) {
		return fixedCountry, fixedLocation
	}

	if position := aokiPositionStart(raw); position > 0 {
		if code := aokiCodeBefore(raw, position); code != "" && isKnownAokiCountry(code) {
			return code, aokiField(raw, 85, position-len(code)-1)
		}
	}

	// Neither alignment produced a known code: keep the raw columns so the row
	// is still imported (private stations often have no administration).
	return fixedCountry, fixedLocation
}

// isKnownAokiCountry reports whether code is an administration code present in
// the country table.
func isKnownAokiCountry(code string) bool {
	if code == "" {
		return false
	}
	referenceTablesMu.RLock()
	defer referenceTablesMu.RUnlock()

	_, ok := ITUCountryMap[strings.ToUpper(code)]
	return ok
}

// aokiCodeBefore returns the administration code that ends immediately before
// the position field at offset position, e.g. for
//
//	"...HusovaCZE 485920N0142937E"
//
// it returns "CZE": the letters immediately before the single space that
// separates the code from the position.
func aokiCodeBefore(raw []byte, position int) string {
	// Step over the single space separating the code from the position.
	end := position
	if end > 0 && raw[end-1] == ' ' {
		end--
	}

	// The code is at most three letters.
	start := end
	for start > 0 && end-start < 3 && isAokiLetter(raw[start-1]) {
		start--
	}
	if start == end {
		return ""
	}
	return aokiField(raw, start, end)
}

// isAokiLetter reports whether b is an ASCII letter.
func isAokiLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// aokiTimeColumn is the byte offset of the time column when no name overflow
// pushes it right.
const aokiTimeColumn = 38

// aokiTimePattern matches the "0000-2400" time range.
var aokiTimePattern = regexp.MustCompile(`\d{4}-\d{4}`)

// aokiTimeField locates the time column and returns its offset and value.
//
// The column normally starts at aokiTimeColumn, but a station name longer than
// its 32-byte field shifts it right. The range is searched for instead of being
// sliced at a fixed offset, and the search starts after the name column so a
// digits-only station name cannot be mistaken for a time.
func aokiTimeField(raw []byte) (start int, value string, ok bool) {
	const searchFrom = 30
	if len(raw) <= searchFrom {
		return 0, "", false
	}

	loc := aokiTimePattern.FindIndex(raw[searchFrom:])
	if loc == nil {
		return 0, "", false
	}
	offset := searchFrom + loc[0]
	return offset, string(raw[offset : offset+loc[1]-loc[0]]), true
}

// aokiPositionStart returns the byte offset of the latitude/longitude field.
// It searches from before the expected column (a long location pushes the field
// right) so an "N" inside a location name cannot match, and requires the field
// to be a space-separated 6-digit latitude followed by N/S.
func aokiPositionStart(raw []byte) int {
	for i := 100; i+7 < len(raw); i++ {
		if !isDigits(raw[i : i+6]) {
			continue
		}
		if raw[i+6] != 'N' && raw[i+6] != 'S' {
			continue
		}
		// The position must be preceded by a space separator.
		if i == 0 || raw[i-1] != ' ' {
			continue
		}
		return i
	}
	return -1
}

// isDigits reports whether b is non-empty and made only of ASCII digits.
func isDigits(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// aokiDecode converts Shift_JIS (CP932) bytes to UTF-8, falling back to the raw
// bytes when decoding fails so that a single bad character cannot drop a row.
func aokiDecode(raw []byte) string {
	decoded, _, err := transform.Bytes(japanese.ShiftJIS.NewDecoder(), raw)
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

// LoadAokiList loads data from aoki-list.json if it exists
func (a *App) LoadAokiList() ([]CSVRow, error) {
	jsonPath, err := appDataPath("aoki-list.json")
	if err != nil {
		return nil, err
	}

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
	jsonPath, err := appDataPath("aoki-list.json")
	if err != nil {
		return err
	}

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

	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return fmt.Errorf("error creating data directory: %w", err)
	}
	err = os.WriteFile(jsonPath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing JSON file: %w", err)
	}

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
