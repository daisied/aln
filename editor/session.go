package editor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"editor/buffer"
)

type SessionData struct {
	WorkingDir string      `json:"working_dir"`
	ActiveTab  int         `json:"active_tab"`
	Files      []FileState `json:"files"`
}

type FileState struct {
	Path    string `json:"path"`
	Line    int    `json:"cursor_line"`
	Col     int    `json:"cursor_col"`
	ScrollY int    `json:"scroll_y"`
	ScrollX int    `json:"scroll_x"`
}

func sessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "aln", "sessions")
}

func sessionPath(workDir string) string {
	hash := sha256.Sum256([]byte(workDir))
	return filepath.Join(sessionDir(), fmt.Sprintf("%x.json", hash[:8]))
}

func (e *Editor) SaveSession() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	path := sessionPath(wd)

	session := SessionData{
		WorkingDir: wd,
		ActiveTab:  e.activeTab,
	}

	for _, buf := range e.buffers {
		if buf.Path == "" {
			continue
		}
		view := e.views[buf]
		fs := FileState{
			Path: buf.Path,
			Line: buf.Cursor.Line,
			Col:  buf.Cursor.Col,
		}
		if view != nil {
			fs.ScrollY = view.scrollY
			fs.ScrollX = view.scrollX
		}
		session.Files = append(session.Files, fs)
	}

	if len(session.Files) == 0 {
		// No open file-backed tabs: clear any stale session so closed tabs don't return.
		_ = os.Remove(path)
		return
	}

	dir := sessionDir()
	os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(path, data, 0644)
}

func (e *Editor) RestoreSession() bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}

	path := sessionPath(wd)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return false
	}

	if session.WorkingDir != wd {
		return false
	}

	restored := false
	for _, fs := range session.Files {
		if _, err := os.Stat(fs.Path); err != nil {
			continue
		}
		e.openFile(fs.Path)
		buf := e.activeBuffer()
		if buf != nil && buf.Path == fs.Path {
			if fs.Line < len(buf.Lines) {
				buf.Cursor.Line = fs.Line
				lineLen := buffer.RuneLen(buf.Lines[fs.Line])
				if fs.Col <= lineLen {
					buf.Cursor.Col = fs.Col
				}
			}
			view := e.activeView()
			if view != nil {
				view.scrollY = fs.ScrollY
				view.scrollX = fs.ScrollX
			}
			restored = true
		}
	}

	if restored && session.ActiveTab >= 0 && session.ActiveTab < len(e.buffers) {
		e.switchTab(session.ActiveTab)
	}

	return restored
}
