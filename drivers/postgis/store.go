// Package postgis provides a small transaction boundary for PostGIS writes.
package postgis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gogis/drivers"
	"gogis/internal/core"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// Store reads and writes layers to a table with id, geometry, and JSONB
// properties columns. Attribute columns are intentionally represented in the
// JSONB document so schema evolution does not require a migration per field.
type Store struct {
	DB    *sql.DB
	Table string
}

var _ drivers.TransactionalStore = (*Store)(nil)

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
func (s *Store) Begin(ctx context.Context) (drivers.Transaction, error) {
	return s.begin(ctx)
}

func (s *Store) begin(ctx context.Context) (*Transaction, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Transaction{tx: tx, table: quoteIdentifier(s.Table)}, nil
}

// ReadLayer reads the configured table into a detached core layer snapshot.
func (s *Store) ReadLayer(ctx context.Context) (core.Layer, error) {
	if s == nil || s.DB == nil {
		return core.Layer{}, fmt.Errorf("PostGIS database is not configured")
	}
	rows, err := s.DB.QueryContext(ctx, readLayerQuery(s.Table))
	if err != nil {
		return core.Layer{}, fmt.Errorf("read PostGIS layer %q: %w", s.Table, err)
	}
	defer rows.Close()

	layer := core.Layer{Name: tableName(s.Table), Editable: true}
	for rows.Next() {
		var id int64
		var wkt string
		var srid int
		var properties []byte
		if err := rows.Scan(&id, &wkt, &srid, &properties); err != nil {
			return core.Layer{}, fmt.Errorf("scan PostGIS feature: %w", err)
		}
		decoded := map[string]any{}
		if len(properties) != 0 {
			if err := json.Unmarshal(properties, &decoded); err != nil {
				return core.Layer{}, fmt.Errorf("feature %d properties: %w", id, err)
			}
		}
		layer.Features = append(layer.Features, core.Feature{
			ID:         uint64(id),
			Geometry:   core.WKTGeometry{WKT: wkt},
			Properties: decoded,
		})
		if layer.CRS.AuthorityCode == "" && srid > 0 {
			layer.CRS.AuthorityCode = "EPSG:" + strconv.Itoa(srid)
		}
	}
	if err := rows.Err(); err != nil {
		return core.Layer{}, fmt.Errorf("iterate PostGIS layer: %w", err)
	}
	layer.Fields = inferFields(layer.Features)
	return layer, nil
}

// SaveLayer creates the minimal table schema and atomically replaces the
// configured table snapshot with the supplied layer.
func (s *Store) SaveLayer(ctx context.Context, layer core.Layer) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tx.EnsureSchema(ctx); err != nil {
		return err
	}
	if err := tx.ClearLayer(ctx); err != nil {
		return err
	}
	if err := tx.WriteLayer(ctx, layer); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Transaction is an all-or-nothing PostGIS writer.
type Transaction struct {
	tx    *sql.Tx
	table string
}

// EnsureSchema creates the storage columns used by WriteLayer if they do not
// exist. PostGIS itself must already be installed in the database.
func (t *Transaction) EnsureSchema(ctx context.Context) error {
	if _, err := t.tx.ExecContext(ctx, schemaQuery(t.table)); err != nil {
		return fmt.Errorf("create PostGIS schema for %s: %w", t.table, err)
	}
	return nil
}

// ClearLayer removes the previous snapshot inside the active transaction.
// SaveLayer therefore behaves as an atomic replacement rather than appending
// rows to an existing table.
func (t *Transaction) ClearLayer(ctx context.Context) error {
	if _, err := t.tx.ExecContext(ctx, clearLayerQuery(t.table)); err != nil {
		return fmt.Errorf("clear PostGIS layer %s: %w", t.table, err)
	}
	return nil
}

// WriteLayer inserts features as WKT and JSONB properties. The target table
// must expose `geom geometry` and `properties jsonb` columns.
func (t *Transaction) WriteLayer(ctx context.Context, layer core.Layer) error {
	srid, err := srid(layer.CRS.AuthorityCode)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("INSERT INTO %s (id, geom, properties) VALUES ($1, ST_GeomFromText($2, $3), $4::jsonb)", t.table)
	for _, feature := range layer.Features {
		if err := ctx.Err(); err != nil {
			return err
		}
		geometry, err := core.ToWKT(feature.Geometry)
		if err != nil {
			return fmt.Errorf("feature %d geometry: %w", feature.ID, err)
		}
		properties, err := json.Marshal(feature.Properties)
		if err != nil {
			return fmt.Errorf("feature %d properties: %w", feature.ID, err)
		}
		if _, err := t.tx.ExecContext(ctx, query, feature.ID, geometry.WKT, srid, properties); err != nil {
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

func schemaQuery(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id BIGINT PRIMARY KEY,
    geom geometry NOT NULL,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb
)`, quoteIdentifier(table))
}

func readLayerQuery(table string) string {
	return fmt.Sprintf("SELECT id, ST_AsText(geom), ST_SRID(geom), properties FROM %s ORDER BY id", quoteIdentifier(table))
}

func clearLayerQuery(table string) string {
	return fmt.Sprintf("DELETE FROM %s", quoteIdentifier(table))
}

func tableName(table string) string {
	parts := strings.Split(table, ".")
	return parts[len(parts)-1]
}

func inferFields(features []core.Feature) []core.Field {
	types := map[string]core.FieldType{}
	for _, feature := range features {
		for name, value := range feature.Properties {
			if _, exists := types[name]; exists {
				continue
			}
			types[name] = fieldType(value)
		}
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]core.Field, 0, len(names))
	for _, name := range names {
		fields = append(fields, core.Field{Name: name, Type: types[name]})
	}
	return fields
}

func fieldType(value any) core.FieldType {
	switch value.(type) {
	case bool:
		return core.FieldTypeBool
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return core.FieldTypeNumber
	default:
		return core.FieldTypeText
	}
}
