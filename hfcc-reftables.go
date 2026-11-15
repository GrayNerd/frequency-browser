package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// Reference table file names inside an HFCC operational data archive.
const (
	hfccSitesTableName        = "site.txt"
	hfccBroadcastersTableName = "broadcas.txt"
	hfccAdminTableName        = "admin.txt"
	hfccLanguageTableName     = "language.txt"
)

// hfccReferenceCacheFile is written beside the executable so refreshed
// reference tables survive a restart.
const hfccReferenceCacheFile = "hfcc-reftables.json"

// referenceTablesMu guards the reference lookup maps so an import can refresh
// them while the UI is reading them.
var referenceTablesMu sync.RWMutex

// HfccBroadcaster is one entry of the HFCC broadcaster reference table.
type HfccBroadcaster struct {
	Code string `json:"code"`
	Name string `json:"broadcaster_name"`
}

// hfccReferenceTables holds the reference tables carried by an HFCC archive.
// Only the tables present in the archive are filled in; Missing lists the ones
// that were absent.
type hfccReferenceTables struct {
	Season       string            `json:"season"` // e.g. "B26", for reference
	Sites        []HfccSite        `json:"sites"`
	Broadcasters []HfccBroadcaster `json:"broadcasters"`
	Countries    map[string]string `json:"countries"`
	Languages    map[string]string `json:"languages"`
	Missing      []string          `json:"-"`
}

// hfccReferenceStats describes a refresh: how many entries the archive supplied
// and how many of those were not known before.
type hfccReferenceStats struct {
	Sites, Broadcasters, Countries, Languages             int
	NewSites, NewBroadcasters, NewCountries, NewLanguages int
}

// String renders the refresh results for the import summary.
func (s hfccReferenceStats) String() string {
	return fmt.Sprintf(
		"reference tables: %d sites (%d new), %d broadcasters (%d new), %d countries (%d new), %d languages (%d new)",
		s.Sites, s.NewSites, s.Broadcasters, s.NewBroadcasters,
		s.Countries, s.NewCountries, s.Languages, s.NewLanguages)
}

// hfccDecodeTable converts reference table bytes to UTF-8 text. HFCC publishes
// these tables as Windows-1252, except language.txt which is UTF-8 with a byte
// order mark; valid UTF-8 is therefore detected rather than assumed.
func hfccDecodeTable(content []byte) string {
	if utf8.Valid(content) {
		return strings.TrimPrefix(string(content), "\ufeff")
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(content), charmap.Windows1252.NewDecoder()))
	if err != nil {
		return string(content)
	}
	return string(decoded)
}

// hfccTableRows splits a reference table into data rows, dropping comments
// (lines starting with ';'), the byte order mark and blank lines.
func hfccTableRows(content []byte) []string {
	text := hfccDecodeTable(content)
	rows := make([]string, 0, 1024)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, ";") {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// hfccField returns s[start:end] trimmed of surrounding spaces, tolerating rows
// that are shorter than the column layout.
func hfccField(s string, start, end int) string {
	if start < 0 || start >= len(s) {
		return ""
	}
	if end > len(s) {
		end = len(s)
	}
	return strings.TrimSpace(s[start:end])
}

// hfccParseSites parses site.txt. Columns: code (0-2), site name (4-33),
// ADM (34-37), then latitude/longitude which are not needed here.
func hfccParseSites(rows []string) []HfccSite {
	sites := make([]HfccSite, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		code := hfccField(row, 0, 3)
		name := hfccField(row, 4, 34)
		adm := hfccField(row, 34, 38)
		if code == "" || name == "" {
			continue
		}
		key := strings.ToUpper(code)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		sites = append(sites, HfccSite{Code: code, SiteName: name, ADM: adm})
	}
	return sites
}

// hfccParseBroadcasters parses broadcas.txt. Columns: code (0-2), broadcaster
// name (rest of the line).
func hfccParseBroadcasters(rows []string) []HfccBroadcaster {
	broadcasters := make([]HfccBroadcaster, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		code := hfccField(row, 0, 3)
		name := hfccField(row, 3, len(row))
		if code == "" || name == "" {
			continue
		}
		key := strings.ToUpper(code)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		broadcasters = append(broadcasters, HfccBroadcaster{Code: code, Name: name})
	}
	return broadcasters
}

// hfccParseCountries parses admin.txt, keeping the English administration name
// (columns 4-54; the French and Spanish names follow).
func hfccParseCountries(rows []string) map[string]string {
	countries := make(map[string]string, len(rows))
	for _, row := range rows {
		code := hfccField(row, 0, 3)
		name := hfccField(row, 4, 55)
		if code == "" || name == "" {
			continue
		}
		countries[strings.ToUpper(code)] = name
	}
	return countries
}

// hfccParseLanguages parses language.txt. Columns: code (0-2), language name
// (rest of the line).
func hfccParseLanguages(rows []string) map[string]string {
	languages := make(map[string]string, len(rows))
	for _, row := range rows {
		code := hfccField(row, 0, 3)
		name := hfccField(row, 3, len(row))
		if code == "" || name == "" {
			continue
		}
		languages[strings.ToUpper(code)] = name
	}
	return languages
}

// hfccReferenceTablesFromZip extracts site.txt, broadcas.txt, admin.txt and
// language.txt from an operational data archive. Tables that are not present are
// reported in Missing instead of failing the import.
func hfccReferenceTablesFromZip(archive []byte, archiveName string) (hfccReferenceTables, error) {
	var tables hfccReferenceTables

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return tables, fmt.Errorf("opening zip archive %s: %w", archiveName, err)
	}

	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		entries[strings.ToLower(path.Base(file.Name))] = file
	}

	read := func(name string) ([]byte, bool) {
		file, ok := entries[name]
		if !ok {
			return nil, false
		}
		fileReader, err := file.Open()
		if err != nil {
			return nil, false
		}
		defer fileReader.Close()

		data, err := io.ReadAll(fileReader)
		if err != nil {
			return nil, false
		}
		return data, true
	}

	if data, ok := read(hfccSitesTableName); ok {
		tables.Sites = hfccParseSites(hfccTableRows(data))
	} else {
		tables.Missing = append(tables.Missing, hfccSitesTableName)
	}

	if data, ok := read(hfccBroadcastersTableName); ok {
		tables.Broadcasters = hfccParseBroadcasters(hfccTableRows(data))
	} else {
		tables.Missing = append(tables.Missing, hfccBroadcastersTableName)
	}

	if data, ok := read(hfccAdminTableName); ok {
		tables.Countries = hfccParseCountries(hfccTableRows(data))
	} else {
		tables.Missing = append(tables.Missing, hfccAdminTableName)
	}

	if data, ok := read(hfccLanguageTableName); ok {
		tables.Languages = hfccParseLanguages(hfccTableRows(data))
	} else {
		tables.Missing = append(tables.Missing, hfccLanguageTableName)
	}

	return tables, nil
}

// hfccApplyReferenceTables merges freshly downloaded reference tables into the
// lookup maps used by the importers and returns what changed.
//
// The merge policy differs per table because the archive files are not equally
// fresh:
//   - site.txt and broadcas.txt are refreshed with every operational season, so
//     they are authoritative: new codes are added and existing entries take the
//     archive's name (this also repairs mis-encoded names from earlier imports).
//   - admin.txt (last revised 2012) and language.txt (last revised 2014) are
//     older than the curated embedded countries.json / language.json, which also
//     carry extra codes used by the EiBi list. Only codes missing locally are
//     added, so curated names are never regressed.
func hfccApplyReferenceTables(tables hfccReferenceTables) hfccReferenceStats {
	referenceTablesMu.Lock()
	defer referenceTablesMu.Unlock()

	stats := hfccReferenceStats{
		Sites:        len(tables.Sites),
		Broadcasters: len(tables.Broadcasters),
		Countries:    len(tables.Countries),
		Languages:    len(tables.Languages),
	}

	if hfccSiteMap == nil {
		hfccSiteMap = make(map[string]HfccSite)
	}
	for _, site := range tables.Sites {
		key := strings.ToUpper(strings.TrimSpace(site.Code))
		if key == "" {
			continue
		}
		if _, exists := hfccSiteMap[key]; !exists {
			stats.NewSites++
		}
		hfccSiteMap[key] = site
	}

	if HfccStationMap == nil {
		HfccStationMap = make(map[string]string)
	}
	for _, broadcaster := range tables.Broadcasters {
		key := strings.ToUpper(strings.TrimSpace(broadcaster.Code))
		if key == "" {
			continue
		}
		if _, exists := HfccStationMap[key]; !exists {
			stats.NewBroadcasters++
		}
		HfccStationMap[key] = broadcaster.Name
	}

	if ITUCountryMap == nil {
		ITUCountryMap = make(map[string]string)
	}
	for code, name := range tables.Countries {
		if code == "" || name == "" {
			continue
		}
		if _, exists := ITUCountryMap[code]; exists {
			continue // keep the curated local name
		}
		ITUCountryMap[code] = name
		stats.NewCountries++
	}

	if LanguageMap == nil {
		LanguageMap = make(map[string]string)
	}
	for code, name := range tables.Languages {
		if code == "" || name == "" {
			continue
		}
		if _, exists := LanguageMap[code]; exists {
			continue // keep the curated local name
		}
		LanguageMap[code] = name
		stats.NewLanguages++
	}

	return stats
}

// hfccReferenceCachePath returns the path of the reference table cache: a file
// next to the executable.
func hfccReferenceCachePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("error getting executable path: %w", err)
	}
	return filepath.Join(filepath.Dir(exePath), hfccReferenceCacheFile), nil
}

// hfccSaveReferenceTables stores the refreshed tables next to the executable so
// they are reused on the next start (see hfccLoadReferenceCache).
func hfccSaveReferenceTables(tables hfccReferenceTables) error {
	cachePath, err := hfccReferenceCachePath()
	if err != nil {
		return err
	}
	return hfccSaveReferenceTablesTo(cachePath, tables)
}

// hfccSaveReferenceTablesTo writes the reference tables to cachePath.
func hfccSaveReferenceTablesTo(cachePath string, tables hfccReferenceTables) error {
	data, err := json.MarshalIndent(tables, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling reference tables: %w", err)
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("error writing %s: %w", cachePath, err)
	}

	fmt.Printf("Saved HFCC reference tables to %s\n", cachePath)
	return nil
}

// hfccLoadReferenceCache merges the cached reference tables (if any) into the
// embedded lookup maps. It runs at startup and never fails the application.
func hfccLoadReferenceCache() {
	cachePath, err := hfccReferenceCachePath()
	if err != nil {
		return
	}
	hfccLoadReferenceCacheFrom(cachePath)
}

// hfccLoadReferenceCacheFrom merges the reference tables cached in cachePath.
func hfccLoadReferenceCacheFrom(cachePath string) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}

	var tables hfccReferenceTables
	if err := json.Unmarshal(data, &tables); err != nil {
		fmt.Printf("Warning: could not parse %s: %v\n", cachePath, err)
		return
	}

	stats := hfccApplyReferenceTables(tables)
	fmt.Printf("Loaded cached HFCC %s %s\n", tables.Season, stats)
}
