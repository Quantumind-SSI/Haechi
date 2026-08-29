// Package postgres is the production adapter for the control-api Store port,
// wiring PostgreSQL (third_party/manifest.yaml: postgres 16.4) via database/sql.
// Schema SSOT: deploy/postgres/schema.sql. It is the ONLY place with control-
// plane SQL.
//
// The adapter depends only on the standard library's database/sql. No executable
// currently selects it; the production composition gate is tracked in
// dev-docs/engineering/readiness.md. Array columns use jsonb to remain driver-agnostic.
package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
)

// Store implements controlapi.Store over *sql.DB.
type Store struct{ db *sql.DB }

var _ controlapi.Store = (*Store)(nil)

// New wraps an established *sql.DB (opened by the entrypoint with the pgx driver).
func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateTenant(t model.Tenant, initialPolicy controlapi.TenantPolicy) error {
	classes, transports, dns, err := marshalPolicy(initialPolicy)
	if err != nil {
		return err
	}
	return inTransaction(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO tenants(id,name,idp_issuer,created_at) VALUES($1,$2,$3,$4)`,
			t.ID, t.Name, t.IdPIssuer, t.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(
			`INSERT INTO tenant_policies(tenant_id,allowed_service_classes,allowed_transports,pq_requirement,kill_switch,dns,version)
			 VALUES($1,$2,$3,$4,$5,$6,1)`,
			t.ID, classes, transports, string(initialPolicy.PQRequirement), initialPolicy.KillSwitch, dns)
		return err
	})
}

func (s *Store) GetTenant(id string) (model.Tenant, error) {
	var t model.Tenant
	err := s.db.QueryRow(
		`SELECT id,name,idp_issuer,created_at FROM tenants WHERE id=$1`, id).
		Scan(&t.ID, &t.Name, &t.IdPIssuer, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Tenant{}, controlapi.ErrNotFound
	}
	return t, err
}

func (s *Store) PutEnrollmentCode(c controlapi.EnrollmentCode) error {
	res, err := s.db.Exec(
		`INSERT INTO enrollment_codes(code,tenant_id,challenge,expires_at,used)
		 VALUES($1,$2,$3,$4,$5) ON CONFLICT(code) DO NOTHING`,
		c.Code, c.TenantID, c.Challenge, c.ExpiresAt, c.Used)
	if err != nil {
		return err
	}
	if err := mustAffect(res); err != nil {
		return controlapi.ErrConflict
	}
	return nil
}

func (s *Store) GetEnrollmentCode(code string) (controlapi.EnrollmentCode, error) {
	var c controlapi.EnrollmentCode
	err := s.db.QueryRow(
		`SELECT code,tenant_id,challenge,expires_at,used FROM enrollment_codes WHERE code=$1`, code).
		Scan(&c.Code, &c.TenantID, &c.Challenge, &c.ExpiresAt, &c.Used)
	if errors.Is(err, sql.ErrNoRows) {
		return controlapi.EnrollmentCode{}, controlapi.ErrNotFound
	}
	return c, err
}

func (s *Store) ConsumeEnrollmentCode(code string, now time.Time, d model.Device) (err error) {
	return inTransaction(s.db, func(tx *sql.Tx) error {
		var tenantID string
		var expiresAt time.Time
		var used bool
		err := tx.QueryRow(
			`SELECT tenant_id,expires_at,used FROM enrollment_codes WHERE code=$1 FOR UPDATE`, code).
			Scan(&tenantID, &expiresAt, &used)
		if errors.Is(err, sql.ErrNoRows) {
			return controlapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		if used {
			return controlapi.ErrCodeUsed
		}
		if now.After(expiresAt) {
			return controlapi.ErrCodeStale
		}
		if tenantID != d.TenantID {
			return controlapi.ErrConflict
		}
		if _, err := tx.Exec(
			`INSERT INTO devices(id,tenant_id,public_key,platform,posture,enrolled_at,revoked,revoked_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			d.ID, d.TenantID, d.PublicKey.Bytes(), d.Platform, d.Posture, d.EnrolledAt, d.Revoked, d.RevokedAt); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE enrollment_codes SET used=true WHERE code=$1 AND used=false`, code)
		if err != nil {
			return err
		}
		if err := mustAffect(res); err != nil {
			return controlapi.ErrCodeUsed
		}
		return nil
	})
}

func (s *Store) GetDevice(id string) (model.Device, error) {
	var (
		d      model.Device
		pubRaw []byte
	)
	err := s.db.QueryRow(
		`SELECT id,tenant_id,public_key,platform,posture,enrolled_at,revoked,revoked_at
		 FROM devices WHERE id=$1`, id).
		Scan(&d.ID, &d.TenantID, &pubRaw, &d.Platform, &d.Posture, &d.EnrolledAt, &d.Revoked, &d.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, controlapi.ErrNotFound
	}
	if err != nil {
		return model.Device{}, err
	}
	pub, err := signing.PublicKeyFromBytes(pubRaw)
	if err != nil {
		return model.Device{}, err
	}
	d.PublicKey = pub
	return d, nil
}

func (s *Store) RevokeDevice(id string, at time.Time) error {
	res, err := s.db.Exec(`UPDATE devices SET revoked=true, revoked_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) UpsertGateway(g model.Gateway) error {
	classes, err := json.Marshal(g.ServiceClasses)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(
		`INSERT INTO gateways(id,tenant_id,region,audience,service_classes,endpoint,wireguard_public_key,pq_capable,healthy,load,last_seen)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT(id) DO UPDATE SET region=$3,audience=$4,service_classes=$5,endpoint=$6,
		   wireguard_public_key=$7,pq_capable=$8,healthy=$9,load=$10,last_seen=$11
		 WHERE gateways.tenant_id=$2`,
		g.ID, g.TenantID, g.Region, g.Audience, classes, g.Endpoint, g.WireGuardPublicKey,
		g.PQCapable, g.Healthy, g.Load, g.LastSeen)
	if err != nil {
		return err
	}
	if err := mustAffect(res); err != nil {
		return controlapi.ErrConflict
	}
	return nil
}

func (s *Store) GetGateway(id string) (model.Gateway, error) {
	g, classes, err := scanGateway(s.db.QueryRow(
		`SELECT id,tenant_id,region,audience,service_classes,endpoint,wireguard_public_key,pq_capable,healthy,load,last_seen
		 FROM gateways WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Gateway{}, controlapi.ErrNotFound
	}
	if err != nil {
		return model.Gateway{}, err
	}
	if err := json.Unmarshal(classes, &g.ServiceClasses); err != nil {
		return model.Gateway{}, err
	}
	return g, nil
}

func (s *Store) ListGateways(tenantID string) ([]model.Gateway, error) {
	rows, err := s.db.Query(
		`SELECT id,tenant_id,region,audience,service_classes,endpoint,wireguard_public_key,pq_capable,healthy,load,last_seen
		 FROM gateways WHERE tenant_id=$1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Gateway
	for rows.Next() {
		g, classes, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(classes, &g.ServiceClasses); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceTenantPolicy(tenantID string, p controlapi.TenantPolicy) error {
	classes, transports, dns, err := marshalPolicy(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO tenant_policies(tenant_id,allowed_service_classes,allowed_transports,pq_requirement,kill_switch,dns,version)
		 VALUES($1,$2,$3,$4,$5,$6,1)
		 ON CONFLICT(tenant_id) DO UPDATE SET allowed_service_classes=$2,allowed_transports=$3,
		   pq_requirement=$4,kill_switch=$5,dns=$6,version=tenant_policies.version+1`,
		tenantID, classes, transports, string(p.PQRequirement), p.KillSwitch, dns)
	return err
}

func (s *Store) GetTenantPolicy(tenantID string) (controlapi.TenantPolicy, error) {
	var (
		p          controlapi.TenantPolicy
		classes    []byte
		transports []byte
		dns        []byte
		pq         string
	)
	err := s.db.QueryRow(
		`SELECT allowed_service_classes,allowed_transports,pq_requirement,kill_switch,dns,version
		 FROM tenant_policies WHERE tenant_id=$1`, tenantID).
		Scan(&classes, &transports, &pq, &p.KillSwitch, &dns, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return controlapi.TenantPolicy{}, controlapi.ErrNotFound
	}
	if err != nil {
		return controlapi.TenantPolicy{}, err
	}
	if err := json.Unmarshal(classes, &p.AllowedServiceClasses); err != nil {
		return controlapi.TenantPolicy{}, err
	}
	if err := json.Unmarshal(transports, &p.AllowedTransports); err != nil {
		return controlapi.TenantPolicy{}, err
	}
	if err := json.Unmarshal(dns, &p.DNS); err != nil {
		return controlapi.TenantPolicy{}, err
	}
	p.PQRequirement = model.PQMode(pq)
	return p, nil
}

// rowScanner abstracts *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanGateway(sc rowScanner) (model.Gateway, []byte, error) {
	var g model.Gateway
	var classes []byte
	err := sc.Scan(&g.ID, &g.TenantID, &g.Region, &g.Audience, &classes, &g.Endpoint,
		&g.WireGuardPublicKey, &g.PQCapable, &g.Healthy, &g.Load, &g.LastSeen)
	return g, classes, err
}

func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return controlapi.ErrNotFound
	}
	return nil
}

func marshalPolicy(p controlapi.TenantPolicy) ([]byte, []byte, []byte, error) {
	classes, err := json.Marshal(p.AllowedServiceClasses)
	if err != nil {
		return nil, nil, nil, err
	}
	transports, err := json.Marshal(p.AllowedTransports)
	if err != nil {
		return nil, nil, nil, err
	}
	dns, err := json.Marshal(p.DNS)
	if err != nil {
		return nil, nil, nil, err
	}
	return classes, transports, dns, nil
}

func inTransaction(db *sql.DB, fn func(*sql.Tx) error) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, rollbackErr)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
