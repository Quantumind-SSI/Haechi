// Package wireguard contains the prototype WireGuard UAPI configuration builder.
// It does not create interfaces, configure routes, or implement tunnel.Tunnel.
// The builder enforces the ADR-005 rule that a post-quantum-required route must
// carry a Rosenpass-derived pre-shared key or fail closed.
package wireguard

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/QuantmindSSI/haechi/agent/tunnel"
	"github.com/QuantmindSSI/haechi/internal/model"
)

// Errors.
var (
	ErrBadPeerKey    = errors.New("wireguard: gateway public key is not 32 bytes")
	ErrPQNoPSK       = errors.New("wireguard: post-quantum required but no Rosenpass pre-shared key supplied")
	ErrBadPrivateKey = errors.New("wireguard: device private key is not 32-byte hexadecimal data")
	ErrBadPSK        = errors.New("wireguard: pre-shared key is not 32-byte hexadecimal data")
	ErrBadEndpoint   = errors.New("wireguard: invalid gateway endpoint")
	ErrBadKeepalive  = errors.New("wireguard: keepalive must be between 1 and 65535 seconds")
)

// UAPIParams are the secret inputs held only on-device.
type UAPIParams struct {
	DevicePrivateKeyHex string // 64 hex chars
	PresharedKeyHex     string // Rosenpass-refreshed PSK (ADR-005); empty if none
	KeepaliveSeconds    int
}

// BuildUAPI renders a wireguard-go UAPI set_device payload from Haechi config.
// It fails closed if PQ is required but no PSK is provided (no silent downgrade).
func BuildUAPI(cfg tunnel.Config, p UAPIParams) (string, error) {
	if cfg.PQRequirement == model.PQRequired && p.PresharedKeyHex == "" {
		return "", ErrPQNoPSK
	}
	peerHex, err := base64PubToHex(cfg.GatewayWGPublicKey)
	if err != nil {
		return "", err
	}
	if !validHexKey(p.DevicePrivateKeyHex) {
		return "", ErrBadPrivateKey
	}
	if p.PresharedKeyHex != "" && !validHexKey(p.PresharedKeyHex) {
		return "", ErrBadPSK
	}
	if cfg.GatewayEndpoint != "" {
		host, _, err := net.SplitHostPort(cfg.GatewayEndpoint)
		if err != nil || host == "" || strings.ContainsAny(cfg.GatewayEndpoint, "\r\n\x00") {
			return "", ErrBadEndpoint
		}
	}
	if p.KeepaliveSeconds < 0 || p.KeepaliveSeconds > 65535 {
		return "", ErrBadKeepalive
	}

	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", strings.ToLower(p.DevicePrivateKeyHex))
	b.WriteString("replace_peers=true\n")
	fmt.Fprintf(&b, "public_key=%s\n", peerHex)
	if p.PresharedKeyHex != "" {
		fmt.Fprintf(&b, "preshared_key=%s\n", strings.ToLower(p.PresharedKeyHex))
	}
	if cfg.GatewayEndpoint != "" {
		fmt.Fprintf(&b, "endpoint=%s\n", cfg.GatewayEndpoint)
	}
	ka := p.KeepaliveSeconds
	if ka <= 0 {
		ka = 25
	}
	fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", ka)
	// Route all traffic through the tunnel; the kill switch (enforced by the
	// agent/privileged helper) prevents leakage outside this path.
	b.WriteString("allowed_ip=0.0.0.0/0\n")
	b.WriteString("allowed_ip=::/0\n")
	return b.String(), nil
}

func validHexKey(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32
}

func base64PubToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("wireguard: decode peer key: %w", err)
	}
	if len(raw) != 32 {
		return "", ErrBadPeerKey
	}
	return hex.EncodeToString(raw), nil
}
