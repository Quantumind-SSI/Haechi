package wireguard

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantmindSSI/haechi/agent/tunnel"
	"github.com/QuantmindSSI/haechi/internal/model"
)

func randPeerKeyB64(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func TestBuildUAPIBasic(t *testing.T) {
	cfg := tunnel.Config{
		GatewayEndpoint:    "gw.example:51820",
		GatewayWGPublicKey: randPeerKeyB64(t),
		PQRequirement:      model.PQPreferred,
	}
	out, err := BuildUAPI(cfg, UAPIParams{DevicePrivateKeyHex: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{"private_key=", "public_key=", "endpoint=gw.example:51820", "allowed_ip=0.0.0.0/0", "persistent_keepalive_interval=25"} {
		if !strings.Contains(out, want) {
			t.Fatalf("UAPI missing %q:\n%s", want, out)
		}
	}
}

func TestBuildUAPIPQRequiresPSK(t *testing.T) {
	cfg := tunnel.Config{
		GatewayWGPublicKey: randPeerKeyB64(t),
		PQRequirement:      model.PQRequired,
	}
	// No PSK -> must fail closed.
	if _, err := BuildUAPI(cfg, UAPIParams{DevicePrivateKeyHex: strings.Repeat("ab", 32)}); err != ErrPQNoPSK {
		t.Fatalf("want ErrPQNoPSK, got %v", err)
	}
	// With Rosenpass PSK -> succeeds and includes it.
	out, err := BuildUAPI(cfg, UAPIParams{
		DevicePrivateKeyHex: strings.Repeat("ab", 32),
		PresharedKeyHex:     strings.Repeat("cd", 32),
	})
	if err != nil {
		t.Fatalf("build with psk: %v", err)
	}
	if !strings.Contains(out, "preshared_key=") {
		t.Fatal("PSK not written to UAPI")
	}
}

func TestBuildUAPIRejectsBadPeerKey(t *testing.T) {
	cfg := tunnel.Config{GatewayWGPublicKey: base64.StdEncoding.EncodeToString([]byte("too-short"))}
	if _, err := BuildUAPI(cfg, UAPIParams{DevicePrivateKeyHex: "ab"}); err != ErrBadPeerKey {
		t.Fatalf("want ErrBadPeerKey, got %v", err)
	}
}

func TestBuildUAPIRejectsInjectedOrMalformedFields(t *testing.T) {
	validPrivateKey := strings.Repeat("ab", 32)
	validPeerKey := randPeerKeyB64(t)
	tests := []struct {
		name   string
		cfg    tunnel.Config
		params UAPIParams
		want   error
	}{
		{
			name: "private key", cfg: tunnel.Config{GatewayWGPublicKey: validPeerKey},
			params: UAPIParams{DevicePrivateKeyHex: "ab\nreplace_peers=false"}, want: ErrBadPrivateKey,
		},
		{
			name: "pre-shared key", cfg: tunnel.Config{GatewayWGPublicKey: validPeerKey},
			params: UAPIParams{DevicePrivateKeyHex: validPrivateKey, PresharedKeyHex: "invalid"}, want: ErrBadPSK,
		},
		{
			name: "endpoint", cfg: tunnel.Config{
				GatewayWGPublicKey: validPeerKey, GatewayEndpoint: "gw.example:51820\nreplace_peers=false",
			},
			params: UAPIParams{DevicePrivateKeyHex: validPrivateKey}, want: ErrBadEndpoint,
		},
		{
			name: "keepalive", cfg: tunnel.Config{GatewayWGPublicKey: validPeerKey},
			params: UAPIParams{DevicePrivateKeyHex: validPrivateKey, KeepaliveSeconds: 65536}, want: ErrBadKeepalive,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildUAPI(test.cfg, test.params); err != test.want {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
