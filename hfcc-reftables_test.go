package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sample rows copied from a real HFCC archive's reference tables.
const (
	hfccTestSiteTable = ";         11-Nov-2026 SITE.TXT REFERENCE TABLE\r\n" +
		";\r\n" +
		";--+------------------------------+---+-----+------\r\n" +
		";Co Site Name                      ADM Lati  Longi\r\n" +
		";de                                    tude  tude\r\n" +
		";--+------------------------------+---+-----+------\r\n" +
		"A-A Alma Ata                       KAZ 43N17 077E00\r\n" +
		"ANK Ankum                          HOL 52N32 06E15\r\n" +
		"ISH Ishoej                         DNK 55N36 012E21\r\n"

	hfccTestBroadcasterTable = ";         11-Nov-2026 BROADCAS.TXT REFERENCE TABLE\r\n" +
		";-- --------------------------------------------------\r\n" +
		";CO BROADCASTER\r\n" +
		";DE\r\n" +
		";-- --------------------------------------------------\r\n" +
		"ABC ABC-Radio Australia\r\n" +
		"DIS disco.miami\r\n" +
		"HVR H. Visser\r\n"

	hfccTestAdminTable = ";         19-MAR-2012 ADMIN.TXT REFERENCE TABLE\r\n" +
		";\r\n" +
		";CO ADMINISTRATION ENGLISH NAME                        ADMINISTRATION FRENCH NAME                         ADMINISTRATION SPANISH NAME\r\n" +
		";DE\r\n" +
		"OMA Oman                                               Oman                                               Oman\r\n" +
		"USA United States                                      Etats-Unis                                         Estados Unidos\r\n"

	// language.txt is UTF-8 with a BOM and Windows line endings.
	hfccTestLanguageTable = "\ufeff;         23-JAN-2014  Reference Table Language\r\n" +
		";\r\n" +
		"Bis Bislama\r\n" +
		"Eng English\r\n"
)

func TestHfccParseSites(t *testing.T) {
	sites := hfccParseSites(hfccTableRows([]byte(hfccTestSiteTable)))
	if len(sites) != 3 {
		t.Fatalf("parsed %d sites, want 3: %+v", len(sites), sites)
	}

	want := HfccSite{Code: "ANK", SiteName: "Ankum", ADM: "HOL"}
	if sites[1] != want {
		t.Errorf("site[1] = %+v, want %+v", sites[1], want)
	}
	if sites[0].SiteName != "Alma Ata" || sites[0].ADM != "KAZ" {
		t.Errorf("site[0] = %+v", sites[0])
	}
	if sites[2].ADM != "DNK" {
		t.Errorf("site[2] ADM = %q, want DNK", sites[2].ADM)
	}
}

func TestHfccParseBroadcasters(t *testing.T) {
	broadcasters := hfccParseBroadcasters(hfccTableRows([]byte(hfccTestBroadcasterTable)))
	if len(broadcasters) != 3 {
		t.Fatalf("parsed %d broadcasters, want 3: %+v", len(broadcasters), broadcasters)
	}
	if broadcasters[1].Code != "DIS" || broadcasters[1].Name != "disco.miami" {
		t.Errorf("broadcaster[1] = %+v", broadcasters[1])
	}
	if broadcasters[2].Name != "H. Visser" {
		t.Errorf("broadcaster[2] = %+v", broadcasters[2])
	}
}

func TestHfccParseCountries(t *testing.T) {
	countries := hfccParseCountries(hfccTableRows([]byte(hfccTestAdminTable)))
	if len(countries) != 2 {
		t.Fatalf("parsed %d countries, want 2: %+v", len(countries), countries)
	}
	if countries["USA"] != "United States" {
		t.Errorf("USA = %q, want United States", countries["USA"])
	}
	// Only the English name is kept, not the French/Spanish columns.
	if strings.Contains(countries["USA"], "Etats-Unis") {
		t.Errorf("USA = %q, should not include the French name", countries["USA"])
	}
}

func TestHfccParseLanguagesHandlesBOMAndUTF8(t *testing.T) {
	languages := hfccParseLanguages(hfccTableRows([]byte(hfccTestLanguageTable)))
	if len(languages) != 2 {
		t.Fatalf("parsed %d languages, want 2: %+v", len(languages), languages)
	}
	if languages["BIS"] != "Bislama" {
		t.Errorf("BIS = %q, want Bislama", languages["BIS"])
	}
	if languages["ENG"] != "English" {
		t.Errorf("ENG = %q, want English", languages["ENG"])
	}
}

func TestHfccReferenceTablesFromZip(t *testing.T) {
	archive := hfccBuildZip(t, map[string]string{
		"B26all00.TXT": hfccTestSchedule(),
		"site.txt":     hfccTestSiteTable,
		"broadcas.txt": hfccTestBroadcasterTable,
		"admin.txt":    hfccTestAdminTable,
		"language.txt": hfccTestLanguageTable,
		"antenna.txt":  "unused",
	})

	tables, err := hfccReferenceTablesFromZip(archive, "b26allx2.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tables.Missing) != 0 {
		t.Errorf("Missing = %v, want none", tables.Missing)
	}
	if len(tables.Sites) != 3 || len(tables.Broadcasters) != 3 {
		t.Errorf("sites=%d broadcasters=%d, want 3 and 3", len(tables.Sites), len(tables.Broadcasters))
	}
	if tables.Countries["USA"] != "United States" {
		t.Errorf("countries[USA] = %q", tables.Countries["USA"])
	}
	if tables.Languages["ENG"] != "English" {
		t.Errorf("languages[ENG] = %q", tables.Languages["ENG"])
	}
}

func TestHfccReferenceTablesFromZipReportsMissingTables(t *testing.T) {
	archive := hfccBuildZip(t, map[string]string{
		"B26all00.TXT": hfccTestSchedule(),
		"site.txt":     hfccTestSiteTable,
	})

	tables, err := hfccReferenceTablesFromZip(archive, "b26allx2.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"broadcas.txt", "admin.txt", "language.txt"}
	if strings.Join(tables.Missing, ",") != strings.Join(want, ",") {
		t.Errorf("Missing = %v, want %v", tables.Missing, want)
	}
	if len(tables.Sites) != 3 {
		t.Errorf("parsed %d sites, want 3", len(tables.Sites))
	}
}

// TestHfccApplyReferenceTablesAddsUnknownCodes checks that codes the embedded
// tables do not know resolve once the archive tables are applied, and that
// locally-known countries survive the merge. It uses synthetic codes so it does
// not depend on whether a live import already refreshed the tables.
func TestHfccApplyReferenceTablesAddsUnknownCodes(t *testing.T) {
	const siteCode, broadcasterCode, countryCode, languageCode = "ZQ1", "ZQ2", "ZQ3", "ZQ4"

	if _, ok := GetHfccSite(siteCode); ok {
		t.Fatalf("test code %s is already known", siteCode)
	}
	if _, ok := ITUCountryMap[countryCode]; ok {
		t.Fatalf("test code %s is already known", countryCode)
	}

	// An embedded-only entry that must not be dropped by the merge.
	const localOnlyCountry = "AL"
	localName := GetCountryName(localOnlyCountry)

	tables := hfccReferenceTables{
		Sites:        []HfccSite{{Code: siteCode, SiteName: "Putten", ADM: "HOL"}},
		Broadcasters: []HfccBroadcaster{{Code: broadcasterCode, Name: "H. Visser"}},
		Countries:    map[string]string{countryCode: "Oman"},
		Languages:    map[string]string{languageCode: "Bislama"},
	}

	stats := hfccApplyReferenceTables(tables)
	if stats.NewSites != 1 || stats.NewBroadcasters != 1 || stats.NewCountries != 1 || stats.NewLanguages != 1 {
		t.Errorf("stats = %+v, want one new entry per table", stats)
	}

	if got := GetHfccSiteName(siteCode); got != "Putten" {
		t.Errorf("site name = %q, want Putten", got)
	}
	if got := GetHfccSiteADM(siteCode); got != "HOL" {
		t.Errorf("site ADM = %q, want HOL", got)
	}
	if got := GetHfccStationName(broadcasterCode); got != "H. Visser" {
		t.Errorf("broadcaster = %q, want H. Visser", got)
	}
	if got := GetCountryName(countryCode); got != "Oman" {
		t.Errorf("country = %q, want Oman", got)
	}
	if got := getLanguageName(languageCode); got != "Bislama" {
		t.Errorf("language = %q, want Bislama", got)
	}
	if got := GetCountryName(localOnlyCountry); got != localName {
		t.Errorf("locally known country changed: %q -> %q", localName, got)
	}

	// Applying the same tables again reports no new entries.
	if again := hfccApplyReferenceTables(tables); again.NewSites != 0 || again.NewCountries != 0 {
		t.Errorf("second apply reported new entries: %+v", again)
	}
}

// TestHfccApplyReferenceTablesMergePolicy pins down which tables are
// authoritative. site.txt/broadcas.txt are refreshed every season, so they
// overwrite; admin.txt/language.txt are older than the curated JSON, so they only
// fill gaps.
func TestHfccApplyReferenceTablesMergePolicy(t *testing.T) {
	// Pick codes the embedded tables already know.
	siteCode := "BCQ"
	oldSite := GetHfccSiteName(siteCode)
	if oldSite == "Unknown" || oldSite == "" {
		t.Skipf("embedded tables do not know site %s", siteCode)
	}
	broadcasterCode := "VBT"
	oldBroadcaster := GetHfccStationName(broadcasterCode)
	if strings.Contains(oldBroadcaster, "Unknown") {
		t.Skipf("embedded tables do not know broadcaster %s", broadcasterCode)
	}
	countryCode := "USA"
	oldCountry := GetCountryName(countryCode)
	languageCode := "ENG"
	oldLanguage := getLanguageName(languageCode)

	tables := hfccReferenceTables{
		Sites:        []HfccSite{{Code: siteCode, SiteName: "Refreshed Site", ADM: "USA"}},
		Broadcasters: []HfccBroadcaster{{Code: broadcasterCode, Name: "Refreshed Broadcaster"}},
		Countries:    map[string]string{countryCode: "Stale Country Name"},
		Languages:    map[string]string{languageCode: "Stale Language Name"},
	}
	hfccApplyReferenceTables(tables)

	// Fresh tables win.
	if got := GetHfccSiteName(siteCode); got != "Refreshed Site" {
		t.Errorf("site name = %q, want the refreshed value", got)
	}
	if got := GetHfccStationName(broadcasterCode); got != "Refreshed Broadcaster" {
		t.Errorf("broadcaster = %q, want the refreshed value", got)
	}

	// Stale tables must not overwrite curated names.
	if got := GetCountryName(countryCode); got != oldCountry {
		t.Errorf("country = %q, want the curated %q to be kept", got, oldCountry)
	}
	if got := getLanguageName(languageCode); got != oldLanguage {
		t.Errorf("language = %q, want the curated %q to be kept", got, oldLanguage)
	}
}

// TestHfccReferenceStatsString keeps the import log line readable.
func TestHfccReferenceStatsString(t *testing.T) {
	stats := hfccReferenceStats{
		Sites: 702, NewSites: 7,
		Broadcasters: 310, NewBroadcasters: 6,
		Countries: 190, NewCountries: 1,
		Languages: 7699, NewLanguages: 0,
	}
	got := stats.String()
	for _, want := range []string{
		"702 sites (7 new)",
		"310 broadcasters (6 new)",
		"190 countries (1 new)",
		"7699 languages (0 new)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stats string %q does not contain %q", got, want)
		}
	}
}

// TestHfccReferenceTablesCacheRoundTrip checks that a refresh survives a
// restart: what is written by hfccSaveReferenceTablesTo is read back and merged
// by hfccLoadReferenceCacheFrom.
func TestHfccReferenceTablesCacheRoundTrip(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), hfccReferenceCacheFile)

	tables := hfccReferenceTables{
		Season:       "B26",
		Sites:        []HfccSite{{Code: "ZZZ", SiteName: "Cache Test Site", ADM: "HOL"}},
		Broadcasters: []HfccBroadcaster{{Code: "ZZB", Name: "Cache Test Broadcaster"}},
		Countries:    map[string]string{"ZZC": "Cache Test Country"},
		Languages:    map[string]string{"ZZL": "Cache Test Language"},
	}

	if err := hfccSaveReferenceTablesTo(cachePath, tables); err != nil {
		t.Fatalf("saving reference tables: %v", err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	hfccLoadReferenceCacheFrom(cachePath)

	if got := GetHfccSiteName("ZZZ"); got != "Cache Test Site" {
		t.Errorf("site name = %q, want Cache Test Site", got)
	}
	if got := GetHfccSiteADM("ZZZ"); got != "HOL" {
		t.Errorf("site ADM = %q, want HOL", got)
	}
	if got := GetHfccStationName("ZZB"); got != "Cache Test Broadcaster" {
		t.Errorf("broadcaster = %q", got)
	}
	if got := GetCountryName("ZZC"); got != "Cache Test Country" {
		t.Errorf("country = %q", got)
	}
	if got := getLanguageName("ZZL"); got != "Cache Test Language" {
		t.Errorf("language = %q", got)
	}
}

// TestHfccLoadReferenceCacheMissingFile ensures a first run without a cache is
// harmless.
func TestHfccLoadReferenceCacheMissingFile(t *testing.T) {
	hfccLoadReferenceCacheFrom(filepath.Join(t.TempDir(), "does-not-exist.json"))
}

// TestHfccReferenceCachePath uses the executable directory.
func TestHfccReferenceCachePath(t *testing.T) {
	cachePath, err := hfccReferenceCachePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(cachePath) != hfccReferenceCacheFile {
		t.Errorf("cache file = %q, want %q", filepath.Base(cachePath), hfccReferenceCacheFile)
	}
}
