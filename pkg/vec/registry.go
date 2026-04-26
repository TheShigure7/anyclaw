package vec

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"fmt"
	"path/filepath"
	"sort"

	pkgsqlite "github.com/1024XEngineer/anyclaw/pkg/sqlite"
	chromem "github.com/philippgille/chromem-go"
)

const (
	vecRegistryTable    = "vec_document_registry"
	vecRegistryFilename = "vec-registry.sqlite"
)

func (vs *VecStore) ensureRegistryUpToDate(ctx context.Context, db *chromem.DB, count int) error {
	registryCount, err := vs.registryCount(ctx)
	if err != nil {
		return err
	}
	if registryCount == count {
		return nil
	}

	return vs.rebuildRegistry(ctx, db)
}

func (vs *VecStore) rebuildRegistry(ctx context.Context, db *chromem.DB) error {
	ids, err := vs.exportedCollectionIDs(db)
	if err != nil {
		return err
	}
	return vs.replaceRegistryIDs(ctx, ids)
}

func (vs *VecStore) listItemsFromRegistry(ctx context.Context, col *chromem.Collection, limit int) ([]VecItem, bool, error) {
	ids, err := vs.listRegistryIDs(ctx, limit)
	if err != nil {
		return nil, false, err
	}

	items := make([]VecItem, 0, len(ids))
	stale := false
	for _, id := range ids {
		doc, err := col.GetByID(ctx, id)
		if err != nil {
			stale = true
			continue
		}
		items = append(items, vecItemFromDocument(doc))
	}

	return items, stale, nil
}

func (vs *VecStore) exportedCollectionIDs(db *chromem.DB) ([]string, error) {
	var buf bytes.Buffer
	if err := db.ExportToWriter(&buf, false, "", vs.tableName); err != nil {
		return nil, fmt.Errorf("export collection: %w", err)
	}

	type persistenceCollection struct {
		Name      string
		Metadata  map[string]string
		Documents map[string]*chromem.Document
	}
	persistenceDB := struct {
		Collections map[string]*persistenceCollection
	}{}

	if err := gob.NewDecoder(&buf).Decode(&persistenceDB); err != nil {
		return nil, fmt.Errorf("decode collection export: %w", err)
	}

	pc, ok := persistenceDB.Collections[vs.tableName]
	if !ok || len(pc.Documents) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(pc.Documents))
	for id := range pc.Documents {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return lessDocumentID(ids[i], ids[j])
	})

	return ids, nil
}

func (vs *VecStore) openRegistry() (*sql.DB, func() error, error) {
	if vs.legacyDB != nil {
		return vs.legacyDB, func() error { return nil }, nil
	}
	if vs.persistPath == "" {
		return nil, nil, nil
	}

	wrapper, err := pkgsqlite.Open(pkgsqlite.DefaultConfig(filepath.Join(filepath.Clean(vs.persistPath), vecRegistryFilename)))
	if err != nil {
		return nil, nil, fmt.Errorf("open vec registry: %w", err)
	}

	return wrapper.DB, wrapper.Close, nil
}

func (vs *VecStore) ensureRegistrySchema(ctx context.Context) (*sql.DB, func() error, error) {
	db, closeFn, err := vs.openRegistry()
	if err != nil || db == nil {
		return db, closeFn, err
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		collection_name TEXT NOT NULL,
		doc_id TEXT NOT NULL,
		is_numeric INTEGER NOT NULL DEFAULT 0,
		numeric_id INTEGER,
		PRIMARY KEY (collection_name, doc_id)
	)`, vecRegistryTable)); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("create vec registry table: %w", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS idx_%s_collection_order ON %s (collection_name, is_numeric DESC, numeric_id ASC, doc_id ASC)",
		vecRegistryTable, vecRegistryTable,
	)); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("create vec registry index: %w", err)
	}

	return db, closeFn, nil
}

func (vs *VecStore) upsertRegistryIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		if vs.ephemeralRegistry == nil {
			vs.ephemeralRegistry = make(map[string]struct{}, len(ids))
		}
		for _, id := range ids {
			vs.ephemeralRegistry[id] = struct{}{}
		}
		vs.mu.Unlock()
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vec registry tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (collection_name, doc_id, is_numeric, numeric_id) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT(collection_name, doc_id) DO UPDATE SET is_numeric = excluded.is_numeric, numeric_id = excluded.numeric_id",
		vecRegistryTable,
	))
	if err != nil {
		return fmt.Errorf("prepare vec registry upsert: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		isNumeric, numericID := registryIDOrder(id)
		if _, err := stmt.ExecContext(ctx, vs.tableName, id, isNumeric, numericID); err != nil {
			return fmt.Errorf("upsert vec registry id %q: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vec registry tx: %w", err)
	}
	return nil
}

func (vs *VecStore) deleteRegistryIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		for _, id := range ids {
			delete(vs.ephemeralRegistry, id)
		}
		vs.mu.Unlock()
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vec registry delete tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE collection_name = ? AND doc_id = ?",
		vecRegistryTable,
	))
	if err != nil {
		return fmt.Errorf("prepare vec registry delete: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, vs.tableName, id); err != nil {
			return fmt.Errorf("delete vec registry id %q: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vec registry delete tx: %w", err)
	}
	return nil
}

func (vs *VecStore) clearRegistry(ctx context.Context) error {
	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		vs.ephemeralRegistry = nil
		vs.mu.Unlock()
		return nil
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE collection_name = ?",
		vecRegistryTable,
	), vs.tableName); err != nil {
		return fmt.Errorf("clear vec registry: %w", err)
	}
	return nil
}

func (vs *VecStore) replaceRegistryIDs(ctx context.Context, ids []string) error {
	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		if len(ids) == 0 {
			vs.ephemeralRegistry = nil
		} else {
			vs.ephemeralRegistry = make(map[string]struct{}, len(ids))
			for _, id := range ids {
				vs.ephemeralRegistry[id] = struct{}{}
			}
		}
		vs.mu.Unlock()
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vec registry rebuild tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE collection_name = ?",
		vecRegistryTable,
	), vs.tableName); err != nil {
		return fmt.Errorf("clear vec registry before rebuild: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (collection_name, doc_id, is_numeric, numeric_id) VALUES (?, ?, ?, ?)",
		vecRegistryTable,
	))
	if err != nil {
		return fmt.Errorf("prepare vec registry rebuild: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		isNumeric, numericID := registryIDOrder(id)
		if _, err := stmt.ExecContext(ctx, vs.tableName, id, isNumeric, numericID); err != nil {
			return fmt.Errorf("rebuild vec registry id %q: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vec registry rebuild tx: %w", err)
	}
	return nil
}

func (vs *VecStore) registryCount(ctx context.Context) (int, error) {
	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return 0, err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		defer vs.mu.Unlock()
		return len(vs.ephemeralRegistry), nil
	}

	var count int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE collection_name = ?",
		vecRegistryTable,
	), vs.tableName).Scan(&count); err != nil {
		return 0, fmt.Errorf("count vec registry: %w", err)
	}
	return count, nil
}

func (vs *VecStore) listRegistryIDs(ctx context.Context, limit int) ([]string, error) {
	db, closeFn, err := vs.ensureRegistrySchema(ctx)
	if err != nil {
		return nil, err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if db == nil {
		vs.mu.Lock()
		ids := make([]string, 0, len(vs.ephemeralRegistry))
		for id := range vs.ephemeralRegistry {
			ids = append(ids, id)
		}
		vs.mu.Unlock()

		sort.Slice(ids, func(i, j int) bool {
			return lessDocumentID(ids[i], ids[j])
		})
		if limit > 0 && limit < len(ids) {
			ids = ids[:limit]
		}
		return ids, nil
	}

	query := fmt.Sprintf(
		"SELECT doc_id FROM %s WHERE collection_name = ? ORDER BY is_numeric DESC, numeric_id ASC, doc_id ASC",
		vecRegistryTable,
	)
	args := []any{vs.tableName}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list vec registry ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan vec registry id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vec registry ids: %w", err)
	}

	return ids, nil
}

func registryIDOrder(docID string) (int, sql.NullInt64) {
	numericID, ok := numericDocumentID(docID)
	if !ok {
		return 0, sql.NullInt64{}
	}

	return 1, sql.NullInt64{Int64: numericID, Valid: true}
}
