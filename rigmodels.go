package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// RigModel is one radio Hamlib supports. ID is the internal model number
// Hamlib assigns, which is exactly what rigctld's -m option takes (3078 is the
// Icom IC-7610).
type RigModel struct {
	ID     int    `json:"id"`
	Mfg    string `json:"mfg"`
	Model  string `json:"model"`
	Status string `json:"status"`

	// PortType is how this backend reaches the radio: "RS-232", "USB",
	// "Network link", "None", and so on. It decides whether the Serial Port
	// field applies at all, so the dialog can stop a serial device being
	// handed to a network or virtual backend.
	PortType string `json:"portType"`
}

// SerialPort is one candidate CAT device under /dev. Label carries the stable
// /dev/serial/by-id description when the system provides one, so two identical
// adapters can be told apart.
type SerialPort struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// rigListTimeout bounds the model-list call; `rigctld -l` answers immediately.
var rigListTimeout = 10 * time.Second

// GetRigModels lists the radios known to the rigctld binary this app will
// launch, so the model numbers offered always match that binary.
func (a *App) GetRigModels() ([]RigModel, error) {
	bin, err := rigctldPath()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), rigListTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("could not list rig models from %s: %w", bin, err)
	}
	models := parseRigModels(string(out))
	if len(models) == 0 {
		return nil, fmt.Errorf("%s -l returned no rig models", bin)
	}

	// Look up each model's capabilities so the dialog knows which ones take a
	// serial port. This is cheap (a few milliseconds per model) and is done
	// in parallel; a model whose capabilities cannot be read simply keeps an
	// empty PortType, which the UI treats as "unknown, allow a serial port".
	fillPortTypes(ctx, bin, models)
	return models, nil
}

// fillPortTypes sets RigModel.PortType from `rigctld -m <id> -u`.
func fillPortTypes(ctx context.Context, bin string, models []RigModel) {
	const workers = 8
	ids := make(chan int)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ids {
				out, err := exec.CommandContext(ctx, bin, "-m", strconv.Itoa(models[idx].ID), "-u").Output()
				if err != nil {
					continue
				}
				models[idx].PortType = parsePortType(string(out))
			}
		}()
	}
	for idx := range models {
		ids <- idx
	}
	close(ids)
	wg.Wait()
}

// parsePortType pulls the "Port type:" line out of a capabilities dump.
func parsePortType(caps string) string {
	for _, line := range strings.Split(caps, "\n") {
		if !strings.HasPrefix(line, "Port type:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Port type:"))
		value = strings.TrimSpace(strings.TrimSuffix(value, "(deprecated)"))
		return value
	}
	return ""
}

// serialBackend reports whether a rig backend is driven through a serial port.
// Backends with an empty PortType are treated as serial (unknown => allow), so a
// hamlib version that does not report capabilities never blocks the user.
func serialBackend(portType string) bool {
	switch portType {
	case "Network link", "None":
		return false
	default:
		return true
	}
}

// rigctlPath resolves the rigctl utility through PATH. It is only used for
// informational queries such as the version banner.
func rigctlPath() (string, error) {
	p, err := exec.LookPath("rigctl")
	if err != nil {
		return "", fmt.Errorf("rigctl was not found on PATH; install the Hamlib utilities")
	}
	return p, nil
}

// GetRigctlVersion returns Hamlib's version banner, as printed by `rigctl -V`.
// The Settings window shows it so the user can tell which hamlib is actually
// installed, which matters when a rig model needs a recent release.
func (a *App) GetRigctlVersion() (string, error) {
	bin, err := rigctlPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), rigCapsTimeout)
	defer cancel()

	// `rigctl -V` prints the banner and exits; it prints to stdout on the
	// versions this app targets, so Output() captures it. CombinedOutput is used
	// anyway because a build that writes the banner to stderr must still work.
	out, err := exec.CommandContext(ctx, bin, "-V").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("could not read the rigctl version: %w", err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The banner starts with the program name; anything else on the first
		// line (a wrapper script's noise) is skipped.
		if strings.HasPrefix(line, "rigctl") {
			return line, nil
		}
	}
	return "", fmt.Errorf("rigctl -V printed no version line")
}

// rigctldPath resolves the daemon binary through PATH, the same way the app
// starts it.
func rigctldPath() (string, error) {
	p, err := exec.LookPath("rigctld")
	if err != nil {
		return "", fmt.Errorf("rigctld was not found on PATH; install the Hamlib utilities")
	}
	return p, nil
}

// parseRigModels reads the fixed-width table `rigctld -l` prints:
//
//	Rig #  Mfg                    Model                   Version         Status      Macro
//	 3078  Icom                   IC-7610                 20230109.11     Stable      RIG_MODEL_IC7610
//
// The columns are rig# 0-6, mfg 8-30, model 31-54, version 55-70, status 71-82,
// macro 83- . Rows whose layout does not match are retried with a split on runs
// of spaces, and anything unparseable is skipped: a single odd entry (Hamlib
// prints "(null)" for one macro) must not break the whole list.
func parseRigModels(out string) []RigModel {
	var models []RigModel
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		idText, mfg, model, status := rigListFields(line)
		id, err := strconv.Atoi(idText)
		if err != nil || id <= 0 || model == "" {
			continue
		}
		models = append(models, RigModel{ID: id, Mfg: mfg, Model: model, Status: status})
	}

	// Group by manufacturer so the dropdown can show one optgroup per maker.
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Mfg != models[j].Mfg {
			return models[i].Mfg < models[j].Mfg
		}
		if models[i].Model != models[j].Model {
			return models[i].Model < models[j].Model
		}
		return models[i].ID < models[j].ID
	})
	return models
}

// rigListSeparator splits a row on runs of two or more spaces, which is the
// fallback when a row does not match the fixed-width layout.
var rigListSeparator = regexp.MustCompile("[ ]{2,}")

// rigListFields pulls the model number, manufacturer, model name and status out
// of one `rigctld -l` row.
func rigListFields(line string) (id, mfg, model, status string) {
	if len(line) >= 83 {
		id = strings.TrimSpace(line[0:7])
		mfg = strings.TrimSpace(line[8:31])
		model = strings.TrimSpace(line[31:55])
		status = strings.TrimSpace(line[71:83])
		if _, err := strconv.Atoi(id); err == nil && model != "" {
			return id, mfg, model, status
		}
	}
	parts := rigListSeparator.Split(strings.TrimSpace(line), -1)
	if len(parts) >= 3 {
		return parts[0], parts[1], parts[2], ""
	}
	return "", "", "", ""
}

// GetSerialPorts lists the serial devices on this machine for the Serial Port
// dropdown. Every /dev entry is offered, including the phantom ttyS ports that
// most PCs have: the dialog shows what the system exposes.
func (a *App) GetSerialPorts() ([]SerialPort, error) {
	return listSerialPorts("/dev", "/dev/serial/by-id")
}

// serialPortPatterns are the device names CAT adapters appear as: motherboard
// and add-in UARTs (ttyS), USB adapters (ttyUSB, ttyACM), ARM SoCs (ttyAMA),
// Bluetooth serial (rfcomm) and Exar cards (ttyXR).
var serialPortPatterns = []string{"ttyS*", "ttyUSB*", "ttyACM*", "ttyAMA*", "ttyXR*", "rfcomm*"}

func listSerialPorts(devDir, byIDDir string) ([]SerialPort, error) {
	seen := make(map[string]bool)
	var ports []SerialPort
	for _, pattern := range serialPortPatterns {
		matches, err := filepath.Glob(filepath.Join(devDir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if seen[match] {
				continue
			}
			seen[match] = true
			ports = append(ports, SerialPort{Path: match, Label: match})
		}
	}

	labels := serialPortLabels(byIDDir)
	for i := range ports {
		if label := labels[filepath.Base(ports[i].Path)]; label != "" {
			// The label is shown in a dropdown that must fit the settings
			// window, so the long USB descriptor is trimmed here rather than
			// forcing the dialog to scroll sideways. The full description is
			// still available from the device's by-id name.
			ports[i].Label = ports[i].Path + " - " + shortenLabel(label, 40)
		}
	}

	sort.Slice(ports, func(i, j int) bool { return portNameLess(ports[i].Path, ports[j].Path) })
	if len(ports) == 0 {
		return nil, fmt.Errorf("no serial ports found in %s", devDir)
	}
	return ports, nil
}

// serialPortLabels maps /dev/ttyUSB0 to its /dev/serial/by-id description, so
// an adapter can be recognised as "the IC-7610 one" rather than by number.
func serialPortLabels(byIDDir string) map[string]string {
	labels := make(map[string]string)
	entries, err := os.ReadDir(byIDDir)
	if err != nil {
		return labels
	}
	for _, entry := range entries {
		target, err := filepath.EvalSymlinks(filepath.Join(byIDDir, entry.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimPrefix(entry.Name(), "usb-")
		name = strings.TrimSuffix(name, "-if00-port0")
		name = strings.TrimSuffix(name, "-if01-port0")
		name = strings.ReplaceAll(name, "_", " ")
		labels[filepath.Base(target)] = name
	}
	return labels
}

// shortenLabel cuts a device description down to at most max bytes, on a word
// boundary where possible, appending an ellipsis when it had to cut. It never
// splits a multi-byte UTF-8 sequence.
func shortenLabel(label string, max int) string {
	if len(label) <= max {
		return label
	}
	cut := max - 1 // leave room for the ellipsis
	for cut > 0 && !utf8.RuneStart(label[cut]) {
		cut--
	}
	// Prefer cutting at the last space so the result reads as words.
	if space := strings.LastIndexByte(label[:cut], ' '); space > cut/2 {
		cut = space
	}
	return strings.TrimRight(label[:cut], " _-") + "…"
}

// portNameLess orders /dev/ttyS2 before /dev/ttyS10 and keeps the device
// families together.
func portNameLess(a, b string) bool {
	prefixA, numberA := splitTrailingNumber(a)
	prefixB, numberB := splitTrailingNumber(b)
	if prefixA != prefixB {
		return prefixA < prefixB
	}
	return numberA < numberB
}

func splitTrailingNumber(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(s[i:])
	return s[:i], n
}
