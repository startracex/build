package compileropts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
)

// key fingerprints a projection by hashing its JSON directly, avoiding a buffer and field collisions from hand-rolled separators.
func key(v any) string {
	h := sha256.New()
	if err := json.MarshalWrite(h, v); err != nil {
		// Projections hold only scalars, so this cannot happen; keep fields distinct rather than dropping the key.
		h = sha256.New()
		_, _ = fmt.Fprintf(h, "%#v", v)
	}
	return hex.EncodeToString(h.Sum(nil))
}
