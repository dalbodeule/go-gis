package postgis

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type windowBudgetDriver struct{ reads *atomic.Int64 }

func (d windowBudgetDriver) Open(string) (driver.Conn, error) {
	return windowBudgetConn{reads: d.reads}, nil
}

type windowBudgetConn struct{ reads *atomic.Int64 }

func (windowBudgetConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (windowBudgetConn) Close() error              { return nil }
func (windowBudgetConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c windowBudgetConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &windowBudgetRows{reads: c.reads}, nil
}

type windowBudgetRows struct{ reads *atomic.Int64 }

func (windowBudgetRows) Columns() []string {
	return []string{"id", "wkt", "srid", "properties", "points", "geometry_bytes", "properties_bytes"}
}
func (windowBudgetRows) Close() error { return nil }
func (r *windowBudgetRows) Next(values []driver.Value) error {
	ordinal := r.reads.Add(1)
	if ordinal > 100 {
		return io.EOF
	}
	properties := `{"name":"` + strings.Repeat("x", 100) + `"}`
	copy(values, []driver.Value{ordinal, "POINT (0 0)", int64(4326), properties,
		int64(1), int64(20), int64(len(properties))})
	return nil
}

func TestPostGISWindowStopsDecodingAtByteBudget(t *testing.T) {
	var reads atomic.Int64
	const driverName = "gogis_postgis_window_budget_test"
	sql.Register(driverName, windowBudgetDriver{reads: &reads})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db, Table: "roads"}
	_, err = store.ReadWindow(context.Background(), [4]float64{-1, -1, 1, 1}, true, 1000, 400)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("window byte budget error = %v", err)
	}
	if got := reads.Load(); got > 2 {
		t.Fatalf("window decoded %d rows after reaching byte budget", got)
	}
}
