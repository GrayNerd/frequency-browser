package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// eibiReferenceCacheFile is written to the per-user data directory (as resolved
// by go-app-paths) so refreshed EiBi reference tables survive a restart.
const eibiReferenceCacheFile = "eibi-reftables.json"

// eibiSiteMap holds the transmitter site table parsed from EiBi's README.TXT:
// country (ITU code) -> site code -> site name. An empty site code holds the
// country's main transmitter site.
var eibiSiteMap map[string]map[string]string

// eibiReferenceTables holds the reference tables parsed out of EiBi's README.TXT.
type eibiReferenceTables struct {
	Season    string                       `json:"season"` // e.g. "B26", for reference
	Languages map[string]string            `json:"languages"`
	Countries map[string]string            `json:"countries"`
	Targets   map[string]string            `json:"targets"`
	Sites     map[string]map[string]string `json:"sites"` // country -> site abbrev -> name
	Missing   []string                     `json:"-"`
}

// eibiReferenceStats describes an EiBi refresh: how many entries the README
// supplied and how many of those were not known before.
type eibiReferenceStats struct {
	Languages, Countries, Targets, Sites             int
	NewLanguages, NewCountries, NewTargets, NewSites int
}

// String renders the refresh results for the import summary.
func (s eibiReferenceStats) String() string {
	return fmt.Sprintf(
		"reference tables: %d languages (%d new), %d countries (%d new), %d targets (%d new), %d transmitter sites (%d new)",
		s.Languages, s.NewLanguages, s.Countries, s.NewCountries,
		s.Targets, s.NewTargets, s.Sites, s.NewSites)
}

const (
	eibiLangHeading    = "I) Language codes."
	eibiCountryHeading = "II) Country codes."
	eibiTargetHeading  = "III) Target-area codes."
	eibiSiteHeading    = "IV) Transmitter site codes."
	eibiSiteSectionEnd = "US Air Force Messages"
)

var (
	// "   code   Description [iso]" for languages, "   ABC  Country" for
	// countries, "   ABC  - Description" for target areas.
	eibiLanguageRow = regexp.MustCompile(`^   (\S+)\s{2,}(\S.*)$`)
	eibiCountryRow  = regexp.MustCompile(`^   ([A-Z]{1,3})\s{2,}(\S.*)$`)
	eibiTargetRow   = regexp.MustCompile(`^   (\S{1,3})\s+-\s+(\S.*)$`)

	// Transmitter site lines: "   ABC: ..." starts a country, "        ab-Name"
	// adds a site. Latitude/longitude is dropped from the name.
	eibiSiteCountryRow = regexp.MustCompile(`^   (/[A-Z0-9]{1,3}|[A-Z0-9]{1,4}):\s*(.*)$`)
	eibiSiteCodeRow    = regexp.MustCompile(`^ {8}([A-Za-z0-9]{1,3})-(.*)$`)

	// A trailing position such as "34N32-69E20", "43N17", "26S07'40\"-28E12'20\"".
	eibiCoordinates = regexp.MustCompile(`\s+-?\d{1,3}[NS]\d{1,3}.*$`)

	// Language descriptions end with the speaker count "(3m)" and/or the ISO
	// 639-3 reference "[arb]".
	eibiSpeakerCount = regexp.MustCompile(`\s*\([\d.,]+m?\)\s*$`)
	eibiISOCode      = regexp.MustCompile(`\s*\[[a-z]{3}\]\s*$`)
)

// eibiReadmeLines splits README.TXT (Windows-1252, CRLF) into lines.
func eibiReadmeLines(content []byte) []string {
	text := eibiDecodeText(content)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// eibiDecodeText converts README.TXT / sked CSV bytes to UTF-8.
func eibiDecodeText(content []byte) string {
	if utf8.Valid(content) {
		return strings.TrimPrefix(string(content), "\ufeff")
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(content), charmap.Windows1252.NewDecoder()))
	if err != nil {
		return string(content)
	}
	return string(decoded)
}

// eibiSection returns the index of the first line whose trimmed text starts
// with heading, searching from start. It returns -1 when not present.
func eibiSection(lines []string, heading string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), heading) {
			return i
		}
	}
	return -1
}

// eibiParseReadme extracts the four reference tables from README.TXT.
func eibiParseReadme(content []byte) eibiReferenceTables {
	tables := eibiReferenceTables{
		Languages: make(map[string]string),
		Countries: make(map[string]string),
		Targets:   make(map[string]string),
		Sites:     make(map[string]map[string]string),
	}

	lines := eibiReadmeLines(content)

	langStart := eibiSection(lines, eibiLangHeading, 0)
	if langStart < 0 {
		tables.Missing = append(tables.Missing, "language codes")
		return tables
	}

	countryStart := eibiSection(lines, eibiCountryHeading, langStart+1)
	targetStart := eibiSection(lines, eibiTargetHeading, max(countryStart, 0)+1)
	siteStart := eibiSection(lines, eibiSiteHeading, max(targetStart, 0)+1)

	// Languages run to the country section, or to the end of the file.
	langEnd := len(lines)
	if countryStart > 0 {
		langEnd = countryStart
	}
	eibiParseLanguages(lines[langStart+1:langEnd], tables.Languages)

	if countryStart < 0 {
		tables.Missing = append(tables.Missing, "country codes")
	} else {
		countryEnd := len(lines)
		if targetStart > 0 {
			countryEnd = targetStart
		}
		eibiParseCountries(lines[countryStart+1:countryEnd], tables.Countries)
	}

	if targetStart < 0 {
		tables.Missing = append(tables.Missing, "target-area codes")
	} else {
		targetEnd := len(lines)
		if siteStart > 0 {
			targetEnd = siteStart
		}
		eibiParseTargets(lines[targetStart+1:targetEnd], tables.Targets)
	}

	if siteStart < 0 {
		tables.Missing = append(tables.Missing, "transmitter site codes")
		return tables
	}

	// The transmitter site table is last; it ends at the trailing
	// "US Air Force Messages" note.
	siteEnd := len(lines)
	for i := siteStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], eibiSiteSectionEnd) {
			siteEnd = i
			break
		}
	}
	eibiParseSites(lines[siteStart+1:siteEnd], tables.Sites)

	return tables
}

// eibiParseLanguages reads "   ENG   English (400m)   [eng]" rows.
func eibiParseLanguages(lines []string, into map[string]string) {
	for _, line := range lines {
		match := eibiLanguageRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := eibiISOCode.ReplaceAllString(match[2], "")
		name = strings.TrimSpace(eibiSpeakerCount.ReplaceAllString(name, ""))
		if match[1] == "" || name == "" {
			continue
		}
		if _, exists := into[match[1]]; !exists {
			into[match[1]] = name
		}
	}
}

// eibiParseCountries reads "   ALB  Albania" rows.
func eibiParseCountries(lines []string, into map[string]string) {
	for _, line := range lines {
		match := eibiCountryRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		// A trailing "*" marks a non-official abbreviation.
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(match[2]), "*"))
		if name == "" {
			continue
		}
		if _, exists := into[match[1]]; !exists {
			into[match[1]] = name
		}
	}
}

// eibiParseTargets reads "   NAO - North Atlantic Ocean" rows.
func eibiParseTargets(lines []string, into map[string]string) {
	for _, line := range lines {
		match := eibiTargetRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if _, exists := into[match[1]]; !exists {
			into[match[1]] = strings.TrimSpace(match[2])
		}
	}
}

// eibiParseSites reads the transmitter site table:
//
//	AFG: k-Kabul / Pol-e-Charkhi 34N32-69E20
//	     x-Khost 33N14-69E49
//
// A country line without a site code ("AFS: Meyerton 26S35-28E08 except:")
// names the country's main transmitter site, stored under the empty key.
func eibiParseSites(lines []string, into map[string]map[string]string) {
	country := ""
	for _, line := range lines {
		if match := eibiSiteCountryRow.FindStringSubmatch(line); match != nil {
			country = match[1]
			if into[country] == nil {
				into[country] = make(map[string]string)
			}

			rest := strings.TrimSpace(match[2])
			rest = strings.TrimSpace(strings.TrimSuffix(rest, "except:"))
			if rest == "" {
				continue
			}
			if code, name, ok := eibiSplitSiteEntry(rest); ok {
				eibiAddSite(into, country, code, name)
			} else {
				eibiAddSite(into, country, "", rest)
			}
			continue
		}

		if country == "" {
			continue
		}
		if match := eibiSiteCodeRow.FindStringSubmatch(line); match != nil {
			eibiAddSite(into, country, match[1], match[2])
		}
	}
}

// eibiSplitSiteEntry splits "k-Kabul / Pol-e-Charkhi" into code and name.
func eibiSplitSiteEntry(text string) (code, name string, ok bool) {
	dash := strings.Index(text, "-")
	if dash < 1 || dash > 3 {
		return "", "", false
	}
	code = strings.TrimSpace(text[:dash])
	if !isAlphaNumeric(code) {
		return "", "", false
	}
	return code, strings.TrimSpace(text[dash+1:]), true
}

// eibiStripCoordinates removes a trailing "34N32-69E20" position.
func eibiStripCoordinates(text string) string {
	return strings.TrimSpace(eibiCoordinates.ReplaceAllString(text, ""))
}

// eibiAddSite stores one site, dropping the trailing position and keeping the
// first name seen for a code.
func eibiAddSite(into map[string]map[string]string, country, code, name string) {
	name = eibiStripCoordinates(name)
	if name == "" {
		return
	}
	if into[country] == nil {
		into[country] = make(map[string]string)
	}
	if _, exists := into[country][code]; !exists {
		into[country][code] = name
	}
}

// isAlphaNumeric reports whether s is non-empty and made only of letters and
// digits, which is what EiBi uses for site and language codes.
func isAlphaNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// eibiApplyReferenceTables merges freshly parsed EiBi reference tables into the
// lookup maps used by the importers and returns what changed.
//
// README.TXT is published with every season and is the authoritative source for
// EiBi's own codes, so the maps are replaced rather than gap-filled. Regions the
// two lists share (which are the ITU/HFCC ones) keep their existing wording so a
// country is never named differently just because it came from a different list.
func eibiApplyReferenceTables(tables eibiReferenceTables) eibiReferenceStats {
	referenceTablesMu.Lock()
	defer referenceTablesMu.Unlock()

	stats := eibiReferenceStats{
		Languages: len(tables.Languages),
		Countries: len(tables.Countries),
		Targets:   len(tables.Targets),
	}
	for _, sites := range tables.Sites {
		stats.Sites += len(sites)
	}

	if LanguageMap == nil {
		LanguageMap = make(map[string]string)
	}
	for code, name := range tables.Languages {
		if code == "" || name == "" {
			continue
		}
		key := strings.ToUpper(code)
		if _, exists := LanguageMap[key]; !exists {
			stats.NewLanguages++
			LanguageMap[key] = name
		}
	}

	if ITUCountryMap == nil {
		ITUCountryMap = make(map[string]string)
	}
	for code, name := range tables.Countries {
		if code == "" || name == "" {
			continue
		}
		key := strings.ToUpper(code)
		if _, exists := ITUCountryMap[key]; !exists {
			stats.NewCountries++
			ITUCountryMap[key] = name
		}
	}

	if TargetMap == nil {
		TargetMap = make(map[string]string)
	}
	for code, name := range tables.Targets {
		if code == "" || name == "" {
			continue
		}
		if _, exists := TargetMap[code]; !exists {
			stats.NewTargets++
		}
		// Target codes are case sensitive ("Af" vs "AF").
		TargetMap[code] = name
	}

	if eibiSiteMap == nil {
		eibiSiteMap = make(map[string]map[string]string)
	}
	for country, sites := range tables.Sites {
		key := strings.ToUpper(country)
		if key == "" || len(sites) == 0 {
			continue
		}

		// The README lists every transmitter site of a country, so a country
		// present in it replaces the embedded sites.json entry instead of
		// merging: sites.json was generated from an older README and contains
		// artifacts (e.g. "L-Lukang" stored as a country's default site).
		merged := make(map[string]string, len(sites))
		for code, name := range sites {
			if name == "" {
				continue
			}
			if _, exists := eibiSiteMap[key][code]; !exists {
				stats.NewSites++
			}
			merged[code] = name
		}
		if len(merged) > 0 {
			eibiSiteMap[key] = merged
		}
	}

	return stats
}

// eibiReferenceCachePath returns the path of the EiBi reference table cache,
// inside the per-user data directory resolved by go-app-paths.
func eibiReferenceCachePath() (string, error) {
	return appDataPath(eibiReferenceCacheFile)
}

// eibiSaveReferenceTables stores the refreshed tables in the per-user data
// directory so they are reused on the next start.
func eibiSaveReferenceTables(tables eibiReferenceTables) error {
	cachePath, err := eibiReferenceCachePath()
	if err != nil {
		return err
	}
	return eibiSaveReferenceTablesTo(cachePath, tables)
}

// eibiSaveReferenceTablesTo writes the reference tables to cachePath.
func eibiSaveReferenceTablesTo(cachePath string, tables eibiReferenceTables) error {
	data, err := json.MarshalIndent(tables, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling reference tables: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("error creating reference cache directory: %w", err)
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("error writing %s: %w", cachePath, err)
	}

	fmt.Printf("Saved EiBi reference tables to %s\n", cachePath)
	return nil
}

// eibiLoadReferenceCache merges the cached reference tables (if any) at
// startup. It never fails the application.
func eibiLoadReferenceCache() {
	cachePath, err := eibiReferenceCachePath()
	if err != nil {
		return
	}
	eibiLoadReferenceCacheFrom(cachePath)
}

// eibiLoadReferenceCacheFrom merges the reference tables cached in cachePath.
func eibiLoadReferenceCacheFrom(cachePath string) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}

	var tables eibiReferenceTables
	if err := json.Unmarshal(data, &tables); err != nil {
		fmt.Printf("Warning: could not parse %s: %v\n", cachePath, err)
		return
	}

	stats := eibiApplyReferenceTables(tables)
	fmt.Printf("Loaded cached EiBi %s %s\n", tables.Season, stats)
}
