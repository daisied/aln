package buffer

type Cursor struct {
	Line, Col int
}

func (c Cursor) Before(other Cursor) bool {
	if c.Line != other.Line {
		return c.Line < other.Line
	}
	return c.Col < other.Col
}

func (c Cursor) Equal(other Cursor) bool {
	return c.Line == other.Line && c.Col == other.Col
}

type Selection struct {
	Start, End Cursor
}

func NewSelection(a, b Cursor) Selection {
	if a.Before(b) {
		return Selection{Start: a, End: b}
	}
	return Selection{Start: b, End: a}
}

func (s Selection) Contains(c Cursor) bool {
	if c.Before(s.Start) || s.End.Before(c) {
		return false
	}
	return true
}

func (s Selection) Empty() bool {
	return s.Start.Equal(s.End)
}
