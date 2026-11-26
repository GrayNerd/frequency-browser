package main

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// TestDumpStateWithoutTerminatorIsFast is the regression test for the startup
// delay: hamlib 4.5.x ends a dump_state reply after the capability masks with no
// "done" marker, and the reader used to block until its full deadline waiting
// for one. Here a stand-in daemon sends a terminator-less reply and the call must
// return promptly with all of it.
func TestDumpStateWithoutTerminatorIsFast(t *testing.T) {
	reply := buildDumpStateReply()
	address := serveOnce(t, reply)

	a := NewApp()
	a.config.Address = address

	start := time.Now()
	dump, err := a.queryDumpState()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("queryDumpState: %v", err)
	}

	if !strings.Contains(dump, "3078") {
		t.Errorf("dump is missing the model line: %q", dump)
	}
	if !strings.HasSuffix(dump, "0x35\n") {
		t.Errorf("dump should end with the capability masks, got %q", tailOf(dump, 60))
	}

	// The old behaviour took the whole total timeout (5s+, now 8s). Anything
	// near that means the terminator wait came back.
	limit := dumpStateIdleTimeout * 6
	if elapsed > limit {
		t.Errorf("queryDumpState took %v, want under %v: it is probably waiting for a \ndone\" marker again", elapsed, limit)
	}
	t.Logf("terminator-less dump_state returned %d bytes in %v", len(dump), elapsed.Round(time.Millisecond))
}

// TestDumpStateHonoursDoneMarker keeps compatibility with hamlib versions that
// do terminate the reply, so the faster path does not break them.
func TestDumpStateHonoursDoneMarker(t *testing.T) {
	reply := strings.Replace(buildDumpStateReply(), "0x35\n0x35\n", "0x35\n0x35\ndone\n", 1)
	address := serveOnce(t, reply)

	a := NewApp()
	a.config.Address = address

	dump, err := a.queryDumpState()
	if err != nil {
		t.Fatalf("queryDumpState: %v", err)
	}
	if !strings.Contains(dump, "done") {
		t.Errorf("the done marker should be included in the collected reply: %q", tailOf(dump, 60))
	}
}

// TestDumpStateRangeStillParses proves the faster reader does not truncate the
// reply: the range must still come out of the same bytes.
func TestDumpStateRangeStillParses(t *testing.T) {
	address := serveOnce(t, buildDumpStateReply())

	a := NewApp()
	a.config.Address = address

	r, err := a.GetFrequencyRange()
	if err != nil {
		t.Fatalf("GetFrequencyRange: %v", err)
	}
	if r.MinKHz != 30 || r.MaxKHz != 60000 {
		t.Errorf("range = %v..%v kHz, want 30..60000", r.MinKHz, r.MaxKHz)
	}
}

// TestDumpStateEmptyReplyIsAnError guards the case where the daemon answers
// nothing at all.
func TestDumpStateEmptyReplyIsAnError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	a := NewApp()
	a.config.Address = ln.Addr().String()

	oldTotal := dumpStateTotalTimeout
	dumpStateTotalTimeout = 300 * time.Millisecond
	t.Cleanup(func() { dumpStateTotalTimeout = oldTotal })

	if _, err := a.queryDumpState(); err == nil {
		t.Error("expected an error when the daemon sends nothing")
	}
}

// serveOnce starts a listener that answers one connection with reply, then
// returns its address.
func serveOnce(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read the request, then reply in one write, like rigctld does.
				bufio.NewReader(c).ReadString('\n')
				c.Write([]byte(reply))
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// buildDumpStateReply mirrors a real hamlib 4.5.x reply: protocol version, model,
// trx, receive ranges, then the capability masks - and deliberately no "done".
func buildDumpStateReply() string {
	var sb strings.Builder
	sb.WriteString("1\n")
	sb.WriteString("3078\n")
	sb.WriteString("0\n")
	sb.WriteString("30000.000000 60000000.000000 0xc0401dbf -1 -1 0x16000000 0x3\n")
	sb.WriteString("0 0 0 0 0 0 0\n")
	sb.WriteString("1810000.000000 2000000.000000 0xc0001dbf 2000 100000 0x16000000 0x3\n")
	sb.WriteString("0 0 0 0 0 0 0\n")
	sb.WriteString("0x9c20c1013bfe\n")
	sb.WriteString("0x35\n0x35\n")
	return sb.String()
}

// tailOf returns at most the last n characters of s.
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
