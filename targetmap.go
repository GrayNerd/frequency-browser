package main

import (
	"embed"
	"encoding/json"
	"fmt"
	// "os"
	// "path/filepath"
	"strings"
)

// TargetMap is the exportable variable containing all target codes and descriptions
var TargetMap map[string]string

// init initializes the module and loads the target map
func init() {
	loadTargetMap()
}

//go:embed targets.json
var targetsJSON embed.FS

// loadTargetMap loads the target data from target.json into TargetMap
func loadTargetMap() {
	// Get the current directory
	// exePath, err := os.Executable()
	// if err != nil {
	// 	fmt.Printf("Error getting executable path: %v\n", err)
	// 	TargetMap = make(map[string]string)
	// 	return
	// }
	// dir := filepath.Dir(exePath)
	// jsonPath := filepath.Join(dir, "targets.json")

	// // Check if file exists
	// if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
	// 	fmt.Printf("targets.json file not found in: %s\n", jsonPath)
	// 	TargetMap = make(map[string]string)
	// 	return
	// }

	// Read the file
	fileData, err := targetsJSON.ReadFile("targets.json")
	if err != nil {
		fmt.Printf("Error reading targets.json: %v\n", err)
		TargetMap = make(map[string]string)
		return
	}

	// Parse JSON into temporary slice
	var targets []struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}

	err = json.Unmarshal(fileData, &targets)
	if err != nil {
		fmt.Printf("Error parsing targets.json: %v\n", err)
		TargetMap = make(map[string]string)
		return
	}

	// Convert slice to map for faster lookups
	TargetMap = make(map[string]string)
	for _, target := range targets {
		TargetMap[target.Code] = target.Description
	}

	fmt.Printf("Loaded %d target entries into TargetMap\n", len(TargetMap))
}

// GetTargetDescription returns the description for a target code
// Returns empty string if code not found
func GetTargetDescription(code string) string {
	// fmt.Printf("GetTargetDescription(%s)\n", code)
	code = strings.TrimSpace(code)
	if description, exists := TargetMap[code]; exists {
		return description
	}
	if description, ok := resolveComposedTarget(code); ok {
		return description
	}
	return ""
}

// eibiComposedPrefixes are the region prefixes EiBi documents in its README
// ("N.. - North .." and so on). The CSV database combines them with an area
// suffix, e.g. "NAm" (North America) or "WEu" (West Europe).
var eibiComposedPrefixes = map[string]string{
	"N": "North",
	"S": "South",
	"E": "East",
	"W": "West",
	"C": "Central",
}

var eibiComposedAreas = map[string]string{
	"Am": "America",
	"As": "Asia",
	"Af": "Africa",
	"Eu": "Europe",
	"Oc": "Oceania",
	"In": "Indian subcontinent",
	"NA": "North America",
	"SA": "South America",
}

// resolveComposedTarget expands an EiBi target code built from a region prefix
// and an area suffix ("NAm" -> "North America", "WEu" -> "West Europe").
func resolveComposedTarget(code string) (string, bool) {
	if len(code) < 2 || len(code) > 3 {
		return "", false
	}

	direction, ok := eibiComposedPrefixes[strings.ToUpper(code[:1])]
	if !ok {
		return "", false
	}

	suffix := code[1:]
	if area, ok := eibiComposedAreas[suffix]; ok {
		return direction + " " + area, true
	}

	// The README documents the suffix as an abbreviation finished with "..",
	// so look one up in the table ("Af", "As", "Eu", "Oc", "In").
	if area, ok := TargetMap[suffix]; ok && area != "" && !strings.HasSuffix(area, "..") {
		return direction + " " + strings.ToLower(area), true
	}
	return "", false
}

// GetTargetDescriptionWithError returns the description for a target code with error handling
// Returns error if code not found
func GetTargetDescriptionWithError(code string) (string, error) {
	code = strings.TrimSpace(code)
	if description, exists := TargetMap[code]; exists {
		return description, nil
	}
	return code, fmt.Errorf("target code '%s' not found", code)
}
