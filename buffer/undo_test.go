package buffer

import (
	"testing"
	"time"
)

func TestUndoGroupedInsertPasteLikeSequence(t *testing.T) {
	b := NewBuffer(4)
	for _, ch := range "block" {
		b.InsertChar(ch)
	}

	// Force a group boundary before the next rapid insert burst.
	if len(b.Undo.undos) == 0 {
		t.Fatalf("expected undo ops after initial insert")
	}
	b.Undo.undos[len(b.Undo.undos)-1].Time = time.Now().Add(-undoGroupInterval - time.Millisecond)

	for _, ch := range "ock" {
		b.InsertChar(ch)
	}
	if got := b.Lines[0]; got != "blockock" {
		t.Fatalf("expected blockock before undo, got %q", got)
	}

	b.ApplyUndo()
	if got := b.Lines[0]; got != "block" {
		t.Fatalf("expected block after undo, got %q", got)
	}

	b.ApplyRedo()
	if got := b.Lines[0]; got != "blockock" {
		t.Fatalf("expected blockock after redo, got %q", got)
	}
}

func TestUndoRedoSingleGroupedWordInsert(t *testing.T) {
	b := NewBuffer(4)
	for _, ch := range "block" {
		b.InsertChar(ch)
	}
	if got := b.Lines[0]; got != "block" {
		t.Fatalf("expected block before undo, got %q", got)
	}

	b.ApplyUndo()
	if got := b.Lines[0]; got != "" {
		t.Fatalf("expected empty line after undo, got %q", got)
	}

	b.ApplyRedo()
	if got := b.Lines[0]; got != "block" {
		t.Fatalf("expected block after redo, got %q", got)
	}
}
