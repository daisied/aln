package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"editor/config"
	"github.com/gdamore/tcell/v2"
)

type FileNode struct {
	Name     string
	Path     string
	IsDir    bool
	Children []*FileNode
	Expanded bool
	Depth    int
}

type FileTree struct {
	root      *FileNode
	flatList  []*FileNode
	selected  int
	scrollOff int
	focused   bool
	x, y, w, h int
	Theme     *config.ColorScheme
	
	// Mouse tracking
	mousePressX, mousePressY int
	mousePressed             bool
	mouseX, mouseY           int // current mouse position for hover effects

	OnFileOpen   func(path string)
	OnNewFile    func(dirPath string)     // Request new file creation in dir
	OnNewDir     func(dirPath string)     // Request new directory creation in dir
	OnDeleteFile func(path string)        // Request file/dir deletion
	OnRenameFile func(oldPath string)     // Request file/dir rename
}

func NewFileTree(rootPath string) *FileTree {
	ft := &FileTree{
		root: &FileNode{
			Name:     filepath.Base(rootPath),
			Path:     rootPath,
			IsDir:    true,
			Expanded: true,
			Depth:    0,
		},
		mouseX:    -1,
		mouseY:    -1,
		selected:  0,
		scrollOff: 0,
	}
	ft.loadChildren(ft.root)
	ft.flatten()
	return ft
}

func (ft *FileTree) loadChildren(node *FileNode) {
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		return
	}

	node.Children = nil
	var dirs, files []*FileNode

	for _, e := range entries {
		name := e.Name()
		// Skip common noise directories, but ALLOW hidden files
		// Blocked: .git, node_modules, __pycache__, target, .DS_Store
		if name == ".git" || name == "node_modules" || name == "__pycache__" || name == "target" || name == ".DS_Store" {
			continue
		}
		
		child := &FileNode{
			Name:  name,
			Path:  filepath.Join(node.Path, name),
			IsDir: e.IsDir(),
			Depth: node.Depth + 1,
		}
		if e.IsDir() {
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	node.Children = append(dirs, files...)
}

func (ft *FileTree) flatten() {
	ft.flatList = ft.flatList[:0]
	ft.flattenNode(ft.root)
}

func (ft *FileTree) flattenNode(node *FileNode) {
	ft.flatList = append(ft.flatList, node)
	if node.IsDir && node.Expanded {
		for _, child := range node.Children {
			ft.flattenNode(child)
		}
	}
}

func (ft *FileTree) toggle(node *FileNode) {
	if !node.IsDir {
		return
	}
	node.Expanded = !node.Expanded
	if node.Expanded && node.Children == nil {
		ft.loadChildren(node)
	}
	ft.flatten()
}

func (ft *FileTree) Render(screen tcell.Screen, x, y, width, height int) {
	ft.x = x
	ft.y = y
	ft.w = width
	ft.h = height

	theme := ft.Theme
	if theme == nil {
		theme = config.Themes["monokai"]
	}

	bgStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.TreeFileFg)
	selBg := tcell.StyleDefault.Background(theme.TreeSelectionBg).Foreground(theme.TreeFileFg)
	dirStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.TreeDirFg).Bold(true)
	fileStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.TreeFileFg)
	headerStyle := tcell.StyleDefault.Background(theme.Background).Foreground(theme.TreeHeaderFg).Bold(true)

	// Clear area with background
	for cy := y; cy < y+height; cy++ {
		for cx := x; cx < x+width; cx++ {
			screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	// Header: "EXPLORER"
	title := "EXPLORER"
	for i, ch := range title {
		if x+i < x+width {
			screen.SetContent(x+i, y, ch, nil, headerStyle)
		}
	}

	// Ensure scroll keeps selected visible
	if ft.selected < ft.scrollOff {
		ft.scrollOff = ft.selected
	}
	if ft.selected >= ft.scrollOff+height-1 {
		ft.scrollOff = ft.selected - height + 2
	}

	row := y + 1
	for i := ft.scrollOff; i < len(ft.flatList) && row < y+height; i++ {
		node := ft.flatList[i]
		style := fileStyle
		if node.IsDir {
			style = dirStyle
		}
		
		// Apply selection style
		if i == ft.selected && ft.focused {
			style = selBg
		} else if i == ft.selected && !ft.focused {
			// Show dim selection when not focused (like VSCode)
			style = tcell.StyleDefault.Background(theme.Selection).Foreground(theme.Foreground).Dim(true)
		}
		
		// Apply hover style
		if ft.mouseY == row && ft.mouseX >= ft.x && ft.mouseX < ft.x+ft.w {
			// Only apply hover if not selected (or blend it)
			if i != ft.selected {
				// Use a slightly lighter background for hover
				hoverColor := theme.Background.TrueColor().Hex() + 0x101010
				style = style.Background(tcell.NewHexColor(int32(hoverColor)))
			}
		}

		// Clear line with proper background
		for cx := x; cx < x+width-1; cx++ {
			screen.SetContent(cx, row, ' ', nil, style)
		}

		col := x
		// Indent
		indent := node.Depth * 2
		for j := 0; j < indent && col < x+width; j++ {
			screen.SetContent(col, row, ' ', nil, style)
			col++
		}

		// Directory indicator
		if node.IsDir {
			icon := '▶'
			if node.Expanded {
				icon = '▼'
			}
			if col < x+width {
				screen.SetContent(col, row, icon, nil, style)
				col++
			}
			if col < x+width {
				screen.SetContent(col, row, ' ', nil, style)
				col++
			}
		} else {
			if col < x+width {
				screen.SetContent(col, row, ' ', nil, style)
				col++
			}
			if col < x+width {
				screen.SetContent(col, row, ' ', nil, style)
				col++
			}
		}

		// Name
		for _, ch := range node.Name {
			if col >= x+width {
				break
			}
			screen.SetContent(col, row, ch, nil, style)
			col++
		}

		row++
	}

	// Fill remaining space under file list with background
	for cy := row; cy < y+height; cy++ {
		for cx := x; cx < x+width-1; cx++ {
			screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	// Draw right border
	borderStyle := tcell.StyleDefault.Foreground(theme.TreeBorder).Background(theme.Background)
	for cy := y; cy < y+height; cy++ {
		screen.SetContent(x+width-1, cy, '│', nil, borderStyle)
	}
}

func (ft *FileTree) HandleKey(ev *tcell.EventKey) bool {
	if !ft.focused {
		return false
	}
	switch ev.Key() {
	case tcell.KeyUp:
		if ft.selected > 0 {
			ft.selected--
		}
		return true
	case tcell.KeyDown:
		if ft.selected < len(ft.flatList)-1 {
			ft.selected++
		}
		return true
	case tcell.KeyEnter:
		if ft.selected >= 0 && ft.selected < len(ft.flatList) {
			node := ft.flatList[ft.selected]
			if node.IsDir {
				ft.toggle(node)
			} else if ft.OnFileOpen != nil {
				ft.OnFileOpen(node.Path)
			}
		}
		return true
	case tcell.KeyRight:
		if ft.selected >= 0 && ft.selected < len(ft.flatList) {
			node := ft.flatList[ft.selected]
			if node.IsDir && !node.Expanded {
				ft.toggle(node)
			}
		}
		return true
	case tcell.KeyLeft:
		if ft.selected >= 0 && ft.selected < len(ft.flatList) {
			node := ft.flatList[ft.selected]
			if node.IsDir && node.Expanded {
				ft.toggle(node)
			}
		}
		return true
	case tcell.KeyDelete:
		if ft.selected >= 0 && ft.selected < len(ft.flatList) {
			node := ft.flatList[ft.selected]
			if node != ft.root && ft.OnDeleteFile != nil {
				ft.OnDeleteFile(node.Path)
			}
		}
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'n':
			// New file in selected directory (or parent of selected file)
			dirPath := ft.selectedDirPath()
			if ft.OnNewFile != nil {
				ft.OnNewFile(dirPath)
			}
			return true
		case 'N':
			// New directory
			dirPath := ft.selectedDirPath()
			if ft.OnNewDir != nil {
				ft.OnNewDir(dirPath)
			}
			return true
		case 'd':
			// Delete
			if ft.selected >= 0 && ft.selected < len(ft.flatList) {
				node := ft.flatList[ft.selected]
				if node != ft.root && ft.OnDeleteFile != nil {
					ft.OnDeleteFile(node.Path)
				}
			}
			return true
		case 'r':
			// Rename
			if ft.selected >= 0 && ft.selected < len(ft.flatList) {
				node := ft.flatList[ft.selected]
				if node != ft.root && ft.OnRenameFile != nil {
					ft.OnRenameFile(node.Path)
				}
			}
			return true
		}
	}
	return false
}

// selectedDirPath returns the directory path for the selected item.
// If a file is selected, returns its parent directory.
func (ft *FileTree) selectedDirPath() string {
	if ft.selected >= 0 && ft.selected < len(ft.flatList) {
		node := ft.flatList[ft.selected]
		if node.IsDir {
			return node.Path
		}
		return filepath.Dir(node.Path)
	}
	return ft.root.Path
}

// GetRoot returns the root path of the file tree
func (ft *FileTree) GetRoot() string {
	return ft.root.Path
}

func (ft *FileTree) HandleMouse(ev *tcell.EventMouse) bool {
	mx, my := ev.Position()
	
	// Update mouse position for hover effects
	ft.mouseX, ft.mouseY = mx, my
	
	if mx < ft.x || mx >= ft.x+ft.w || my < ft.y || my >= ft.y+ft.h {
		// Mouse outside tree area
		return false
	}

	btn := ev.Buttons()
	switch {
	case btn == tcell.WheelUp:
		if ft.scrollOff > 0 {
			ft.scrollOff--
		}
		return true
	case btn == tcell.WheelDown:
		if ft.scrollOff < len(ft.flatList)-1 {
			ft.scrollOff++
		}
		return true
	case btn == tcell.Button1:
		// Record mouse press position
		if !ft.mousePressed {
			ft.mousePressX, ft.mousePressY = mx, my
			ft.mousePressed = true
		}
		return true
	case btn == tcell.ButtonNone && ft.mousePressed:
		// Mouse release - only trigger if released at same position
		ft.mousePressed = false
		
		if mx == ft.mousePressX && my == ft.mousePressY {
			// Calculate which item was clicked
			row := my - ft.y - 1 // -1 for header
			idx := row + ft.scrollOff
			if idx >= 0 && idx < len(ft.flatList) {
				ft.selected = idx
				// Set focus to file tree on click
				if !ft.focused {
					ft.focused = true
				}
				
				node := ft.flatList[idx]
				
				// Check if the arrow/chevron was clicked (first 2-3 chars of the line)
				// Arrow is at position: x + depth*2
				arrowX := ft.x + node.Depth*2
				clickedArrow := mx >= arrowX && mx < arrowX+2
				
				if node.IsDir {
					if clickedArrow {
						// Clicked the arrow - just toggle
						ft.toggle(node)
					} else {
						// Clicked the directory name - toggle
						ft.toggle(node)
					}
				} else if ft.OnFileOpen != nil {
					// Clicked a file - open it
					// DO NOT change selection when opening file (keeps tree context)
					ft.OnFileOpen(node.Path)
				}
			}
		}
		return true
	}
	return false
}

// SelectPath expands the tree to reveal the given path and selects it.
func (ft *FileTree) SelectPath(targetPath string) {
	if !strings.HasPrefix(targetPath, ft.root.Path) {
		return
	}

	rel, err := filepath.Rel(ft.root.Path, targetPath)
	if err != nil || rel == "." {
		return
	}

	// Split path into components
	parts := strings.Split(rel, string(os.PathSeparator))
	
	current := ft.root
	// Ensure root is expanded
	current.Expanded = true
	if current.Children == nil {
		ft.loadChildren(current)
	}

	// Traverse and expand
	for i, part := range parts {
		var found *FileNode
		for _, child := range current.Children {
			if child.Name == part {
				found = child
				break
			}
		}

		if found == nil {
			return // Path not found in tree
		}

		current = found
		
		// Expand if it's a directory and it is a parent of our target
		// (Don't necessarily expand the target directory itself unless we want to?)
		// User said "expand the appropriate folders wherever the file is".
		// So parents MUST be expanded. The file itself is a leaf (if file).
		// If target is a directory, maybe we expand it? Let's assume parents only.
		if current.IsDir && i < len(parts)-1 {
			current.Expanded = true
			if current.Children == nil {
				ft.loadChildren(current)
			}
		}
	}

	// Re-flatten to update the view
	ft.flatten()

	// Find and select the node
	for i, node := range ft.flatList {
		if node.Path == targetPath {
			ft.selected = i
			
			// Ensure visible (scroll)
			// ft.h might not be set yet if not rendered, but we try
			if ft.h > 0 {
				if ft.selected < ft.scrollOff {
					ft.scrollOff = ft.selected
				} else if ft.selected >= ft.scrollOff+ft.h {
					ft.scrollOff = ft.selected - ft.h + 1
				}
			}
			// If not rendered yet, don't touch scrollOff - let render handle it
			break
		}
	}
}

func (ft *FileTree) IsFocused() bool   { return ft.focused }
func (ft *FileTree) SetFocused(f bool) { ft.focused = f }

// Refresh rescans the file tree while preserving expanded/collapsed state.
func (ft *FileTree) Refresh() {
	// Save expanded paths
	expandedPaths := make(map[string]bool)
	for _, node := range ft.flatList {
		if node.IsDir && node.Expanded {
			expandedPaths[node.Path] = true
		}
	}

	// Save selected path
	var selectedPath string
	if ft.selected >= 0 && ft.selected < len(ft.flatList) {
		selectedPath = ft.flatList[ft.selected].Path
	}

	// Reload root
	ft.loadChildren(ft.root)

	// Restore expanded state recursively
	ft.restoreExpandedState(ft.root, expandedPaths)

	// Rebuild flat list
	ft.flatten()

	// Restore selection
	if selectedPath != "" {
		for i, node := range ft.flatList {
			if node.Path == selectedPath {
				ft.selected = i
				break
			}
		}
	}
}

func (ft *FileTree) restoreExpandedState(node *FileNode, expandedPaths map[string]bool) {
	if !node.IsDir {
		return
	}
	if expandedPaths[node.Path] {
		node.Expanded = true
		if node.Children == nil {
			ft.loadChildren(node)
		}
		for _, child := range node.Children {
			ft.restoreExpandedState(child, expandedPaths)
		}
	}
}
