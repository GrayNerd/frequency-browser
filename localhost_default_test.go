package main

import (
	"net"
	"strings"
	"testing"
)

// TestDefaultRemoteAddressIsLocalhost pins the default offered for a network
// backend. It must be the name "localhost", not this machine's IP: a network rig
// normally runs on the same host, and the name keeps working when the address
// changes (DHCP, VPN, a different network).
func TestDefaultRemoteAddressIsLocalhost(t *testing.T) {
	if !strings.HasPrefix(defaultRemoteAddress, "localhost:") {
		t.Errorf("defaultRemoteAddress = %q, want it to start with \"localhost:\"", defaultRemoteAddress)
	}

	host, port, err := net.SplitHostPort(defaultRemoteAddress)
	if err != nil {
		t.Fatalf("defaultRemoteAddress %q is not host:port: %v", defaultRemoteAddress, err)
	}
	if host != "localhost" {
		t.Errorf("host = %q, want \"localhost\"", host)
	}
	if port == "" {
		t.Error("default remote address has no port")
	}

	// A numeric address here would be this machine's own IP after resolution,
	// which is exactly what must not be baked in.
	if ip := net.ParseIP(host); ip != nil {
		t.Errorf("defaultRemoteAddress uses an IP (%s) instead of the localhost name", ip)
	}
}

// TestDefaultRemoteAddressIsValid checks the default passes the same validation
// applied to a typed address, so choosing a network rig does not open the dialog
// in a state that Save would reject.
func TestDefaultRemoteAddressIsValid(t *testing.T) {
	if err := validateHostPort(defaultRemoteAddress); err != nil {
		t.Errorf("validateHostPort(%q) = %v, want nil", defaultRemoteAddress, err)
	}
}

// TestNetworkDaemonArgsUseLocalhostDefault makes sure a freshly configured
// network rig produces a usable command line with no user input at all.
func TestNetworkDaemonArgsUseLocalhostDefault(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link")
	}

	s := defaultSettings()
	s.RigModel = 2 // hamlib NET rigctl, as chosen in the Rig dropdown

	args, err := daemonArgs(s)
	if err != nil {
		t.Fatalf("daemonArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-r "+defaultRemoteAddress) {
		t.Errorf("args should point at %s, got %v", defaultRemoteAddress, args)
	}
	t.Logf("network args: %v", args)
}

// TestFreshDefaultsPointRemoteAtLocalhost guards the wiring: a brand new config
// must already carry the localhost default, so the field is populated the first
// time a network rig is selected.
func TestFreshDefaultsPointRemoteAtLocalhost(t *testing.T) {
	s := defaultSettings()
	if s.RemoteAddress != defaultRemoteAddress {
		t.Errorf("defaultSettings().RemoteAddress = %q, want %q", s.RemoteAddress, defaultRemoteAddress)
	}

	// It must survive normalisation unchanged.
	if got := s.normalized().RemoteAddress; got != defaultRemoteAddress {
		t.Errorf("normalized RemoteAddress = %q, want %q", got, defaultRemoteAddress)
	}
}
