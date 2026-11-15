package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// aokiSiteURL is the AOKI Bi Newsletter site; its root is a frameset, so the
	// frame document below holds the actual links.
	aokiSiteURL  = "http://www1.s2.starcat.ne.jp/ndxc/"
	aokiFrameURL = "http://www1.s2.starcat.ne.jp/ndxc/nnk.htm"

	// aokiHTTPTimeout bounds each HTTP request made while importing.
	aokiHTTPTimeout = 2 * time.Minute
)

var (
	// A schedule archive is named <prefix><season>.zip, e.g. "xta26.zip"; the
	// matching data file inside it is "xta26.txt".
	aokiScheduleLinkRE = regexp.MustCompile(`(?i)href="([^"]*/[a-z0-9]+[ab][0-9]{2}\.zip)"`)
	aokiArchiveNameRE  = regexp.MustCompile(`(?i)^([a-z0-9]+)([ab])([0-9]{2})\.zip$`)
	aokiFrameLinkRE    = regexp.MustCompile(`(?i)src="([^"]*\.html?)"`)

	// The first line of a schedule file names its season and revision, e.g.
	// "B26 Shortwave Frequecy List  November 14 2026,  1000 UTC".
	aokiScheduleHeaderRE = regexp.MustCompile(`(?i)^\s*([AB][0-9]{2})\b.*?\b([A-Z][a-z]+)\s+([0-9]{1,2})\s+([0-9]{4})`)
)

// aokiSchedule is one downloadable AOKI schedule archive.
type aokiSchedule struct {
	Name    string    // Archive file name, e.g. "xta26.zip"
	URL     string    // Absolute URL of the archive
	Season  string    // Season from the name, e.g. "A26"
	Prefix  string    // File name stem, e.g. "xta26"
	Header  string    // First line of the schedule file, once known
	Revised time.Time // Revision date parsed from Header
}

// aokiDownload fetches rawURL and returns the response body.
func aokiDownload(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "frequency-browser/1.0")

	client := &http.Client{Timeout: aokiHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: unexpected status %s", rawURL, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	return body, nil
}

// aokiFetchLinkPage returns the markup that lists the schedule archives and the
// URL it came from. The site root is a frameset, so the frame document is
// fetched when the root itself carries no archive links.
func aokiFetchLinkPage() ([]byte, string, error) {
	page, err := aokiDownload(aokiSiteURL)
	if err != nil {
		return nil, aokiSiteURL, err
	}
	if aokiScheduleLinkRE.Match(page) {
		return page, aokiSiteURL, nil
	}

	match := aokiFrameLinkRE.FindSubmatch(page)
	if match == nil {
		return page, aokiSiteURL, nil
	}
	base, err := url.Parse(aokiSiteURL)
	if err != nil {
		return page, aokiSiteURL, nil
	}
	ref, err := url.Parse(string(match[1]))
	if err != nil {
		return page, aokiSiteURL, nil
	}
	frameURL := base.ResolveReference(ref).String()

	frame, err := aokiDownload(frameURL)
	if err != nil {
		return nil, frameURL, err
	}
	return frame, frameURL, nil
}

// aokiParseScheduleLinks extracts the schedule archives listed on the page, in
// the order they appear (the newsletter lists the current season first).
func aokiParseScheduleLinks(page []byte, pageURL string) []aokiSchedule {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	matches := aokiScheduleLinkRE.FindAllSubmatchIndex(page, -1)
	schedules := make([]aokiSchedule, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for _, match := range matches {
		ref, err := url.Parse(string(page[match[2]:match[3]]))
		if err != nil {
			continue
		}

		name := path.Base(ref.Path)
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		season, prefix := aokiArchiveParts(name)
		if season == "" {
			continue
		}
		schedules = append(schedules, aokiSchedule{
			Name:   name,
			URL:    base.ResolveReference(ref).String(),
			Season: season,
			Prefix: prefix,
		})
	}
	return schedules
}

// aokiArchiveParts splits an archive name into its season code and file stem:
// "xta26.zip" -> "A26", "xta26".
func aokiArchiveParts(name string) (season, prefix string) {
	match := aokiArchiveNameRE.FindStringSubmatch(name)
	if match == nil {
		return "", ""
	}
	return strings.ToUpper(match[2] + match[3]), strings.ToLower(match[1] + match[2] + match[3])
}

// aokiSeasonRank orders season codes chronologically: A25, B25, A26, B26.
func aokiSeasonRank(season string) (year int, half int) {
	if len(season) < 3 {
		return 0, 0
	}
	year, err := strconv.Atoi(season[1:])
	if err != nil {
		return 0, 0
	}
	if strings.EqualFold(season[:1], "A") {
		return year, 0
	}
	return year, 1
}

// aokiParseScheduleHeader reads the season and revision date from a schedule
// file's first line, e.g.
// "B26 Shortwave Frequecy List  November 14 2026,  1000 UTC".
func aokiParseScheduleHeader(line string) (season string, revised time.Time, ok bool) {
	match := aokiScheduleHeaderRE.FindStringSubmatch(line)
	if match == nil {
		return "", time.Time{}, false
	}

	// Groups: 1 = season, 2 = month name, 3 = day, 4 = year.
	month := strings.ToUpper(match[2][:1]) + strings.ToLower(match[2][1:])
	layout := "2-Jan-2006"
	if len(month) > 3 {
		layout = "2-January-2006"
	}
	when, err := time.Parse(layout, match[3]+"-"+month+"-"+match[4])
	if err != nil {
		return strings.ToUpper(match[1]), time.Time{}, false
	}
	return strings.ToUpper(match[1]), when, true
}

// aokiScheduleFromZip returns the contents of the data file inside archive whose
// name matches the archive's own stem and carries the .txt extension
// ("xta26.zip" -> "xta26.txt").
func aokiScheduleFromZip(archive []byte, archiveName string) (string, []byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", nil, fmt.Errorf("opening zip archive %s: %w", archiveName, err)
	}

	_, prefix := aokiArchiveParts(archiveName)
	if prefix == "" {
		prefix = strings.ToLower(strings.TrimSuffix(archiveName, path.Ext(archiveName)))
	}

	var entries []string
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := path.Base(file.Name)
		entries = append(entries, name)

		// Match "<stem>.txt" exactly, so "xta26.xlsx" and "userlist1.txt" are
		// skipped.
		if !strings.EqualFold(name, prefix+".txt") {
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return "", nil, fmt.Errorf("opening %s in %s: %w", name, archiveName, err)
		}
		content, readErr := io.ReadAll(fileReader)
		closeErr := fileReader.Close()
		if readErr != nil {
			return "", nil, fmt.Errorf("reading %s in %s: %w", name, archiveName, readErr)
		}
		if closeErr != nil {
			return "", nil, fmt.Errorf("closing %s in %s: %w", name, archiveName, closeErr)
		}
		return name, content, nil
	}

	return "", nil, fmt.Errorf("no %s.txt in %s (entries: %s)", prefix, archiveName, strings.Join(entries, ", "))
}

// aokiNewestArchive picks the newest schedule from the archives listed on the
// page.
//
// Archives are ranked by the season in their name ("xzb26.zip" -> B26, newer
// than "xta26.zip" -> A26), so the current one comes out first regardless of
// the order or the labels on the newsletter page. The season stated inside the
// file is then used as a cross-check, because a file can be labelled with an old
// season on the page.
func aokiNewestArchive(schedules []aokiSchedule) (aokiSchedule, string, []byte, error) {
	if len(schedules) == 0 {
		return aokiSchedule{}, "", nil, fmt.Errorf("no schedule archives found on %s", aokiSiteURL)
	}

	sorted := make([]aokiSchedule, len(schedules))
	copy(sorted, schedules)
	sort.SliceStable(sorted, func(i, j int) bool {
		iy, ih := aokiSeasonRank(sorted[i].Season)
		jy, jh := aokiSeasonRank(sorted[j].Season)
		if iy != jy {
			return iy > jy
		}
		return ih > jh
	})

	var lastErr error
	for _, schedule := range sorted {
		archive, err := aokiDownload(schedule.URL)
		if err != nil {
			lastErr = err
			fmt.Printf("Warning: could not download %s: %v\n", schedule.Name, err)
			continue
		}

		name, content, err := aokiScheduleFromZip(archive, schedule.Name)
		if err != nil {
			lastErr = err
			fmt.Printf("Warning: skipping %s: %v\n", schedule.Name, err)
			continue
		}

		// Cross-check the season the file itself declares.
		if header := aokiFirstLine(content); header != "" {
			schedule.Header = header
			if season, revised, ok := aokiParseScheduleHeader(header); ok {
				schedule.Revised = revised
				if season != schedule.Season {
					fmt.Printf("%s contains a %s schedule\n", schedule.Name, season)
				}
				schedule.Season = season
			}
		}

		if schedule.Season == "" {
			schedule.Season = sorted[0].Season
		}
		return schedule, name, content, nil
	}

	if lastErr != nil {
		return aokiSchedule{}, "", nil, lastErr
	}
	return aokiSchedule{}, "", nil, fmt.Errorf("could not read a schedule from %s", aokiSiteURL)
}

// aokiFirstLine returns the first line of a schedule file, trimming the CR of
// the CRLF line ending.
func aokiFirstLine(content []byte) string {
	line := content
	if i := bytes.IndexByte(content, '\n'); i >= 0 {
		line = content[:i]
	}
	return strings.TrimRight(string(line), "\r")
}
