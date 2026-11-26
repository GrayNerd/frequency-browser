package main

import (
	"strings"
	"testing"
)

// TestHfccUnknownSiteFallsBackToCode pins the behaviour the live test relies on:
// when HFCC's schedule references a site code its own site.txt does not define,
// the row must show that code and the row's administration as country, not
// "Unknown, Unknown". Season B26 references "ZWL" this way.
//
// This runs entirely offline: it builds a one-line schedule in the fixed-width
// layout the importer expects, using a site code that is guaranteed absent.
func TestHfccUnknownSiteFallsBackToCode(t *testing.T) {
	const missingSite = "ZZZ"
	if _, exists := GetHfccSite(missingSite); exists {
		t.Skipf("site %s unexpectedly exists in the reference tables", missingSite)
	}

	// Fixed-width record: kHz 0-4, start 6-9, end 11-14, days 72-78,
	// language 102-104, ITU 113-115, station 117-119, site 47-49.
	line := buildHfccRecord(" 5970", "0000", "2400", "1234567", "Nld", "HOL", "BSR", missingSite)
	if len(line) < 120 {
		t.Fatalf("built record is only %d columns: %q", len(line), line)
	}

	rows, err := hfccParseScheduleFile([]byte(line + "\r\n"))
	if err != nil {
		t.Fatalf("hfccParseScheduleFile: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1: %+v", len(rows), rows)
	}

	got := rows[0]
	if !strings.HasPrefix(got.Transmitter, missingSite+",") {
		t.Errorf("Transmitter = %q, want it to start with %q", got.Transmitter, missingSite+",")
	}
	if strings.Contains(got.Transmitter, "Unknown") {
		t.Errorf("Transmitter = %q still contains Unknown", got.Transmitter)
	}
	site, country, ok := splitTransmitter(got.Transmitter)
	if !ok || site != missingSite {
		t.Errorf("site = %q (ok=%v), want %q", site, ok, missingSite)
	}
	if country == "" || country == "Unknown" {
		t.Errorf("country = %q, want the row's administration", country)
	}
	t.Logf("unknown site renders as %q", got.Transmitter)
}

// buildHfccRecord assembles a fixed-width HFCC schedule line from its fields.
func buildHfccRecord(khz, start, end, days, lang, adm, station, site string) string {
	b := make([]byte, 130)
	for i := range b {
		b[i] = ' '
	}
	put := func(start, end int, value string) {
		for i := start; i < end && i < len(b); i++ {
			b[i] = ' '
		}
		copy(b[start:], value)
	}
	put(0, 5, khz)
	put(6, 10, start)
	put(11, 15, end)
	put(47, 50, site)
	put(72, 79, days)
	put(102, 105, lang)
	put(113, 116, adm)
	put(117, 120, station)
	return string(b)
}
