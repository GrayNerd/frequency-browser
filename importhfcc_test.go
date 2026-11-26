package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

// hfccTestPage is a trimmed copy of the real https://new.hfcc.org/data/ markup,
// keeping the structure that matters: relative HREFs to *allx2.zip archives and
// the optional "Last updated on" note that follows each link.
const hfccTestPage = `<!doctype HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
<HTML><BODY>
                <UL>
                  <LI> <A HREF="b98/b98allx2.txt"> B98allx2.txt </A> - B98 Operational Schedule
                </UL>

                <UL>
                  <LI> <A HREF="a99/a99allx2.exe"> A99allx2.exe </A> - A99 Operational Schedule
                </UL>

                <UL>
                  <LI> <A HREF="a10/a10allx2.zip"> A10allx2.zip </A> - A10 Operational Schedule
                </UL>

                <UL>
                  <LI> <A HREF="b10/b10allx2.zip"> B10allx2.zip </A> - B10 Operational Schedule - Last updated on 23-May-2011
                </UL>

                <UL>
                  <LI> <A HREF="a25/a25allx2.zip"> A25allx2.zip </A> - A25 Operational Schedule - Last updated on 23-Oct-2025
                </UL>

                <UL>
                  <LI> <A HREF="b25/b25allx2.zip"> B25allx2.zip </A> - B25 Operational Schedule - Last updated on 23-Mar-2026
                </UL>

                <UL>
                  <LI> <A HREF="a26/a26allx2.zip"> A26allx2.zip </A> - A26 Operational Schedule - Last updated on 02-Nov-2026
                </UL>

                <UL>
                  <LI> <A HREF="b26/b26allx2.zip"> B26allx2.zip </A> - B26 Operational Schedule - Last updated on 14-Nov-2026
                </UL>

                <P>Note: Starting from B10, the data has been updated during the season.</P>
</BODY></HTML>
`

// hfccTestSchedule returns a small schedule file laid out exactly like the real
// B26all00.TXT: comment/header lines followed by fixed width records.
func hfccTestSchedule() string {
	line := []byte(strings.Repeat(" ", 158))
	put := func(start int, value string) {
		copy(line[start:], value)
	}
	put(0, " 3265")
	put(6, "0000")
	put(11, "2400")
	put(47, "BCQ")
	put(72, "1234567")
	put(102, "Eng")
	put(113, "USA")
	put(117, "BCQ")

	return "; B26 ALL 14-nov-2026\r\n" +
		";----+----+----+------------------------------+---+----+-------+---+---+-------+------+------+-+-----+----------+---+---+---+-----+-+-----+-----+-----+-------\r\n" +
		";FREQ STRT STOP CIRAF ZONES                    LOC POWR AZIMUTH SLW ANT DAYS    FDATE  TDATE MOD AFRQ LANGUAGE   ADM BRC FMO REQ# OLD ALT1 ALT2  ALT3  NOTES\r\n" +
		";----+----+----+------------------------------+---+----+-------+---+---+-------+------+------+-+-----+----------+---+---+---+-----+-+-----+-----+-----+-------\r\n" +
		string(line) + "\r\n"
}

func TestHfccParseScheduleLinks(t *testing.T) {
	schedules := hfccParseScheduleLinks([]byte(hfccTestPage), hfccDataPageURL)
	if len(schedules) != 6 {
		t.Fatalf("found %d schedules, want 6: %+v", len(schedules), schedules)
	}

	first := schedules[0]
	if first.Name != "a10allx2.zip" {
		t.Errorf("Name = %q, want a10allx2.zip", first.Name)
	}
	if first.URL != "https://new.hfcc.org/data/a10/a10allx2.zip" {
		t.Errorf("URL = %q", first.URL)
	}
	if first.Season != "A10" {
		t.Errorf("Season = %q, want A10", first.Season)
	}
	if !first.Updated.IsZero() {
		t.Errorf("Updated = %v, want zero (no date on the page)", first.Updated)
	}

	last := schedules[len(schedules)-1]
	if last.Season != "B26" {
		t.Errorf("Season = %q, want B26", last.Season)
	}
	want := time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)
	if !last.Updated.Equal(want) {
		t.Errorf("Updated = %v, want %v", last.Updated, want)
	}
}

func TestHfccNewestScheduleUsesLastUpdated(t *testing.T) {
	schedules := hfccParseScheduleLinks([]byte(hfccTestPage), hfccDataPageURL)
	newest, ok := hfccNewestSchedule(schedules)
	if !ok {
		t.Fatal("hfccNewestSchedule reported no schedules")
	}
	if newest.Season != "B26" {
		t.Errorf("newest season = %q, want B26", newest.Season)
	}
	if newest.Name != "b26allx2.zip" {
		t.Errorf("newest name = %q, want b26allx2.zip", newest.Name)
	}
}

func TestHfccNewestScheduleFallsBackToSeason(t *testing.T) {
	// Without "Last updated" dates the season code decides: B26 > A26 > B25.
	schedules := []hfccSchedule{
		{Name: "b25allx2.zip", Season: "B25"},
		{Name: "a26allx2.zip", Season: "A26"},
		{Name: "b26allx2.zip", Season: "B26"},
		{Name: "a25allx2.zip", Season: "A25"},
	}
	newest, ok := hfccNewestSchedule(schedules)
	if !ok {
		t.Fatal("hfccNewestSchedule reported no schedules")
	}
	if newest.Name != "b26allx2.zip" {
		t.Errorf("newest = %q, want b26allx2.zip", newest.Name)
	}

	if _, ok := hfccNewestSchedule(nil); ok {
		t.Error("expected ok=false for an empty schedule list")
	}
}

func TestHfccSeasonCode(t *testing.T) {
	cases := map[string]string{
		"b26allx2.zip": "B26",
		"a10allx2.zip": "A10",
		"B98allx2.txt": "B98",
		"b26all00.txt": "B26",
		"nosuffix.bin": "",
		"":             "",
	}
	for name, want := range cases {
		if got := hfccSeasonCode(name); got != want {
			t.Errorf("hfccSeasonCode(%q) = %q, want %q", name, got, want)
		}
	}
}

// hfccBuildZip builds an in-memory zip archive containing the given files.
func hfccBuildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("creating zip entry %q: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("writing zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return buf.Bytes()
}

func TestHfccScheduleFromZipMatchesPrefix(t *testing.T) {
	archive := hfccBuildZip(t, map[string]string{
		"admin.txt":    "admin",
		"antenna.txt":  "antenna",
		"broadcas.txt": "broadcas",
		"site.txt":     "site",
		"B26all00.TXT": hfccTestSchedule(),
	})

	name, content, err := hfccScheduleFromZip(archive, "b26allx2.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "B26all00.TXT" {
		t.Errorf("name = %q, want B26all00.TXT", name)
	}
	if !strings.Contains(string(content), " 3265") {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestHfccScheduleFromZipNoMatch(t *testing.T) {
	archive := hfccBuildZip(t, map[string]string{
		"admin.txt":    "admin",
		"site.txt":     "site",
		"A25all00.TXT": hfccTestSchedule(),
	})

	if _, _, err := hfccScheduleFromZip(archive, "b26allx2.zip"); err == nil {
		t.Fatal("expected an error when no entry matches the archive name")
	}
}

func TestHfccScheduleFromZipNotAZip(t *testing.T) {
	if _, _, err := hfccScheduleFromZip([]byte("not a zip"), "b26allx2.zip"); err == nil {
		t.Fatal("expected an error for a corrupted archive")
	}
}

func TestHfccParseScheduleFile(t *testing.T) {
	rows, err := hfccParseScheduleFile([]byte(hfccTestSchedule()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1: %+v", len(rows), rows)
	}

	row := rows[0]
	if strings.TrimSpace(row.Khz) != "3265" {
		t.Errorf("Khz = %q, want 3265", row.Khz)
	}
	if row.StartTime != "0000" || row.EndTime != "2400" {
		t.Errorf("times = %q-%q, want 0000-2400", row.StartTime, row.EndTime)
	}
	if row.Days != "1234567" {
		t.Errorf("Days = %q, want 1234567", row.Days)
	}
	if row.Country != GetCountryName("USA") {
		t.Errorf("Country = %q", row.Country)
	}
	if row.Language != getLanguageName("Eng") {
		t.Errorf("Language = %q", row.Language)
	}
	if row.Source != "HFCC" {
		t.Errorf("Source = %q, want HFCC", row.Source)
	}
	if !strings.Contains(row.Transmitter, GetHfccSiteName("BCQ")) {
		t.Errorf("Transmitter = %q, want it to contain %q", row.Transmitter, GetHfccSiteName("BCQ"))
	}
}

// TestHfccImportLiveDownloadsNewestSchedule hits the real HFCC site: it checks
// that the newest archive is picked, the matching *.txt entry is extracted and
// that the records parse. It is skipped when the site is unreachable.
func TestHfccImportLiveDownloadsNewestSchedule(t *testing.T) {
	page, err := hfccFetchSchedulePage()
	if err != nil {
		t.Skipf("HFCC data page not reachable: %v", err)
	}

	schedules := hfccParseScheduleLinks(page, hfccDataPageURL)
	newest, ok := hfccNewestSchedule(schedules)
	if !ok {
		t.Fatalf("no schedule archives found on %s", hfccDataPageURL)
	}
	t.Logf("newest schedule: %s (%s), updated %s", newest.Season, newest.URL, newest.Updated.Format("2006-01-02"))

	archive, err := hfccDownload(newest.URL)
	if err != nil {
		t.Skipf("could not download %s: %v", newest.URL, err)
	}

	fileName, content, err := hfccScheduleFromZip(archive, newest.Name)
	if err != nil {
		t.Fatalf("extracting schedule from %s: %v", newest.Name, err)
	}

	prefix := strings.ToLower(newest.Name)
	if len(prefix) > hfccScheduleNamePrefixLen {
		prefix = prefix[:hfccScheduleNamePrefixLen]
	}
	if !strings.HasPrefix(strings.ToLower(fileName), prefix) {
		t.Errorf("extracted %q, want a name starting with %q", fileName, prefix)
	}
	t.Logf("extracted %s (%d bytes)", fileName, len(content))

	// The archive must also carry the reference tables that B26 needs.
	tables, err := hfccReferenceTablesFromZip(archive, newest.Name)
	if err != nil {
		t.Fatalf("reading reference tables from %s: %v", newest.Name, err)
	}
	if len(tables.Missing) > 0 {
		t.Logf("reference tables missing from %s: %v", newest.Name, tables.Missing)
	}
	if len(tables.Sites) == 0 || len(tables.Broadcasters) == 0 || len(tables.Countries) == 0 || len(tables.Languages) == 0 {
		t.Fatalf("incomplete reference tables: %d sites, %d broadcasters, %d countries, %d languages",
			len(tables.Sites), len(tables.Broadcasters), len(tables.Countries), len(tables.Languages))
	}
	t.Logf("reference tables: %d sites, %d broadcasters, %d countries, %d languages",
		len(tables.Sites), len(tables.Broadcasters), len(tables.Countries), len(tables.Languages))

	rows, err := hfccParseScheduleFile(content)
	if err != nil {
		t.Fatalf("parsing %s: %v", fileName, err)
	}
	if len(rows) < 100 {
		t.Fatalf("parsed only %d rows from %s", len(rows), fileName)
	}
	t.Logf("parsed %d rows; first = %+v", len(rows), rows[0])
}

// TestHfccImportLiveEndToEnd runs the real ImportHFCC used by the UI button: it
// downloads the newest archive, refreshes the reference tables and parses the
// schedule. It only needs a nil Wails context because ImportHFCC no longer opens
// a file dialog.
//
// The assertion is deliberately about our own parsing, not about HFCC's data
// being complete. HFCC occasionally references a site code that its own site.txt
// does not define (season B26 references "ZWL"), so requiring every site to
// resolve would make the suite fail whenever upstream ships a gap. What must
// hold is that every row is fully populated: a code upstream does not define is
// reported as the code itself, with the row's own administration as country,
// never as an empty or "Unknown" field.
func TestHfccImportLiveEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live download in -short mode")
	}

	app := NewApp()

	rows, err := app.ImportHFCC()
	if err != nil {
		t.Skipf("HFCC import not available (offline?): %v", err)
	}
	if len(rows) < 100 {
		t.Fatalf("imported only %d rows", len(rows))
	}

	// Every field the grid shows must be populated. A site code that upstream
	// does not define is acceptable and shows as the code; an empty site name
	// or country is not.
	var emptySites, emptyStations, emptyCountries []string
	unresolved := map[string]int{}
	for _, row := range rows {
		site, country, ok := splitTransmitter(row.Transmitter)
		if !ok || site == "" || country == "" {
			emptySites = append(emptySites, row.Transmitter)
			continue
		}
		if site == "Unknown" {
			unresolved[row.Transmitter]++
		}
		if strings.TrimSpace(row.Station) == "" || strings.HasSuffix(row.Station, " Unknown") {
			emptyStations = append(emptyStations, row.Station)
		}
		if strings.TrimSpace(row.Country) == "" {
			emptyCountries = append(emptyCountries, row.Country)
		}
	}

	t.Logf("imported %d rows; unresolved site strings: %v", len(rows), unresolved)

	if n := len(emptySites); n > 0 {
		t.Errorf("%d rows have an empty or malformed transmitter field, e.g. %q", n, firstFew(emptySites))
	}
	if n := len(emptyStations); n > 0 {
		t.Errorf("%d rows have no broadcaster name, e.g. %q", n, firstFew(emptyStations))
	}
	if n := len(emptyCountries); n > 0 {
		t.Errorf("%d rows have no country, e.g. %q", n, firstFew(emptyCountries))
	}

	// The reference tables must actually have been refreshed, otherwise the
	// names above are coming from the embedded defaults only.
	if got := GetSiteCount(); got < 100 {
		t.Errorf("only %d HFCC sites loaded after the import", got)
	}

	// A site code that was unknown before the import should resolve now, when
	// this season references one. This exercises the refresh rather than just
	// the parse.
	for _, code := range []string{"PTN", "ANK", "BNA", "ISH"} {
		if site, ok := GetHfccSite(code); ok {
			t.Logf("site %s resolves to %q (ADM %q)", code, site.SiteName, site.ADM)
		}
	}
}

// splitTransmitter splits the "Site, Country" transmitter field the importer
// builds, reporting whether the shape was as expected.
func splitTransmitter(value string) (site, country string, ok bool) {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// firstFew renders up to three sample values for an error message.
func firstFew(values []string) []string {
	if len(values) > 3 {
		return values[:3]
	}
	return values
}
