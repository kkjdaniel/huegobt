package huegobt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cachePath is the address cache file under the user's config dir, e.g.
// ~/Library/Application Support/huego/addresses.json on macOS.
func cachePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "huego")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "addresses.json"), nil
}

// loadCache reads the name->address map, or an empty map on any error.
func loadCache() map[string]string {
	path, err := cachePath()
	if err != nil {
		return map[string]string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

// rememberAddress records name->address for a later DiscoverCached.
func rememberAddress(name, address string) error {
	if name == "" || address == "" {
		return nil
	}
	path, err := cachePath()
	if err != nil {
		return err
	}
	m := loadCache()
	m[cacheKey(name)] = address
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func cacheKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// DiscoverCached connects to a light by name, reusing a cached address to skip the
// scan. It scans on the first call (or if the cached address is stale), then records
// the result. A zero timeout means DefaultScanTimeout.
func DiscoverCached(name string, timeout time.Duration) (*HueLight, error) {
	if addr, ok := loadCache()[cacheKey(name)]; ok {
		if light, err := Discover(ByAddress(addr), timeout); err == nil {
			return light, nil
		}
	}

	light, err := Discover(ByName(name), timeout)
	if err != nil {
		return nil, err
	}
	if err := rememberAddress(name, light.Address); err != nil {
		return light, fmt.Errorf("huegobt: connected, but caching address failed: %w", err)
	}
	return light, nil
}
