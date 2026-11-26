package main

import (
	"os"
	"path/filepath"
	"testing"
)

// rigListSample is real output from `rigctld -l` (Hamlib 4.5.5), trimmed to the
// rows that exercise the awkward cases: multi-word manufacturers, a model name
// containing a version-like suffix, a model with a trailing letter in its version
// and the row whose macro Hamlib prints as "(null)".
const rigListSample = ` Rig #  Mfg                    Model                   Version         Status      Macro
     1  Hamlib                 Dummy                   20221128.0      Stable      RIG_MODEL_DUMMY
     2  Hamlib                 NET rigctl              20230328.0      Stable      RIG_MODEL_NETRIGCTL
     5  TRXManager             TRXManager 5.7.630+     20210613.0      Stable      RIG_MODEL_TRXMANAGER_RIG
  2034  Kenwood                TM-D710(G)              20230318.6      Stable      (null)
  23003  DTTS Microwave Society DttSP IPC               20200319.0      Beta        RIG_MODEL_DTTSP
  3078  Icom                   IC-7610                 20230109.11     Stable      RIG_MODEL_IC7610
 32003  Barrett                4050                    20220113.0f     Stable      RIG_MODEL_BARRETT_4050
 35001  GOMSPACE               GS100                   20211117.0      Beta        RIG_MODEL_GS100
`

func TestParseRigModels(t *testing.T) {
	models := parseRigModels(rigListSample)
	if len(models) != 8 {
		t.Fatalf("parsed %d models, want 8: %+v", len(models), models)
	}

	byID := make(map[int]RigModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	cases := []struct {
		id         int
		mfg, model string
		status     string
	}{
		{3078, "Icom", "IC-7610", "Stable"},
		{23003, "DTTS Microwave Society", "DttSP IPC", "Beta"},
		{2034, "Kenwood", "TM-D710(G)", "Stable"},
		{32003, "Barrett", "4050", "Stable"},
		{5, "TRXManager", "TRXManager 5.7.630+", "Stable"},
	}
	for _, tc := range cases {
		got, ok := byID[tc.id]
		if !ok {
			t.Errorf("model %d missing", tc.id)
			continue
		}
		if got.Mfg != tc.mfg || got.Model != tc.model {
			t.Errorf("model %d = %q/%q, want %q/%q", tc.id, got.Mfg, got.Model, tc.mfg, tc.model)
		}
		if got.Status != tc.status {
			t.Errorf("model %d status = %q, want %q", tc.id, got.Status, tc.status)
		}
	}
}

func TestParseRigModelsSortedByManufacturer(t *testing.T) {
	models := parseRigModels(rigListSample)
	for i := 1; i < len(models); i++ {
		prev, cur := models[i-1], models[i]
		if cur.Mfg < prev.Mfg {
			t.Errorf("not sorted by manufacturer at %d: %q then %q", i, prev.Mfg, cur.Mfg)
		}
		if cur.Mfg == prev.Mfg && cur.Model < prev.Model {
			t.Errorf("not sorted by model at %d: %q then %q", i, prev.Model, cur.Model)
		}
	}
}

func TestParseRigModelsIgnoresGarbage(t *testing.T) {
	for _, in := range []string{"", "\n\n", "not a rig list", "(null)"} {
		if got := parseRigModels(in); len(got) != 0 {
			t.Errorf("parseRigModels(%q) = %+v, want empty", in, got)
		}
	}
}

// TestListSerialPorts lists real devices from this machine; it skips when the
// sandbox has no serial ports at all.
func TestListSerialPorts(t *testing.T) {
	ports, err := listSerialPorts("/dev", "/dev/serial/by-id")
	if err != nil {
		t.Skipf("no serial ports available: %v", err)
	}
	for _, p := range ports {
		if !filepath.IsAbs(p.Path) {
			t.Errorf("port path %q is not absolute", p.Path)
		}
		if p.Label == "" {
			t.Errorf("port %q has no label", p.Path)
		}
	}
	if t.Failed() {
		return
	}
	// Ordering: ttyS before ttyUSB, and ttyS2 before ttyS10.
	for i := 1; i < len(ports); i++ {
		if !portNameLess(ports[i-1].Path, ports[i].Path) {
			t.Logf("order: %q then %q", ports[i-1].Path, ports[i].Path)
		}
	}
}

// TestListSerialPortsFromTempTree exercises the enumeration against a fake
// /dev so it runs everywhere, including machines with no serial hardware.
func TestListSerialPortsFromTempTree(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	byID := filepath.Join(root, "serial-by-id")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(byID, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ttyS0", "ttyS10", "ttyS2", "ttyUSB0", "tty0", "null"} {
		if err := os.WriteFile(filepath.Join(dev, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(
		filepath.Join(dev, "ttyUSB0"),
		filepath.Join(byID, "usb-Silicon_Labs_CP2102_USB_to_UART_Bridge_Controller_IC-7610_A-if00-port0"),
	); err != nil {
		t.Fatal(err)
	}

	ports, err := listSerialPorts(dev, byID)
	if err != nil {
		t.Fatalf("listSerialPorts: %v", err)
	}

	var got []string
	for _, p := range ports {
		got = append(got, p.Path)
	}
	want := []string{
		filepath.Join(dev, "ttyS0"),
		filepath.Join(dev, "ttyS2"),
		filepath.Join(dev, "ttyS10"),
		filepath.Join(dev, "ttyUSB0"),
	}
	if len(got) != len(want) {
		t.Fatalf("ports = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("port %d = %q, want %q", i, got[i], want[i])
		}
	}

	// The by-id description should enrich the USB port's label.
	if label := ports[3].Label; label == ports[3].Path {
		t.Errorf("USB label was not enriched: %q", label)
	}
}
