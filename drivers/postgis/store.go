// Package postgis provides a small transaction boundary for PostGIS writes.
package postgis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gogis/internal/core"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// Store writes layers to a table with a geometry column and a JSONB properties
// column. Schema creation is intentionally left to migrations.
type Store struct {
	DB    *sql.DB
	Table string
}

// Open opens a pgx-backed database/sql connection.
func Open(ctx context.Context, dsn, table string) (*Store, error) {
	if !identifierPattern.MatchString(table) {
		return nil, fmt.Errorf("invalid PostGIS table identifier %q", table)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, Table: table}, nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.DB.Close() }

// Begin starts a database transaction.
func (s *Store) Begin(ctx context.Context) (*Transaction, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Transaction{tx: tx, table: quoteIdentifier(s.Table)}, nil
}

// Transaction is an all-or-nothing PostGIS writer.
type Transaction struct {
	tx    *sql.Tx
	table string
}

// WriteLayer inserts features as WKT and JSONB properties. The target table
// must expose `geom geometry` and `properties jsonb` columns.
func (t *Transaction) WriteLayer(ctx context.Context, layer core.Layer) error {
	srid, err := srid(layer.CRS.AuthorityCode)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("INSERT INTO %s (geom, properties) VALUES (ST_GeomFromText($1, $2), $3::jsonb)", t.table)
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return err
		}
		geometry, ok := feature.Geometry.(core.WKTGeometry)
		if !ok {
			return fmt.Errorf("feature %d geometry is not core.WKTGeometry", feature.ID)
		}
		properties, err := json.Marshal(feature.Properties)
		if err != nil {
			return fmt.Errorf("feature %d properties: %w", feature.ID, err)
		}
		if _, err := t.tx.ExecContext(ctx, query, geometry.WKT, srid, properties); err != nil {
			return fmt.Errorf("insert feature %d: %w", feature.ID, err)
		}
	}
	return nil
}

// Commit commits all writes.
func (t *Transaction) Commit(ctx context.Context) error { return t.tx.Commit() }

// Rollback discards all writes. It is safe to call after Commit.
func (t *Transaction) Rollback(ctx context.Context) error { return t.tx.Rollback() }

func srid(authority string) (int, error) {
	parts := strings.Split(authority, ":")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "EPSG") {
		return 0, fmt.Errorf("CRS must use EPSG authority, got %q", authority)
	}
	value, err := strconv.Atoi(parts[1])
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid EPSG code %q", authority)
	}
	return value, nil
}

func quoteIdentifier(identifier string) string {
	parts := strings.Split(identifier, ".")
	for i := range parts {
		parts[i] = `"` + strings.ReplaceAll(parts[i], `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}
