package main

import (
	"net"
	"os/exec"
	"testing"
	"time"
)

// TestExternalDaemonIsAdoptedNotOwned is the core safety rule: a daemon that was
// already running when the app starts must be used but never terminated.
func TestExternalDaemonIsAdoptedNotOwned(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	bin, err := rigctldPath()
	if err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	// Stand in for a daemon the user started themselves (systemd, shell, ...).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	external := exec.Command(bin, "-m", "1", "-T", "127.0.0.1", "-t", portOf(address))
	if err := external.Start(); err != nil {
		t.Fatalf("start external daemon: %v", err)
	}
	t.Cleanup(func() {
		if external.Process != nil {
			external.Process.Kill()
			external.Wait()
		}
	})

	a := NewApp()
	deadline := time.Now().Add(10 * time.Second)
	for !a.probeDaemon(address) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !a.probeDaemon(address) {
		t.Fatalf("external daemon never came up on %s", address)
	}

	a.settings.RadioAddress = address
	if err := a.ensureDaemon(); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
	}

	if a.ownsDaemon() {
		t.Error("an external daemon must not be treated as owned")
	}
	status := a.GetDaemonStatus()
	if !status.Running {
		t.Errorf("status.Running = false for a live external daemon: %+v", status)
	}
	if status.Owned {
		t.Errorf("status.Owned = true for an external daemon")
	}
	if status.Note == "" {
		t.Error("expected a note explaining the external daemon is left alone")
	}

	// Exiting the app must leave it running.
	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	if !a.probeDaemon(address) {
		t.Fatal("ServiceShutdown killed a daemon this app did not start")
	}

	// Saving new radio settings must not kill it either.
	saved := a.currentSettings()
	saved.RigModel = 3078
	if err := a.restartDaemon(saved); err != nil {
		t.Fatalf("restartDaemon: %v", err)
	}
	if !a.probeDaemon(address) {
		t.Fatal("restartDaemon killed an external daemon")
	}
	t.Logf("note for external daemon: %s", a.GetDaemonStatus().Note)
}

func portOf(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "4532"
	}
	return port
}
