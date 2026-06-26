<img src="assets/logo.png" alt="Huego BT logo" width="140">

# Huego BT

Control a Philips Hue light directly over Bluetooth LE from Go. No Hue Bridge required.

[![Go Reference](https://pkg.go.dev/badge/github.com/kkjdaniel/huegobt.svg)](https://pkg.go.dev/github.com/kkjdaniel/huegobt)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Built on [`tinygo.org/x/bluetooth`](https://github.com/tinygo-org/bluetooth), which uses
the native OS BLE stack (CoreBluetooth on macOS).

> Unofficial. Not affiliated with or endorsed by Signify / Philips Hue.

## Pairing

A Hue light only accepts commands from one bonded controller (usually the phone app),
so a new machine is rejected until it pairs. To pair this machine:

1. In the **Philips Hue Bluetooth** app, go to
   Settings → Voice Assistants → [any assistant] → "Make visible".
2. While that's open, run `./huego on`.

The OS stores the bond, so you only do this once per machine.

## Install

```sh
go get github.com/kkjdaniel/huegobt # library
go install github.com/kkjdaniel/huegobt/cmd/huego@latest # CLI
```

## Layout

- `huegobt.go`, `cache.go`: the library (package `huegobt`).
- `cmd/huego/`: a thin CLI on top of the library.

## Library usage

```go
import "github.com/kkjdaniel/huegobt"

light, err := huegobt.Discover(huegobt.ByName("Office Light"), 0) // 0 = default 10s scan
if err != nil {
    log.Fatal(err)
}
defer light.Disconnect()

light.On()
light.Off()
light.SetPower(true)

light.SetBrightness(200) // 1..254 (huegobt.MinBrightness..MaxBrightness)
light.SetColor(0xff, 0x88, 0x00) // 8-bit R, G, B
```

Once you know a light's address (printed on connect), you can skip the name scan:

```go
light, _ := huegobt.Discover(huegobt.ByAddress("c1452770-7032-9fea-7421-aaf1a2ffa8a8"), 0)
```

Or let the library cache it for you. `DiscoverCached` scans the first time, then
reuses the resolved address on later calls (falling back to a scan if it goes stale):

```go
light, _ := huegobt.DiscoverCached("Office Light", 0)
```

## CLI usage

```sh
go build -o huego ./cmd/huego

./huego on                # defaults to --name "Office Light"
./huego off
./huego brightness 30     # percentage, 0-100
./huego warmth 70         # white temperature, 0-100 (0 cool, 100 warm)
./huego color ff8800      # RRGGBB hex (with or without leading #)
./huego on --name Office  # partial, case-insensitive match
./huego scan              # list nearby BLE devices to find a light's name
```

A light shows either a white temperature or an RGB color, not both: `warmth` switches
it to white and `color` switches it to RGB.

The CLI caches each light's resolved address under your OS config dir
(`~/Library/Application Support/huego/addresses.json` on macOS) so repeat calls
skip the name scan.

## Presets

A preset sets brightness and either warmth or color in one command. It doesn't touch
power, so turn the light on first.

```sh
./huego presets         # list available presets
./huego preset relax    # apply one
```

Built-ins: `relax`, `concentrate`, `reading`, `nightlight`, `energize`, `sunset`.

Add your own (or override a built-in by name) in `presets.json` next to the address
cache:

```json
{
  "movie":  { "brightness": 15, "color": "1a0033" },
  "wakeup": { "brightness": 90, "warmth": 60 }
}
```

Each preset can set `brightness` (0-100), and either `warmth` (0-100) or `color`
(RRGGBB hex). From Go, use `light.ApplyPreset("relax")`.

## Use it with an AI agent

`huego` is self-documenting, so an AI coding agent can drive your bulb. Paste this:

```text
I have a CLI called huego that controls a Philips Hue bulb. Run huego help and
huego scan to learn the commands and find my bulb's name, then pass it with
--name and control my light when I ask.
```

## Credits

The Bluetooth characteristic formats were reverse-engineered by the community:
[npaun/philble](https://github.com/npaun/philble) and
[shinyquagsire23's GATT notes](https://gist.github.com/shinyquagsire23/f7907fdf6b470200702e75a30135caf3).
