package main

import (
	"encoding/json"
	"os"
	"log"
)

var ITUCountryMap map[string]string

func init() {
	// Load country mappings from JSON file on startup
	loadCountryMap("countries.json")
}

func loadCountryMap(filename string) {
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("Warning: Could not load country map from %s: %v", filename, err)
		// Fall back to a minimal map if file can't be loaded
		ITUCountryMap = map[string]string{
			"USA": "United States",
			"GBR": "United Kingdom",
			"DEU": "Germany",
			"FRA": "France",
			"JPN": "Japan",
			// Add other essential mappings as needed
		}
		return
	}
	
	err = json.Unmarshal(file, &ITUCountryMap)
	if err != nil {
		log.Printf("Warning: Could not parse country map from %s: %v", filename, err)
		// Fall back to a minimal map if JSON is invalid
		ITUCountryMap = map[string]string{
			"USA": "United States",
			"GBR": "United Kingdom",
			"DEU": "Germany",
			"FRA": "France",
			"JPN": "Japan",
		}
	}
}

// getCountryName returns the full country name for an ITU abbreviation
func GetCountryName(ituCode string) string {
	if ituCode == "" {
		return ""
	}
	
	// Convert to uppercase for case-insensitive lookup
	code := ituCode
	if len(ituCode) <= 4 { // Most codes are 3-4 characters
		code = ituCode
	}
	
	if country, exists := ITUCountryMap[code]; exists {
		return country
	}
	
	// Return original code if not found
	return ituCode
}