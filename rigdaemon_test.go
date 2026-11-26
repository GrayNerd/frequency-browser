package main

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDaemonArgs(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     []string
		wantErr  bool
	}{
		{
			name: "ic7610 with every option set",
			settings: Settings{
				RadioAddress: defaultRadioAddress,
				RigModel:     3078,
				SerialPort:   "/dev/ttyUSB0",
				BaudRate:     19200,
				DataBits:     "8",
				StopBits:     "1",
				Handshake:    "Hardware",
				DTR:          "High",
				RTS:          "Low",
			},
			want: []string{
				"-m", "3078",
				"-r", "/dev/ttyUSB0",
				"-s", "19200",
				"-C", "data_bits=8,stop_bits=1,serial_handshake=Hardware,dtr_state=ON,rts_state=OFF",
				"-T", "127.0.0.1", "-t", "4532",
			},
		},
		{
			name: "defaults omit the serial parameters",
			settings: Settings{
				RadioAddress: defaultRadioAddress,
				RigModel:     3078,
				SerialPort:   "/dev/ttyUSB0",
				BaudRate:     19200,
			},
			want: []string{
				"-m", "3078",
				"-r", "/dev/ttyUSB0",
				"-s", "19200",
				"-T", "127.0.0.1", "-t", "4532",
			},
		},
		{
			// A network backend gets no serial options at all: its -r is the
			// remote daemon's address, not a device file. Whether that address
			// is required depends on hamlib reporting the backend as a network
			// link, which is covered by netaddress_test.go.
			name: "no serial port: no -s or -C are passed",
			settings: Settings{
				RadioAddress:  defaultRadioAddress,
				RigModel:      2,
				RemoteAddress: "127.0.0.1:4532",
				BaudRate:      9600,
				Handshake:     "Hardware",
			},
			want: nil, // checked separately: the -r value is backend-dependent
		},
		{
			name:     "no rig selected is an error",
			settings: Settings{RadioAddress: defaultRadioAddress},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := daemonArgs(tc.settings)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("daemonArgs: %v", err)
			}
			if tc.want == nil {
				// Shape-only case: no serial options, whatever -r looks like.
				joined := strings.Join(got, " ")
				for _, unwanted := range []string{"-s ", "-C "} {
					if strings.Contains(joined, unwanted) {
						t.Errorf("args contain %q with no serial port: %v", unwanted, got)
					}
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("args = %v\nwant   %v", got, tc.want)
			}
		})
	}
}

func TestControlLineValue(t *testing.T) {
	cases := map[string]string{"High": "ON", "Low": "OFF", "": "", "bogus": ""}
	for in, want := range cases {
		if got := controlLineValue(in); got != want {
			t.Errorf("controlLineValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct{ in, host, port string }{
		{"", "127.0.0.1", "4532"},
		{"localhost:4532", "127.0.0.1", "4532"},
		{"127.0.0.1:4533", "127.0.0.1", "4533"},
		{"garbage", "127.0.0.1", "4532"},
	}
	for _, tc := range tests {
		host, port := splitHostPort(tc.in)
		if host != tc.host || port != tc.port {
			t.Errorf("splitHostPort(%q) = %q,%q, want %q,%q", tc.in, host, port, tc.host, tc.port)
		}
	}
}

// TestProbeDaemon uses a closed listener address: nothing can be listening there
// after Close, so the probe must report false.
func TestProbeDaemonClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := NewApp()
	if a.probeDaemon(address) {
		t.Errorf("probeDaemon(%q) = true with nothing listening", address)
	}
}

// TestStartStopDaemon runs the real rigctld binary against the Dummy rig, so it
// is skipped in -short runs and when hamlib is not installed.
func TestStartStopDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live daemon test in -short mode")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := ln.Addr().String()
	ln.Close() // free the port for rigctld to bind

	a := NewApp()
	settings := Settings{RigModel: 1, RadioAddress: address, PollIntervalMs: defaultPollIntervalMs} // 1 = Dummy

	if err := a.startDaemon(settings); err != nil {
		t.Fatalf("startDaemon: %v", err)
	}
	t.Cleanup(a.stopDaemon)

	if !a.probeDaemon(address) {
		t.Fatal("daemon is not reachable after startDaemon returned")
	}
	if !a.ownsDaemon() {
		t.Error("ownsDaemon() = false, want true for a daemon we started")
	}
	status := a.GetDaemonStatus()
	if !status.Running || !status.Owned || status.PID <= 0 {
		t.Errorf("unexpected status: %+v", status)
	}

	a.stopDaemon()
	if a.probeDaemon(address) {
		t.Error("daemon still reachable after stopDaemon")
	}
	if a.ownsDaemon() {
		t.Error("ownsDaemon() = true after stopDaemon")
	}
}

// TestStartDaemonRejectsMissingSerialPort covers the pre-flight check: a port
// that does not exist is reported immediately instead of after a long wait.
func TestStartDaemonRejectsMissingSerialPort(t *testing.T) {
	if testing.Short() {
		t.Skip("live daemon test")
	}

	a := NewApp()
	start := time.Now()
	err := a.startDaemon(Settings{
		RigModel:     3078,
		RadioAddress: "127.0.0.1:1",
		SerialPort:   "/dev/ttyDOESNOTEXIST",
	})
	if err == nil {
		t.Fatal("expected an error for a missing serial port")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("missing port took %v to report, want an immediate failure", elapsed)
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should name the missing port, got %v", err)
	}
	t.Logf("missing port error: %v", err)
}

// TestStartDaemonRejectsBadModel surfaces rigctld's own error text, which is
// what the Settings window shows instead of a generic timeout.
func TestStartDaemonRejectsBadModel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live daemon test in -short mode")
	}
	if _, err := rigctldPath(); err != nil {
		t.Skipf("rigctld unavailable: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	a := NewApp()
	err = a.startDaemon(Settings{RigModel: 999999, RadioAddress: address})
	if err == nil {
		t.Fatal("expected an error for an unknown rig model")
	}
	t.Logf("startDaemon error: %v", err)
	if a.ownsDaemon() {
		t.Error("a failed start must not be recorded as an owned daemon")
	}
}
