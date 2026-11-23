package compileropts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
)

// key fingerprints an options projection by streaming its JSON straight into
// the hash, so no intermediate buffer is built. JSON quotes and separates every
// value, so a value that contains a hand-rolled separator cannot collide with
// another field, and the result stays stable across runs.
func key(v any) string {
	h := sha256.New()
	if err := json.MarshalWrite(h, v); err != nil {
		// The projections hold only scalars, so this cannot happen; keep the
		// fields distinct instead of dropping the key if it ever does.
		h = sha256.New()
		_, _ = fmt.Fprintf(h, "%#v", v)
	}
	return hex.EncodeToString(h.Sum(nil))
}
