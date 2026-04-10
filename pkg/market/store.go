package market

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/config"
)

type Store struct {
	root         string
	statePath    string
	packagesRoot string
	receiptsRoot string
	cacheRoot    string
	trustRoot    string
	historyPath  string
}

func NewStore(workDir string) (*Store, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = ".anyclaw"
	}
	root := filepath.Join(workDir, "packages")
	store := &Store{
		root:         root,
		statePath:    filepath.Join(root, "installed.json"),
		packagesRoot: filepath.Join(root, "packages"),
		receiptsRoot: filepath.Join(root, "receipts"),
		cacheRoot:    filepath.Join(root, "cache"),
		trustRoot:    filepath.Join(root, "trust"),
		historyPath:  filepath.Join(root, "history.json"),
	}
	for _, dir := range []string{root, store.packagesRoot, store.receiptsRoot, store.cacheRoot, store.trustRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) ListInstalled() ([]InstalledPackage, error) {
	return s.ListInstalledFiltered(ListFilter{})
}

func (s *Store) ListInstalledFiltered(filter ListFilter) ([]InstalledPackage, error) {
	state, err := s.load()
	if err != nil {
		return nil, err
	}
	items := make([]InstalledPackage, 0, len(state.Packages))
	for _, item := range state.Packages {
		if filter.Kind != "" && item.Manifest.Kind != filter.Kind {
			continue
		}
		if kw := strings.TrimSpace(strings.ToLower(filter.Keyword)); kw != "" {
			candidate := strings.ToLower(strings.Join([]string{
				item.Manifest.ID,
				item.Manifest.Name,
				item.Manifest.DisplayName,
				item.Manifest.Description,
			}, " "))
			if !strings.Contains(candidate, kw) {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Manifest.Kind != items[j].Manifest.Kind {
			return items[i].Manifest.Kind < items[j].Manifest.Kind
		}
		return strings.ToLower(items[i].Manifest.ID) < strings.ToLower(items[j].Manifest.ID)
	})
	return items, nil
}

func (s *Store) GetInstalled(id string) (InstalledPackage, bool, error) {
	state, err := s.load()
	if err != nil {
		return InstalledPackage{}, false, err
	}
	id = strings.TrimSpace(strings.ToLower(id))
	for _, item := range state.Packages {
		if strings.EqualFold(strings.TrimSpace(item.Manifest.ID), id) {
			return item, true, nil
		}
	}
	return InstalledPackage{}, false, nil
}

func (s *Store) InstallManifestFile(path string) (PackageManifest, error) {
	manifest, err := LoadManifestFile(path)
	if err != nil {
		return PackageManifest{}, err
	}
	if err := s.InstallManifest(manifest, path); err != nil {
		return PackageManifest{}, err
	}
	return manifest, nil
}

func (s *Store) InstallManifest(manifest PackageManifest, source string) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	state, err := s.load()
	if err != nil {
		return err
	}

	normalizedID := strings.TrimSpace(manifest.ID)
	record := InstalledPackage{
		Manifest:    manifest,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      strings.TrimSpace(source),
	}

	replaced := false
	for i, item := range state.Packages {
		if strings.EqualFold(strings.TrimSpace(item.Manifest.ID), normalizedID) {
			state.Packages[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		state.Packages = append(state.Packages, record)
	}
	manifestPath, packageRoot, err := s.persistManifest(record)
	if err != nil {
		return err
	}
	if err := s.saveReceipt(InstallReceipt{
		PackageID:    record.Manifest.ID,
		Kind:         record.Manifest.Kind,
		Version:      record.Manifest.Version,
		InstalledAt:  record.InstalledAt,
		Source:       record.Source,
		ManifestPath: manifestPath,
		PackageRoot:  packageRoot,
		Manifest:     record.Manifest,
	}); err != nil {
		_ = os.RemoveAll(packageRoot)
		return err
	}
	if err := s.save(state); err != nil {
		_ = os.RemoveAll(packageRoot)
		_ = os.Remove(s.receiptPath(record.Manifest.ID))
		return err
	}
	_ = s.appendHistory(InstallHistoryRecord{
		Action:    "install",
		PackageID: record.Manifest.ID,
		Kind:      record.Manifest.Kind,
		Version:   record.Manifest.Version,
		At:        record.InstalledAt,
		Source:    record.Source,
		Status:    "ok",
	})
	return nil
}

func (s *Store) Uninstall(id string) error {
	state, err := s.load()
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	filtered := make([]InstalledPackage, 0, len(state.Packages))
	removed := false
	removeRoot := ""
	for _, item := range state.Packages {
		if strings.EqualFold(strings.TrimSpace(item.Manifest.ID), id) {
			removed = true
			removeRoot = filepath.Join(s.packagesRoot, string(item.Manifest.Kind), sanitizePathName(item.Manifest.ID))
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		return fmt.Errorf("package not installed: %s", id)
	}
	state.Packages = filtered
	if removeRoot != "" {
		_ = os.RemoveAll(removeRoot)
	}
	_ = os.Remove(s.receiptPath(id))
	if err := s.save(state); err != nil {
		return err
	}
	_ = s.appendHistory(InstallHistoryRecord{
		Action:    "uninstall",
		PackageID: id,
		At:        time.Now().UTC().Format(time.RFC3339),
		Status:    "ok",
	})
	return nil
}

func (s *Store) PersistentSubagentProfiles() ([]config.PersistentSubagentProfile, error) {
	items, err := s.ListInstalled()
	if err != nil {
		return nil, err
	}
	profiles := make([]config.PersistentSubagentProfile, 0)
	for _, item := range items {
		if item.Manifest.Kind != KindAgent || item.Manifest.Agent == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Manifest.Agent.Mode), "persistent_subagent") {
			continue
		}
		profiles = append(profiles, persistentSubagentProfileFromManifest(item.Manifest))
	}
	return profiles, nil
}

func (s *Store) Receipt(id string) (*InstallReceipt, error) {
	data, err := os.ReadFile(s.receiptPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("receipt not found: %s", id)
		}
		return nil, err
	}
	var receipt InstallReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (s *Store) History() ([]InstallHistoryRecord, error) {
	data, err := os.ReadFile(s.historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var history []InstallHistoryRecord
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	sort.Slice(history, func(i, j int) bool {
		return history[i].At > history[j].At
	})
	return history, nil
}

func (s *Store) load() (*InstalledState, error) {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstalledState{}, nil
		}
		return nil, err
	}
	var state InstalledState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) save(state *InstalledState) error {
	if state == nil {
		state = &InstalledState{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath, data, 0o644)
}

func (s *Store) persistManifest(item InstalledPackage) (string, string, error) {
	dir := filepath.Join(s.packagesRoot, string(item.Manifest.Kind), sanitizePathName(item.Manifest.ID), sanitizePathName(firstNonEmpty(item.Manifest.Version, "latest")))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	data, err := json.MarshalIndent(item.Manifest, "", "  ")
	if err != nil {
		return "", "", err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return "", "", err
	}
	return manifestPath, dir, nil
}

func (s *Store) saveReceipt(receipt InstallReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.receiptPath(receipt.PackageID), data, 0o644)
}

func (s *Store) receiptPath(id string) string {
	return filepath.Join(s.receiptsRoot, sanitizePathName(id)+".json")
}

func (s *Store) appendHistory(record InstallHistoryRecord) error {
	history, err := s.History()
	if err != nil {
		return err
	}
	history = append(history, record)
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.historyPath, data, 0o644)
}

func sanitizePathName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "item"
	}
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "_", "-")
	value = replacer.Replace(value)
	return strings.Trim(value, "-.")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
