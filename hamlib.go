package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
    "strconv"
    "strings"
    "time"
    "math"
    
    "github.com/wailsapp/wails/v2/pkg/runtime"
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

func (a *App) sendRigCommand(command string) (string, error) {
    conn, err := net.Dial("tcp", a.config.Address)
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to connect to rigctld: %v", err)
    }
    defer conn.Close()
    
    // Set read timeout
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    
    // Send command
    _, err = conn.Write([]byte(command + "\n"))
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to send command: %v", err)
    }
    
    // Use bufio.Reader to read until newline
    reader := bufio.NewReader(conn)
    response, err := reader.ReadString('\n')
    if err != nil {
        a.isConnected = false
        return "", fmt.Errorf("failed to read response: %v", err)
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
func (a *App) pollRadioFrequency() {
    ticker := time.NewTicker(250 * time.Millisecond)
    defer ticker.Stop()
    
    var lastFrequency float64 = -1
    var errorCount int = 0
    const maxErrorCount = 3
    
    for {
        select {
        case <-a.ctx.Done():
            return
            
        case <-ticker.C:
            frequency, err := a.getFrequency()
            if err != nil {
                errorCount++
                if errorCount >= maxErrorCount {
                    a.isConnected = false
                    runtime.EventsEmit(a.ctx, "connectionStatus", false)
                    log.Printf("Connection lost: %v", err)
                }
                continue
            }
            
            // Reset error count on successful communication
            errorCount = 0
            a.isConnected = true
            runtime.EventsEmit(a.ctx, "connectionStatus", true)

			// Emit modeUpdate if mode changed
			mode, err := a.getMode()
			if err == nil && mode != a.lastMode {
				a.lastMode = mode
				runtime.EventsEmit(a.ctx, "modeUpdate", mode)
			}

            
            // Only send update if frequency changed significantly
            if math.Abs(frequency - lastFrequency) > 0.01 { // More than 1 Hz difference
                lastFrequency = frequency
                
                // Send frequency to frontend as float
                runtime.EventsEmit(a.ctx, "frequencyUpdate", frequency)
            }
        }
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


