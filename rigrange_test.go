package main

import "testing"

func TestParseFrequencyRange(t *testing.T) {
	// Real reply captured from rigctld with an Icom IC-7610 attached.
	dump := `1
3078
0
30000.000000 60000000.000000 0xc0401dbf -1 -1 0x16000000 0x3
0 0 0 0 0 0 0
1810000.000000 2000000.000000 0xc0001dbf 2000 100000 0x16000000 0x3
3500000.000000 3800000.000000 0xc0001dbf 2000 100000 0x16000000 0x3
28000000.000000 29700000.000000 0xc0001dbf 2000 100000 0x16000000 0x3
50000000.000000 54000000.000000 0xc0001dbf 2000 100000 0x16000000 0x3
0 0 0 0 0 0 0
0xc0401dbf 1
9999
done`
	r, err := parseFrequencyRange(dump)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.MinKHz != 30 {
		t.Errorf("MinKHz = %v, want 30", r.MinKHz)
	}
	if r.MaxKHz != 60000 {
		t.Errorf("MaxKHz = %v, want 60000", r.MaxKHz)
	}
	t.Logf("parsed range: %v kHz - %v kHz", r.MinKHz, r.MaxKHz)
}

func TestParseFrequencyRangeFromLiveRig(t *testing.T) {
	a := NewApp()
	a.config.Address = "localhost:4532"
	r, err := a.GetFrequencyRange()
	if err != nil {
		t.Skipf("rigctld not reachable: %v", err)
	}
	t.Logf("live rig range: %v kHz - %v kHz", r.MinKHz, r.MaxKHz)
	if r.MinKHz <= 0 || r.MaxKHz <= r.MinKHz {
		t.Fatalf("implausible range: %+v", r)
	}
}

func TestParseFrequencyRangeEmpty(t *testing.T) {
	if _, err := parseFrequencyRange("garbage"); err == nil {
		t.Error("expected error for unparseable input")
	}
}
