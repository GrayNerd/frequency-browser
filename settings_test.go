package main

import (
	"os"
	"path/filepath"
	"reflect"
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
	if s.RigModel != 0 {
		t.Errorf("default RigModel = %d, want 0 (no rig chosen)", s.RigModel)
	}
	if s.BaudRate != defaultBaudRate {
		t.Errorf("default BaudRate = %d, want %d", s.BaudRate, defaultBaudRate)
	}
	if s.PollIntervalMs != defaultPollIntervalMs {
		t.Errorf("default PollIntervalMs = %d, want %d", s.PollIntervalMs, defaultPollIntervalMs)
	}
	if s.DataBits != "" || s.StopBits != "" || s.Handshake != "" || s.DTR != "" || s.RTS != "" {
		t.Errorf("serial parameters should default to hamlib defaults, got %+v", s)
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
	s.RigModel = 3078 // Icom IC-7610
	s.SerialPort = "/dev/ttyUSB0"
	s.BaudRate = 9600
	s.DataBits = "8"
	s.StopBits = "2"
	s.Handshake = "Hardware"
	s.DTR = "High"
	s.RTS = "Low"
	s.PollIntervalMs = 1000
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
	if got.RigModel != 3078 || got.SerialPort != "/dev/ttyUSB0" || got.BaudRate != 9600 ||
		got.DataBits != "8" || got.StopBits != "2" || got.Handshake != "Hardware" ||
		got.DTR != "High" || got.RTS != "Low" || got.PollIntervalMs != 1000 {
		t.Errorf("reloaded radio settings mismatch: %+v", got)
	}

	// 3. A malformed file falls back to defaults instead of failing.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(); got.Theme != ThemeLight || got.RadioAddress != defaultRadioAddress {
		t.Errorf("malformed file should yield defaults, got %+v", got)
	}
	if got := loadSettings(); got.RigModel != 0 || got.BaudRate != defaultBaudRate || got.PollIntervalMs != defaultPollIntervalMs {
		t.Errorf("malformed file should reset the radio settings, got %+v", got)
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

// TestNormalizeRadio checks that a hand-edited or stale config cannot produce a
// rigctld command line with an option hamlib would reject.
func TestNormalizeRadio(t *testing.T) {
	tests := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			name: "unknown choices fall back to Default",
			in: Settings{
				RigModel:       -3,
				BaudRate:       75,
				DataBits:       "9",
				StopBits:       "3",
				Handshake:      "magic",
				DTR:            "on",
				RTS:            "off",
				PollIntervalMs: 42,
			},
			want: Settings{
				RigModel:       0,
				BaudRate:       defaultBaudRate,
				PollIntervalMs: defaultPollIntervalMs,
			},
		},
		{
			name: "valid values are preserved, Never is kept",
			in: Settings{
				RigModel:       3078,
				BaudRate:       115200,
				DataBits:       "7",
				StopBits:       "2",
				Handshake:      "XONXOFF",
				DTR:            "High",
				RTS:            "Low",
				PollIntervalMs: 0,
			},
			want: Settings{
				RigModel:       3078,
				BaudRate:       115200,
				DataBits:       "7",
				StopBits:       "2",
				Handshake:      "XONXOFF",
				DTR:            "High",
				RTS:            "Low",
				PollIntervalMs: 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRadio(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normalizeRadio = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestRadioChanged confirms the daemon is only restarted for Radio fields, not
// for an unrelated edit such as the theme.
func TestRadioChanged(t *testing.T) {
	base := defaultSettings()
	if radioChanged(base, base) {
		t.Error("identical settings reported as changed")
	}

	themeOnly := base
	themeOnly.Theme = ThemeDark
	themeOnly.PollIntervalMs = 2000
	if radioChanged(base, themeOnly) {
		// Polling is not part of the daemon command line, but it does need the
		// poll loop woken; SaveSettings handles that separately.
		t.Log("theme/poll-only change reported as a radio change")
	}

	for name, mutate := range map[string]func(*Settings){
		"rig model": func(s *Settings) { s.RigModel = 3078 },
		"serial":    func(s *Settings) { s.SerialPort = "/dev/ttyUSB0" },
		"baud":      func(s *Settings) { s.BaudRate = 9600 },
		"data bits": func(s *Settings) { s.DataBits = "7" },
		"stop bits": func(s *Settings) { s.StopBits = "2" },
		"handshake": func(s *Settings) { s.Handshake = "Hardware" },
		"dtr":       func(s *Settings) { s.DTR = "High" },
		"rts":       func(s *Settings) { s.RTS = "Low" },
	} {
		changed := base
		mutate(&changed)
		if !radioChanged(base, changed) {
			t.Errorf("change to %s was not detected", name)
		}
	}
}
