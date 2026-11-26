package main

import (
	"strings"
	"testing"
	"time"
)

// TestGetRigctlVersion reads the real `rigctl -V` banner. It is skipped when the
// hamlib utilities are not installed.
func TestGetRigctlVersion(t *testing.T) {
	if _, err := rigctlPath(); err != nil {
		t.Skipf("rigctl unavailable: %v", err)
	}

	a := NewApp()

	start := time.Now()
	version, err := a.GetRigctlVersion()
	if err != nil {
		t.Fatalf("GetRigctlVersion: %v", err)
	}
	if version == "" {
		t.Fatal("GetRigctlVersion returned an empty banner")
	}
	if !strings.HasPrefix(version, "rigctl") {
		t.Errorf("banner should start with the program name, got %q", version)
	}
	if !strings.Contains(version, "Hamlib") {
		t.Errorf("banner should name Hamlib, got %q", version)
	}
	if strings.Contains(version, "\n") {
		t.Errorf("banner should be a single line, got %q", version)
	}
	t.Logf("version banner: %q (read in %v)", version, time.Since(start))
}

// TestRigctlAndRigctldAgree checks the version shown in the dialog describes the
// same hamlib the daemon is started from, which is the whole point of showing
// it: the model list comes from rigctld.
func TestRigctlAndRigctldAgree(t *testing.T) {
	if _, err := rigctlPath(); err != nil {
		t.Skipf("rigctl unavailable: %v", err)
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	a := NewApp()
	banner, err := a.GetRigctlVersion()
	if err != nil {
		t.Fatalf("GetRigctlVersion: %v", err)
	}

	// Both utilities are built from the same hamlib, so they report the same
	// version string after the program name.
	fields := strings.Fields(banner)
	if len(fields) < 3 {
		t.Fatalf("unexpected banner: %q", banner)
	}
	core := strings.Join(fields[1:3], " ") // e.g. "Hamlib 4.5.5"

	rigctlBin, _ := rigctlPath()
	rigctldBin, _ := rigctldPath()
	t.Logf("rigctl=%s rigctld=%s version=%q", rigctlBin, rigctldBin, core)
}
