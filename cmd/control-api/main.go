// Command control-api is the control-plane composition root. It selects each
// adapter for its port from the environment: StaticEngine vs OPA, in-process
// signer vs OpenBao Transit. It serves api/openapi/control-api.yaml.
//
// This is the one place that knows which concrete upstreams are in play; the
// Service and its ports remain adapter-agnostic (ADR-003, dev-docs/architecture).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/QuantmindSSI/haechi/adapters/keycloak"
	"github.com/QuantmindSSI/haechi/adapters/opa"
	"github.com/QuantmindSSI/haechi/adapters/openbao"
	"github.com/QuantmindSSI/haechi/internal/httpapi"
	"github.com/QuantmindSSI/haechi/internal/signing"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatalf("control-api: %v", err)
	}
}

func run(ctx context.Context) error {
	environment := envDefault("HAECHI_ENV", "development")
	if err := validateEnvironment(environment); err != nil {
		return err
	}
	addr := envDefault("HAECHI_LISTEN", ":8080")
	var engine controlapi.PolicyEngine
	if url := os.Getenv("OPA_URL"); url != "" {
		engine = opa.New(url)
		log.Printf("authz: OPA adapter at %s", url)
	} else {
		engine = controlapi.StaticEngine{}
		log.Printf("authz: StaticEngine (deny-by-default dev adapter)")
	}
	signer, keyID, err := buildSigner(ctx)
	if err != nil {
		return err
	}
	verifier, err := buildIdentityVerifier(environment)
	if err != nil {
		return err
	}
	store := controlapi.NewMemoryStore()
	svc := controlapi.NewService(store, engine, signer, keyID)
	handler := httpapi.New(svc, verifier)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go shutdownOnCancel(ctx, srv)
	log.Printf("control-api listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func validateEnvironment(environment string) error {
	if environment == "development" {
		return nil
	}
	return fmt.Errorf("environment %q is blocked: this build has no durable store", environment)
}

func buildSigner(ctx context.Context) (signing.Signer, string, error) {
	if addr := os.Getenv("OPENBAO_ADDR"); addr != "" {
		s, err := openbao.New(ctx, openbao.Config{
			Address: addr,
			Token:   os.Getenv("OPENBAO_TOKEN"),
			KeyName: envDefault("OPENBAO_KEY", "haechi-control"),
		})
		if err != nil {
			return nil, "", fmt.Errorf("openbao signer: %w", err)
		}
		log.Printf("signer: OpenBao Transit key %q", envDefault("OPENBAO_KEY", "haechi-control"))
		return s, s.KeyID(), nil
	}
	key, err := signing.Generate()
	if err != nil {
		return nil, "", fmt.Errorf("dev signer: %w", err)
	}
	log.Printf("signer: in-process dev key (NOT for production)")
	return key, "control-api-dev-key", nil
}

func buildIdentityVerifier(environment string) (entitlement.IdentityVerifier, error) {
	issuer := strings.TrimRight(os.Getenv("KEYCLOAK_ISSUER"), "/")
	audience := os.Getenv("KEYCLOAK_AUDIENCE")
	if issuer != "" || audience != "" {
		if issuer == "" || audience == "" {
			return nil, errors.New("KEYCLOAK_ISSUER and KEYCLOAK_AUDIENCE must be configured together")
		}
		return keycloak.New(keycloak.Config{Issuer: issuer, Audience: audience}), nil
	}
	devToken := os.Getenv("HAECHI_DEV_ADMIN_TOKEN")
	if environment != "development" || len(devToken) < 32 {
		return nil, errors.New("configure Keycloak or a development admin token of at least 32 characters")
	}
	verifier := entitlement.NewStaticVerifier()
	verifier.Add(devToken, entitlement.Identity{
		Subject:      "local-control-admin",
		Entitlements: []string{httpapi.ControlAdminEntitlement},
	})
	log.Printf("authentication: static development token")
	return verifier, nil
}

func shutdownOnCancel(ctx context.Context, srv *http.Server) {
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
