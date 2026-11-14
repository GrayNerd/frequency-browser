package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

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

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}

	if _, err := conn.Write([]byte("\\dump_state\n")); err != nil {
		return "", fmt.Errorf("failed to send dump_state: %v", err)
	}

	var sb strings.Builder
	reader := bufio.NewReader(conn)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			sb.WriteString(line)
			if strings.TrimSpace(line) == "done" {
				break
			}
		}
		if readErr != nil {
			break // deadline or EOF; use whatever we collected
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
