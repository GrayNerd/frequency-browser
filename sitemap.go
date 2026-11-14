package main

import (
	"encoding/json"
	"fmt"
	"log"
	"embed"
	"regexp"
	"sync"
)

// SiteEntry represents a single site entry from the JSON file
type SiteEntry struct {
	Country    string `json:"country"`
	SiteAbbrev string `json:"site_abbrev"`
	SiteName   string `json:"site_name"`
}

// SitesMap is the global map that will store our site data
var SitesMap map[string]map[string]string
var once sync.Once

// init initializes the SitesMap by reading and parsing the sites.json file
func init() {
	once.Do(func() {
		SitesMap = make(map[string]map[string]string)
		loadSitesData()
	})
}

//go:embed sites.json
var sitesJSON embed.FS

// loadSitesData reads the JSON file and populates the SitesMap
func loadSitesData() {
	// Read the JSON file
	data, err := sitesJSON.ReadFile("sites.json")
	if err != nil {
		log.Printf("Warning: Could not load site map from sites.json: %v", err)
		SitesMap = make(map[string]map[string]string)
		return
	}

	// Parse the JSON data
	var entries []SiteEntry
	err = json.Unmarshal(data, &entries)
	if err != nil {
		log.Fatalf("Error parsing JSON: %v", err)
	}

	// Populate the map
	for _, entry := range entries {
		// Initialize the inner map if it doesn't exist
		if _, exists := SitesMap[entry.Country]; !exists {
			SitesMap[entry.Country] = make(map[string]string)
		}

		// Store the site name with the site abbreviation as key
		SitesMap[entry.Country][entry.SiteAbbrev] = entry.SiteName

	}

	// fmt.Printf("Loaded %d countries with site data\n", len(SitesMap))
}

// GetSiteDescription returns the site name for the given country and site abbreviations
// If siteAbbrev is empty, it returns the site name with empty site abbreviation 
// if siteAbbrev is /XXX, it returns the country name
// if siteAbbrev is /XXX-xx, it returns the country name and site name 
func GetSiteDescription(countryAbbrev, siteAbbrev string) string {
	// Check if the country exists
	countrySites, exists := SitesMap[countryAbbrev]
	if !exists {
		return fmt.Sprintf("Country '%s' not found", countryAbbrev)
	}

	// If site abbreviation is empty, look for entry with empty key
	if siteAbbrev == "" {
		if siteName, exists := countrySites[""]; exists {
			return siteName
		}
		// return fmt.Sprintf("No site found for country '%s' with empty abbreviation", countryAbbrev)
		return ""
	}

	// Look for the specific site abbreviation
	if siteName, exists := countrySites[siteAbbrev]; exists {
		return siteName
	}
	// Regex for /XXX
	slashAlphaRegex := regexp.MustCompile(`^/([a-zA-Z]{1,3})$`)
	if matches := slashAlphaRegex.FindStringSubmatch(siteAbbrev); matches != nil {
		// matches[1] contains the alpha characters without the slash
			return GetCountryName(matches[1])
	}

	// Regex for /XXX-xx
	slashAlphaDashRegex := regexp.MustCompile(`^/([a-zA-Z]{1,3})-([a-zA-Z0-9]+)$`)
	if matches := slashAlphaDashRegex.FindStringSubmatch(siteAbbrev); matches != nil {
		// matches[1] contains the part before the dash (alpha characters)
		// matches[2] contains the part after the dash
		if countrySites, exists := SitesMap[matches[1]]; exists {
			if siteName, exists := countrySites[matches[2]]; exists {
				return GetCountryName(matches[1]) + "  - " + siteName
			} else {
				return GetCountryName(matches[1])
			}
		}
	}
	return fmt.Sprintf("Site '%s' not found for country '%s'", siteAbbrev, countryAbbrev)
}
