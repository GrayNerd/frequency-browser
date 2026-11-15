package main

import (
	"encoding/json"
	"fmt"
	"log"
	"embed"
	"strings"
)

var HfccStationMap map[string]string

func init() {
	// Load language mappings from JSON file on startup
	loadHfccStationMap("hfcc-station.json")
}

//go:embed hfcc-station.json
var hfccStationJSON embed.FS

func loadHfccStationMap(filename string) {
    file, err := hfccStationJSON.ReadFile(filename)
    if err != nil {
        return
    }

    // Define a struct to match the JSON array elements
    type Station struct {
        Code           string `json:"code"`
        BroadcasterName string `json:"broadcaster_name"`
    }
    
    var stations []Station
    err = json.Unmarshal(file, &stations)
    if err != nil {
        log.Printf("Warning: Could not parse hfcc station map from %s: %v", filename, err)
        return
    }

    // Convert the array to a map
    HfccStationMap = make(map[string]string)
    for _, station := range stations {
        HfccStationMap[strings.ToUpper(station.Code)] = station.BroadcasterName
    }
    
    fmt.Printf("Loaded %d station mappings\n", len(HfccStationMap))
}
// GetHfccStationName returns the broadcaster name for a given station code
func GetHfccStationName(stationCode string) string {
	code := strings.TrimSpace(strings.ToUpper(stationCode))
	referenceTablesMu.RLock()
	stationName, exists := HfccStationMap[code]
	referenceTablesMu.RUnlock()
	if exists {
		return stationName
	}
	return stationCode+" Unknown"
}
