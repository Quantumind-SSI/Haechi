// Package capverifier builds the gateway-side capability verifier from a
// published issuer directory (Integration Document §6.3, §9 step 8). The verifier
// admits capabilities locally, without contacting the issuer or identity
// provider, using only the trusted public keys for its own audience and the
// current epoch window.
package capverifier

import (
	"github.com/QuantmindSSI/haechi/pkg/capability"
)

// FromDirectory constructs a fail-closed verifier for a gateway that serves the
// given audience and service classes, accepting only the supplied epochs. Only
// directory entries matching the audience are trusted; everything else is
// implicitly denied (unknown key).
func FromDirectory(
	audience string,
	serviceClasses []string,
	epochs []uint64,
	dir []capability.DirectoryEntry,
	replay capability.ReplayCache,
) *capability.Verifier {
	var keys []capability.VerifierKey
	for _, e := range dir {
		if e.Attrs.Audience != audience {
			continue // never trust keys for other audiences
		}
		keys = append(keys, capability.VerifierKey{Pub: e.Pub, Attrs: e.Attrs})
	}
	return capability.NewVerifier(audience, serviceClasses, epochs, keys, replay)
}
