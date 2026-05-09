package main

// JSON parsing for VM configuration.
// Note: The C reference uses a custom JSON parser (json.c) that supports
// comments and unquoted identifiers. This Go implementation uses the standard
// library encoding/json package instead, which is sufficient for config files.
// Reference: tinyemu-2019-12-21/json.c, tinyemu-2019-12-21/json.h

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VMConfigVersion is the expected configuration file version.
const VMConfigVersion = 1

// VMConfig represents the JSON configuration file format.
// Reference: tinyemu-2019-12-21/machine.c:223-404 (virt_machine_parse_config)
type VMConfig struct {
	Version    int    `json:"version"`
	Machine    string `json:"machine"`
	MemorySize uint64 `json:"-"` // Parsed from memory_size (MB)
	BIOS       string `json:"bios"`
	Kernel     string `json:"kernel"`
	Initrd     string `json:"initrd"`
	CmdLine    string `json:"cmdline"`

	// Drives
	Drives []DriveConfig `json:"-"`

	// Filesystems
	Filesystems []FSConfig `json:"-"`

	// Network interfaces
	Networks []NetworkConfig `json:"-"`

	// Display
	Display *DisplayConfig `json:"-"`

	// Input device
	InputDevice string `json:"input_device"`

	// Acceleration
	Accel string `json:"accel"`

	// RTC settings
	RTCLocalTime bool `json:"rtc_local_time"`

	// Internal: path to config file for resolving relative paths
	configDir string
}

// DriveConfig represents a block device configuration.
type DriveConfig struct {
	File   string `json:"file"`
	Device string `json:"device"`
}

// FSConfig represents a filesystem configuration.
type FSConfig struct {
	File string `json:"file"`
	Tag  string `json:"tag"`
}

// NetworkConfig represents a network interface configuration.
type NetworkConfig struct {
	Driver string `json:"driver"`
	IFName string `json:"ifname"`
}

// DisplayConfig represents a display device configuration.
type DisplayConfig struct {
	Device  string `json:"device"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	VGABios string `json:"vga_bios"`
}

// rawConfig is used for initial JSON unmarshaling to handle dynamic fields.
type rawConfig struct {
	Version      int            `json:"version"`
	Machine      string         `json:"machine"`
	MemorySizeMB int            `json:"memory_size"`
	BIOS         string         `json:"bios"`
	Kernel       string         `json:"kernel"`
	Initrd       string         `json:"initrd"`
	CmdLine      string         `json:"cmdline"`
	InputDevice  string         `json:"input_device"`
	Accel        string         `json:"accel"`
	RTCLocalTime bool           `json:"rtc_local_time"`
	Display0     *DisplayConfig `json:"display0"`
	Drive0       *DriveConfig   `json:"drive0"`
	Drive1       *DriveConfig   `json:"drive1"`
	Drive2       *DriveConfig   `json:"drive2"`
	Drive3       *DriveConfig   `json:"drive3"`
	FS0          *FSConfig      `json:"fs0"`
	FS1          *FSConfig      `json:"fs1"`
	FS2          *FSConfig      `json:"fs2"`
	FS3          *FSConfig      `json:"fs3"`
	Eth0         *NetworkConfig `json:"eth0"`
}

// MaxEthDevice is the maximum number of ethernet interfaces.
// Reference: tinyemu-2019-12-21/machine.h:45
const MaxEthDevice = 1

// LoadConfig loads a VM configuration from a JSON file.
// Reference: tinyemu-2019-12-21/machine.c:223-404 (virt_machine_parse_config)
// Reference: tinyemu-2019-12-21/machine.c:515-529 (virt_machine_load_config_file)
func LoadConfig(path string) (*VMConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate version
	if raw.Version != VMConfigVersion {
		if raw.Version > VMConfigVersion {
			return nil, fmt.Errorf("config version %d is too new (expected %d)", raw.Version, VMConfigVersion)
		}
		return nil, fmt.Errorf("config version %d is too old (expected %d)", raw.Version, VMConfigVersion)
	}

	// Validate machine type
	if raw.Machine != "riscv64" && raw.Machine != "riscv32" &&
		raw.Machine != "x86" && raw.Machine != "x86_64" {
		return nil, fmt.Errorf("unsupported machine type: %s", raw.Machine)
	}

	// Validate accel setting
	// Reference: tinyemu-2019-12-21/machine.c:376-387
	if raw.Accel != "" && raw.Accel != "none" && raw.Accel != "auto" {
		return nil, fmt.Errorf("unsupported 'accel' config: %s", raw.Accel)
	}

	cfg := &VMConfig{
		Version:      raw.Version,
		Machine:      raw.Machine,
		MemorySize:   uint64(raw.MemorySizeMB) << 20,
		BIOS:         raw.BIOS,
		Kernel:       raw.Kernel,
		Initrd:       raw.Initrd,
		CmdLine:      substCmdLine(raw.CmdLine),
		InputDevice:  raw.InputDevice,
		Accel:        raw.Accel,
		RTCLocalTime: raw.RTCLocalTime,
		Display:      raw.Display0,
		configDir:    filepath.Dir(path),
	}

	// Collect drives
	for _, d := range []*DriveConfig{raw.Drive0, raw.Drive1, raw.Drive2, raw.Drive3} {
		if d != nil && d.File != "" {
			cfg.Drives = append(cfg.Drives, *d)
		}
	}

	// Collect filesystems
	for i, f := range []*FSConfig{raw.FS0, raw.FS1, raw.FS2, raw.FS3} {
		if f != nil && f.File != "" {
			fs := *f
			if fs.Tag == "" {
				if i == 0 {
					fs.Tag = "/dev/root"
				} else {
					fs.Tag = fmt.Sprintf("/dev/root%d", i)
				}
			}
			cfg.Filesystems = append(cfg.Filesystems, fs)
		}
	}

	// Collect network interfaces
	// Reference: tinyemu-2019-12-21/machine.h:45 - MAX_ETH_DEVICE = 1
	if raw.Eth0 != nil && raw.Eth0.Driver != "" {
		cfg.Networks = append(cfg.Networks, *raw.Eth0)
	}

	// Resolve relative paths
	cfg.BIOS = cfg.resolvePath(cfg.BIOS)
	cfg.Kernel = cfg.resolvePath(cfg.Kernel)
	cfg.Initrd = cfg.resolvePath(cfg.Initrd)
	for i := range cfg.Drives {
		cfg.Drives[i].File = cfg.resolvePath(cfg.Drives[i].File)
	}
	for i := range cfg.Filesystems {
		cfg.Filesystems[i].File = cfg.resolvePath(cfg.Filesystems[i].File)
	}
	if cfg.Display != nil {
		cfg.Display.VGABios = cfg.resolvePath(cfg.Display.VGABios)
	}

	return cfg, nil
}

// resolvePath resolves a path relative to the config file directory.
// Reference: tinyemu-2019-12-21/machine.c:424-446 (get_file_path)
func (cfg *VMConfig) resolvePath(path string) string {
	if path == "" {
		return ""
	}
	// Check for URL (contains ':' like http:// or ftp://)
	// Reference: tinyemu-2019-12-21/machine.c:431-432
	if strings.Contains(path, ":") {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cfg.configDir, path)
}

// substCmdLine performs variable substitution in the command line.
// Currently only supports ${TZ} for timezone.
// Unknown variables are silently removed (matching C behavior).
// Note: C behavior consumes ${...} even if closing brace is missing.
// Reference: tinyemu-2019-12-21/machine.c:129-172
func substCmdLine(cmdline string) string {
	if cmdline == "" {
		return ""
	}

	result := make([]byte, 0, len(cmdline))
	i := 0
	for i < len(cmdline) {
		if i+1 < len(cmdline) && cmdline[i] == '$' && cmdline[i+1] == '{' {
			// Extract variable name (up to '}' or end of string)
			// C limits var_name to 31 chars, matching that behavior
			end := i + 2
			for end < len(cmdline) && cmdline[end] != '}' {
				end++
			}
			varName := cmdline[i+2 : end]
			if len(varName) > 31 {
				varName = varName[:31]
			}
			// Substitute known variables
			if varName == "TZ" {
				result = append(result, []byte(getTimezoneString())...)
			}
			// Unknown variables output nothing (matching C)
			// Advance past closing brace if present
			if end < len(cmdline) && cmdline[end] == '}' {
				i = end + 1
			} else {
				i = end
			}
			continue
		}
		result = append(result, cmdline[i])
		i++
	}
	return string(result)
}

// getTimezoneString returns a timezone string for kernel command line.
// Uses POSIX TZ format where the sign is inverted from conventional notation:
// - East of UTC (positive offset) gets '-' sign
// - West of UTC (negative offset) gets '+' sign
// Reference: tinyemu-2019-12-21/machine.c:149-164
func getTimezoneString() string {
	_, offset := time.Now().Zone()
	// offset is in seconds, convert to minutes
	n := offset / 60
	// POSIX TZ format uses inverted signs
	sign := '-'
	if n < 0 {
		sign = '+'
		n = -n
	}
	return fmt.Sprintf("UTC%c%02d:%02d", sign, n/60, n%60)
}
