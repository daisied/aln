package buffer

import "time"

type OpType int

const (
	OpInsert OpType = iota
	OpDelete
)

type Operation struct {
	Type   OpType
	Pos    Cursor
	Text   string
	Before Cursor    // cursor position before op
	Time   time.Time // when the operation was recorded
	Group  int       // group ID for batched undo (0 = ungrouped)
}

type UndoStack struct {
	undos     []Operation
	redos     []Operation
	nextGroup int // next group ID to assign
}

const undoGroupInterval = 300 * time.Millisecond

func NewUndoStack() *UndoStack {
	return &UndoStack{nextGroup: 1}
}

func (u *UndoStack) Push(op Operation) {
	op.Time = time.Now()

	// Auto-group sequential single-character inserts/deletes within the time window
	if len(u.undos) > 0 {
		prev := &u.undos[len(u.undos)-1]
		if prev.Type == op.Type && len(op.Text) == 1 && len(prev.Text) == 1 &&
			op.Time.Sub(prev.Time) < undoGroupInterval &&
			!isGroupBreak(prev, &op) {
			if prev.Group == 0 {
				prev.Group = u.nextGroup
				u.nextGroup++
			}
			op.Group = prev.Group
		}
	}

	u.undos = append(u.undos, op)
	u.redos = u.redos[:0]
}

// PushGrouped pushes an operation with a specific group ID (for atomic ops like paste, indent).
func (u *UndoStack) PushGrouped(op Operation, groupID int) {
	op.Time = time.Now()
	op.Group = groupID
	u.undos = append(u.undos, op)
	u.redos = u.redos[:0]
}

// NewGroup returns a fresh group ID for batching multiple operations as one undo.
func (u *UndoStack) NewGroup() int {
	id := u.nextGroup
	u.nextGroup++
	return id
}

// isGroupBreak returns true if consecutive ops should NOT be grouped
// (e.g., space/newline breaks a word group, or non-adjacent positions).
func isGroupBreak(prev, cur *Operation) bool {
	ch := cur.Text[0]
	if ch == ' ' || ch == '\n' || ch == '\t' {
		return true
	}
	prevCh := prev.Text[0]
	if prevCh == ' ' || prevCh == '\n' || prevCh == '\t' {
		return true
	}
	// For inserts, cursor should be adjacent
	if cur.Type == OpInsert {
		if cur.Pos.Line != prev.Pos.Line || cur.Pos.Col != prev.Pos.Col+1 {
			return true
		}
	}
	return false
}

func (u *UndoStack) CanUndo() bool { return len(u.undos) > 0 }
func (u *UndoStack) CanRedo() bool { return len(u.redos) > 0 }

// PopUndo pops the top operation and all others in the same group.
func (u *UndoStack) PopUndo() (Operation, bool) {
	if len(u.undos) == 0 {
		return Operation{}, false
	}
	op := u.undos[len(u.undos)-1]
	u.undos = u.undos[:len(u.undos)-1]
	u.redos = append(u.redos, op)

	// If grouped, also pop all preceding ops in the same group
	if op.Group != 0 {
		for len(u.undos) > 0 && u.undos[len(u.undos)-1].Group == op.Group {
			grouped := u.undos[len(u.undos)-1]
			u.undos = u.undos[:len(u.undos)-1]
			u.redos = append(u.redos, grouped)
		}
	}
	return op, true
}

// PopRedo pops the top redo operation and all others in the same group.
func (u *UndoStack) PopRedo() (Operation, bool) {
	if len(u.redos) == 0 {
		return Operation{}, false
	}
	op := u.redos[len(u.redos)-1]
	u.redos = u.redos[:len(u.redos)-1]
	u.undos = append(u.undos, op)

	// If grouped, also pop all following ops in the same group
	if op.Group != 0 {
		for len(u.redos) > 0 && u.redos[len(u.redos)-1].Group == op.Group {
			grouped := u.redos[len(u.redos)-1]
			u.redos = u.redos[:len(u.redos)-1]
			u.undos = append(u.undos, grouped)
		}
	}
	return op, true
}
