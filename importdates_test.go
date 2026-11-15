package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestImportDatesRoundTrip records a date and reads it back through the same
// code path the UI uses.
func TestImportDatesRoundTrip(t *testing.T) {
	dates := ImportDates{
		"EiBi": time.Date(2026, time.November, 14, 18, 30, 0, 0, time.UTC).Format(time.RFC3339),
		"AOKI": time.Date(2026, time.November, 14, 19, 5, 0, 0, time.UTC).Format(time.RFC3339),
	}

	path := filepath.Join(t.TempDir(), importDatesFile)
	if err := writeImportDatesTo(path, dates); err != nil {
		t.Fatalf("writing import dates: %v", err)
	}

	read, err := readImportDatesFrom(path)
	if err != nil {
		t.Fatalf("reading import dates: %v", err)
	}
	if len(read) != len(dates) {
		t.Fatalf("read %d entries, want %d: %v", len(read), len(dates), read)
	}
	for source, want := range dates {
		if got := read[source]; got != want {
			t.Errorf("%s = %q, want %q", source, got, want)
		}
	}

	// The recorded value must round-trip through Date, since the UI formats it.
	when, err := time.Parse(time.RFC3339, read["EiBi"])
	if err != nil {
		t.Fatalf("recorded date is not RFC 3339: %v", err)
	}
	if when.Hour() != 18 || when.Minute() != 30 {
		t.Errorf("parsed time = %v, want 18:30", when)
	}
}

// TestReadImportDatesMissingFile ensures a first run without recorded dates is
// treated as "nothing imported yet" rather than an error.
func TestReadImportDatesMissingFile(t *testing.T) {
	dates, err := readImportDatesFrom(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dates) != 0 {
		t.Errorf("dates = %v, want empty", dates)
	}
}

// TestLoadImportDatesEmpty confirms the exported binding returns a usable map
// when no file exists.
func TestLoadImportDatesEmpty(t *testing.T) {
	dates, err := NewApp().LoadImportDates()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dates == nil {
		t.Error("LoadImportDates returned nil, want an empty map")
	}
}

// TestRecordImportDateUpdatesEntry checks that recording one source leaves the
// others alone.
func TestRecordImportDateUpdatesEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), importDatesFile)

	if err := writeImportDatesTo(path, ImportDates{"AOKI": "old-value"}); err != nil {
		t.Fatalf("seeding import dates: %v", err)
	}

	if err := recordImportDateTo(path, "EiBi"); err != nil {
		t.Fatalf("recording import date: %v", err)
	}

	read, err := readImportDatesFrom(path)
	if err != nil {
		t.Fatalf("reading import dates: %v", err)
	}
	if read["AOKI"] != "old-value" {
		t.Errorf("AOKI = %q, want the existing value preserved", read["AOKI"])
	}
	if _, err := time.Parse(time.RFC3339, read["EiBi"]); err != nil {
		t.Errorf("EiBi = %q, want an RFC 3339 timestamp: %v", read["EiBi"], err)
	}
}

// TestImportDatesPath checks the record sits beside the executable.
func TestImportDatesPath(t *testing.T) {
	path, err := importDatesPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != importDatesFile {
		t.Errorf("file = %q, want %q", filepath.Base(path), importDatesFile)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	if filepath.Dir(path) != filepath.Dir(exe) {
		t.Errorf("dir = %q, want %q", filepath.Dir(path), filepath.Dir(exe))
	}
}
