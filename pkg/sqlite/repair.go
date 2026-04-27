package sqlite

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

type RepairResult struct {
	Success       bool
	IssuesFound   []string
	IssuesFixed   []string
	BackupCreated string
	Duration      time.Duration
}

type RepairConfig struct {
	AutoRepair      bool
	CreateBackup    bool
	MaxRepairTime   time.Duration
	OnIssueDetected func(issue string)
	OnIssueFixed    func(fix string)
}

func DefaultRepairConfig() RepairConfig {
	return RepairConfig{
		AutoRepair:    true,
		CreateBackup:  true,
		MaxRepairTime: 5 * time.Minute,
	}
}

type RepairManager struct {
	mu  sync.Mutex
	cfg RepairConfig
}

func NewRepairManager(cfg RepairConfig) *RepairManager {
	if cfg.MaxRepairTime <= 0 {
		cfg.MaxRepairTime = 5 * time.Minute
	}
	return &RepairManager{cfg: cfg}
}

func (rm *RepairManager) CheckDatabase(ctx context.Context, db *DB) (*RepairResult, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	start := time.Now()
	result := &RepairResult{}

	issues, err := rm.runIntegrityCheck(ctx, db)
	if err != nil {
		result.IssuesFound = append(result.IssuesFound, fmt.Sprintf("integrity check failed: %v", err))
	}
	result.IssuesFound = append(result.IssuesFound, issues...)

	if len(result.IssuesFound) == 0 {
		return result, nil
	}

	if rm.cfg.AutoRepair {
		fixResult, err := rm.repair(ctx, db, result.IssuesFound)
		if err != nil {
			return nil, fmt.Errorf("repair failed: %w", err)
		}
		result.IssuesFixed = fixResult.IssuesFixed
		result.BackupCreated = fixResult.BackupCreated
		result.Success = fixResult.Success
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (rm *RepairManager) RepairDatabase(ctx context.Context, db *DB) (*RepairResult, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	start := time.Now()
	result := &RepairResult{}

	issues, err := rm.runIntegrityCheck(ctx, db)
	if err != nil {
		result.IssuesFound = append(result.IssuesFound, fmt.Sprintf("integrity check failed: %v", err))
	}
	result.IssuesFound = append(result.IssuesFound, issues...)

	if len(result.IssuesFound) == 0 {
		result.Success = true
		result.Duration = time.Since(start)
		return result, nil
	}

	fixResult, err := rm.repair(ctx, db, result.IssuesFound)
	if err != nil {
		return nil, fmt.Errorf("repair failed: %w", err)
	}
	result.IssuesFixed = fixResult.IssuesFixed
	result.BackupCreated = fixResult.BackupCreated
	result.Success = fixResult.Success
	result.Duration = time.Since(start)

	return result, nil
}

func (rm *RepairManager) QuickFix(ctx context.Context, db *DB) error {
	fixes := []struct {
		name string
		sql  string
	}{
		{"reindex", "REINDEX"},
		{"analyze", "ANALYZE"},
		{"optimize", "PRAGMA optimize"},
		{"integrity_check", "PRAGMA integrity_check"},
	}

	for _, fix := range fixes {
		if _, err := db.ExecContext(ctx, fix.sql); err != nil {
			return fmt.Errorf("quick fix %q failed: %w", fix.name, err)
		}
	}

	return nil
}

func (rm *RepairManager) RecoverFromBackup(ctx context.Context, db *DB, backupDir string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !db.isClosed() {
		return fmt.Errorf("sqlite: close database before restore")
	}

	backups, err := listBackups(normalizeBackupConfig(BackupConfig{BackupDir: backupDir}))
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backups found in %s", backupDir)
	}

	latestBackup := backups[0].Path

	if rm.cfg.CreateBackup {
		currentDB, err := sqliteFilePathFromDSN(db.DSN())
		if err == nil && isReadableFile(currentDB) {
			brokenBackup := currentDB + ".broken." + time.Now().Format("20060102_150405")
			_ = copyFile(currentDB, brokenBackup)
		}
	}

	dstPath, err := sqliteFilePathFromDSN(db.DSN())
	if err != nil {
		return err
	}

	if err := copyFile(latestBackup, dstPath); err != nil {
		return fmt.Errorf("restore from backup: %w", err)
	}

	return nil
}

func (rm *RepairManager) runIntegrityCheck(ctx context.Context, db *DB) ([]string, error) {
	var issues []string

	var integrityResult string
	err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrityResult)
	if err != nil {
		return nil, fmt.Errorf("integrity check: %w", err)
	}
	if integrityResult != "ok" {
		issues = append(issues, fmt.Sprintf("integrity check: %s", integrityResult))
	}

	var quickCheckResult string
	err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheckResult)
	if err != nil {
		return nil, fmt.Errorf("quick check: %w", err)
	}
	if quickCheckResult != "ok" {
		issues = append(issues, fmt.Sprintf("quick check: %s", quickCheckResult))
	}

	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var table string
			var rowid int64
			var parent string
			var fkid int64
			if err := rows.Scan(&table, &rowid, &parent, &fkid); err == nil {
				issues = append(issues, fmt.Sprintf("foreign key violation: table=%s, rowid=%d, parent=%s",
					table, rowid, parent))
			}
		}
	}

	return issues, nil
}

func (rm *RepairManager) repair(ctx context.Context, db *DB, issues []string) (*RepairResult, error) {
	ctx, cancel := context.WithTimeout(ctx, rm.cfg.MaxRepairTime)
	defer cancel()

	result := &RepairResult{}

	if rm.cfg.CreateBackup {
		currentDB, err := sqliteFilePathFromDSN(db.DSN())
		if err == nil {
			backupPath := currentDB + ".repair_backup." + time.Now().Format("20060102_150405")
			if err := sqliteBackupInto(ctx, db.DB, backupPath); err != nil {
				return nil, fmt.Errorf("create repair backup: %w", err)
			}
			result.BackupCreated = backupPath
		}
	}

	for _, issue := range issues {
		if rm.cfg.OnIssueDetected != nil {
			rm.cfg.OnIssueDetected(issue)
		}

		fixed := false

		if containsStr(issue, "integrity check") || containsStr(issue, "quick check") {
			if _, err := db.ExecContext(ctx, "REINDEX"); err == nil {
				result.IssuesFixed = append(result.IssuesFixed, "reindexed database")
				fixed = true
			}
		}

		if containsStr(issue, "foreign key violation") {
			if _, err := db.ExecContext(ctx, "PRAGMA foreign_key_check"); err == nil {
				result.IssuesFixed = append(result.IssuesFixed, "checked foreign key constraints")
				fixed = true
			}
		}

		if !fixed {
			result.IssuesFixed = append(result.IssuesFixed, "attempted general repair (reindex + analyze)")
			db.ExecContext(ctx, "REINDEX")
			db.ExecContext(ctx, "ANALYZE")
			db.ExecContext(ctx, "PRAGMA optimize")
		}

		if rm.cfg.OnIssueFixed != nil && fixed {
			rm.cfg.OnIssueFixed(result.IssuesFixed[len(result.IssuesFixed)-1])
		}
	}

	ok, err := db.IntegrityCheck(ctx)
	if err == nil && ok {
		result.Success = true
	}

	return result, nil
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isReadableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}
