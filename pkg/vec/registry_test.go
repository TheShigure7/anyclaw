package vec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pkgsqlite "github.com/1024XEngineer/anyclaw/pkg/sqlite"
	chromem "github.com/philippgille/chromem-go"
)

func TestRegistryEphemeralLifecycle(t *testing.T) {
	ctx := context.Background()
	vs := &VecStore{tableName: "ephemeral_registry"}

	db, closeFn, err := vs.openRegistry()
	if err != nil {
		t.Fatalf("open ephemeral registry: %v", err)
	}
	if db != nil || closeFn != nil {
		t.Fatalf("expected ephemeral registry to use in-memory map only, got db=%v closeFnNil=%t", db, closeFn == nil)
	}

	if err := vs.upsertRegistryIDs(ctx, nil); err != nil {
		t.Fatalf("upsert empty registry ids: %v", err)
	}
	if err := vs.upsertRegistryIDs(ctx, []string{"10", "2", "doc"}); err != nil {
		t.Fatalf("upsert registry ids: %v", err)
	}

	ids, err := vs.listRegistryIDs(ctx, 0)
	if err != nil {
		t.Fatalf("list registry ids: %v", err)
	}
	if want := []string{"2", "10", "doc"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected registry ids %v, got %v", want, ids)
	}

	limited, err := vs.listRegistryIDs(ctx, 2)
	if err != nil {
		t.Fatalf("list limited registry ids: %v", err)
	}
	if want := []string{"2", "10"}; !reflect.DeepEqual(limited, want) {
		t.Fatalf("expected limited registry ids %v, got %v", want, limited)
	}

	count, err := vs.registryCount(ctx)
	if err != nil {
		t.Fatalf("registry count: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 registry ids, got %d", count)
	}

	if err := vs.deleteRegistryIDs(ctx, nil); err != nil {
		t.Fatalf("delete empty registry ids: %v", err)
	}
	if err := vs.deleteRegistryIDs(ctx, []string{"10"}); err != nil {
		t.Fatalf("delete registry id: %v", err)
	}

	ids, err = vs.listRegistryIDs(ctx, 0)
	if err != nil {
		t.Fatalf("list registry ids after delete: %v", err)
	}
	if want := []string{"2", "doc"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected registry ids after delete %v, got %v", want, ids)
	}

	if err := vs.replaceRegistryIDs(ctx, []string{"z", "1"}); err != nil {
		t.Fatalf("replace registry ids: %v", err)
	}

	ids, err = vs.listRegistryIDs(ctx, 0)
	if err != nil {
		t.Fatalf("list registry ids after replace: %v", err)
	}
	if want := []string{"1", "z"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected registry ids after replace %v, got %v", want, ids)
	}

	if err := vs.clearRegistry(ctx); err != nil {
		t.Fatalf("clear registry: %v", err)
	}
	if err := vs.replaceRegistryIDs(ctx, nil); err != nil {
		t.Fatalf("replace empty registry ids: %v", err)
	}

	count, err = vs.registryCount(ctx)
	if err != nil {
		t.Fatalf("registry count after clear: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty registry after clear, got %d", count)
	}
}

func TestRegistryLifecycleWithLegacyDB(t *testing.T) {
	ctx := context.Background()
	db, err := pkgsqlite.Open(pkgsqlite.InMemoryConfig())
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	vs := &VecStore{
		tableName: "legacy_registry",
		legacyDB:  db.DB,
	}

	registryDB, closeFn, err := vs.openRegistry()
	if err != nil {
		t.Fatalf("open legacy registry: %v", err)
	}
	if registryDB != db.DB {
		t.Fatalf("expected legacy registry to reuse sqlite db")
	}
	if closeFn == nil {
		t.Fatal("expected legacy registry closeFn")
	}
	if err := closeFn(); err != nil {
		t.Fatalf("close legacy registry no-op: %v", err)
	}

	if err := vs.upsertRegistryIDs(ctx, []string{"20", "3", "doc"}); err != nil {
		t.Fatalf("upsert legacy registry ids: %v", err)
	}

	ids, err := vs.listRegistryIDs(ctx, 0)
	if err != nil {
		t.Fatalf("list legacy registry ids: %v", err)
	}
	if want := []string{"3", "20", "doc"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected legacy registry ids %v, got %v", want, ids)
	}

	if err := vs.clearRegistry(ctx); err != nil {
		t.Fatalf("clear legacy registry: %v", err)
	}
	count, err := vs.registryCount(ctx)
	if err != nil {
		t.Fatalf("legacy registry count after clear: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty legacy registry after clear, got %d", count)
	}
}

func TestOpenRegistryPersistentPathError(t *testing.T) {
	ctx := context.Background()
	blockedPath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockedPath, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	vs := &VecStore{
		tableName:   "broken_registry",
		persistPath: blockedPath,
	}

	db, closeFn, err := vs.openRegistry()
	if err == nil {
		t.Fatal("expected persistent registry open to fail for file path")
	}
	if db != nil || closeFn != nil {
		t.Fatalf("expected failed registry open to return nil handles, got db=%v closeFnNil=%t", db, closeFn == nil)
	}

	db, closeFn, err = vs.ensureRegistrySchema(ctx)
	if err == nil {
		t.Fatal("expected ensureRegistrySchema to fail when registry open fails")
	}
	if db != nil || closeFn != nil {
		t.Fatalf("expected failed schema setup to return nil handles, got db=%v closeFnNil=%t", db, closeFn == nil)
	}
}

func TestRegistryEnsureUpToDateAndListItems(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()

	db, err := chromem.NewPersistentDB(path, false)
	if err != nil {
		t.Fatalf("open persistent chromem db: %v", err)
	}

	col, err := db.CreateCollection("registry_docs", map[string]string{
		"distance":   "cosine",
		"dimensions": "2",
		"backend":    "chromem-go",
	}, nil)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	if err := col.AddDocuments(ctx, []chromem.Document{
		{ID: "10", Embedding: normalized([]float32{1, 0})},
		{ID: "2", Embedding: normalized([]float32{0, 1})},
		{ID: "doc", Embedding: normalized([]float32{1, 1})},
	}, 1); err != nil {
		t.Fatalf("seed collection: %v", err)
	}

	vs := &VecStore{
		tableName:   "registry_docs",
		persistPath: path,
	}

	if err := vs.ensureRegistryUpToDate(ctx, db, col.Count()); err != nil {
		t.Fatalf("ensure registry up to date: %v", err)
	}
	if err := vs.ensureRegistryUpToDate(ctx, db, col.Count()); err != nil {
		t.Fatalf("ensure registry up to date no-op: %v", err)
	}

	exportedIDs, err := vs.exportedCollectionIDs(db)
	if err != nil {
		t.Fatalf("exported collection ids: %v", err)
	}
	if want := []string{"2", "10", "doc"}; !reflect.DeepEqual(exportedIDs, want) {
		t.Fatalf("expected exported ids %v, got %v", want, exportedIDs)
	}

	items, stale, err := vs.listItemsFromRegistry(ctx, col, 2)
	if err != nil {
		t.Fatalf("list items from registry: %v", err)
	}
	if stale {
		t.Fatal("expected fresh registry items, got stale")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 listed items, got %d", len(items))
	}
	if items[0].RowID != 2 || items[1].RowID != 10 {
		t.Fatalf("expected sorted registry items [2 10], got %+v", items)
	}
}

func TestListItemsFromRegistryMarksStale(t *testing.T) {
	ctx := context.Background()
	db := chromem.NewDB()

	col, err := db.CreateCollection("stale_registry_docs", nil, nil)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if err := col.AddDocument(ctx, chromem.Document{
		ID:        "1",
		Embedding: normalized([]float32{1, 0}),
	}); err != nil {
		t.Fatalf("seed collection: %v", err)
	}

	vs := &VecStore{tableName: "stale_registry_docs"}
	if err := vs.replaceRegistryIDs(ctx, []string{"1", "missing"}); err != nil {
		t.Fatalf("replace registry ids: %v", err)
	}

	items, stale, err := vs.listItemsFromRegistry(ctx, col, 10)
	if err != nil {
		t.Fatalf("list items from stale registry: %v", err)
	}
	if !stale {
		t.Fatal("expected stale registry to be detected")
	}
	if len(items) != 1 || items[0].RowID != 1 {
		t.Fatalf("expected only existing registry item to be returned, got %+v", items)
	}
}

func TestExportedCollectionIDsMissingCollection(t *testing.T) {
	vs := &VecStore{tableName: "missing_registry_docs"}

	ids, err := vs.exportedCollectionIDs(chromem.NewDB())
	if err != nil {
		t.Fatalf("export missing collection ids: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no ids for missing collection, got %v", ids)
	}
}

func TestRegistryMethodsPropagateSchemaErrors(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := pkgsqlite.Open(pkgsqlite.InMemoryConfig())
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	_ = sqliteDB.Close()

	vs := &VecStore{
		tableName: "broken_registry_methods",
		legacyDB:  sqliteDB.DB,
	}

	if db, closeFn, err := vs.ensureRegistrySchema(ctx); err == nil {
		t.Fatal("expected ensureRegistrySchema to fail on closed legacy db")
	} else if db != nil || closeFn != nil {
		t.Fatalf("expected failed schema setup to return nil handles, got db=%v closeFnNil=%t", db, closeFn == nil)
	}

	if err := vs.upsertRegistryIDs(ctx, []string{"1"}); err == nil {
		t.Fatal("expected upsertRegistryIDs to fail on closed legacy db")
	}
	if err := vs.deleteRegistryIDs(ctx, []string{"1"}); err == nil {
		t.Fatal("expected deleteRegistryIDs to fail on closed legacy db")
	}
	if err := vs.clearRegistry(ctx); err == nil {
		t.Fatal("expected clearRegistry to fail on closed legacy db")
	}
	if _, err := vs.registryCount(ctx); err == nil {
		t.Fatal("expected registryCount to fail on closed legacy db")
	}
	if _, err := vs.listRegistryIDs(ctx, 1); err == nil {
		t.Fatal("expected listRegistryIDs to fail on closed legacy db")
	}

	chromemDB := chromem.NewDB()
	col, err := chromemDB.CreateCollection("broken_registry_methods", nil, nil)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	if _, stale, err := vs.listItemsFromRegistry(ctx, col, 1); err == nil {
		t.Fatal("expected listItemsFromRegistry to fail when registry lookup fails")
	} else if stale {
		t.Fatal("expected stale=false when listItemsFromRegistry fails before lookup")
	}
	if err := vs.ensureRegistryUpToDate(ctx, chromemDB, 1); err == nil {
		t.Fatal("expected ensureRegistryUpToDate to fail when registry count fails")
	}
	if err := vs.rebuildRegistry(ctx, chromemDB); err == nil {
		t.Fatal("expected rebuildRegistry to fail when registry replacement fails")
	}
}
