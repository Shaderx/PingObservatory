// Package catalogue contains the starter data embedded in every executable.
package catalogue

import _ "embed"

//go:embed v1.json
var starter []byte

// Starter returns a copy so callers cannot change the embedded catalogue.
func Starter() []byte { return append([]byte(nil), starter...) }
