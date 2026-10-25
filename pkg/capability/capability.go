package capability

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// Sentinel errors. Every one denies admission; there is no degraded allow path.
var (
	ErrUnknownKey             = errors.New("capability: unknown key id")
	ErrWrongAudience          = errors.New("capability: wrong audience")
	ErrServiceClassNotAllowed = errors.New("capability: service class not permitted at this gateway")
	ErrEpochExpired           = errors.New("capability: epoch outside validity window")
	ErrBadSignature           = errors.New("capability: signature verification failed")
	ErrReplay                 = errors.New("capability: token already spent (replay)")
	ErrReplayCacheFull        = errors.New("capability: replay cache capacity exhausted")
	ErrNoKeyForAttributes     = errors.New("capability: no signing key for attributes")
	ErrFinalizeCheck          = errors.New("capability: finalized signature failed self-check")
	ErrNonceLength            = errors.New("capability: nonce is empty or longer than MaxNonceLen")
)

// Attributes are the coarse, low-cardinality bindings of a capability. They are
// deliberately NOT a browsing destination (ADR-004): audience is a gateway
// class, service class is a coarse tier, epoch bounds validity in time.
type Attributes struct {
	Audience     string `json:"audience"`
	ServiceClass string `json:"service_class"`
	Epoch        uint64 `json:"epoch"`
}

func (a Attributes) canonical() string {
	return fmt.Sprintf("%d:%s%d:%s%d", len(a.Audience), a.Audience, len(a.ServiceClass), a.ServiceClass, a.Epoch)
}

// KeyID identifies the issuer key bound to a specific Attributes tuple. Because
// the message is blinded, attribute authenticity comes from the *key* that
// signs it: a signature that verifies under key K proves the K's attributes.
type KeyID string

func keyIDFromPub(pub *rsa.PublicKey) KeyID {
	h := sha256.New()
	h.Write(pub.N.Bytes())
	var eb [8]byte
	binary.BigEndian.PutUint64(eb[:], uint64(pub.E))
	h.Write(eb[:])
	return KeyID(base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:16]))
}

// Token is the unlinkable bearer capability presented to a gateway. It contains
// no user identifier; the nonce is random and reveals nothing about issuance.
type Token struct {
	KeyID KeyID  `json:"kid"`
	Nonce []byte `json:"nonce"`
	Sig   []byte `json:"sig"`
}

// -------------------------------------------------------------------------
// Issuer
// -------------------------------------------------------------------------

// Issuer holds one RSA key per Attributes tuple and blind-signs capabilities
// after upstream entitlement verification. In production these keys live in
// OpenBao and are pre-generated per epoch; here they are generated lazily.
type Issuer struct {
	mu      sync.RWMutex
	bits    int
	byAttrs map[string]*attrEntry
	byKeyID map[KeyID]*attrEntry
}

type attrEntry struct {
	attrs Attributes
	priv  *rsa.PrivateKey
	keyID KeyID
}

// NewIssuer creates an issuer generating RSA keys with a 2048-bit minimum.
func NewIssuer(bits int) *Issuer {
	if bits < 2048 {
		bits = 2048
	}
	return &Issuer{
		bits:    bits,
		byAttrs: map[string]*attrEntry{},
		byKeyID: map[KeyID]*attrEntry{},
	}
}

func (is *Issuer) ensureKey(attrs Attributes) (*attrEntry, error) {
	ck := attrs.canonical()
	is.mu.RLock()
	e := is.byAttrs[ck]
	is.mu.RUnlock()
	if e != nil {
		return e, nil
	}
	is.mu.Lock()
	defer is.mu.Unlock()
	if e = is.byAttrs[ck]; e != nil {
		return e, nil
	}
	priv, err := rsa.GenerateKey(rand.Reader, is.bits)
	if err != nil {
		return nil, fmt.Errorf("capability: generate issuer key: %w", err)
	}
	e = &attrEntry{attrs: attrs, priv: priv, keyID: keyIDFromPub(&priv.PublicKey)}
	is.byAttrs[ck] = e
	is.byKeyID[e.keyID] = e
	return e, nil
}

// PublicKey returns the key id and RSA public key a client must use to blind a
// capability for the given attributes.
func (is *Issuer) PublicKey(attrs Attributes) (KeyID, *rsa.PublicKey, error) {
	e, err := is.ensureKey(attrs)
	if err != nil {
		return "", nil, err
	}
	return e.keyID, &e.priv.PublicKey, nil
}

// BlindSign signs a blinded capability for attrs. The issuer selects the key by
// attrs (which it may gate on entitlement) but never sees the token itself.
func (is *Issuer) BlindSign(attrs Attributes, blinded []byte) (KeyID, []byte, error) {
	e, err := is.ensureKey(attrs)
	if err != nil {
		return "", nil, err
	}
	sig, err := blindSign(e.priv, blinded)
	if err != nil {
		return "", nil, err
	}
	return e.keyID, sig, nil
}

// DirectoryEntry is a publishable mapping trusted by verifiers.
type DirectoryEntry struct {
	KeyID KeyID
	Attrs Attributes
	Pub   *rsa.PublicKey
}

// Directory returns the public keyset for provisioning verifiers.
func (is *Issuer) Directory() []DirectoryEntry {
	is.mu.RLock()
	defer is.mu.RUnlock()
	out := make([]DirectoryEntry, 0, len(is.byKeyID))
	for _, e := range is.byKeyID {
		out = append(out, DirectoryEntry{KeyID: e.keyID, Attrs: e.attrs, Pub: &e.priv.PublicKey})
	}
	return out
}

// -------------------------------------------------------------------------
// Client
// -------------------------------------------------------------------------

// Blinding is the client-side state between requesting and finalizing a token.
type Blinding struct {
	keyID   KeyID
	attrs   Attributes
	pub     *rsa.PublicKey
	nonce   []byte
	state   *blindState
	Blinded []byte
}

// KeyID is the issuer key the blinded request must be signed with.
func (b *Blinding) KeyID() KeyID { return b.keyID }

// Prepare generates a random nonce and blinds it under the issuer public key.
func Prepare(keyID KeyID, pub *rsa.PublicKey, attrs Attributes) (*Blinding, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	blinded, st, err := blind(pub, nonce)
	if err != nil {
		return nil, err
	}
	return &Blinding{keyID: keyID, attrs: attrs, pub: pub, nonce: nonce, state: st, Blinded: blinded}, nil
}

// Finalize unblinds the issuer's blind signature into a presentable Token and
// self-checks it before returning (fail closed on any inconsistency).
func (b *Blinding) Finalize(blindSig []byte) (*Token, error) {
	sig, err := finalize(b.pub, b.state, blindSig)
	if err != nil {
		return nil, err
	}
	if !verifyBlindRSA(b.pub, b.nonce, sig) {
		return nil, ErrFinalizeCheck
	}
	return &Token{KeyID: b.keyID, Nonce: b.nonce, Sig: sig}, nil
}

// -------------------------------------------------------------------------
// Verifier (gateway side)
// -------------------------------------------------------------------------

// MaxNonceLen bounds the nonce a replay store is asked to record.
//
// Prepare issues 32-byte nonces and a longer one cannot carry a valid
// signature, so this is defence in depth rather than the primary control. It
// exists so that a store backed by durable media can never be asked to write an
// unbounded value on behalf of a remote party.
const MaxNonceLen = 64

// ReplayCache records spent tokens to enforce single use within an epoch.
//
// Release-1 gateways run as more than one process, so a process-local cache
// admits the same token once per process. An implementation must therefore be
// atomic across every verifier sharing an epoch, and must survive a restart.
type ReplayCache interface {
	// Spend atomically records (epoch, nonce). It returns ErrReplay when the
	// token was already spent and must return an error when durability cannot be
	// guaranteed. Verifiers deny admission for every error.
	//
	// The context carries the admission deadline. An implementation that talks
	// to a remote store must honour it and must treat cancellation as a failure
	// to record, never as a successful spend.
	Spend(ctx context.Context, epoch uint64, nonce []byte) error
}

// ReplayPurger releases records for epochs that can no longer be presented.
//
// A verifier accepts only the epochs in its window, so a record older than the
// oldest accepted epoch can never authorise anything again. Purging is separate
// from Spend because it is an operator or scheduler concern, not part of
// admission, and because a store whose retention is handled by the medium
// itself does not need to implement it.
type ReplayPurger interface {
	// PurgeBefore deletes every record for an epoch strictly below the given
	// epoch and reports how many were removed.
	PurgeBefore(ctx context.Context, epoch uint64) (int64, error)
}

// VerifierKey is a trusted issuer key and its authenticated attributes.
type VerifierKey struct {
	Pub   *rsa.PublicKey
	Attrs Attributes
}

// Verifier admits capabilities at a gateway. It is provisioned with its own
// audience, the service classes it serves, the trusted issuer keys for the
// current/previous epochs, and a replay cache. It never contacts the issuer or
// identity provider at admission time (§9 step 8).
type Verifier struct {
	audience       string
	serviceClasses map[string]bool
	validEpochs    map[uint64]bool
	keyset         map[KeyID]VerifierKey
	replay         ReplayCache
}

// NewVerifier builds a fail-closed verifier.
func NewVerifier(audience string, serviceClasses []string, validEpochs []uint64, keys []VerifierKey, replay ReplayCache) *Verifier {
	sc := map[string]bool{}
	for _, c := range serviceClasses {
		sc[c] = true
	}
	ep := map[uint64]bool{}
	for _, e := range validEpochs {
		ep[e] = true
	}
	ks := map[KeyID]VerifierKey{}
	for _, k := range keys {
		ks[keyIDFromPub(k.Pub)] = k
	}
	if replay == nil {
		replay = NewMemoryReplayCache()
	}
	return &Verifier{audience: audience, serviceClasses: sc, validEpochs: ep, keyset: ks, replay: replay}
}

// Verify admits or denies a token. On success it returns the authenticated
// attributes. Every failure denies (fail closed).
//
// The context bounds the replay check, which is the only step that may touch a
// remote store. Every other step is local and constant work.
func (v *Verifier) Verify(ctx context.Context, tok *Token) (Attributes, error) {
	if tok == nil {
		return Attributes{}, ErrBadSignature
	}
	vk, ok := v.keyset[tok.KeyID]
	if !ok {
		return Attributes{}, ErrUnknownKey
	}
	if vk.Attrs.Audience != v.audience {
		return Attributes{}, ErrWrongAudience
	}
	if !v.serviceClasses[vk.Attrs.ServiceClass] {
		return Attributes{}, ErrServiceClassNotAllowed
	}
	if !v.validEpochs[vk.Attrs.Epoch] {
		return Attributes{}, ErrEpochExpired
	}
	if !verifyBlindRSA(vk.Pub, tok.Nonce, tok.Sig) {
		return Attributes{}, ErrBadSignature
	}
	// Replay check LAST so an invalid token never consumes cache space.
	if err := v.replay.Spend(ctx, vk.Attrs.Epoch, tok.Nonce); err != nil {
		return Attributes{}, err
	}
	return vk.Attrs, nil
}

// MemoryReplayCache is a bounded in-memory double-spend cache.
//
// It is correct only for a single process and does not survive a restart, so it
// is the development adapter. Any deployment running more than one verifier for
// an audience must use a shared store; see adapters/postgres.ReplayStore.
type MemoryReplayCache struct {
	mu   sync.Mutex
	seen map[uint64]map[string]struct{}
	n    int
	max  int
}

var (
	_ ReplayCache  = (*MemoryReplayCache)(nil)
	_ ReplayPurger = (*MemoryReplayCache)(nil)
)

// NewMemoryReplayCache returns a cache bounded to a default capacity.
func NewMemoryReplayCache() *MemoryReplayCache {
	return &MemoryReplayCache{seen: map[uint64]map[string]struct{}{}, max: 1 << 20}
}

// Spend records a token exactly once and fails closed when capacity is reached.
//
// Records are grouped by epoch so PurgeBefore is a map delete per epoch rather
// than a scan of every record.
func (m *MemoryReplayCache) Spend(ctx context.Context, epoch uint64, nonce []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(nonce) == 0 || len(nonce) > MaxNonceLen {
		return ErrNonceLength
	}
	key := base64.RawURLEncoding.EncodeToString(nonce)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.seen[epoch][key]; ok {
		return ErrReplay
	}
	if m.n >= m.max {
		return ErrReplayCacheFull
	}
	if m.seen[epoch] == nil {
		m.seen[epoch] = map[string]struct{}{}
	}
	m.seen[epoch][key] = struct{}{}
	m.n++
	return nil
}

// PurgeBefore drops every record for an epoch below the given epoch.
//
// Complexity: O(number of distinct epochs held), which the verifier window
// keeps small.
func (m *MemoryReplayCache) PurgeBefore(ctx context.Context, epoch uint64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var removed int64
	for e, records := range m.seen {
		if e >= epoch {
			continue
		}
		removed += int64(len(records))
		m.n -= len(records)
		delete(m.seen, e)
	}
	return removed, nil
}
