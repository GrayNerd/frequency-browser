package main

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// TestE2ENetworkBackendDrivesRemoteDaemon is the end-to-end proof for the new
// Network Address field: our daemon is started in NET rigctl mode pointed at a
// second daemon, and a tune issued to ours must reach that second one.
func TestE2ENetworkBackendDrivesRemoteDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link")
	}

	// The "remote rig": a Dummy daemon standing in for a rig on the network.
	remoteAddr := freeAddr(t)
	remote := NewApp()
	if err := remote.startDaemon(Settings{
		RadioAddress: remoteAddr,
		RigModel:     1, // Dummy
	}); err != nil {
		t.Fatalf("could not start the stand-in remote daemon: %v", err)
	}
	t.Cleanup(remote.stopDaemon)

	// Our daemon, in NET rigctl mode, pointed at it.
	localAddr := freeAddr(t)
	a := NewApp()
	if err := a.startDaemon(Settings{
		RadioAddress:  localAddr,
		RigModel:      2, // hamlib NET rigctl
		RemoteAddress: remoteAddr,
	}); err != nil {
		t.Fatalf("startDaemon(network): %v", err)
	}
	t.Cleanup(a.stopDaemon)
	a.config.Address = localAddr

	// Commands through our daemon reach the remote one.
	if err := a.SetFrequency(7074); err != nil {
		t.Fatalf("SetFrequency through the network backend: %v", err)
	}
	got, err := a.getFrequency()
	if err != nil {
		t.Fatalf("getFrequency through the network backend: %v", err)
	}
	if diff := got - 7074; diff > 0.01 || diff < -0.01 {
		t.Errorf("frequency = %v kHz, want 7074", got)
	}

	// And the remote daemon itself shows the new frequency.
	conn, err := net.DialTimeout("tcp", remoteAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial remote: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("f\n"))
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	if !strings.HasPrefix(strings.TrimSpace(reply), "7074000") {
		t.Errorf("remote daemon reports %q, want 7074000", strings.TrimSpace(reply))
	}
	t.Logf("network backend drove the remote daemon to %s Hz", strings.TrimSpace(reply))
}

// freeAddr reserves and releases a loopback port, returning host:port.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}
