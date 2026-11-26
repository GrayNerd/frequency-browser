package main

import (
    "bufio"
    "errors"
    "fmt"
    "io"
    "log"
    "net"
    "strconv"
    "strings"
    "syscall"
    "time"
    "math"
    
    "github.com/wailsapp/wails/v3/pkg/application"
)

// RadioInterface defines the methods required for radio communication.
type RadioInterface interface {
    Connect(address string, model int) error
    Disconnect() error
    GetFrequency() (int, error)
    SetFrequency(freq int) error
    GetMode() (string, error)
}

type RadioConfig struct {
    Address string `json:"address"`
    Model   int    `json:"model"`
}

// rigCommandAttempts is how many times a command is tried. A single retry covers
// the transient connection resets rigctld produces while a client connection is
// being torn down (most visible right after a daemon restart), which would
// otherwise surface as a spurious "cannot tune" error. Every command the app
// sends is idempotent - they only get or set rig state - so a retry is safe.
const rigCommandAttempts = 3

// rigCommandTimeout bounds one rigctld request/response exchange. It is a
// variable so tests can shorten it.
var rigCommandTimeout = 8 * time.Second

func (a *App) sendRigCommand(command string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= rigCommandAttempts; attempt++ {
		response, err := a.sendRigCommandOnce(command)
		if err == nil {
			return response, nil
		}
		if !isTransientRigError(err) {
			return "", err
		}
		lastErr = err
		if attempt < rigCommandAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return "", lastErr
}

// isTransientRigError reports whether err is a connection-level failure worth
// retrying, as opposed to a rig error reply ("RPRT -8" and friends), which is a
// real answer that will not change on a retry.
func isTransientRigError(err error) bool {
	if err == nil {
		return false
	}
	if strings.HasPrefix(err.Error(), "rig error:") {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE)
}

func (a *App) sendRigCommandOnce(command string) (string, error) {
    conn, err := net.Dial("tcp", a.config.Address)
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to connect to rigctld: %w", err)
    }
    defer conn.Close()
    
    // Set the read timeout. A real radio can take longer than a virtual one to
    // answer (an Icom IC-7610 over CI-V needs a few hundred milliseconds, and
    // more right after the port is opened), so the deadline is generous; it
    // only exists to stop a wedged daemon from hanging the UI forever.
    conn.SetDeadline(time.Now().Add(rigCommandTimeout))
    
    // Send command
    _, err = conn.Write([]byte(command + "\n"))
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to send command: %w", err)
    }
    
    // Use bufio.Reader to read until newline
    reader := bufio.NewReader(conn)
    response, err := reader.ReadString('\n')
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to read response: %w", err)
    }
    
    a.isConnected = true

	response = strings.TrimSpace(response)
    
    // Check for rigctl error response (starts with RPRT)
    if strings.HasPrefix(response, "RPRT") {
        parts := strings.Split(response, " ")
        if len(parts) > 1 {
            if errorCode, err := strconv.Atoi(parts[1]); err == nil && errorCode != 0 {
                return "", fmt.Errorf("rig error: %d (%s)", errorCode, hamlibErrorText(errorCode))
            }
        }
    }

    return response, nil
}

// hamlibErrorText maps hamlib rig error codes to readable messages.
// See hamlib rig.h: RIG_OK 0, RIG_EINVAL -1, RIG_EPROTO -2, RIG_ETIMEOUT -3,
// RIG_EARGS -4, RIG_EIO -5, RIG_EINTERNAL -6, RIG_EPOWER -7, RIG_ELIMIT -8,
// RIG_ERJCTED -9, RIG_ETRUNC -10, RIG_ENAVAIL -11, RIG_ENIMPL -12 ...
func hamlibErrorText(code int) string {
	switch code {
	case -1:
		return "invalid parameter"
	case -2:
		return "communication problem with the radio"
	case -3:
		return "communication timed out"
	case -4:
		return "invalid arguments"
	case -5:
		return "I/O error talking to the radio"
	case -6:
		return "internal radio error"
	case -7:
		return "radio is not powered on or not responding"
	case -8:
		return "frequency out of the radio's tuning range"
	case -9:
		return "radio rejected the command (frequency not supported)"
	case -10:
		return "value truncated by the radio"
	case -11:
		return "feature not available"
	case -12:
		return "operation not implemented"
	default:
		return fmt.Sprintf("radio error %d", code)
	}
}

func (a *App) getFrequency() (float64, error) {
    response, err := a.sendRigCommand("f")
    if err != nil {
        return 0, err
    }
    
    // Parse frequency response (in Hz)
    frequencyHz, err := strconv.ParseFloat(response, 64)
    if err != nil {
        return 0, fmt.Errorf("failed to parse frequency: %v", err)
    }
    
    // Convert Hz to kHz
    frequencykHz := frequencyHz / 1000.0
    
    return frequencykHz, nil
}
func (a *App) getMode() (string, error) {
    response, err := a.sendRigCommand("m")
    if err != nil {
        return "", err
    }
    
    // Mode response format: "MODE PASSBAND"
    parts := strings.Split(response, " ")
    if len(parts) > 0 {
        return parts[0], nil
    }
    
    return response, nil
}

func (a *App) SetFrequency(frequencykHz float64) error {
	if frequencykHz <= 0 {
		return fmt.Errorf("invalid frequency: %v kHz", frequencykHz)
	}
	// Convert kHz to Hz
	frequencyHz := int(frequencykHz*1000 + 0.5)
	_, err := a.sendRigCommand(fmt.Sprintf("F %d", frequencyHz))
	if err != nil {
		// Surface the frequency that failed so out-of-range rows are obvious
		return fmt.Errorf("cannot tune to %s kHz: %v", formatKHz(frequencykHz), err)
	}
	return nil
}

// formatKHz renders a frequency in kHz with up to 3 decimals, trimming
// trailing zeros (e.g. 16300 Hz -> "16.3", 5900 kHz -> "5900").
func formatKHz(khz float64) string {
	s := strconv.FormatFloat(khz, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// pollRadioFrequency reads the rig's frequency and mode every PollIntervalMs and
// emits the updates the main window listens for. The interval is re-read on
// every pass, so changing it (including to "Never") takes effect immediately;
// when it is "Never" the rig is not polled at all and this loop simply waits.
func (a *App) pollRadioFrequency() {
	var lastFrequency float64 = -1
	var errorCount int = 0
	const maxErrorCount = 3

	for {
		interval := a.pollInterval()
		if interval <= 0 {
			// Polling is switched off: sleep until the settings change or
			// the application shuts down.
			select {
			case <-a.ctx.Done():
				return
			case <-a.pollWake:
				continue
			}
		}

		select {
		case <-a.ctx.Done():
			return
		case <-a.pollWake:
			// Interval or daemon changed: restart the countdown.
			continue
		case <-time.After(interval):
		}

		frequency, err := a.getFrequency()
		if err != nil {
			errorCount++
			if errorCount >= maxErrorCount {
				a.isConnected = false
				application.Get().Event.Emit("connectionStatus", false)
				log.Printf("Connection lost: %v", err)
			}
			continue
		}

		// Reset error count on successful communication
		errorCount = 0
		a.isConnected = true
		application.Get().Event.Emit("connectionStatus", true)

		// Emit modeUpdate if mode changed
		mode, err := a.getMode()
		if err == nil && mode != a.lastMode {
			a.lastMode = mode
			application.Get().Event.Emit("modeUpdate", mode)
		}

		// Only send update if frequency changed significantly
		if math.Abs(frequency-lastFrequency) > 0.01 { // More than 1 Hz difference
			lastFrequency = frequency

			// Send frequency to frontend as float
			application.Get().Event.Emit("frequencyUpdate", frequency)
		}
	}
}

// pollInterval returns the configured polling period, or 0 for "Never".
func (a *App) pollInterval() time.Duration {
	a.settingsMu.Lock()
	ms := a.settings.PollIntervalMs
	a.settingsMu.Unlock()
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// wakePolling nudges the poll loop so new settings take effect at once
// instead of after the current interval.
func (a *App) wakePolling() {
	select {
	case a.pollWake <- struct{}{}:
	default:
	}
}

// Update GetRadioStatus to handle float frequency
func (a *App) GetRadioStatus() map[string]interface{} {
    status := map[string]interface{}{
        "connected": a.isConnected,
        "address":   a.config.Address,
    }
    
    if a.isConnected {
        freq, err := a.getFrequency()
        if err == nil {
            status["frequency"] = freq
        }
        
        mode, err := a.getMode()
        if err == nil {
            status["mode"] = mode
        }
    }
    
    return status
}

func (a *App) GetCurrentFrequency() (float64, error) {
    return a.getFrequency()
}



func (a *App) GetMode() (string, error) {
    return a.getMode()
}
func (a *App) UpdateConfig(address string) error {
    oldAddress := a.config.Address
    a.config.Address = address
    
    // Test connection with new config
    _, err := a.getFrequency()
    if err != nil {
        // Revert if new config fails
        a.config.Address = oldAddress
        return fmt.Errorf("failed to connect to %s: %v", address, err)
    }
    
    return nil
}

func (a *App) SendCustomCommand(command string) (string, error) {
    return a.sendRigCommand(command)
}

func (a *App) TestConnection() (bool, error) {
    _, err := a.getFrequency()
    if err != nil {
        return false, err
    }
    return true, nil
}


