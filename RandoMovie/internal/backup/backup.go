package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"RandoMovie/internal/steam"
	"RandoMovie/internal/util"
)

// ChangeRecord documents a single modification for the audit history.
type ChangeRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Category   string    `json:"category"`
	Action     string    `json:"action"` // "backup", "swap", "restore"
	FromHash   string    `json:"from_hash"`
	ToHash     string    `json:"to_hash"`
	SourceFile string    `json:"source_file,omitempty"` // path of the swapped-in file
}

// BackupInfo tracks the state of one category's video file.
type BackupInfo struct {
	OriginalHash string `json:"original_hash"`
	CurrentHash  string `json:"current_hash"`
	CurrentFile  string `json:"current_file"` // actual filename on disk
	IsOriginal   bool   `json:"is_original"`
}

// State is the central backup state, persisted as JSON inside the game directory.
type State struct {
	GamePath   string                `json:"game_path"`
	Categories map[string]BackupInfo `json:"categories"`
	History    []ChangeRecord        `json:"history"`
	backupDir  string
}

func NewState(gamePath string) *State {
	s := &State{
		GamePath:   gamePath,
		Categories: make(map[string]BackupInfo),
		History:    []ChangeRecord{},
		backupDir:  filepath.Join(gamePath, "RandoMovie_Backups"),
	}
	s.Load() // restore persisted state if it exists
	return s
}

func (s *State) BackupDir() string {
	return s.backupDir
}

// BackupOriginal copies the current active video for category to the backup
// directory. It is idempotent: if a verified backup already exists for this
// category it returns immediately without touching the file system.
func (s *State) BackupOriginal(category string) error {
	if info, exists := s.Categories[category]; exists && info.OriginalHash != "" {
		backupFile, err := s.backupFilePath(category)
		if err == nil {
			if hash, err := util.HashFile(backupFile); err == nil && hash == info.OriginalHash {
				return nil
			}
		}
	}

	srcFile, err := s.activeFilePath(category)
	if err != nil {
		return err
	}

	srcHash, err := util.HashFile(srcFile)
	if err != nil {
		return fmt.Errorf("hash failed for %s: %w", category, err)
	}

	backupCatDir := filepath.Join(s.backupDir, category)
	if err := os.MkdirAll(backupCatDir, 0755); err != nil {
		return fmt.Errorf("cannot create backup dir: %w", err)
	}

	dstFile := filepath.Join(backupCatDir, filepath.Base(srcFile))
	if err := util.CopyFile(srcFile, dstFile); err != nil {
		return fmt.Errorf("backup copy failed: %w", err)
	}

	// Verify the copy before trusting it.
	dstHash, err := util.HashFile(dstFile)
	if err != nil || dstHash != srcHash {
		os.Remove(dstFile)
		return fmt.Errorf("backup verification failed for %s", category)
	}

	s.Categories[category] = BackupInfo{
		OriginalHash: srcHash,
		CurrentHash:  srcHash,
		CurrentFile:  filepath.Base(srcFile),
		IsOriginal:   true,
	}

	s.record(category, "backup", "", srcHash, "")
	return s.Save()
}

// BackupAll backs up every known category. Partial failures are collected and
// returned alongside the count of successful backups.
func (s *State) BackupAll() (int, []error) {
	backed := 0
	var errs []error
	for _, cat := range steam.Categories {
		if err := s.BackupOriginal(cat); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cat, err))
		} else {
			backed++
		}
	}
	return backed, errs
}

// Restore copies the backed-up original back over the active video file.
// It verifies the backup's integrity before overwriting anything.
func (s *State) Restore(category string) error {
	info, exists := s.Categories[category]
	if !exists {
		return fmt.Errorf("no backup recorded for %s", category)
	}
	if info.IsOriginal {
		return nil
	}

	backupFile, err := s.backupFilePath(category)
	if err != nil {
		return err
	}

	hash, err := util.HashFile(backupFile)
	if err != nil {
		return fmt.Errorf("backup file missing for %s: %w", category, err)
	}
	if hash != info.OriginalHash {
		return fmt.Errorf("backup corrupted for %s", category)
	}

	activeFile, err := s.activeFilePath(category)
	if err != nil {
		return err
	}

	oldHash := info.CurrentHash

	if err := util.CopyFile(backupFile, activeFile); err != nil {
		return fmt.Errorf("restore failed for %s: %w", category, err)
	}

	info.CurrentHash = info.OriginalHash
	info.IsOriginal = true
	s.Categories[category] = info

	s.record(category, "restore", oldHash, info.OriginalHash, "")
	return s.Save()
}

// RestoreAll restores every modified category. Already-original categories are
// skipped. Partial failures are collected alongside the success count.
func (s *State) RestoreAll() (int, []error) {
	restored := 0
	var errs []error
	for _, cat := range steam.Categories {
		if info, exists := s.Categories[cat]; exists && !info.IsOriginal {
			if err := s.Restore(cat); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", cat, err))
			} else {
				restored++
			}
		}
	}
	return restored, errs
}

// RecordSwap is called by the swap package after a successful video replacement
// to keep the backup state in sync. The caller is responsible for calling Save.
func (s *State) RecordSwap(category, oldHash, newHash, sourceFile string) {
	info := s.Categories[category]
	info.CurrentHash = newHash
	info.IsOriginal = false
	s.Categories[category] = info

	s.record(category, "swap", oldHash, newHash, sourceFile)
}

// Status returns a human-readable status string for every known category.
func (s *State) Status() map[string]string {
	status := make(map[string]string)
	for _, cat := range steam.Categories {
		if info, exists := s.Categories[cat]; exists {
			if info.IsOriginal {
				status[cat] = "original"
			} else {
				status[cat] = "modified"
			}
		} else {
			status[cat] = "no backup"
		}
	}
	return status
}

// VerifyBackups checks that every backed-up file on disk still matches the
// stored original hash. Returns true per category when the file is intact.
func (s *State) VerifyBackups() map[string]bool {
	result := make(map[string]bool)
	for _, cat := range steam.Categories {
		info, exists := s.Categories[cat]
		if !exists {
			continue
		}
		bPath, err := s.backupFilePath(cat)
		if err != nil {
			result[cat] = false
			continue
		}
		hash, err := util.HashFile(bPath)
		result[cat] = err == nil && hash == info.OriginalHash
	}
	return result
}

// --- internal helpers ---

func (s *State) activeFilePath(category string) (string, error) {
	return steam.FindCategoryVideo(s.GamePath, category)
}

// backupFilePath resolves the path of the backed-up file for category.
// It mirrors the original filename so the backup is human-readable.
// Falls back to scanning the backup directory if the live file is gone.
func (s *State) backupFilePath(category string) (string, error) {
	srcPath, err := steam.FindCategoryVideo(s.GamePath, category)
	if err != nil {
		// Live file not found — scan the backup directory for any .mp4.
		backupCatDir := filepath.Join(s.backupDir, category)
		entries, _ := os.ReadDir(backupCatDir)
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
				return filepath.Join(backupCatDir, e.Name()), nil
			}
		}
		return "", fmt.Errorf("no video found for %s", category)
	}
	return filepath.Join(s.backupDir, category, filepath.Base(srcPath)), nil
}

func (s *State) record(category, action, fromHash, toHash, sourceFile string) {
	s.History = append(s.History, ChangeRecord{
		Timestamp:  time.Now(),
		Category:   category,
		Action:     action,
		FromHash:   fromHash,
		ToHash:     toHash,
		SourceFile: sourceFile,
	})
}

func (s *State) Save() error {
	os.MkdirAll(s.backupDir, 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.backupDir, "state.json"), data, 0644)
}

func (s *State) Load() error {
	data, err := os.ReadFile(filepath.Join(s.backupDir, "state.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, s)
}
