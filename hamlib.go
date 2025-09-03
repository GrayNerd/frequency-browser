package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
    "strconv"
    "strings"
    "time"
    
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
                return "", fmt.Errorf("rig error: %d", errorCode)
            }
        }
    }

    return response, nil
}
func (a *App) getFrequency() (int, error) {
    response, err := a.sendRigCommand("f")
    if err != nil {
        return 0, err
    }
    
    // Parse frequency response
    frequency, err := strconv.Atoi(response)
    if err != nil {
        return 0, fmt.Errorf("failed to parse frequency: %v", err)
    }
    
    return frequency, nil
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

func (a *App) setFrequency(frequency int) error {
    response, err := a.sendRigCommand(fmt.Sprintf("F %d", frequency))
    if err != nil {
        return err
    }
    
    // Check if set command returned an error
    if response != "" && strings.HasPrefix(response, "RPRT") {
        parts := strings.Split(response, " ")
        if len(parts) > 1 {
            if errorCode, err := strconv.Atoi(parts[1]); err == nil && errorCode != 0 {
                return fmt.Errorf("set frequency failed with error: %d", errorCode)
            }
        }
    }
    
    return nil
}

func (a *App) pollRadioFrequency() {
    ticker := time.NewTicker(250 * time.Millisecond)
    defer ticker.Stop()
    
    var lastFrequency int = -1
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
            
            // Only send update if frequency changed
            if frequency != lastFrequency {
                lastFrequency = frequency
                
                // Send frequency to frontend
                runtime.EventsEmit(a.ctx, "frequencyUpdate", strconv.Itoa(frequency))
                
                // Also get mode if needed (but not every time to reduce load)
                if time.Now().Second()%5 == 0 { // Get mode every 5 seconds
                    mode, err := a.getMode()
                    if err == nil {
                        runtime.EventsEmit(a.ctx, "modeUpdate", mode)
                    }
                }
            }
        }
    }
}

func (a *App) GetCurrentFrequency() (string, error) {
    frequency, err := a.getFrequency()
    if err != nil {
        return "", err
    }
    return strconv.Itoa(frequency), nil
}

func (a *App) SetFrequency(frequency int) error {
    return a.setFrequency(frequency)
}

func (a *App) GetMode() (string, error) {
    return a.getMode()
}
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


