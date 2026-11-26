package main

import (
	"strings"
	"testing"
)

// TestDaemonArgsNetworkBackendUsesRemoteAddress is the counterpart to the
// serial-port rejection: hamlib's NET rigctl backend takes the address of the
// daemon it controls as its -r argument.
func TestDaemonArgsNetworkBackendUsesRemoteAddress(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link (got %q)", portType)
	}

	args, err := daemonArgs(Settings{
		RadioAddress:  defaultRadioAddress,
		RigModel:      2, // hamlib NET rigctl
		RemoteAddress: "192.168.1.50:4532",
	})
	if err != nil {
		t.Fatalf("daemonArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-r 192.168.1.50:4532") {
		t.Errorf("remote address was not passed as -r: %v", args)
	}
	// A network backend has no serial settings to pass.
	for _, unwanted := range []string{"-s ", "-C "} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("network backend should not get %q: %v", unwanted, args)
		}
	}
}

// TestDaemonArgsNetworkBackendRequiresAddress keeps a half-configured network
// rig from launching a daemon that cannot do anything.
func TestDaemonArgsNetworkBackendRequiresAddress(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link")
	}

	_, err := daemonArgs(Settings{RadioAddress: defaultRadioAddress, RigModel: 2})
	if err == nil {
		t.Fatal("expected an error when no remote address is given")
	}
	if !strings.Contains(err.Error(), "host:port") {
		t.Errorf("error should ask for host:port, got: %v", err)
	}
}

// TestDaemonArgsNetworkBackendRejectsBareHost checks the address is validated
// before rigctld is started, not after the user has waited for a timeout.
func TestDaemonArgsNetworkBackendRejectsBareHost(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link")
	}

	for _, bad := range []string{"192.168.1.50", "just-a-name", "host:notaport"} {
		_, err := daemonArgs(Settings{
			RadioAddress:  defaultRadioAddress,
			RigModel:      2,
			RemoteAddress: bad,
		})
		if err == nil {
			t.Errorf("address %q was accepted; it is not host:port", bad)
		}
	}
}

// TestNetworkBackendIgnoresStraySerialPort makes sure a leftover serial port in
// an old config cannot reach a network rig's command line.
func TestNetworkBackendIgnoresStraySerialPort(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	if portType, known := rigPortType(2); !known || portType != "Network link" {
		t.Skipf("this hamlib does not report model 2 as a network link")
	}

	_, err := daemonArgs(Settings{
		RadioAddress:  defaultRadioAddress,
		RigModel:      2,
		RemoteAddress: "127.0.0.1:4532",
		SerialPort:    "/dev/ttyUSB0",
	})
	if err == nil {
		t.Fatal("a serial port on a network backend should be refused")
	}
	if !strings.Contains(err.Error(), "no serial port") {
		t.Errorf("error should explain the backend takes no serial port, got: %v", err)
	}
}

// TestRemoteAddressPersists covers the storage side of the new field.
func TestRemoteAddressPersists(t *testing.T) {
	a := settingsTestApp(t)

	s := a.currentSettings()
	s.RigModel = 2
	s.RemoteAddress = "192.168.1.50:4532"
	s.SerialPort = ""
	if err := saveSettings(s); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	got := loadSettings()
	if got.RemoteAddress != "192.168.1.50:4532" {
		t.Errorf("persisted RemoteAddress = %q", got.RemoteAddress)
	}

	// Whitespace is trimmed so a pasted address still validates.
	s.RemoteAddress = "  10.0.0.7:4532  "
	if err := saveSettings(s); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	if got := loadSettings(); got.RemoteAddress != "10.0.0.7:4532" {
		t.Errorf("RemoteAddress was not trimmed, got %q", got.RemoteAddress)
	}
}

// TestRadioChangedTracksRemoteAddress confirms editing the network address
// restarts the daemon, since it is part of its command line.
func TestRadioChangedTracksRemoteAddress(t *testing.T) {
	base := defaultSettings()
	changed := base
	changed.RemoteAddress = "192.168.1.50:4532"
	if !radioChanged(base, changed) {
		t.Error("changing RemoteAddress was not treated as a radio change")
	}
}

// TestNoPortBackendNeedsNoAddress covers hamlib's "None" backends (Dummy, Dummy
// No VFO, and similar). They have no port at all, so they must not be asked for
// a remote address and must not receive serial options either.
func TestNoPortBackendNeedsNoAddress(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	portType, known := rigPortType(1) // Hamlib Dummy
	if !known || portType != "None" {
		t.Skipf("this hamlib does not report model 1 as a portless backend (got %q)", portType)
	}

	args, err := daemonArgs(Settings{RadioAddress: defaultRadioAddress, RigModel: 1})
	if err != nil {
		t.Fatalf("a portless backend must need no address: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, unwanted := range []string{"-r ", "-s ", "-C "} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("portless backend got %q: %v", unwanted, args)
		}
	}
}

// TestNoPortBackendIgnoresSerialSettings makes sure leftovers from a serial rig
// do not leak into a portless backend's command line.
func TestNoPortBackendIgnoresSerialSettings(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	portType, known := rigPortType(1)
	if !known || portType != "None" {
		t.Skipf("this hamlib does not report model 1 as a portless backend")
	}

	args, err := daemonArgs(Settings{
		RadioAddress: defaultRadioAddress,
		RigModel:     1,
		SerialPort:   "/dev/ttyUSB0",
		BaudRate:     115200,
		Handshake:    "Hardware",
	})
	if err != nil {
		t.Fatalf("daemonArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, unwanted := range []string{"-r ", "-s ", "-C "} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("portless backend got %q: %v", unwanted, args)
		}
	}
}
