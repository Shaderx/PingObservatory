// Package catalogue contains the starter data embedded in every executable.
package catalogue

import _ "embed"

// DefaultURL is a data-only release channel. Each complete manifest has a
// higher version; its versioned snapshot and pinned evidence remain immutable.
const DefaultURL = "https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json"

//go:embed v3.json
var starter []byte

// Starter returns a copy so callers cannot change the embedded catalogue.
func Starter() []byte { return append([]byte(nil), starter...) }
