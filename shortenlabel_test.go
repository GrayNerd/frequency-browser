package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestShortenLabel keeps long USB descriptors from widening the settings dialog.
func TestShortenLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		max   int
		want  string
	}{
		{
			name:  "short enough is untouched",
			label: "/dev/ttyUSB0",
			max:   40,
			want:  "/dev/ttyUSB0",
		},
		{
			name:  "exactly max is untouched",
			label: strings.Repeat("a", 40),
			max:   40,
			want:  strings.Repeat("a", 40),
		},
		{
			name:  "cut on a word boundary",
			label: "Silicon Labs CP2102 USB to UART Bridge Controller IC-7610",
			max:   40,
			want:  "Silicon Labs CP2102 USB to UART Bridge\u2026",
		},
		{
			name:  "no spaces still cuts",
			label: strings.Repeat("x", 80),
			max:   40,
			want:  strings.Repeat("x", 39) + "\u2026",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shortenLabel(tc.label, tc.max)
			if got != tc.want {
				t.Errorf("shortenLabel(%q, %d) = %q, want %q", tc.label, tc.max, got, tc.want)
			}
			if utf8.RuneCountInString(got) > tc.max+1 {
				t.Errorf("result %q is longer than %d runes", got, tc.max+1)
			}
		})
	}
}

// TestShortenLabelKeepsValidUTF8 guards against cutting through a multi-byte
// character, which would render as a replacement glyph in the dropdown.
func TestShortenLabelKeepsValidUTF8(t *testing.T) {
	label := "Adap\u00e9rtateur r\u00e9seau \u00e9l\u00e9phant tr\u00e8s long description"
	for max := 2; max < len(label); max++ {
		got := shortenLabel(label, max)
		if !utf8.ValidString(got) {
			t.Fatalf("shortenLabel(label, %d) produced invalid UTF-8: %q", max, got)
		}
	}
}

// TestSerialPortLabelsAreShort checks the real enumeration against this machine.
func TestSerialPortLabelsAreShort(t *testing.T) {
	ports, err := listSerialPorts("/dev", "/dev/serial/by-id")
	if err != nil {
		t.Skipf("no serial ports available: %v", err)
	}
	for _, p := range ports {
		if len(p.Label) > 80 {
			t.Errorf("label for %s is %d bytes, too wide for the dialog: %q", p.Path, len(p.Label), p.Label)
		}
	}
}
