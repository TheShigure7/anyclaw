package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSidecarDirFileDB(t *testing.T) {
	dbPath := filepath.Join("tmp", "anyclaw.db")
	db := &DB{cfg: Config{DSN: dbPath}}

	got := db.SidecarDir("vec")
	want := filepath.Join("tmp", "anyclaw.vec")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSidecarDirInMemoryDB(t *testing.T) {
	db := &DB{cfg: Config{DSN: ":memory:"}}

	if got := db.SidecarDir("vec"); got != "" {
		t.Fatalf("expected empty sidecar dir for in-memory db, got %q", got)
	}
}

func TestSidecarDirNilAndMemoryVariants(t *testing.T) {
	var nilDB *DB
	if got := nilDB.SidecarDir("vec"); got != "" {
		t.Fatalf("expected empty sidecar dir for nil db, got %q", got)
	}

	blank := &DB{cfg: Config{DSN: "   "}}
	if got := blank.SidecarDir("vec"); got != "" {
		t.Fatalf("expected empty sidecar dir for blank dsn, got %q", got)
	}

	mem := &DB{cfg: Config{DSN: "file:memdb1?mode=memory&cache=shared"}}
	if got := mem.SidecarDir("vec"); got != "" {
		t.Fatalf("expected empty sidecar dir for mode=memory dsn, got %q", got)
	}
}

func TestSidecarDirFileDSNVariants(t *testing.T) {
	dbPath := filepath.Join("tmp", "anyclaw.db")
	db := &DB{cfg: Config{DSN: fmt.Sprintf("file:%s?cache=shared", dbPath)}}

	if got := db.SidecarDir(""); got != filepath.Join("tmp", "anyclaw") {
		t.Fatalf("expected base sidecar path %q, got %q", filepath.Join("tmp", "anyclaw"), got)
	}
	if got := db.SidecarDir("vec"); got != filepath.Join("tmp", "anyclaw.vec") {
		t.Fatalf("expected vec sidecar path %q, got %q", filepath.Join("tmp", "anyclaw.vec"), got)
	}
}

func TestSidecarDirForSQLDBFileDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "anyclaw.db")
	db, err := Open(DefaultConfig(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	got := SidecarDirForSQLDB(context.Background(), db.DB, "vec")
	want := filepath.Join(filepath.Dir(dbPath), "anyclaw.vec")
	if got != want {
		t.Fatalf("expected sidecar path %q, got %q", want, got)
	}
}

func TestSidecarDirForSQLDBInMemoryDB(t *testing.T) {
	db, err := Open(InMemoryConfig())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if got := SidecarDirForSQLDB(context.Background(), db.DB, "vec"); got != "" {
		t.Fatalf("expected empty sidecar path for in-memory sql db, got %q", got)
	}
}
