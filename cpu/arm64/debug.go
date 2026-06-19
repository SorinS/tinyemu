package arm64

import "os"

// faultDebug, when TINYEMU_ARM64_FAULT=1, dumps the instruction + register state
// at a data/instruction abort to a low (likely-bogus, emulator-bug) address.
var faultDebug = os.Getenv("TINYEMU_ARM64_FAULT") == "1"
