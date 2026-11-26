package main

import (
	"strings"
	"testing"
)

// capsSample is the shape of `rigctld -m <id> -u` output; only the Port type
// line matters here.
const capsSample = `Caps dump for model: 3078
Model name:	IC-7610
Mfg name:	Icom
Backend version:	20230109.11
Backend copyright:	LGPL
Backend status:	Stable
Rig type:	Other Receiver Transmitter Transceiver 
PTT type:	Rig capable
DCD type:	Rig capable
Port type:	RS-232
Serial speed: 300..115200 bauds, 8N1 
`

func TestParsePortType(t *testing.T) {
	tests := []struct {
		name string
		caps string
		want string
	}{
		{"rs-232", capsSample, "RS-232"},
		{"network", "Port type:\tNetwork link\n", "Network link"},
		{"none", "Port type:\tNone\n", "None"},
		{"usb", "Port type:\tUSB\n", "USB"},
		{"missing", "Model name:\tX\n", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePortType(tc.caps); got != tc.want {
				t.Errorf("parsePortType = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSerialBackend(t *testing.T) {
	// Unknown (empty) must be allowed, so a hamlib that does not report
	// capabilities never blocks a real serial rig.
	serial := []string{"RS-232", "USB", "Parallel", ""}
	for _, portType := range serial {
		if !serialBackend(portType) {
			t.Errorf("serialBackend(%q) = false, want true", portType)
		}
	}
	notSerial := []string{"Network link", "None"}
	for _, portType := range notSerial {
		if serialBackend(portType) {
			t.Errorf("serialBackend(%q) = true, want false", portType)
		}
	}
}

// TestDaemonArgsRejectsSerialPortOnNetworkBackend covers the reported bug: a
// serial port on a network backend produced a daemon that bound its socket and
// then never answered, surfacing as "i/o timeout" on every tune.
func TestDaemonArgsRejectsSerialPortOnNetworkBackend(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	// Model 2 is "NET rigctl": Port type "Network link".
	portType, known := rigPortType(2)
	if !known {
		t.Skip("this hamlib does not report capabilities for model 2")
	}
	if serialBackend(portType) {
		t.Fatalf("model 2 reports %q, which this test assumed was non-serial", portType)
	}

	_, err := daemonArgs(Settings{
		RadioAddress: defaultRadioAddress,
		RigModel:     2,
		SerialPort:   "/dev/ttyS0",
		BaudRate:     19200,
	})
	if err == nil {
		t.Fatal("daemonArgs accepted a serial port on a network backend")
	}
	if !strings.Contains(err.Error(), "no serial port") {
		t.Errorf("error should explain the backend takes no serial port, got: %v", err)
	}
	t.Logf("rejected as expected: %v", err)
}

// TestDaemonArgsAllowsSerialPortOnRealRig is the counterpart: the attached
// IC-7610 is an RS-232 backend, so its serial port must be accepted.
func TestDaemonArgsAllowsSerialPortOnRealRig(t *testing.T) {
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}
	portType, known := rigPortType(3078) // Icom IC-7610
	if !known {
		t.Skip("this hamlib does not report capabilities for model 3078")
	}
	if !serialBackend(portType) {
		t.Fatalf("model 3078 reports %q, expected a serial backend", portType)
	}

	args, err := daemonArgs(Settings{
		RadioAddress: defaultRadioAddress,
		RigModel:     3078,
		SerialPort:   "/dev/ttyUSB0",
		BaudRate:     115200,
	})
	if err != nil {
		t.Fatalf("daemonArgs rejected the IC-7610's serial port: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-m 3078") || !strings.Contains(joined, "-r /dev/ttyUSB0") {
		t.Errorf("unexpected args: %v", args)
	}
}

// TestDaemonArgsAllowsRigWithoutSerialPort keeps the network/virtual case usable:
// no serial port means no -r/-s/-C at all, which hamlib accepts.
func TestDaemonArgsAllowsRigWithoutSerialPort(t *testing.T) {
	args, err := daemonArgs(Settings{
		RadioAddress:  defaultRadioAddress,
		RigModel:      2,
		RemoteAddress: "127.0.0.1:4532",
		BaudRate:      19200,
	})
	if err != nil {
		t.Fatalf("daemonArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	// -s and -C are serial-only, so a network rig must never get them. -r is
	// expected here: for this backend it is the remote daemon's address.
	for _, unwanted := range []string{"-s", "-C"} {
		if strings.Contains(joined, unwanted+" ") {
			t.Errorf("args contain %q with no serial port set: %v", unwanted, args)
		}
	}
	if !strings.Contains(joined, "-r 127.0.0.1:4532") {
		t.Errorf("network backend should pass its address as -r: %v", args)
	}
}
