package main

import (
	"encoding/json"
	"log"
	"embed"
	"strings"
)

var LanguageMap map[string]string

//go:embed language.json
var languageJSON embed.FS

func init() {
	// Load language mappings from JSON file on startup
	loadLanguageMap("language.json")
}

func loadLanguageMap(filename string) {
	file, err := languageJSON.ReadFile(filename)
	if err != nil {
		log.Printf("Warning: Could not load language map from %s: %v", filename, err)
		// Fall back to a minimal map if file can't be loaded
		LanguageMap = map[string]string{
			"E": "English",
			"S": "Spanish",
			"F": "French",
			"D": "German",
			"R": "Russian",
			"C": "Chinese",
			// Add other essential mappings as needed
		}
		return
	}
	
	err = json.Unmarshal(file, &LanguageMap)
	if err != nil {
		log.Printf("Warning: Could not parse language map from %s: %v", filename, err)
		// Fall back to a minimal map if JSON is invalid
		LanguageMap = map[string]string{
			"E": "English",
			"S": "Spanish",
			"F": "French",
			"D": "German",
			"R": "Russian",
			"C": "Chinese",
		}
	}
}

// getLanguageName returns the full language name for an abbreviation or multiple abbreviations
func getLanguageName(langCode string) string {
    if langCode == "" {
        return ""
    }
    
    // Split by comma to handle multiple languages
    codes := strings.Split(langCode, ",")
    var languageNames []string
    
    for _, code := range codes {
        // Remove anything in parentheses and trim spaces
        cleanCode := strings.TrimSpace(code)
        if idx := strings.Index(cleanCode, "("); idx != -1 {
            cleanCode = strings.TrimSpace(cleanCode[:idx])
        }
        
        // Convert to uppercase for case-insensitive lookup
        upperCode := strings.ToUpper(cleanCode)
        
		// fmt.Printf("upperCode: %s\n", upperCode)
        if language, exists := LanguageMap[upperCode]; exists {
			// fmt.Printf("language: %s exists\n", language)
            languageNames = append(languageNames, language)
        } else {
            // Return original code if not found
            languageNames = append(languageNames, cleanCode)
        }
    }
    
    // Join multiple languages with comma and space
    return strings.Join(languageNames, ", ")
}