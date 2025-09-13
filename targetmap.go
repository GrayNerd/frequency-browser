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
	return ""
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



