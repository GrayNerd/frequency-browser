package main

import (
	"fmt"

	gap "github.com/muesli/go-app-paths"
)

// appDataPath returns the full path of filename inside this application's
// per-user data directory, as resolved by the go-app-paths library. Every file
// the application creates for its own use (settings, import dates, reference
// caches, schedule lists) is persisted there, so user files never land next to
// the executable.
func appDataPath(filename string) (string, error) {
	scope := gap.NewScope(gap.User, "frequency-browser")
	p, err := scope.DataPath(filename)
	if err != nil {
		return "", fmt.Errorf("could not resolve app data path for %q: %w", filename, err)
	}
	return p, nil
}
