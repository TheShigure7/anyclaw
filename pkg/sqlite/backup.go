package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type BackupConfig struct {
	BackupDir     string
	MaxBackups    int
	Interval      time.Duration
	Compress      bool
	OnBackupStart func(path string)
	OnBackupDone  func(path string, size int64, duration time.Duration)
	OnBackupError func(err error)
}

func DefaultBackupConfig(backupDir string) BackupConfig {
	return BackupConfig{
		BackupDir:  backupDir,
		MaxBackups: 10,
		Interval:   1 * time.Hour,
		Compress:   false,
	}
}

type BackupInfo struct {
	Path      string    `json:"path"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
	Name      string    `json:"name"`
}

type BackupManager struct {
	mu      sync.Mutex
	cfg     BackupConfig
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	counter int64
}

func NewBackupManager(cfg BackupConfig) *BackupManager {
	cfg = normalizeBackupConfig(cfg)
	return &BackupManager{
		cfg: cfg,
	}
}

func (bm *BackupManager) Start(ctx context.Context, db *DB) error {
	if err := os.MkdirAll(bm.cfg.BackupDir, 0o755); err != nil {
		return fmt.Errorf("sqlite: create backup dir: %w", err)
	}

	bm.mu.Lock()
	if bm.running {
		bm.mu.Unlock()
		return fmt.Errorf("sqlite: backup manager already running")
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	bm.stopCh = stopCh
	bm.doneCh = doneCh
	bm.running = true
	bm.mu.Unlock()

	go bm.runLoop(ctx, db, stopCh, doneCh)

	return nil
}

func (bm *BackupManager) Stop() {
	bm.mu.Lock()
	if !bm.running {
		bm.mu.Unlock()
		return
	}
	stopCh := bm.stopCh
	doneCh := bm.doneCh
	bm.running = false
	close(stopCh)
	bm.mu.Unlock()

	if doneCh != nil {
		<-doneCh
	}
}

func (bm *BackupManager) Wait() {
	bm.mu.Lock()
	doneCh := bm.doneCh
	bm.mu.Unlock()

	if doneCh != nil {
		<-doneCh
	}
}

func (bm *BackupManager) BackupOnce(ctx context.Context, db *DB) (string, error) {
	bm.mu.Lock()
	cfg := bm.cfg
	bm.counter++
	counter := bm.counter
	bm.mu.Unlock()

	if err := os.MkdirAll(cfg.BackupDir, 0o755); err != nil {
		return "", fmt.Errorf("sqlite: create backup dir: %w", err)
	}

	start := time.Now()
	timestamp := start.Format("20060102_150405")
	backupPath := filepath.Join(cfg.BackupDir, fmt.Sprintf("backup_%s_%03d.db", timestamp, counter))

	if cfg.OnBackupStart != nil {
		cfg.OnBackupStart(backupPath)
	}

	if err := bm.performBackup(ctx, db, backupPath); err != nil {
		if cfg.OnBackupError != nil {
			cfg.OnBackupError(err)
		}
		return "", fmt.Errorf("sqlite: backup: %w", err)
	}

	info, err := os.Stat(backupPath)
	if err != nil {
		return "", err
	}

	if cfg.OnBackupDone != nil {
		cfg.OnBackupDone(backupPath, info.Size(), time.Since(start))
	}

	if err := pruneOldBackups(cfg); err != nil {
		return backupPath, fmt.Errorf("sqlite: prune backups: %w", err)
	}

	return backupPath, nil
}

func (bm *BackupManager) performBackup(ctx context.Context, db *DB, backupPath string) error {
	if _, err := sqliteFilePathFromDSN(db.DSN()); err != nil {
		return err
	}

	return sqliteBackupInto(ctx, db.DB, backupPath)
}

func (bm *BackupManager) ListBackups() ([]BackupInfo, error) {
	bm.mu.Lock()
	cfg := bm.cfg
	bm.mu.Unlock()

	return listBackups(cfg)
}

func listBackups(cfg BackupConfig) ([]BackupInfo, error) {
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		return nil, err
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "backup_") || !strings.HasSuffix(name, ".db") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		base := strings.TrimSuffix(strings.TrimPrefix(name, "backup_"), ".db")
		parts := strings.SplitN(base, "_", 3)
		if len(parts) < 2 {
			continue
		}
		tsStr := parts[0] + "_" + parts[1]
		ts, err := time.Parse("20060102_150405", tsStr)
		if err != nil {
			continue
		}

		backups = append(backups, BackupInfo{
			Path:      filepath.Join(cfg.BackupDir, name),
			Timestamp: ts,
			Size:      info.Size(),
			Name:      name,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

func (bm *BackupManager) LatestBackup() (*BackupInfo, error) {
	backups, err := bm.ListBackups()
	if err != nil {
		return nil, err
	}
	if len(backups) == 0 {
		return nil, fmt.Errorf("sqlite: no backups found")
	}
	return &backups[0], nil
}

func (bm *BackupManager) RestoreFromBackup(ctx context.Context, db *DB, backupPath string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if !db.isClosed() {
		return fmt.Errorf("sqlite: close database before restore")
	}

	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("sqlite: backup file not found: %w", err)
	}

	dstPath, err := sqliteFilePathFromDSN(db.DSN())
	if err != nil {
		return err
	}

	return copyFile(backupPath, dstPath)
}

func pruneOldBackups(cfg BackupConfig) error {
	backups, err := listBackups(cfg)
	if err != nil {
		return err
	}

	if len(backups) <= cfg.MaxBackups {
		return nil
	}

	toDelete := backups[cfg.MaxBackups:]
	for _, backup := range toDelete {
		if err := os.Remove(backup.Path); err != nil {
			return fmt.Errorf("remove old backup %s: %w", backup.Path, err)
		}
	}

	return nil
}

func (bm *BackupManager) runLoop(ctx context.Context, db *DB, stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer func() {
		close(doneCh)
		bm.mu.Lock()
		if bm.stopCh == stopCh {
			bm.running = false
		}
		bm.mu.Unlock()
	}()

	ticker := time.NewTicker(bm.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			if _, err := bm.BackupOnce(ctx, db); err != nil {
				if bm.cfg.OnBackupError != nil {
					bm.cfg.OnBackupError(err)
				}
			}
		}
	}
}

func normalizeBackupConfig(cfg BackupConfig) BackupConfig {
	if cfg.BackupDir == "" {
		cfg.BackupDir = ".anyclaw/backups"
	}
	if cfg.MaxBackups <= 0 {
		cfg.MaxBackups = 10
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	return cfg
}

func sqliteBackupInto(ctx context.Context, db *sql.DB, backupPath string) error {
	if db == nil {
		return fmt.Errorf("sqlite: database is nil")
	}

	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return fmt.Errorf("sqlite: create backup dir: %w", err)
	}

	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Remove(backupPath); err != nil {
			return fmt.Errorf("sqlite: remove existing backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("sqlite: stat backup path: %w", err)
	}

	if _, err := db.ExecContext(ctx, "VACUUM main INTO ?", backupPath); err != nil {
		return fmt.Errorf("sqlite: vacuum into backup: %w", err)
	}

	return nil
}

func sqliteFilePathFromDSN(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || dsn == ":memory:" || strings.Contains(dsn, "mode=memory") {
		return "", fmt.Errorf("sqlite: operation requires a file-backed database")
	}

	if strings.HasPrefix(dsn, "file:") {
		dsn = strings.TrimPrefix(dsn, "file:")
	}
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		dsn = dsn[:idx]
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || dsn == ":memory:" {
		return "", fmt.Errorf("sqlite: operation requires a file-backed database")
	}

	return filepath.Clean(dsn), nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("sync destination file: %w", err)
	}

	return nil
}
