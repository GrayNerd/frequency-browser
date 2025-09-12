package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
)

// HfccSite represents a single HFCC site entry
type HfccSite struct {
	Code     string `json:"code"`
	SiteName string `json:"site_name"`
	ADM      string `json:"ADM"`
}

var (
	hfccSiteMap map[string]HfccSite
	loadOnce    sync.Once
)

// init loads the HFCC site data automatically when the package is imported
func init() {
	loadHfccSiteMap("hfcc-sites.json")
}

// loadHfccSiteMap loads and parses the HFCC site data from JSON file
func loadHfccSiteMap(filename string) {
	loadOnce.Do(func() {
		file, err := os.ReadFile(filename)
		if err != nil {
			log.Printf("Warning: Could not load site map from %s: %v", filename, err)
			hfccSiteMap = make(map[string]HfccSite)
			return
		}

		var sites []HfccSite
		err = json.Unmarshal(file, &sites)
		if err != nil {
			log.Printf("Warning: Could not parse HFCC site map from %s: %v", filename, err)
			hfccSiteMap = make(map[string]HfccSite)
			return
		}

		// Convert array to map for fast lookups
		hfccSiteMap = make(map[string]HfccSite)
		for _, site := range sites {
			key := strings.ToUpper(strings.TrimSpace(site.Code))
			hfccSiteMap[key] = site
		}

		log.Printf("Loaded %d HFCC site mappings", len(hfccSiteMap))
	})
}

// GetHfccSite returns the site information for a given code
// Returns the HfccSite struct containing site_name and ADM, and a boolean indicating if found
func GetHfccSite(code string) (HfccSite, bool) {
	cleanCode := strings.ToUpper(strings.TrimSpace(code))
	site, exists := hfccSiteMap[cleanCode]
	return site, exists
}

// GetHfccSiteName returns just the site name for a given code
// Returns the site name if found, or "Unknown" if not found
func GetHfccSiteName(code string) string {
	if site, exists := GetHfccSite(code); exists {
		return site.SiteName
	}
	return "Unknown"
}

// GetHfccSiteADM returns just the ADM for a given code
// Returns the ADM if found, or "Unknown" if not found
func GetHfccSiteADM(code string) string {
	if site, exists := GetHfccSite(code); exists {
		return site.ADM
	}
	return "Unknown"
}

// GetAllSites returns a copy of all loaded sites
func GetAllSites() map[string]HfccSite {
	result := make(map[string]HfccSite)
	for k, v := range hfccSiteMap {
		result[k] = v
	}
	return result
}

// GetSiteCount returns the number of loaded sites
func GetSiteCount() int {
	return len(hfccSiteMap)
}