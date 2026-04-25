package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

const fakeDatabaseListDriverName = "fake-database-list"

var (
	fakeDatabaseListRegisterOnce sync.Once
	fakeDatabaseListFixturesMu   sync.Mutex
	fakeDatabaseListFixtures     = make(map[string]fakeDatabaseListFixture)
	fakeDatabaseListCounter      atomic.Uint64
)

type fakeDatabaseListFixture struct {
	queryErr error
	rows     [][]driver.Value
}

type fakeDatabaseListDriver struct{}

type fakeDatabaseListConn struct {
	fixture fakeDatabaseListFixture
}

type fakeDatabaseListRows struct {
	rows [][]driver.Value
	idx  int
}

func (d fakeDatabaseListDriver) Open(name string) (driver.Conn, error) {
	fakeDatabaseListFixturesMu.Lock()
	fixture := fakeDatabaseListFixtures[name]
	fakeDatabaseListFixturesMu.Unlock()
	return &fakeDatabaseListConn{fixture: fixture}, nil
}

func (c *fakeDatabaseListConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *fakeDatabaseListConn) Close() error { return nil }

func (c *fakeDatabaseListConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

func (c *fakeDatabaseListConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.fixture.queryErr != nil {
		return nil, c.fixture.queryErr
	}
	return &fakeDatabaseListRows{rows: c.fixture.rows}, nil
}

func (r *fakeDatabaseListRows) Columns() []string {
	return []string{"seq", "name", "file"}
}

func (r *fakeDatabaseListRows) Close() error { return nil }

func (r *fakeDatabaseListRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}

	row := r.rows[r.idx]
	r.idx++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		}
	}
	return nil
}

func openFakeDatabaseListDB(t *testing.T, fixture fakeDatabaseListFixture) *sql.DB {
	t.Helper()

	fakeDatabaseListRegisterOnce.Do(func() {
		sql.Register(fakeDatabaseListDriverName, fakeDatabaseListDriver{})
	})

	name := fmt.Sprintf("fixture-%d", fakeDatabaseListCounter.Add(1))
	fakeDatabaseListFixturesMu.Lock()
	fakeDatabaseListFixtures[name] = fixture
	fakeDatabaseListFixturesMu.Unlock()

	db, err := sql.Open(fakeDatabaseListDriverName, name)
	if err != nil {
		t.Fatalf("open fake database_list db: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		fakeDatabaseListFixturesMu.Lock()
		delete(fakeDatabaseListFixtures, name)
		fakeDatabaseListFixturesMu.Unlock()
	})

	return db
}

func TestSidecarDirForSQLDBNilAndQueryError(t *testing.T) {
	if got := SidecarDirForSQLDB(context.Background(), nil, "vec"); got != "" {
		t.Fatalf("expected empty sidecar path for nil db, got %q", got)
	}

	db := openFakeDatabaseListDB(t, fakeDatabaseListFixture{
		queryErr: errors.New("query failed"),
	})

	if got := SidecarDirForSQLDB(context.Background(), db, "vec"); got != "" {
		t.Fatalf("expected empty sidecar path on query error, got %q", got)
	}
}

func TestSidecarDirForSQLDBSkipsInvalidAndNonMainRows(t *testing.T) {
	mainDBPath := filepath.Join(t.TempDir(), "anyclaw.db")
	ignoredPath := filepath.Join(t.TempDir(), "ignored.db")
	auxPath := filepath.Join(t.TempDir(), "aux.db")
	db := openFakeDatabaseListDB(t, fakeDatabaseListFixture{
		rows: [][]driver.Value{
			{"bad-seq", "main", ignoredPath},
			{int64(1), "aux", auxPath},
			{int64(2), "main", fmt.Sprintf("file:%s?cache=shared", mainDBPath)},
		},
	})

	got := SidecarDirForSQLDB(nil, db, "vec")
	want := filepath.Join(filepath.Dir(mainDBPath), "anyclaw.vec")
	if got != want {
		t.Fatalf("expected sidecar path %q, got %q", want, got)
	}
}

func TestSidecarDirForSQLDBReturnsEmptyWithoutMainRow(t *testing.T) {
	db := openFakeDatabaseListDB(t, fakeDatabaseListFixture{
		rows: [][]driver.Value{
			{int64(1), "temp", filepath.Join(t.TempDir(), "temp.db")},
			{int64(2), "aux", filepath.Join(t.TempDir(), "aux.db")},
		},
	})

	if got := SidecarDirForSQLDB(context.Background(), db, "vec"); got != "" {
		t.Fatalf("expected empty sidecar path without main row, got %q", got)
	}
}
