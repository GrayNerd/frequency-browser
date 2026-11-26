package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// dumpStateIdleTimeout is how long to wait for more of a dump_state reply before
// deciding it is complete. hamlib 4.5.x sends no terminator, so completion is
// detected by the stream going quiet.
//
// Measured on an IC-7610: the entire reply (1878 bytes, 62 lines) arrives in a
// single TCP segment, so this only has to absorb scheduling noise, not a slow
// radio. It stays well under that segment's arrival time to keep startup snappy
// while leaving a wide margin.
var dumpStateIdleTimeout = 150 * time.Millisecond

// dumpStateTotalTimeout caps the whole exchange, so a daemon that accepts the
// command but never answers cannot block the caller.
var dumpStateTotalTimeout = 8 * time.Second

// FrequencyRange describes the span of frequencies the connected rig can tune,
// expressed in kHz to match the rest of the application.
type FrequencyRange struct {
	MinKHz float64 `json:"minKhz"`
	MaxKHz float64 `json:"maxKhz"`
}

// queryDumpState sends rigctld's \dump_state command and returns the full
// multi-line reply. Unlike sendRigCommand this reads until the trailing
// "done" marker because the response spans many lines.
func (a *App) queryDumpState() (string, error) {
	address := a.config.Address
	if address == "" {
		address = "localhost:4532"
	}

	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to connect to rigctld: %v", err)
	}
	defer conn.Close()

	// Overall cap, so a wedged daemon cannot hang the caller forever.
	overallDeadline := time.Now().Add(dumpStateTotalTimeout)
	if err := conn.SetDeadline(overallDeadline); err != nil {
		return "", err
	}

	if _, err := conn.Write([]byte("\\dump_state\n")); err != nil {
		return "", fmt.Errorf("failed to send dump_state: %v", err)
	}

	// hamlib 4.5.x does not end the reply with a marker: it stops after the
	// capability masks, with no trailing "done" line. Waiting for one means
	// blocking until the deadline above expires on every single start, which is
	// the delay this function used to add to the window opening. Older hamlib
	// did print "done", so that is still honoured, and otherwise the reply is
	// considered complete once the daemon pauses: a read that times out after
	// receiving at least one line ends the loop immediately.
	var sb strings.Builder
	reader := bufio.NewReader(conn)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(dumpStateIdleTimeout)); err != nil {
			break
		}
		line, readErr := reader.ReadString('\n')
		if line != "" {
			sb.WriteString(line)
			if strings.TrimSpace(line) == "done" {
				break
			}
		}
		if readErr != nil {
			if sb.Len() > 0 {
				// Idle after data, or the overall deadline: we have the reply.
				break
			}
			// Nothing at all yet: keep trying until the overall deadline.
			if ne, ok := readErr.(net.Error); ok && ne.Timeout() && time.Now().Before(overallDeadline) {
				continue
			}
			break
		}
		if sb.Len() > 1<<20 {
			break
		}
	}

	if sb.Len() == 0 {
		return "", fmt.Errorf("empty response to dump_state")
	}
	return sb.String(), nil
}

// parseFrequencyRange extracts the union of the rig's receive ranges from a
// \dump_state reply.
//
// hamlib's reply layout is:
//
//	protocol_version
//	rig_model
//	trx
//	<rx range lines>          e.g. "30000.000000 60000000.000000 0xc0401dbf -1 -1 0x16000000 0x3"
//	0 0 0 0 0 0 0             (terminates the rx range list)
//	<tx range lines>
//	0 0 0 0 0 0 0
//	...
//	done
func parseFrequencyRange(dump string) (FrequencyRange, error) {
	lines := strings.Split(dump, "\n")

	// Skip the three header lines (protocol version, model, trx).
	nonEmpty := 0
	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty++
		if nonEmpty == 4 {
			start = i
			break
		}
	}
	if start == 0 {
		return FrequencyRange{}, fmt.Errorf("dump_state reply too short")
	}

	var minKHz, maxKHz float64
	found := false

	for _, line := range lines[start:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		low, errLow := strconv.ParseFloat(fields[0], 64)
		high, errHigh := strconv.ParseFloat(fields[1], 64)
		if errLow != nil || errHigh != nil {
			break
		}
		// The range list is terminated by an all-zero line.
		if low == 0 && high == 0 {
			break
		}

		// Values arrive in Hz; convert to kHz.
		lowKHz := low / 1000.0
		highKHz := high / 1000.0
		if !found || lowKHz < minKHz {
			minKHz = lowKHz
		}
		if !found || highKHz > maxKHz {
			maxKHz = highKHz
		}
		found = true
	}

	if !found {
		return FrequencyRange{}, fmt.Errorf("no receive ranges found in dump_state")
	}
	return FrequencyRange{MinKHz: minKHz, MaxKHz: maxKHz}, nil
}

// GetFrequencyRange returns the range of frequencies the connected rig can
// tune. The frontend uses it to hide broadcast entries the rig cannot receive.
func (a *App) GetFrequencyRange() (FrequencyRange, error) {
	dump, err := a.queryDumpState()
	if err != nil {
		return FrequencyRange{}, err
	}
	return parseFrequencyRange(dump)
}
