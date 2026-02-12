package editor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const backupInterval = 30 * time.Second

type backupInfo struct {
	OriginalPath string `json:"original_path"`
	WorkDir      string `json:"work_dir"`
	Timestamp    string `json:"timestamp"`
}

func backupDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "aln", "backups")
}

func backupPathForFile(originalPath string) string {
	h := sha256.Sum256([]byte(originalPath))
	name := fmt.Sprintf("%x.bak", h[:8])
	return filepath.Join(backupDir(), name)
}

func backupMetaPath(backupPath string) string {
	return backupPath + ".json"
}

func (e *Editor) startBackupTimer() {
	go func() {
		ticker := time.NewTicker(backupInterval)
		defer ticker.Stop()
		for {
			<-ticker.C
			if e.quit {
				return
			}
			e.saveBackups()
		}
	}()
}

func (e *Editor) saveBackups() {
	dir := backupDir()
	os.MkdirAll(dir, 0755)

	for _, buf := range e.buffers {
		if !buf.Dirty || buf.Path == "" {
			continue
		}
		bpath := backupPathForFile(buf.Path)
		content := strings.Join(buf.Lines, "\n") + "\n"
		os.WriteFile(bpath, []byte(content), 0644)

		meta := backupInfo{
			OriginalPath: buf.Path,
			WorkDir:      e.watchedRoot,
			Timestamp:    time.Now().Format(time.RFC3339),
		}
		metaData, _ := json.Marshal(meta)
		os.WriteFile(backupMetaPath(bpath), metaData, 0644)
	}
}

func (e *Editor) cleanBackup(path string) {
	if path == "" {
		return
	}
	bpath := backupPathForFile(path)
	os.Remove(bpath)
	os.Remove(backupMetaPath(bpath))
}

func (e *Editor) cleanAllBackups() {
	for _, buf := range e.buffers {
		e.cleanBackup(buf.Path)
	}
}

// checkForBackups checks for backup files on startup and offers recovery.
func (e *Editor) checkForBackups() []backupInfo {
	dir := backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var found []backupInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		metaPath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var info backupInfo
		if json.Unmarshal(data, &info) != nil {
			continue
		}
		// Only show backups from current working directory
		if info.WorkDir == e.watchedRoot {
			// Check if backup file still exists
			backupFile := strings.TrimSuffix(metaPath, ".json")
			if _, err := os.Stat(backupFile); err == nil {
				found = append(found, info)
			}
		}
	}
	return found
}

// recoverBackup restores a backup file to the original path.
func (e *Editor) recoverBackup(info backupInfo) error {
	bpath := backupPathForFile(info.OriginalPath)
	data, err := os.ReadFile(bpath)
	if err != nil {
		return err
	}
	err = os.WriteFile(info.OriginalPath, data, 0644)
	if err != nil {
		return err
	}
	// Clean up backup
	os.Remove(bpath)
	os.Remove(backupMetaPath(bpath))
	return nil
}
