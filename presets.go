package huegobt

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// Preset is a saved look: an optional brightness, and optionally either a warmth
// or a color. Values use the same 0-100 scale as the CLI. A preset never changes
// power; turn the light on separately. Brightness is applied first, then the
// warmth/color, since setting warmth or color can change the light's mode.
type Preset struct {
	Brightness *int    `json:"brightness,omitempty"` // 0-100
	Warmth     *int    `json:"warmth,omitempty"`     // 0-100, 0 cool .. 100 warm
	Color      *string `json:"color,omitempty"`      // RRGGBB hex
}

// builtinPresets ship with the library. User presets with the same name override these.
var builtinPresets = map[string]Preset{
	"relax":       {Brightness: ptr(40), Warmth: ptr(90)},
	"concentrate": {Brightness: ptr(100), Warmth: ptr(20)},
	"reading":     {Brightness: ptr(80), Warmth: ptr(55)},
	"nightlight":  {Brightness: ptr(5), Warmth: ptr(100)},
	"energize":    {Brightness: ptr(100), Warmth: ptr(0)},
	"sunset":      {Brightness: ptr(60), Color: strptr("ff5500")},
}

// Presets returns all known presets (built-ins merged with the user's presets.json,
// where user entries win on name collision).
func Presets() map[string]Preset {
	out := map[string]Preset{}
	maps.Copy(out, builtinPresets)
	for name, p := range loadUserPresets() {
		out[strings.ToLower(name)] = p
	}
	return out
}

// ApplyPreset looks up name (case-insensitive) and applies it to the light.
func (l *HueLight) ApplyPreset(name string) error {
	p, ok := Presets()[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return fmt.Errorf("huegobt: no preset named %q", name)
	}
	return l.Apply(p)
}

// Apply sets the light to the preset. Brightness is applied before warmth/color.
func (l *HueLight) Apply(p Preset) error {
	if p.Brightness != nil {
		if err := l.SetBrightness(pctToBrightness(*p.Brightness)); err != nil {
			return err
		}
	}
	switch {
	case p.Color != nil:
		r, g, b, err := parseHex(*p.Color)
		if err != nil {
			return err
		}
		return l.SetColor(r, g, b)
	case p.Warmth != nil:
		return l.SetWarmth(*p.Warmth * MaxWarmth / 100)
	}
	return nil
}

// pctToBrightness maps a 0-100 percentage onto [MinBrightness, MaxBrightness].
func pctToBrightness(pct int) int {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return MinBrightness + pct*(MaxBrightness-MinBrightness)/100
}

// parseHex reads "RRGGBB" or "#RRGGBB" into 8-bit components.
func parseHex(s string) (r, g, b uint8, err error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, 0, 0, fmt.Errorf("huegobt: color must be RRGGBB hex, got %q", s)
	}
	var v uint64
	if _, err := fmt.Sscanf(s, "%06x", &v); err != nil {
		return 0, 0, 0, fmt.Errorf("huegobt: invalid hex color %q", s)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}

// loadUserPresets reads presets.json from the config dir, or an empty map on any error.
func loadUserPresets() map[string]Preset {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "huego", "presets.json"))
	if err != nil {
		return nil
	}
	m := map[string]Preset{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

func ptr(i int) *int          { return &i }
func strptr(s string) *string { return &s }
