package editor

type GitLineStatus int

const (
	GitUnchanged GitLineStatus = iota
	GitAdded
	GitModified
	GitDeleted // line after a deletion
)

type GitGutter struct {
	lineStatus map[int]GitLineStatus
	available  bool
}

func NewGitGutter() *GitGutter {
	return &GitGutter{
		lineStatus: make(map[int]GitLineStatus),
		available:  false,
	}
}

// Update refreshes git diff data for the given file path.
func (g *GitGutter) Update(filePath string) {
	// Git integration is intentionally disabled.
	g.available = false
	g.lineStatus = make(map[int]GitLineStatus)
}

// StatusAt returns the git status for a given line number (0-indexed).
func (g *GitGutter) StatusAt(line int) GitLineStatus {
	if !g.available {
		return GitUnchanged
	}
	if s, ok := g.lineStatus[line]; ok {
		return s
	}
	return GitUnchanged
}
