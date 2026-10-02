// Package postgis provides a small transaction boundary for PostGIS writes.
package postgis

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gogis/drivers"
	"gogis/internal/core"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
var identifierPartPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const maxPostgreSQLIdentifierBytes = 63

const maxPostGISReadFeatures = 100_000
const maxPostGISReadBytes = 128 << 20
const maxPostGISFeatureBytes = 16 << 20
const maxPostGISGeometryWKTBytes = 8 << 20
const maxPostGISGeometryPoints = 50_000
const maxPostGISGeometryStorageBytes = 2 << 20
const maxPostGISPropertiesBytes = 8 << 20
const maxPostGISPropertyNodes = 1 << 16
const maxPostGISPropertyDepth = 128

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
	if !validTableIdentifier(table) {
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
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("PostGIS database is not configured")
	}
	if !validTableIdentifier(s.Table) {
		return nil, fmt.Errorf("invalid PostGIS table identifier %q", s.Table)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Keep the validated identifier unquoted inside the transaction. Query
	// builders quote it at the SQL boundary; storing an already-quoted name
	// would make schema/delete builders quote it a second time.
	return &Transaction{tx: tx, table: s.Table}, nil
}

// ReadLayer reads the configured table into a detached core layer snapshot.
func (s *Store) ReadLayer(ctx context.Context) (core.Layer, error) {
	if s == nil || s.DB == nil {
		return core.Layer{}, fmt.Errorf("PostGIS database is not configured")
	}
	if !validTableIdentifier(s.Table) {
		return core.Layer{}, fmt.Errorf("invalid PostGIS table identifier %q", s.Table)
	}
	rows, err := s.DB.QueryContext(ctx, readLayerQuery(s.Table))
	if err != nil {
		return core.Layer{}, fmt.Errorf("read PostGIS layer %q: %w", s.Table, err)
	}
	defer rows.Close()

	layer := core.Layer{Name: tableName(s.Table), Editable: true}
	var totalPayloadBytes int64
	for rows.Next() {
		if len(layer.Features) >= maxPostGISReadFeatures {
			return core.Layer{}, fmt.Errorf("PostGIS layer %q exceeds the %d-feature in-memory read limit; use a bounded query or viewport reader", s.Table, maxPostGISReadFeatures)
		}
		var id int64
		var wkt sql.NullString
		var srid int
		var properties sql.NullString
		var geometryPoints, geometryStorageBytes, propertiesBytes sql.NullInt64
		if err := rows.Scan(&id, &wkt, &srid, &properties, &geometryPoints, &geometryStorageBytes, &propertiesBytes); err != nil {
			return core.Layer{}, fmt.Errorf("scan PostGIS feature: %w", err)
		}
		featureID, err := postGISFeatureID(id)
		if err != nil {
			return core.Layer{}, err
		}
		if geometryPoints.Valid && geometryPoints.Int64 > maxPostGISGeometryPoints {
			return core.Layer{}, fmt.Errorf("feature %d geometry exceeds the %d-point PostGIS read limit", id, maxPostGISGeometryPoints)
		}
		if geometryStorageBytes.Valid && geometryStorageBytes.Int64 > maxPostGISGeometryStorageBytes {
			return core.Layer{}, fmt.Errorf("feature %d geometry exceeds the %d MiB PostGIS storage limit", id, maxPostGISGeometryStorageBytes>>20)
		}
		if !wkt.Valid {
			return core.Layer{}, fmt.Errorf("feature %d geometry exceeds the %d MiB PostGIS WKT limit or is NULL", id, maxPostGISGeometryWKTBytes>>20)
		}
		if int64(len(wkt.String)) > maxPostGISGeometryWKTBytes {
			return core.Layer{}, fmt.Errorf("feature %d geometry exceeds the %d MiB PostGIS WKT limit", id, maxPostGISGeometryWKTBytes>>20)
		}
		if propertiesBytes.Valid && propertiesBytes.Int64 > maxPostGISPropertiesBytes {
			return core.Layer{}, fmt.Errorf("feature %d properties exceed the %d MiB PostGIS JSON limit", id, maxPostGISPropertiesBytes>>20)
		}
		decoded := map[string]any{}
		var propertyEstimate int64
		if properties.Valid && properties.String != "" {
			propertyEstimate, err = estimatePostGISPropertyBytes([]byte(properties.String))
			if err != nil {
				return core.Layer{}, fmt.Errorf("feature %d properties: %w", id, err)
			}
			if err := json.Unmarshal([]byte(properties.String), &decoded); err != nil {
				return core.Layer{}, fmt.Errorf("feature %d properties: %w", id, err)
			}
		}
		featurePayloadBytes := int64(len(wkt.String))*2 + propertyEstimate + 128
		if featurePayloadBytes > maxPostGISFeatureBytes {
			return core.Layer{}, fmt.Errorf("feature %d exceeds the %d MiB decoded PostGIS feature limit", id, maxPostGISFeatureBytes>>20)
		}
		if featurePayloadBytes > maxPostGISReadBytes-totalPayloadBytes {
			return core.Layer{}, fmt.Errorf("PostGIS layer %q exceeds the %d MiB in-memory read limit", s.Table, maxPostGISReadBytes>>20)
		}
		totalPayloadBytes += featurePayloadBytes
		layer.Features = append(layer.Features, core.Feature{
			ID:         featureID,
			Geometry:   core.WKTGeometry{WKT: wkt.String},
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
	query := insertLayerQuery(t.table)
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

func insertLayerQuery(table string) string {
	return fmt.Sprintf("INSERT INTO %s (id, geom, properties) VALUES ($1, ST_GeomFromText($2, $3), $4::jsonb)", quoteIdentifier(table))
}

func readLayerQuery(table string) string {
	return fmt.Sprintf(`SELECT id,
    CASE WHEN ST_NPoints(geom) <= %d AND pg_column_size(geom) <= %d THEN ST_AsText(geom) END,
    ST_SRID(geom),
    CASE WHEN octet_length(properties::text) <= %d THEN properties::text END,
    ST_NPoints(geom), pg_column_size(geom), octet_length(properties::text)
FROM %s ORDER BY id LIMIT %d`,
		maxPostGISGeometryPoints, maxPostGISGeometryStorageBytes, maxPostGISPropertiesBytes,
		quoteIdentifier(table), maxPostGISReadFeatures+1)
}

func validTableIdentifier(table string) bool {
	if len(table) == 0 || len(table) > 2*maxPostgreSQLIdentifierBytes+1 {
		return false
	}
	if !identifierPattern.MatchString(table) {
		return false
	}
	parts := strings.Split(table, ".")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if len(part) > maxPostgreSQLIdentifierBytes || !identifierPartPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func postGISFeatureID(id int64) (uint64, error) {
	if id < 0 {
		return 0, fmt.Errorf("PostGIS feature ID %d is negative and cannot be represented", id)
	}
	return uint64(id), nil
}

func estimatePostGISPropertyBytes(data []byte) (int64, error) {
	if len(data) > maxPostGISPropertiesBytes {
		return 0, fmt.Errorf("JSON document exceeds the %d MiB byte limit", maxPostGISPropertiesBytes>>20)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	const saturatedEstimate = int64(maxPostGISFeatureBytes) + 1
	add := func(left, right int64) int64 {
		if right < 0 || left > saturatedEstimate-right {
			return saturatedEstimate
		}
		return left + right
	}
	var readValue func(depth int) (int64, error)
	readValue = func(depth int) (int64, error) {
		if depth > maxPostGISPropertyDepth {
			return 0, fmt.Errorf("JSON nesting exceeds the %d-level limit", maxPostGISPropertyDepth)
		}
		nodes++
		if nodes > maxPostGISPropertyNodes {
			return 0, fmt.Errorf("JSON value count exceeds the %d-node limit", maxPostGISPropertyNodes)
		}
		token, err := decoder.Token()
		if err != nil {
			return 0, err
		}
		delim, isContainer := token.(json.Delim)
		if !isContainer {
			switch value := token.(type) {
			case string:
				return int64(len(value)) + 32, nil
			case json.Number:
				return int64(len(value)) + 32, nil
			default:
				return 32, nil
			}
		}
		var estimate int64
		switch delim {
		case '{':
			estimate = 128
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return 0, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return 0, fmt.Errorf("object member name is not a string")
				}
				nodes++
				if nodes > maxPostGISPropertyNodes {
					return 0, fmt.Errorf("JSON value count exceeds the %d-node limit", maxPostGISPropertyNodes)
				}
				estimate = add(estimate, int64(len(key))+32)
				childEstimate, err := readValue(depth + 1)
				if err != nil {
					return 0, err
				}
				estimate = add(estimate, childEstimate)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				if err == nil {
					err = fmt.Errorf("malformed JSON object")
				}
				return 0, err
			}
		case '[':
			estimate = 24
			for decoder.More() {
				childEstimate, err := readValue(depth + 1)
				if err != nil {
					return 0, err
				}
				estimate = add(estimate, childEstimate)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				if err == nil {
					err = fmt.Errorf("malformed JSON array")
				}
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
		return estimate, nil
	}
	estimate, err := readValue(0)
	if err != nil {
		return 0, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return 0, fmt.Errorf("unexpected trailing JSON data")
		}
		return 0, err
	}
	return estimate, nil
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
