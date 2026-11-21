package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

const (
	// hfccDataPageURL is the HFCC public data files index page. It lists every
	// published operational schedule archive (*allx2.zip).
	hfccDataPageURL = "https://new.hfcc.org/data/"

	// hfccHTTPTimeout bounds each HTTP request made while importing.
	hfccHTTPTimeout = 2 * time.Minute

	// hfccScheduleNamePrefixLen is the number of leading characters shared by a
	// schedule archive and the schedule data file inside it. For example
	// "b26allx2.zip" contains "B26all00.TXT" (both start with "b26a").
	hfccScheduleNamePrefixLen = 4
)

// hfccSchedule describes one downloadable schedule archive listed on the HFCC
// public data files page.
type hfccSchedule struct {
	Name    string    // Archive file name, e.g. "B26allx2.zip"
	URL     string    // Absolute URL of the archive
	Season  string    // Season code taken from the name, e.g. "B26"
	Updated time.Time // "Last updated on" date from the page, zero if absent
}

var (
	hfccScheduleLinkRE = regexp.MustCompile(`(?i)HREF="([^"]*allx2\.zip)"`)
	hfccLastUpdatedRE  = regexp.MustCompile(`(?i)Last updated on\s+(\d{1,2})-([A-Za-z]{3})-(\d{4})`)
	hfccSeasonRE       = regexp.MustCompile(`(?i)^([ab])(\d{2})`)
)

// ImportHFCC downloads the newest operational schedule published on the HFCC
// public data files page, extracts the matching schedule file from the archive
// and returns the parsed rows.
func (a *App) ImportHFCC() ([]CSVRow, error) {
	page, err := hfccFetchSchedulePage()
	if err != nil {
		return nil, err
	}

	schedules := hfccParseScheduleLinks(page, hfccDataPageURL)
	newest, ok := hfccNewestSchedule(schedules)
	if !ok {
		return nil, fmt.Errorf("no schedule archives (*allx2.zip) found on %s", hfccDataPageURL)
	}

	fmt.Printf("Downloading HFCC schedule %s from %s\n", newest.Season, newest.URL)

	archive, err := hfccDownload(newest.URL)
	if err != nil {
		return nil, err
	}

	fileName, content, err := hfccScheduleFromZip(archive, newest.Name)
	if err != nil {
		return nil, err
	}

	// Refresh the reference tables (sites, broadcasters, administrations,
	// languages) from the same archive before parsing, so new codes resolve.
	tables, err := hfccReferenceTablesFromZip(archive, newest.Name)
	if err != nil {
		return nil, err
	}
	tables.Season = newest.Season
	if len(tables.Missing) > 0 {
		fmt.Printf("HFCC archive %s has no %s; keeping existing entries\n",
			newest.Name, strings.Join(tables.Missing, ", "))
	}
	stats := hfccApplyReferenceTables(tables)
	fmt.Printf("Updated HFCC %s\n", stats)
	if err := hfccSaveReferenceTables(tables); err != nil {
		fmt.Printf("Warning: could not cache HFCC reference tables: %v\n", err)
	}

	data, err := hfccParseScheduleFile(content)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Parsed %d data rows from HFCC file %s (season %s)\n", len(data), fileName, newest.Season)
	recordImportDate("HFCC")
	return data, nil
}

// hfccFetchSchedulePage retrieves the HTML of the HFCC public data files page.
func hfccFetchSchedulePage() ([]byte, error) {
	return hfccDownload(hfccDataPageURL)
}

// hfccDownload fetches rawURL and returns the response body.
func hfccDownload(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "frequency-browser/1.0")

	client := &http.Client{Timeout: hfccHTTPTimeout}
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

// hfccParseScheduleLinks extracts every schedule archive reference from the
// HFCC data page HTML, resolving relative links against baseURL.
func hfccParseScheduleLinks(page []byte, baseURL string) []hfccSchedule {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	matches := hfccScheduleLinkRE.FindAllSubmatchIndex(page, -1)
	schedules := make([]hfccSchedule, 0, len(matches))
	for i, match := range matches {
		href := string(page[match[2]:match[3]])

		// The "Last updated on" note follows the link inside the same list item,
		// so only look at the text up to the next archive link.
		textEnd := len(page)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		note := page[match[1]:textEnd]

		ref, err := url.Parse(href)
		if err != nil {
			continue
		}

		name := path.Base(ref.Path)
		schedule := hfccSchedule{
			Name:   name,
			URL:    base.ResolveReference(ref).String(),
			Season: hfccSeasonCode(name),
		}
		if updated, ok := hfccParseLastUpdated(note); ok {
			schedule.Updated = updated
		}
		schedules = append(schedules, schedule)
	}
	return schedules
}

// hfccParseLastUpdated pulls the update date out of the text that follows a
// schedule link, e.g. "Last updated on 14-Nov-2026".
func hfccParseLastUpdated(text []byte) (time.Time, bool) {
	match := hfccLastUpdatedRE.FindSubmatch(text)
	if match == nil {
		return time.Time{}, false
	}

	day := string(match[1])
	month := strings.ToUpper(string(match[2][:1])) + strings.ToLower(string(match[2][1:]))
	year := string(match[3])

	updated, err := time.Parse("2-Jan-2006", day+"-"+month+"-"+year)
	if err != nil {
		return time.Time{}, false
	}
	return updated, true
}

// hfccSeasonCode derives the season code ("B26") from an archive name
// ("b26allx2.zip"). It returns an empty string when the name has no season.
func hfccSeasonCode(name string) string {
	match := hfccSeasonRE.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return strings.ToUpper(match[1] + match[2])
}

// hfccSeasonRank orders season codes chronologically: B25, A26, B26, ...
// The letter is the half of the season (A before B) and the number the year.
func hfccSeasonRank(season string) (year int, order int) {
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

// hfccNewestSchedule returns the most recent schedule in the list. The
// "Last updated" date wins when the page provides one, otherwise the season
// code decides.
func hfccNewestSchedule(schedules []hfccSchedule) (hfccSchedule, bool) {
	var best hfccSchedule
	found := false

	for _, schedule := range schedules {
		if !found {
			best = schedule
			found = true
			continue
		}
		if hfccScheduleNewer(schedule, best) {
			best = schedule
		}
	}
	return best, found
}

// hfccScheduleNewer reports whether schedule a should be preferred over b.
func hfccScheduleNewer(a, b hfccSchedule) bool {
	if !a.Updated.Equal(b.Updated) {
		return a.Updated.After(b.Updated)
	}

	aYear, aOrder := hfccSeasonRank(a.Season)
	bYear, bOrder := hfccSeasonRank(b.Season)
	if aYear != bYear {
		return aYear > bYear
	}
	return aOrder > bOrder
}

// hfccScheduleFromZip returns the name and contents of the schedule data file
// inside archive: the first *.txt entry whose first four characters match the
// first four characters of the archive name (B26allx2.zip -> B26all00.TXT).
func hfccScheduleFromZip(archive []byte, archiveName string) (string, []byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", nil, fmt.Errorf("opening zip archive %s: %w", archiveName, err)
	}

	prefix := strings.ToLower(archiveName)
	if len(prefix) > hfccScheduleNamePrefixLen {
		prefix = prefix[:hfccScheduleNamePrefixLen]
	}

	var entries []string
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name := path.Base(file.Name)
		entries = append(entries, name)

		lowerName := strings.ToLower(name)
		if len(name) < hfccScheduleNamePrefixLen || !strings.HasSuffix(lowerName, ".txt") {
			continue
		}
		if lowerName[:hfccScheduleNamePrefixLen] != prefix {
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return "", nil, fmt.Errorf("opening %s in %s: %w", name, archiveName, err)
		}
		content, readErr := io.ReadAll(fileReader)
		closeErr := fileReader.Close()
		if readErr != nil {
			return "", nil, fmt.Errorf("reading %s in %s: %w", name, archiveName, readErr)
		}
		if closeErr != nil {
			return "", nil, fmt.Errorf("closing %s in %s: %w", name, archiveName, closeErr)
		}

		return name, content, nil
	}

	return "", nil, fmt.Errorf("no *.txt file starting with %q found in %s (entries: %s)",
		strings.ToUpper(prefix), archiveName, strings.Join(entries, ", "))
}

// hfccParseScheduleFile converts an HFCC schedule file (Windows-1252, fixed
// width columns) into rows.
func hfccParseScheduleFile(content []byte) ([]CSVRow, error) {
	// Create a reader that converts from Windows-1252 to UTF-8
	decoder := transform.NewReader(bytes.NewReader(content), charmap.Windows1252.NewDecoder())
	scanner := bufio.NewScanner(decoder)

	// Convert to struct
	var data []CSVRow
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		// Skip comment/header lines and anything too short to hold a record.
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if len(line) < 120 {
			continue
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

		siteCode := line[47:50] // site code (transmitter location)
		siteName := GetHfccSiteName(siteCode)
		siteCountry := GetCountryName(GetHfccSiteADM(siteCode))

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

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading HFCC schedule data: %w", err)
	}
	return data, nil
}

// LoadHfccList loads data from hfcc-list.json if it exists
func (a *App) LoadHfccList() ([]CSVRow, error) {
	jsonPath, err := appDataPath("hfcc-list.json")
	if err != nil {
		return nil, err
	}

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
	jsonPath, err := appDataPath("hfcc-list.json")
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

	// Convert to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return fmt.Errorf("error creating data directory: %w", err)
	}

	// Write to file
	err = os.WriteFile(jsonPath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("error writing JSON file: %w", err)
	}

	return nil
}
