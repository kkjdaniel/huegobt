// Command huego controls a Philips Hue light over Bluetooth from the terminal.
//
//	huego on | off | brightness 0-100 | warmth 0-100 | color RRGGBB --name NAME
//	huego preset NAME --name NAME | presets | scan
//
// See the huegobt package docs for the one-time "Make visible" pairing step.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"

	"github.com/kkjdaniel/huegobt"
)

func main() {
	name := flag.String("name", "", "match the light by (partial, case-insensitive) name (required; see `huego scan`)")
	flag.Usage = usage
	flag.Parse()

	switch flag.Arg(0) {
	case "on":
		withLight(*name, func(l *huegobt.HueLight) error { return l.On() }, "Light turned on.")
	case "off":
		withLight(*name, func(l *huegobt.HueLight) error { return l.Off() }, "Light turned off.")
	case "brightness":
		setBrightness(*name, flag.Arg(1))
	case "warmth":
		setWarmth(*name, flag.Arg(1))
	case "color":
		setColor(*name, flag.Arg(1))
	case "preset":
		applyPreset(*name, flag.Arg(1))
	case "presets":
		listPresets()
	case "scan":
		scan()
	default:
		usage()
		os.Exit(2)
	}
}

// withLight connects to the named light, runs action, and reports msg. The caller
// must say which light via --name.
func withLight(name string, action func(*huegobt.HueLight) error, msg string) {
	if strings.TrimSpace(name) == "" {
		fail(fmt.Errorf("--name is required (run `huego scan` to find your light's name)"))
	}
	light, err := huegobt.DiscoverCached(name, 0)
	if err != nil {
		fail(err)
	}
	defer light.Disconnect()

	// A cached (by-address) connection has no advertised name; fall back to the
	// requested name so the line is never blank.
	shown := light.Name
	if shown == "" {
		shown = name
	}
	fmt.Printf("Found light: %s  [%s]\n", shown, light.Address)
	if err := action(light); err != nil {
		fail(err)
	}
	fmt.Println(msg)
}

// setBrightness maps a 0-100 percentage onto the bulb's native 1-254 range.
func setBrightness(name, arg string) {
	if arg == "" {
		fail(fmt.Errorf("usage: hue brightness 0-100"))
	}
	pct, err := strconv.Atoi(arg)
	if err != nil || pct < 0 || pct > 100 {
		fail(fmt.Errorf("brightness must be an integer 0-100, got %q", arg))
	}
	level := huegobt.MinBrightness +
		pct*(huegobt.MaxBrightness-huegobt.MinBrightness)/100
	withLight(name,
		func(l *huegobt.HueLight) error { return l.SetBrightness(level) },
		fmt.Sprintf("Brightness set to %d%%.", pct))
}

// setWarmth maps a 0-100 percentage onto the bulb's color-temperature range.
func setWarmth(name, arg string) {
	pct, err := strconv.Atoi(arg)
	if arg == "" || err != nil || pct < 0 || pct > 100 {
		fail(fmt.Errorf("warmth must be an integer 0-100, got %q", arg))
	}
	level := pct * huegobt.MaxWarmth / 100
	withLight(name,
		func(l *huegobt.HueLight) error { return l.SetWarmth(level) },
		fmt.Sprintf("Warmth set to %d%%.", pct))
}

// setColor parses an RRGGBB hex string (with or without a leading #) and applies it.
func setColor(name, arg string) {
	r, g, b, err := parseHexColor(arg)
	if err != nil {
		fail(err)
	}
	withLight(name,
		func(l *huegobt.HueLight) error { return l.SetColor(r, g, b) },
		fmt.Sprintf("Color set to #%02x%02x%02x.", r, g, b))
}

// applyPreset applies a named preset (e.g. "relax") to the light.
func applyPreset(name, preset string) {
	if preset == "" {
		fail(fmt.Errorf("usage: huego preset NAME (see `huego presets`)"))
	}
	withLight(name,
		func(l *huegobt.HueLight) error { return l.ApplyPreset(preset) },
		fmt.Sprintf("Applied preset %q.", preset))
}

// listPresets prints the available preset names and what each sets.
func listPresets() {
	presets := huegobt.Presets()
	names := make([]string, 0, len(presets))
	for n := range presets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := presets[n]
		var parts []string
		if p.Brightness != nil {
			parts = append(parts, fmt.Sprintf("brightness %d%%", *p.Brightness))
		}
		if p.Warmth != nil {
			parts = append(parts, fmt.Sprintf("warmth %d%%", *p.Warmth))
		}
		if p.Color != nil {
			parts = append(parts, "color #"+*p.Color)
		}
		fmt.Printf("  %-12s %s\n", n, strings.Join(parts, ", "))
	}
}

// parseHexColor accepts "RRGGBB" or "#RRGGBB".
func parseHexColor(s string) (r, g, b uint8, err error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, 0, 0, fmt.Errorf("color must be RRGGBB hex (e.g. ff8800), got %q", s)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}

// scan lists nearby BLE devices by signal strength, to help find a light's name.
func scan() {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		fail(err)
	}

	type seen struct {
		name string
		addr string
		rssi int16
	}
	devices := map[string]seen{}

	timer := time.AfterFunc(huegobt.DefaultScanTimeout, func() { adapter.StopScan() })
	defer timer.Stop()

	fmt.Printf("Scanning for %.0fs...\n\n", huegobt.DefaultScanTimeout.Seconds())
	err := adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		addr := r.Address.String()
		// Keep the strongest sighting of each device.
		if prev, ok := devices[addr]; !ok || r.RSSI > prev.rssi {
			devices[addr] = seen{name: r.LocalName(), addr: addr, rssi: r.RSSI}
		}
	})
	if err != nil {
		fail(err)
	}

	list := make([]seen, 0, len(devices))
	for _, d := range devices {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].rssi > list[j].rssi })

	for _, d := range list {
		name := d.name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("%4d dBm  %-28s [%s]\n", d.rssi, name, d.addr)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `huego - control a Philips Hue light over Bluetooth

Usage:
  huego on              --name NAME   turn the light on
  huego off             --name NAME   turn the light off
  huego brightness PCT  --name NAME   set brightness, PCT = 0-100
  huego warmth PCT      --name NAME   set white warmth, PCT = 0-100 (0 cool, 100 warm)
  huego color RRGGBB    --name NAME   set color, hex (e.g. ff8800 or #ff8800)
  huego preset PRESET   --name NAME   apply a preset (see: huego presets)
  huego presets                       list available presets
  huego scan                          list nearby BLE devices

--name selects the light by (partial, case-insensitive) name and is required for
every command that controls a light. Run "huego scan" to find your light's name.
`)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
