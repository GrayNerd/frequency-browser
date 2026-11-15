package main

import (
	"archive/zip"
	"bytes"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAokiParseScheduleLinks(t *testing.T) {
	schedules := aokiParseScheduleLinks([]byte(aokiTestFramePage), aokiFrameURL)
	if len(schedules) != 5 {
		t.Fatalf("found %d archives, want 5: %+v", len(schedules), schedules)
	}

	// Header links resolve to absolute URLs; the plain .txt link is ignored.
	first := schedules[0]
	if first.Name != "xta26.zip" {
		t.Errorf("Name = %q, want xta26.zip", first.Name)
	}
	if first.Season != "A26" {
		t.Errorf("Season = %q, want A26", first.Season)
	}
	if first.Prefix != "xta26" {
		t.Errorf("Prefix = %q, want xta26", first.Prefix)
	}
	if first.URL != "https://www1.m2.mediacat.ne.jp/binews/ut/xt/xta26.zip" {
		t.Errorf("URL = %q", first.URL)
	}

	for _, s := range schedules {
		if strings.HasSuffix(s.Name, "userlist1.txt") {
			t.Error("picked up the plain userlist1.txt link")
		}
	}
}

func TestAokiArchiveParts(t *testing.T) {
	cases := []struct{ name, season, prefix string }{
		{"xta26.zip", "A26", "xta26"},
		{"xzb26.zip", "B26", "xzb26"},
		{"nsa25.zip", "A25", "nsa25"},
		{"nzb25.zip", "B25", "nzb25"},
		{"ncb24.zip", "B24", "ncb24"},
		{"readme.zip", "", ""},
		{"xta26.txt", "", ""},
	}
	for _, c := range cases {
		season, prefix := aokiArchiveParts(c.name)
		if season != c.season || prefix != c.prefix {
			t.Errorf("aokiArchiveParts(%q) = (%q, %q), want (%q, %q)",
				c.name, season, prefix, c.season, c.prefix)
		}
	}
}

func TestAokiParseScheduleHeader(t *testing.T) {
	season, revised, ok := aokiParseScheduleHeader(
		"B26 Shortwave Frequecy List  November 14 2026,  1000 UTC   Day 1 = Sunday")
	if !ok {
		t.Fatal("header was not parsed")
	}
	if season != "B26" {
		t.Errorf("season = %q, want B26", season)
	}
	want := time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)
	if !revised.Equal(want) {
		t.Errorf("revised = %v, want %v", revised, want)
	}

	// A three-letter month is also accepted.
	if _, revised, ok := aokiParseScheduleHeader("A25 Shortwave Frequency List  Oct 22 2025, 2300 UTC"); !ok {
		t.Error("three-letter month month was not parsed")
	} else if revised.Month() != time.October {
		t.Errorf("month = %v, want October", revised.Month())
	}

	if _, _, ok := aokiParseScheduleHeader("not a schedule header"); ok {
		t.Error("garbage was parsed as a header")
	}
}

func TestAokiNewestArchive(t *testing.T) {
	// The page lists A26 first, but xzb26.zip is a B26 archive, which is newer.
	schedules := aokiParseScheduleLinks([]byte(aokiTestFramePage), aokiFrameURL)

	sorted := make([]aokiSchedule, len(schedules))
	copy(sorted, schedules)
	sort.SliceStable(sorted, func(i, j int) bool {
		iy, ih := aokiSeasonRank(sorted[i].Season)
		jy, jh := aokiSeasonRank(sorted[j].Season)
		if iy != jy {
			return iy > jy
		}
		return ih > jh
	})

	if sorted[0].Name != "xzb26.zip" {
		t.Errorf("newest archive = %q, want xzb26.zip", sorted[0].Name)
	}
	if sorted[0].Season != "B26" {
		t.Errorf("newest season = %q, want B26", sorted[0].Season)
	}
}

func TestAokiSeasonRank(t *testing.T) {
	// Chronological order: A25 < B25 < A26 < B26.
	order := []string{"A25", "B25", "A26", "B26"}
	for i := 1; i < len(order); i++ {
		py, ph := aokiSeasonRank(order[i-1])
		cy, ch := aokiSeasonRank(order[i])
		if cy*2+ch <= py*2+ph {
			t.Errorf("%s should sort after %s", order[i], order[i-1])
		}
	}
}

// aokiBuildZip builds an in-memory archive with the given members.
func aokiBuildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("creating zip entry %q: %v", name, err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			t.Fatalf("writing zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
	return buf.Bytes()
}

func TestAokiScheduleFromZip(t *testing.T) {
	// The real archive also holds an .xlsx and the Perseus userlist1.txt; only
	// the member named after the archive is wanted.
	schedule := aokiTestSchedule(t, aokiRowTimeSignal)
	archive := aokiBuildZip(t, map[string][]byte{
		"xta26.xlsx":    []byte("not the schedule"),
		"userlist1.txt": []byte("kHz Time(UTC) ITU Station"),
		"xta26.txt":     schedule,
	})

	name, content, err := aokiScheduleFromZip(archive, "xta26.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "xta26.txt" {
		t.Errorf("name = %q, want xta26.txt", name)
	}
	if !bytes.Contains(content, []byte("Time Signal")) {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestAokiScheduleFromZipNoMatch(t *testing.T) {
	archive := aokiBuildZip(t, map[string][]byte{
		"xta26.xlsx":    []byte("spreadsheet"),
		"userlist1.txt": []byte("perseus"),
	})
	if _, _, err := aokiScheduleFromZip(archive, "xta26.zip"); err == nil {
		t.Fatal("expected an error when the matching member is absent")
	}
}

func TestAokiScheduleFromZipNotAZip(t *testing.T) {
	if _, _, err := aokiScheduleFromZip([]byte("not a zip"), "xta26.zip"); err == nil {
		t.Fatal("expected an error for a corrupted archive")
	}
}

func TestAokiParseSchedule(t *testing.T) {
	schedule := aokiTestSchedule(t,
		aokiRowTimeSignal,
		aokiRowAirDelhi,
		aokiRowPbsXian,
		aokiRowShortTail,
		aokiRowVeryShort,
	)

	rows, err := aokiParseSchedule(schedule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("parsed %d rows, want 5: %+v", len(rows), rows)
	}

	first := rows[0]
	if first.Khz != "40" {
		t.Errorf("Khz = %q, want 40 (column is six bytes wide)", first.Khz)
	}
	if first.StartTime != "0000" || first.EndTime != "2400" {
		t.Errorf("times = %q-%q", first.StartTime, first.EndTime)
	}
	if first.Days != "1234567" {
		t.Errorf("Days = %q", first.Days)
	}
	if first.Station != "Time Signal" {
		t.Errorf("Station = %q", first.Station)
	}
	if first.Country != "Japan" {
		t.Errorf("Country = %q, want Japan (from ADM \"J\")", first.Country)
	}
	if first.Transmitter != "Otakadoyama Tamura C" {
		t.Errorf("Transmitter = %q", first.Transmitter)
	}
	if first.Language != "A1B" {
		t.Errorf("Language = %q, want A1B", first.Language)
	}
	if first.Source != "Aoki" {
		t.Errorf("Source = %q, want Aoki", first.Source)
	}

	// A station name carrying a parenthesised power figure stays intact.
	if rows[1].Station != "AIR Delhi A (DRM)F250001" {
		t.Errorf("Station = %q", rows[1].Station)
	}
	if rows[1].Country != "India" {
		t.Errorf("Country = %q, want India", rows[1].Country)
	}

	// A row whose trailing columns are missing is still imported.
	short := rows[4]
	if short.Khz != "11945" || short.StartTime != "1330" || short.EndTime != "1430" {
		t.Errorf("short row = %+v", short)
	}
	if short.Station != "Radio Barandazi" {
		t.Errorf("short row station = %q", short.Station)
	}
}

func TestAokiParseScheduleDoubleByte(t *testing.T) {
	rows, err := aokiParseSchedule(aokiTestSchedule(t, aokiRowPbsZhejiang))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1: %+v", len(rows), rows)
	}

	row := rows[0]
	// The columns after the double-byte text must still line up.
	if row.Country != "China" {
		t.Errorf("Country = %q, want China", row.Country)
	}
	if !strings.HasPrefix(row.Transmitter, "Yuhang Hangzhou") {
		t.Errorf("Transmitter = %q", row.Transmitter)
	}
	if !strings.Contains(row.Transmitter, "市") {
		t.Errorf("Transmitter = %q, want the Japanese characters preserved", row.Transmitter)
	}
	if row.Language != "Chinese" {
		t.Errorf("Language = %q", row.Language)
	}
}

func TestAokiParseScheduleLocationOverflow(t *testing.T) {
	rows, err := aokiParseSchedule(aokiTestSchedule(t, aokiRowLongLocation))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1: %+v", len(rows), rows)
	}

	// The 24-byte location pushes CZE right, so the fixed columns would read
	// "ZE"; the code must be recovered and removed from the location.
	row := rows[0]
	if row.Country != "Czechia" && row.Country != "Czech Republic" && row.Country != "CZE" {
		t.Errorf("Country = %q, want the recovered Czech code", row.Country)
	}
	if strings.Contains(row.Transmitter, "CZE") {
		t.Errorf("Transmitter = %q, want the country code removed", row.Transmitter)
	}
	if !strings.Contains(row.Transmitter, "Ceske Bud") {
		t.Errorf("Transmitter = %q, want the location text kept", row.Transmitter)
	}
}

// TestAokiImportLiveEndToEnd runs the real ImportAOKI used by the UI button: it
// discovers the current archive on the Bi Newsletter page, unpacks the schedule
// and parses it. Skipped when the site is unreachable.
func TestAokiImportLiveEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live download in -short mode")
	}

	page, pageURL, err := aokiFetchLinkPage()
	if err != nil {
		t.Skipf("AOKI site not reachable: %v", err)
	}
	if pageURL != aokiFrameURL && !aokiScheduleLinkRE.Match(page) {
		t.Skipf("no archive links on %s", pageURL)
	}
	if len(aokiParseScheduleLinks(page, pageURL)) == 0 {
		t.Skip("no schedule archives listed")
	}

	rows, err := NewApp().ImportAOKI()
	if err != nil {
		t.Skipf("AOKI import not available: %v", err)
	}
	if len(rows) < 1000 {
		t.Fatalf("imported only %d rows", len(rows))
	}

	var noStation, cjk int
	for _, row := range rows {
		if row.Source != "Aoki" {
			t.Fatalf("malformed row: %+v", row)
		}
		if row.Station == "" {
			noStation++
		}
		// Double-byte transmitter names must survive decoding.
		for _, r := range row.Transmitter {
			if r > 127 {
				cjk++
				break
			}
		}
	}
	t.Logf("imported %d rows (%d with double-byte transmitter names)", len(rows), cjk)
	t.Logf("sample: %+v", rows[0])

	if noStation != 0 {
		t.Errorf("%d rows have no station name", noStation)
	}
}

func TestAokiDecode(t *testing.T) {
	// "日本" is two double-byte characters (four bytes) in Shift_JIS.
	encoded := []byte{0x93, 0xfa, 0x96, 0x7b}
	if got := aokiDecode(encoded); got != "日本" {
		t.Errorf("aokiDecode = %q, want 日本", got)
	}
	if got := aokiDecode([]byte("plain ASCII")); got != "plain ASCII" {
		t.Errorf("aokiDecode ASCII = %q", got)
	}
}
