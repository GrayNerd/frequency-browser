package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// resetSettingsPathCache clears the once-cached path so a test can point the
// settings file at a temporary directory.
func resetSettingsPathCache() {
	settingsPathOnce = sync.Once{}
	settingsPathValue = ""
	settingsPathErr = nil
}

func TestSettingsRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	resetSettingsPathCache()
	t.Cleanup(resetSettingsPathCache)

	// 1. No file yet: loadSettings must create it with defaults.
	s := loadSettings()
	if s.Theme != ThemeLight {
		t.Errorf("default theme = %q, want light", s.Theme)
	}
	if s.SaveWindowPos {
		t.Error("default SaveWindowPos should be false")
	}
	if s.RadioAddress != defaultRadioAddress {
		t.Errorf("default address = %q, want %q", s.RadioAddress, defaultRadioAddress)
	}
	if s.WindowWidth != defaultWindowWidth || s.WindowHeight != defaultWindowHeight {
		t.Errorf("default size = %dx%d, want %dx%d", s.WindowWidth, s.WindowHeight, defaultWindowWidth, defaultWindowHeight)
	}
	if s.WindowSaved {
		t.Error("default WindowSaved should be false")
	}

	path, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	want := filepath.Join(tmp, "frequency-browser", "settings.config")
	if path != want {
		t.Errorf("settings path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file was not created: %v", err)
	}

	// 2. Save modified settings and confirm they survive a reload.
	s.Theme = ThemeDark
	s.SaveWindowPos = true
	s.RadioAddress = "radio.local:4532"
	s.WindowWidth = 1280
	s.WindowHeight = 800
	s.WindowX = 120
	s.WindowY = 90
	s.WindowSaved = true
	if err := saveSettings(s); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	got := loadSettings()
	if got.Theme != ThemeDark || !got.SaveWindowPos || got.RadioAddress != "radio.local:4532" {
		t.Errorf("reloaded settings mismatch: %+v", got)
	}
	if got.WindowWidth != 1280 || got.WindowHeight != 800 || got.WindowX != 120 || got.WindowY != 90 || !got.WindowSaved {
		t.Errorf("reloaded geometry mismatch: %+v", got)
	}

	// 3. A malformed file falls back to defaults instead of failing.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(); got.Theme != ThemeLight || got.RadioAddress != defaultRadioAddress {
		t.Errorf("malformed file should yield defaults, got %+v", got)
	}
}

func TestSettingsNormalized(t *testing.T) {
	s := Settings{Theme: "bogus", RadioAddress: "", WindowWidth: -5}.normalized()
	if s.Theme != ThemeLight {
		t.Errorf("invalid theme should normalize to light, got %q", s.Theme)
	}
	if s.RadioAddress != defaultRadioAddress {
		t.Errorf("empty address should normalize to default, got %q", s.RadioAddress)
	}
	if s.WindowWidth != defaultWindowWidth {
		t.Errorf("invalid width should normalize to default, got %d", s.WindowWidth)
	}
}
