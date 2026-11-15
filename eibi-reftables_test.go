package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEiBiParseReadmeSections(t *testing.T) {
	tables := eibiParseReadme([]byte(eibiTestReadme))
	if len(tables.Missing) != 0 {
		t.Fatalf("Missing = %v, want none", tables.Missing)
	}

	if len(tables.Languages) != 7 {
		t.Errorf("parsed %d languages, want 7: %v", len(tables.Languages), tables.Languages)
	}
	if len(tables.Countries) != 7 {
		t.Errorf("parsed %d countries, want 7: %v", len(tables.Countries), tables.Countries)
	}
	if len(tables.Targets) != 8 {
		t.Errorf("parsed %d targets, want 8: %v", len(tables.Targets), tables.Targets)
	}
	if len(tables.Sites) != 6 {
		t.Errorf("parsed %d site countries, want 6: %v", len(tables.Sites), tables.Sites)
	}
}

func TestEiBiParseReadmeLanguages(t *testing.T) {
	languages := eibiParseReadme([]byte(eibiTestReadme)).Languages

	// Speaker counts and ISO references are stripped from the description.
	if got := languages["A"]; got != "Arabic" {
		t.Errorf("A = %q, want Arabic", got)
	}
	if got := languages["AB"]; got != "Abkhaz: Georgia-Abkhazia" {
		t.Errorf("AB = %q", got)
	}
	if got := languages["ENG"]; got != "English" {
		t.Errorf("ENG = %q, want English", got)
	}
	if got := languages["BIS"]; got != "Bislama" {
		t.Errorf("BIS = %q, want Bislama", got)
	}
	if got := languages["-CW"]; got != "Morse Station" {
		t.Errorf("-CW = %q, want Morse Station", got)
	}
	// The trailing "Zomi-Chin: see Chin-Zomi" note is not a code.
	if _, ok := languages["Zomi-Chin:"]; ok {
		t.Error("parsed a continuation note as a language code")
	}
}

func TestEiBiParseReadmeCountries(t *testing.T) {
	countries := eibiParseReadme([]byte(eibiTestReadme)).Countries

	if got := countries["USA"]; got != "United States of America" {
		t.Errorf("USA = %q", got)
	}
	if got := countries["HOL"]; got != "The Netherlands" {
		t.Errorf("HOL = %q", got)
	}
	// A trailing "*" marks a non-official abbreviation and is dropped.
	if got := countries["TWN"]; got != "Taiwan" {
		t.Errorf("TWN = %q, want Taiwan (without the asterisk)", got)
	}
	// Explanatory prose inside the section must not become a country.
	if _, ok := countries["Asterisks"]; ok {
		t.Error("parsed prose as a country code")
	}
}

func TestEiBiParseReadmeTargets(t *testing.T) {
	targets := eibiParseReadme([]byte(eibiTestReadme)).Targets

	if got := targets["NAO"]; got != "North Atlantic Ocean" {
		t.Errorf("NAO = %q", got)
	}
	if got := targets["N.."]; got != "North .." {
		t.Errorf("N.. = %q, want the documented prefix entry", got)
	}
	if got := targets["Eu"]; !strings.HasPrefix(got, "Europe") {
		t.Errorf("Eu = %q", got)
	}
}

func TestEiBiParseReadmeSites(t *testing.T) {
	sites := eibiParseReadme([]byte(eibiTestReadme)).Sites

	// Country line carrying a site code.
	if got := sites["AFG"]["k"]; got != "Kabul / Pol-e-Charkhi" {
		t.Errorf("AFG/k = %q, want the name without coordinates", got)
	}
	if got := sites["AFG"]["x"]; got != "Khost" {
		t.Errorf("AFG/x = %q", got)
	}
	// Country line without a code is the main site, stored under "".
	if got := sites["AFS"][""]; got != "Meyerton" {
		t.Errorf("AFS default = %q, want Meyerton", got)
	}
	if got := sites["AFS"]["j"]; got != "Johannesburg" {
		t.Errorf("AFS/j = %q, want coordinates stripped", got)
	}
	if got := sites["ALB"]["f"]; got != "Fllake (Durres, 500kW)" {
		t.Errorf("ALB/f = %q", got)
	}
	// Codes may appear inside the name ("f-Fangliao FAN").
	if got := sites["TWN"]["f"]; got != "Fangliao FAN" {
		t.Errorf("TWN/f = %q", got)
	}
	if got := sites["TWN"]["L"]; got != "Lukang" {
		t.Errorf("TWN/L = %q, want Lukang", got)
	}
	if got := sites["USA"]["a"]; got != "Andrews AFB, MD" {
		t.Errorf("USA/a = %q", got)
	}
	// Nothing after the section end marker is parsed.
	if _, ok := sites["US Air Force Messages"]; ok {
		t.Error("parsed the trailing note as a country")
	}
}

func TestEiBiParseReadmeMissingSections(t *testing.T) {
	tables := eibiParseReadme([]byte("nothing useful here\n"))
	if len(tables.Missing) == 0 {
		t.Error("expected missing sections to be reported")
	}
	if len(tables.Languages) != 0 || len(tables.Sites) != 0 {
		t.Error("expected no entries for an unparseable README")
	}
}

func TestEiBiParseSeasonLinks(t *testing.T) {
	page := []byte(`<a href="dx/sked-b26.csv">CSV database</a>
		<a href="dx/README.TXT">README</a>
		<a href="dx/freq-b26.txt">freq</a>
		<a href="archive/sked-a25.csv">old</a>`)

	seasons := eibiParseSeasonLinks(page, "http://www.eibispace.de/")
	if len(seasons) != 2 {
		t.Fatalf("found %d seasons, want 2: %+v", len(seasons), seasons)
	}
	// Sorted by season, so the last entry is the current one.
	if seasons[0].Season != "A25" || seasons[1].Season != "B26" {
		t.Errorf("seasons = %q, %q", seasons[0].Season, seasons[1].Season)
	}
	if seasons[1].URL != "http://www.eibispace.de/dx/sked-b26.csv" {
		t.Errorf("URL = %q", seasons[1].URL)
	}
	if seasons[1].Name != "sked-b26.csv" {
		t.Errorf("Name = %q", seasons[1].Name)
	}
}

func TestEiBiSeasonCode(t *testing.T) {
	cases := map[string]string{
		"sked-b26.csv": "B26",
		"sked-a25.csv": "A25",
		"sked-b26.CSV": "B26",
		"freq-b26.txt": "",
		"README.TXT":   "",
	}
	for name, want := range cases {
		if got := eibiSeasonCode(name); got != want {
			t.Errorf("eibiSeasonCode(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestEiBiParseSchedule(t *testing.T) {
	// The reference tables must be applied first so codes resolve.
	eibiApplyReferenceTables(eibiParseReadme([]byte(eibiTestReadme)))

	rows, err := eibiParseSchedule([]byte(eibiTestCsv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("parsed %d rows, want 4: %+v", len(rows), rows)
	}

	first := rows[0]
	if first.Khz != "3265" || first.StartTime != "0000" || first.EndTime != "2400" {
		t.Errorf("first row = %s %s-%s", first.Khz, first.StartTime, first.EndTime)
	}
	if first.Source != "EiBi" {
		t.Errorf("Source = %q, want EiBi", first.Source)
	}
	if first.Country != "United States" {
		t.Errorf("Country = %q", first.Country)
	}
	// NAm is a composed target code (North + America).
	if first.Target != "North America" {
		t.Errorf("Target = %q, want North America", first.Target)
	}
	if first.Transmitter != "Andrews AFB, MD" {
		t.Errorf("Transmitter = %q", first.Transmitter)
	}

	// /AFG-k is a relay site in another country.
	relay := rows[2]
	if !strings.Contains(relay.Transmitter, "Kabul / Pol-e-Charkhi") {
		t.Errorf("relay Transmitter = %q, want the host-country site", relay.Transmitter)
	}
	if relay.Days != "1....67" {
		t.Errorf("Days = %q, want the converted Fr-Su mask", relay.Days)
	}

	// A row with no site code and several documented sites stays blank rather
	// than inventing one.
	if rows[3].Transmitter != "" {
		t.Errorf("Transmitter = %q, want empty for an unknown site", rows[3].Transmitter)
	}
}

func TestEiBiApplyReferenceTables(t *testing.T) {
	tables := eibiParseReadme([]byte(eibiTestReadme))

	stats := eibiApplyReferenceTables(tables)
	if stats.Sites != 13 {
		t.Errorf("stats.Sites = %d, want 13", stats.Sites)
	}

	// Languages known only to EiBi now resolve.
	if got := getLanguageName("BIS"); got != "Bislama" {
		t.Errorf("BIS = %q", got)
	}
	if got := GetCountryName("OMA"); got != "Oman" {
		t.Errorf("OMA = %q", got)
	}
	if got := GetTargetDescription("N.."); got != "North .." {
		t.Errorf("N.. = %q", got)
	}
	if got := GetEiBiSiteDescription("AFG", "k"); got != "Kabul / Pol-e-Charkhi" {
		t.Errorf("AFG/k = %q", got)
	}

	// Applying twice reports no new entries the second time.
	if again := eibiApplyReferenceTables(tables); again.NewSites != 0 || again.NewLanguages != 0 {
		t.Errorf("second apply reported new entries: %+v", again)
	}
}

func TestEiBiSiteDescriptionForms(t *testing.T) {
	eibiApplyReferenceTables(eibiParseReadme([]byte(eibiTestReadme)))

	cases := []struct{ country, code, want string }{
		{"AFG", "k", "Kabul / Pol-e-Charkhi"},
		{"AFS", "", "Meyerton"}, // country's main site
		{"AFG", "/AFG-k", "Afghanistan - Kabul / Pol-e-Charkhi"},
		{"OMA", "/AFG", "Afghanistan"}, // relay, country only
		{"AFG", "unknown", ""},         // unknown code
	}
	for _, c := range cases {
		if got := GetEiBiSiteDescription(c.country, c.code); got != c.want {
			t.Errorf("GetEiBiSiteDescription(%q, %q) = %q, want %q", c.country, c.code, got, c.want)
		}
	}
}

func TestResolveComposedTarget(t *testing.T) {
	cases := map[string]string{
		"NAm": "North America",
		"SAm": "South America",
		"WEu": "West Europe",
		"NEu": "North Europe",
		"SEu": "South Europe",
		"CEu": "Central Europe",
		"EAf": "East Africa",
		"WAf": "West Africa",
		"CAf": "Central Africa",
		"NAf": "North Africa",
		"WOc": "West Oceania",
	}
	for code, want := range cases {
		if got := GetTargetDescription(code); got != want {
			t.Errorf("GetTargetDescription(%q) = %q, want %q", code, got, want)
		}
	}

	// Documented exact codes still win, and unknown codes stay unresolved.
	if got := GetTargetDescription("NAO"); got != "North Atlantic Ocean" {
		t.Errorf("NAO = %q", got)
	}
	if got := GetTargetDescription("ZZZ"); got != "" {
		t.Errorf("ZZZ = %q, want empty", got)
	}
}

func TestEiBiReferenceTablesCacheRoundTrip(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), eibiReferenceCacheFile)

	tables := eibiReferenceTables{
		Season:    "B26",
		Languages: map[string]string{"ZZL": "Cache Test Language"},
		Countries: map[string]string{"ZZC": "Cache Test Country"},
		Targets:   map[string]string{"ZZT": "Cache Test Target"},
		Sites:     map[string]map[string]string{"ZZS": {"zz": "Cache Test Site"}},
	}

	if err := eibiSaveReferenceTablesTo(cachePath, tables); err != nil {
		t.Fatalf("saving reference tables: %v", err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	eibiLoadReferenceCacheFrom(cachePath)

	if got := getLanguageName("ZZL"); got != "Cache Test Language" {
		t.Errorf("language = %q", got)
	}
	if got := GetCountryName("ZZC"); got != "Cache Test Country" {
		t.Errorf("country = %q", got)
	}
	if got := GetTargetDescription("ZZT"); got != "Cache Test Target" {
		t.Errorf("target = %q", got)
	}
	if got := GetEiBiSiteDescription("ZZS", "zz"); got != "Cache Test Site" {
		t.Errorf("site = %q", got)
	}
}

func TestEiBiLoadReferenceCacheMissingFile(t *testing.T) {
	eibiLoadReferenceCacheFrom(filepath.Join(t.TempDir(), "does-not-exist.json"))
}

func TestEiBiReferenceStatsString(t *testing.T) {
	stats := eibiReferenceStats{
		Languages: 560, NewLanguages: 79,
		Countries: 288, NewCountries: 10,
		Targets: 40, NewTargets: 0,
		Sites: 1698, NewSites: 1698,
	}
	got := stats.String()
	for _, want := range []string{
		"560 languages (79 new)",
		"288 countries (10 new)",
		"40 targets (0 new)",
		"1698 transmitter sites (1698 new)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stats string %q does not contain %q", got, want)
		}
	}
}

// TestEiBiImportLiveEndToEnd runs the real ImportEiBi used by the UI button: it
// downloads the newest sked-???.csv plus README.TXT, refreshes the reference
// tables and parses the schedule. Skipped when the site is unreachable.
func TestEiBiImportLiveEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live download in -short mode")
	}

	season, err := eibiFetchCurrentSeason()
	if err != nil {
		t.Skipf("EiBi site not reachable: %v", err)
	}
	t.Logf("current season: %s (%s)", season.Season, season.URL)

	rows, err := NewApp().ImportEiBi()
	if err != nil {
		t.Skipf("EiBi import not available: %v", err)
	}
	if len(rows) < 1000 {
		t.Fatalf("imported only %d rows", len(rows))
	}

	// Reference tables must have been refreshed from README.TXT.
	if len(eibiSiteMap) == 0 {
		t.Error("no transmitter sites were loaded from README.TXT")
	}

	// Sample a resolved row end to end.
	var resolved, blankTransmitter int
	for _, row := range rows {
		if row.Transmitter != "" {
			resolved++
		} else {
			blankTransmitter++
		}
		if row.Country == "" || row.Source != "EiBi" {
			t.Fatalf("malformed row: %+v", row)
		}
	}
	t.Logf("imported %d rows: %d with a transmitter site, %d without", len(rows), resolved, blankTransmitter)
	t.Logf("sample: %+v", rows[0])

	if resolved == 0 {
		t.Error("no row resolved a transmitter site")
	}
}
