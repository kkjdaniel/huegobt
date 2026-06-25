// Package huegobt controls a Philips Hue light directly over Bluetooth LE,
// without a Hue Bridge.
//
//	light, err := huegobt.Discover(huegobt.ByName("Office Light"), 0)
//	if err != nil { ... }
//	defer light.Disconnect()
//	light.On()
//
// The light must first be bonded to this machine. Hue allows only one bonded
// controller, so to add this one, open the Hue Bluetooth app and do
// Settings -> Voice Assistants -> pick an assistant -> "Make visible", then connect
// within that window. The OS stores the bond, so this is a one-time step.
package huegobt

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

// Characteristics within the "Philips Hue Light Control Service".
const (
	controlServiceUUID = "932c32bd-0000-47a2-835a-a8d455b859dd"
	onOffCharUUID      = "932c32bd-0002-47a2-835a-a8d455b859dd"
	brightnessCharUUID = "932c32bd-0003-47a2-835a-a8d455b859dd"
	colorTempCharUUID  = "932c32bd-0004-47a2-835a-a8d455b859dd"
	colorCharUUID      = "932c32bd-0005-47a2-835a-a8d455b859dd"
)

// Brightness bounds accepted by the bulb.
const (
	MinBrightness = 1
	MaxBrightness = 254
)

// Warmth bounds: 0 is coolest (bluish white), MaxWarmth is warmest (orange white).
const MaxWarmth = 254

// DefaultScanTimeout is used when Discover is given a zero timeout.
const DefaultScanTimeout = 10 * time.Second

// ErrNotFound means no matching light was seen before the scan timed out.
var ErrNotFound = errors.New("huegobt: no matching Hue light found")

var adapter = bluetooth.DefaultAdapter

// Matcher reports whether a scanned device is the light we want.
type Matcher func(bluetooth.ScanResult) bool

// ByName matches a light whose advertised name contains substr (case-insensitive).
func ByName(substr string) Matcher {
	substr = strings.ToLower(substr)
	return func(r bluetooth.ScanResult) bool {
		name := strings.ToLower(r.LocalName())
		return name != "" && strings.Contains(name, substr)
	}
}

// ByAddress matches a light by its host-assigned address (on macOS, a per-host UUID).
func ByAddress(addr string) Matcher {
	addr = strings.ToLower(addr)
	return func(r bluetooth.ScanResult) bool {
		return strings.ToLower(r.Address.String()) == addr
	}
}

// HueLight is a connected Hue light. Not safe for concurrent use; always Disconnect.
type HueLight struct {
	Name    string // advertised local name, if seen during discovery
	Address string // host-assigned address; pass to ByAddress to reconnect

	device     bluetooth.Device
	onOff      bluetooth.DeviceCharacteristic
	brightness bluetooth.DeviceCharacteristic
	colorTemp  bluetooth.DeviceCharacteristic
	color      bluetooth.DeviceCharacteristic
}

// Discover scans for a light matching m and connects to it. A zero timeout means
// DefaultScanTimeout. The returned HueLight owns a live connection; call Disconnect.
func Discover(m Matcher, timeout time.Duration) (*HueLight, error) {
	if timeout <= 0 {
		timeout = DefaultScanTimeout
	}
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("huegobt: enable BLE adapter: %w", err)
	}

	result, err := scan(m, timeout)
	if err != nil {
		return nil, err
	}

	device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("huegobt: connect to %q: %w", result.LocalName(), err)
	}

	light := &HueLight{
		Name:    result.LocalName(),
		Address: result.Address.String(),
		device:  device,
	}
	if err := light.resolveCharacteristics(); err != nil {
		device.Disconnect()
		return nil, err
	}
	return light, nil
}

// scan blocks until m matches a device or the timeout fires StopScan.
func scan(m Matcher, timeout time.Duration) (bluetooth.ScanResult, error) {
	var (
		found    bluetooth.ScanResult
		hasMatch bool
	)
	timer := time.AfterFunc(timeout, func() { adapter.StopScan() })
	defer timer.Stop()

	err := adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
		if !hasMatch && m(r) {
			hasMatch = true
			found = r
			a.StopScan()
		}
	})
	if err != nil {
		return bluetooth.ScanResult{}, fmt.Errorf("huegobt: scan: %w", err)
	}
	if !hasMatch {
		return bluetooth.ScanResult{}, ErrNotFound
	}
	return found, nil
}

// resolveCharacteristics finds and caches the On/Off, Brightness, and Color handles.
func (l *HueLight) resolveCharacteristics() error {
	svcUUID, err := bluetooth.ParseUUID(controlServiceUUID)
	if err != nil {
		return fmt.Errorf("huegobt: parse service uuid: %w", err)
	}
	services, err := l.device.DiscoverServices([]bluetooth.UUID{svcUUID})
	if err != nil {
		return fmt.Errorf("huegobt: discover control service: %w", err)
	}
	if len(services) == 0 {
		return errors.New("huegobt: Hue Light Control Service not found on device")
	}

	wanted := map[string]*bluetooth.DeviceCharacteristic{
		onOffCharUUID:      &l.onOff,
		brightnessCharUUID: &l.brightness,
		colorTempCharUUID:  &l.colorTemp,
		colorCharUUID:      &l.color,
	}
	uuids := make([]bluetooth.UUID, 0, len(wanted))
	for s := range wanted {
		u, err := bluetooth.ParseUUID(s)
		if err != nil {
			return fmt.Errorf("huegobt: parse characteristic uuid %s: %w", s, err)
		}
		uuids = append(uuids, u)
	}

	chars, err := services[0].DiscoverCharacteristics(uuids)
	if err != nil {
		return fmt.Errorf("huegobt: discover characteristics: %w", err)
	}
	found := map[string]bool{}
	for _, c := range chars {
		uuid := c.UUID().String()
		if field, ok := wanted[uuid]; ok {
			*field = c
			found[uuid] = true
		}
	}
	// On/Off is required; brightness/color are optional and guarded at call time.
	if !found[onOffCharUUID] {
		return errors.New("huegobt: On/Off characteristic not found")
	}
	return nil
}

// SetPower turns the light on (true) or off (false).
func (l *HueLight) SetPower(on bool) error {
	var b byte
	if on {
		b = 0x01
	}
	if _, err := l.onOff.Write([]byte{b}); err != nil {
		return fmt.Errorf("huegobt: write on/off (%v): %w", on, classifyWriteErr(err))
	}
	return nil
}

// On turns the light on.
func (l *HueLight) On() error { return l.SetPower(true) }

// Off turns the light off.
func (l *HueLight) Off() error { return l.SetPower(false) }

// SetBrightness sets the level, clamped to [MinBrightness, MaxBrightness]. It does
// not turn the light on.
func (l *HueLight) SetBrightness(level int) error {
	if l.brightness == (bluetooth.DeviceCharacteristic{}) {
		return errors.New("huegobt: this light has no brightness characteristic")
	}
	if level < MinBrightness {
		level = MinBrightness
	}
	if level > MaxBrightness {
		level = MaxBrightness
	}
	if _, err := l.brightness.Write([]byte{byte(level)}); err != nil {
		return fmt.Errorf("huegobt: write brightness: %w", classifyWriteErr(err))
	}
	return nil
}

// SetColor sets the color from 8-bit R, G, B. This sets hue, not brightness (use
// SetBrightness for intensity); see encodeColor for the wire format.
func (l *HueLight) SetColor(r, g, b uint8) error {
	if l.color == (bluetooth.DeviceCharacteristic{}) {
		return errors.New("huegobt: this light has no color characteristic")
	}
	payload := encodeColor(r, g, b)
	if _, err := l.color.Write(payload); err != nil {
		return fmt.Errorf("huegobt: write color: %w", classifyWriteErr(err))
	}
	return nil
}

// SetWarmth sets the white color temperature, clamped to [0, MaxWarmth] where 0 is
// coolest and MaxWarmth is warmest. The bulb is either showing a white temperature
// or an RGB color, never both, so this switches the light back to white mode and
// clears any color set by SetColor (and vice versa).
func (l *HueLight) SetWarmth(warmth int) error {
	if l.colorTemp == (bluetooth.DeviceCharacteristic{}) {
		return errors.New("huegobt: this light has no color-temperature characteristic")
	}
	if warmth < 0 {
		warmth = 0
	}
	if warmth > MaxWarmth {
		warmth = MaxWarmth
	}
	// Format: [TT, 0x01] where TT is the warmth and 0x01 enables temperature mode.
	if _, err := l.colorTemp.Write([]byte{byte(warmth), 0x01}); err != nil {
		return fmt.Errorf("huegobt: write warmth: %w", classifyWriteErr(err))
	}
	return nil
}

// encodeColor builds the bulb's [0x01, R, B, G] payload: channels scaled to sum to
// ~255, with blue and green swapped on the wire. Each channel has a floor of 1
// because the bulb rejects a zero channel or total.
func encodeColor(r, g, b uint8) []byte {
	cr, cg, cb := max8(r), max8(g), max8(b)
	total := int(cr) + int(cg) + int(cb)
	scale := func(c uint8) byte {
		return byte((int(c)*0xff + total/2) / total)
	}
	return []byte{0x01, scale(cr), scale(cb), scale(cg)}
}

func max8(c uint8) uint8 {
	if c < 1 {
		return 1
	}
	return c
}

// Disconnect closes the connection. The OS-level bond is unaffected.
func (l *HueLight) Disconnect() error {
	return l.device.Disconnect()
}

// classifyWriteErr annotates encryption-rejection errors, which mean this host is
// not bonded (see the package doc's "Make visible" step).
func classifyWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "encrypt") {
		return fmt.Errorf("%w (the light rejected an unencrypted write: this host "+
			"may not be bonded; see the huegobt package docs on \"Make visible\")", err)
	}
	return err
}
