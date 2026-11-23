package compileropts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// key fingerprints an options projection. JSON quotes and separates every
// value, so a value that contains a hand-rolled separator cannot collide with
// another field, and the result stays stable across runs.
func key(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		// The projections hold only scalars, so this cannot happen; keep the
		// fields distinct instead of dropping the key if it ever does.
		raw = []byte(fmt.Sprintf("%#v", v))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
