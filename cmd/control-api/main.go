// Command control-api is the control-plane composition root. It selects each
// adapter for its port from the environment: StaticEngine vs OPA, in-process
// signer vs OpenBao Transit. It serves api/openapi/control-api.yaml.
//
// This is the one place that knows which concrete upstreams are in play; the
// Service and its ports remain adapter-agnostic (ADR-003, dev-docs/architecture).
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// Registers the "pgx" driver with database/sql. The adapter itself depends
	// only on database/sql; the concrete driver is chosen here, at the
	// composition root, and nowhere else.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/QuantmindSSI/haechi/adapters/keycloak"
	"github.com/QuantmindSSI/haechi/adapters/opa"
	"github.com/QuantmindSSI/haechi/adapters/openbao"
	"github.com/QuantmindSSI/haechi/adapters/postgres"
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
	store, closeStore, err := buildStore(ctx, environment)
	if err != nil {
		return err
	}
	defer closeStore()

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

// Environments the control plane recognises. Anything else is refused rather
// than guessed at, because the guess decides whether durable storage is
// mandatory.
var knownEnvironments = map[string]bool{
	"development": true,
	"staging":     true,
	"production":  true,
}

func validateEnvironment(environment string) error {
	if knownEnvironments[environment] {
		return nil
	}
	return fmt.Errorf(
		"unknown environment %q: expected development, staging or production",
		environment,
	)
}

// buildStore selects durable state.
//
// HAECHI_DATABASE_URL selects PostgreSQL. Outside development it is mandatory:
// the in-memory store loses every tenant, device, policy and enrolment code on
// restart, so serving real traffic from it would silently discard control-plane
// state. Refusing to start is the safer failure.
func buildStore(ctx context.Context, environment string) (controlapi.Store, func(), error) {
	url := os.Getenv("HAECHI_DATABASE_URL")
	if url == "" {
		if environment != "development" {
			return nil, nil, fmt.Errorf(
				"HAECHI_DATABASE_URL is required in %s: the in-memory store does not survive a restart",
				environment,
			)
		}
		log.Printf("state: in-memory store (development only, not durable)")
		return controlapi.NewMemoryStore(), func() {}, nil
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	// Bound the pool. An unbounded pool converts a slow database into
	// exhausted connections and a control plane that cannot answer at all.
	db.SetMaxOpenConns(envInt("HAECHI_DB_MAX_OPEN_CONNS", 10))
	db.SetMaxIdleConns(envInt("HAECHI_DB_MAX_IDLE_CONNS", 5))
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Fail fast at startup rather than on the first request.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, nil, fmt.Errorf("database unreachable at startup: %w", err)
	}

	log.Printf("state: PostgreSQL (max open %d)", envInt("HAECHI_DB_MAX_OPEN_CONNS", 10))
	closer := func() {
		if err := db.Close(); err != nil {
			log.Printf("closing database: %v", err)
		}
	}
	return postgres.New(db), closer, nil
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		log.Printf("%s=%q is not a positive integer, using %d", key, raw, fallback)
		return fallback
	}
	return value
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
