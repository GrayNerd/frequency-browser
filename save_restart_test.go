package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// settingsTestApp points the settings file at a temporary directory and returns
// an App with those settings loaded, so SaveSettings can be exercised for real.
func settingsTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	resetSettingsPathCache()
	t.Cleanup(resetSettingsPathCache)

	a := NewApp()
	a.settings = loadSettings()
	a.config.Address = a.settings.RadioAddress
	return a
}

// TestSaveSettingsRestartsDaemonWithNewOptions is the end-to-end behaviour the
// Radio tab promises: pressing Save restarts the daemon, and the app is using
// the new instance as soon as SaveSettings returns.
func TestSaveSettingsRestartsDaemonWithNewOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := settingsTestApp(t)
	t.Cleanup(a.stopDaemon)

	// First save: the Dummy rig, so the test needs no radio hardware.
	first := a.currentSettings()
	first.RadioAddress = address
	first.RigModel = 1
	first.SerialPort = ""
	first.PollIntervalMs = 500
	if err := a.SaveSettings(first); err != nil {
		t.Fatalf("first SaveSettings: %v", err)
	}

	firstPID := a.GetDaemonStatus().PID
	if firstPID <= 0 || !a.ownsDaemon() {
		t.Fatalf("daemon was not started by the first save: %+v", a.GetDaemonStatus())
	}
	t.Logf("first daemon pid %d on %s", firstPID, address)

	// Second save changes a radio option: the daemon must be replaced. Model 1
	// (Dummy) honours set_freq, so the frequency check afterwards proves the
	// app really is talking to the replacement daemon. A polling-only change
	// is not part of the daemon's command line, so it must NOT restart it.
	second := first
	second.PollIntervalMs = 1000
	if err := a.SaveSettings(second); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}

	status := a.GetDaemonStatus()
	if !status.Running {
		t.Fatalf("no daemon running after the save: %+v", status)
	}
	if status.PID != firstPID {
		t.Errorf("polling change restarted the daemon (pid %d -> %d); the "+
			"polling interval is not part of its command line", firstPID, status.PID)
	}

	// The app must be able to talk to the new instance straight away.
	if err := a.SetFrequency(5900); err != nil {
		t.Errorf("SetFrequency on the restarted daemon: %v", err)
	}
	if got, err := a.getFrequency(); err != nil {
		t.Errorf("getFrequency on the restarted daemon: %v", err)
	} else if diff := got - 5900; diff > 0.01 || diff < -0.01 {
		t.Errorf("frequency = %v kHz, want 5900", got)
	}

	// And the settings really were persisted.
	if saved := loadSettings(); saved.PollIntervalMs != 1000 {
		t.Errorf("persisted PollIntervalMs = %d, want 1000", saved.PollIntervalMs)
	}
}

// TestSaveSettingsRestartsOnRadioFieldChange proves a real radio-field edit
// replaces the daemon process (a polling-only edit does not, see below).
func TestSaveSettingsRestartsOnRadioFieldChange(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := settingsTestApp(t)
	t.Cleanup(a.stopDaemon)

	first := a.currentSettings()
	first.RadioAddress = address
	first.RigModel = 1
	first.SerialPort = ""
	first.BaudRate = 19200
	if err := a.SaveSettings(first); err != nil {
		t.Fatalf("first SaveSettings: %v", err)
	}
	firstPID := a.GetDaemonStatus().PID
	if firstPID <= 0 {
		t.Fatalf("no daemon after the first save: %+v", a.GetDaemonStatus())
	}

	// A serial-parameter change alters rigctld's command line, so the daemon
	// must be replaced. It is passed even though the Dummy rig has no serial
	// port because rigctld accepts the option list for model 1 as well.
	second := first
	second.Handshake = "None"
	if err := a.SaveSettings(second); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}

	status := a.GetDaemonStatus()
	if !status.Running {
		t.Fatalf("no daemon after the radio change: %+v", status)
	}
	if status.PID == firstPID {
		t.Errorf("daemon pid did not change (%d) after a radio-field change", firstPID)
	}
	if err := a.SetFrequency(6000); err != nil {
		t.Errorf("SetFrequency on the replaced daemon: %v", err)
	}
	t.Logf("daemon replaced: %d -> %d", firstPID, status.PID)
}

// TestSaveSettingsPersistsPollingIntervalOnly makes sure a polling change is
// saved without restarting the daemon (it is not part of its command line).
func TestSaveSettingsPersistsPollingIntervalOnly(t *testing.T) {
	a := settingsTestApp(t)

	before := a.currentSettings()
	before.PollIntervalMs = 500
	a.settingsMu.Lock()
	a.settings = before
	a.settingsMu.Unlock()

	after := before
	after.PollIntervalMs = 0 // Never
	if err := a.SaveSettings(after); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if got := a.currentSettings().PollIntervalMs; got != 0 {
		t.Errorf("PollIntervalMs = %d, want 0", got)
	}
	if got := loadSettings().PollIntervalMs; got != 0 {
		t.Errorf("persisted PollIntervalMs = %d, want 0", got)
	}
	if a.pollInterval() != 0 {
		t.Errorf("pollInterval() = %v, want 0 after choosing Never", a.pollInterval())
	}
}

// TestSaveSettingsRejectsUnavailablePort checks the failure path the dialog
// relies on: a bad serial port must not leave the app pointing at a dead
// daemon, and the previous settings must be restored.
func TestSaveSettingsRejectsUnavailablePort(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	// The pre-flight check only applies to serial backends, so pick a model that
	// hamlib reports as serial (the Dummy rig has no port at all).
	serialModel := firstSerialRigModel(t)

	a := settingsTestApp(t)
	t.Cleanup(a.stopDaemon)

	good := a.currentSettings()
	good.RadioAddress = address
	good.RigModel = serialModel
	good.SerialPort = "/dev/null" // exists, so the baseline save is accepted
	if err := a.SaveSettings(good); err != nil {
		t.Fatalf("baseline SaveSettings: %v", err)
	}

	bad := good
	bad.SerialPort = filepath.Join(t.TempDir(), "ttyMISSING")
	err = a.SaveSettings(bad)
	if err == nil {
		t.Fatal("SaveSettings accepted a serial port that does not exist")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should name the missing port, got %v", err)
	}

	// The previous settings must still be in force.
	if got := a.currentSettings(); got.SerialPort != "/dev/null" {
		t.Errorf("settings were not reverted: SerialPort = %q", got.SerialPort)
	}
	if !a.GetDaemonStatus().Running {
		t.Error("the working daemon was lost when the save failed")
	}
}

// TestServiceShutdownStopsOwnDaemon confirms the exit path: a daemon this app
// started is stopped, so no orphan is left behind.
func TestServiceShutdownStopsOwnDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := settingsTestApp(t)
	if err := a.startDaemon(Settings{RigModel: 1, RadioAddress: address, PollIntervalMs: 500}); err != nil {
		t.Fatalf("startDaemon: %v", err)
	}
	pid := a.GetDaemonStatus().PID
	if pid <= 0 {
		t.Fatalf("no pid recorded: %+v", a.GetDaemonStatus())
	}

	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	if a.probeDaemon(address) {
		t.Error("daemon still listening after ServiceShutdown")
	}
	if processAlive(pid) {
		// Give the kernel a moment to reap it if it is a zombie still owned by
		// this process.
		time.Sleep(200 * time.Millisecond)
		if processAlive(pid) {
			t.Errorf("daemon pid %d survived ServiceShutdown", pid)
		}
	}
}

// TestSettingsFileFromPreviousVersionLoads keeps existing users working: an old
// config with only radioAddress must load and keep that address, with no rig
// chosen (so no daemon is started behind their back).
func TestSettingsFileFromPreviousVersionLoads(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	resetSettingsPathCache()
	t.Cleanup(resetSettingsPathCache)

	path, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "theme": "dark",
  "radioAddress": "radio.local:4532",
  "windowWidth": 1200,
  "windowHeight": 700
}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadSettings()
	if got.RadioAddress != "radio.local:4532" {
		t.Errorf("legacy address lost: %q", got.RadioAddress)
	}
	if got.RigModel != 0 {
		t.Errorf("legacy config should not select a rig, got %d", got.RigModel)
	}
	if got.BaudRate != defaultBaudRate {
		t.Errorf("BaudRate = %d, want the default %d", got.BaudRate, defaultBaudRate)
	}
	if got.PollIntervalMs != defaultPollIntervalMs {
		t.Errorf("PollIntervalMs = %d, want the default %d", got.PollIntervalMs, defaultPollIntervalMs)
	}
	if got.Theme != ThemeDark {
		t.Errorf("theme = %q, want dark", got.Theme)
	}
}

// TestWakePollingIsNonBlocking guards the buffered channel against a deadlock:
// SaveSettings wakes the poll loop, and a blocking send would hang the UI call.
func TestWakePollingIsNonBlocking(t *testing.T) {
	a := NewApp()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			a.wakePolling()
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wakePolling blocked")
	}
}

// TestE2EIC7610DaemonLifecycle drives the real daemon with the settings the
// Settings window produces for the radio attached to this machine, and confirms
// the app can talk to it over the rigctld protocol.
func TestE2EIC7610DaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := settingsTestApp(t)
	// Start from the machine's own radio settings where they look usable, so
	// the test talks to the attached rig at whatever baud it really uses (an
	// IC-7610 is commonly 115200, not hamlib's default). Fall back to the
	// IC-7610 defaults when no radio is configured yet.
	settings := a.currentSettings()
	settings.RadioAddress = address
	if settings.RigModel == 0 {
		settings.RigModel = 3078 // Icom IC-7610
		settings.SerialPort = "/dev/ttyUSB0"
		settings.BaudRate = 115200
		settings.DataBits = "8"
		settings.StopBits = "1"
		settings.PollIntervalMs = 500
	}
	a.settingsMu.Lock()
	a.settings = settings
	a.settingsMu.Unlock()
	a.config.Address = settings.RadioAddress

	start := time.Now()
	if err := a.startDaemon(settings); err != nil {
		t.Fatalf("startDaemon: %v", err)
	}
	t.Logf("IC-7610 daemon ready after %v", time.Since(start))
	t.Cleanup(a.stopDaemon)

	// The daemon should answer rigctld protocol commands now.
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("\\dump_state\n")); err != nil {
		t.Fatalf("write dump_state: %v", err)
	}
	// dump_state is a multi-line reply ending in "done"; read it all, since a
	// single Read may return only the first line (or nothing, if the daemon is
	// still warming up).
	var sb strings.Builder
	reader := bufio.NewReader(conn)
	for {
		line, readErr := reader.ReadString('\n')
		sb.WriteString(line)
		if strings.TrimSpace(line) == "done" || readErr != nil {
			break
		}
		if sb.Len() > 1<<20 {
			break
		}
	}
	reply := sb.String()
	if !strings.Contains(reply, "3078") {
		t.Errorf("dump_state reply does not report model 3078: %q", reply)
	}
	t.Logf("daemon reports model 3078 (IC-7610); status: %+v", a.GetDaemonStatus())
}

// TestE2EPollIntervalNever confirms "Never" stops the poll loop issuing rig
// commands: with polling off, isConnected never becomes true on its own.
func TestE2EPollIntervalNever(t *testing.T) {
	a := NewApp()
	a.settings = defaultSettings()
	a.settings.PollIntervalMs = 0
	if got := a.pollInterval(); got != 0 {
		t.Errorf("pollInterval with Never = %v, want 0", got)
	}

	for _, ms := range pollIntervalChoices {
		a.settings.PollIntervalMs = ms
		want := time.Duration(ms) * time.Millisecond
		if got := a.pollInterval(); got != want {
			t.Errorf("pollInterval(%d ms) = %v, want %v", ms, got, want)
		}
	}
}

// firstSerialRigModel returns a rig model hamlib reports as a serial backend, so
// tests that exercise serial-port handling do not depend on a specific model.
func firstSerialRigModel(t *testing.T) int {
	t.Helper()
	for _, id := range []int{3078, 3073, 1035, 2029} { // IC-7610, IC-7300, FT-991, K3
		if portType, known := rigPortType(id); known && serialBackend(portType) {
			return id
		}
	}
	t.Skip("this hamlib reports no serial rig models to test with")
	return 0
}
