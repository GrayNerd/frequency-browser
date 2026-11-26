package main

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestSendRigCommandRetriesAfterReset proves the retry really works: the first
// connection to the test server is closed immediately (a reset from the client's
// point of view) and the command must still succeed on a later attempt.
func TestSendRigCommandRetriesAfterReset(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	connections := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections++
			first := connections == 1
			mu.Unlock()

			if first {
				conn.Close() // simulate the startup reset
				continue
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				c.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := c.Read(buf); err != nil {
					return
				}
				c.Write([]byte("14074000\n"))
			}(conn)
		}
	}()

	a := NewApp()
	a.config.Address = ln.Addr().String()

	start := time.Now()
	resp, err := a.sendRigCommand("f")
	if err != nil {
		t.Fatalf("sendRigCommand should have retried past the reset: %v", err)
	}
	if resp != "14074000" {
		t.Errorf("response = %q, want 14074000", resp)
	}
	mu.Lock()
	n := connections
	mu.Unlock()
	if n < 2 {
		t.Errorf("expected at least 2 connection attempts, got %d", n)
	}
	t.Logf("recovered after %d attempts in %v", n, time.Since(start))
}

// TestSendRigCommandDoesNotRetryRigErrors confirms a real radio rejection is
// returned immediately rather than retried.
func TestSendRigCommandDoesNotRetryRigErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	connections := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections++
			mu.Unlock()
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				c.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := c.Read(buf); err != nil {
					return
				}
				c.Write([]byte("RPRT -8\n"))
			}(conn)
		}
	}()

	a := NewApp()
	a.config.Address = ln.Addr().String()

	_, err = a.sendRigCommand("F 100")
	if err == nil {
		t.Fatal("expected the rig error to be returned")
	}
	mu.Lock()
	n := connections
	mu.Unlock()
	if n != 1 {
		t.Errorf("rig error was retried (%d connections); it should be returned at once", n)
	}
}
