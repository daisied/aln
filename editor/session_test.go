package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"editor/buffer"
	"editor/config"
)

func TestSaveSessionRemovesStaleFileWhenNoOpenFileTabs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	stalePath := sessionPath(wd)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatalf("write stale session failed: %v", err)
	}

	e := New(config.Default())
	e.SaveSession()

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale session file to be removed, stat err=%v", err)
	}
}

func TestSaveSessionWritesOpenFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	e := New(config.Default())
	b := buffer.NewBuffer(4)
	b.Path = filepath.Join(wd, "a.txt")
	e.buffers = []*buffer.Buffer{b}
	e.views[b] = &EditorView{scrollY: 3, scrollX: 2}
	e.activeTab = 0

	e.SaveSession()

	path := sessionPath(wd)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected session file, read failed: %v", err)
	}

	var got SessionData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.ActiveTab != 0 || len(got.Files) != 1 || got.Files[0].Path != b.Path {
		t.Fatalf("unexpected session data: %+v", got)
	}
}
