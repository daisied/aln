package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type EditorConfigSettings struct {
	IndentStyle            string // "tab" or "space"
	IndentSize             int    // 0 means unset
	TabWidth               int    // 0 means unset
	EndOfLine              string // "lf" or "crlf"
	TrimTrailingWhitespace bool
	InsertFinalNewline     bool
	Charset                string // "utf-8", "latin1", etc.
}

// FindEditorConfig searches for .editorconfig files from the file's directory
// upward, parses matching sections, and returns merged settings.
// Returns nil if no .editorconfig files are found or no sections match.
func FindEditorConfig(filePath string) *EditorConfigSettings {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil
	}

	fileName := filepath.Base(absPath)
	dir := filepath.Dir(absPath)

	// Collect .editorconfig files from closest to farthest
	var configs []map[string]string
	for {
		ecPath := filepath.Join(dir, ".editorconfig")
		if props, isRoot := parseEditorConfig(ecPath, fileName); props != nil {
			configs = append(configs, props)
			if isRoot {
				break
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if len(configs) == 0 {
		return nil
	}

	// Merge: closest file takes precedence (already first in slice).
	// Start from farthest and let closer files overwrite.
	merged := make(map[string]string)
	for i := len(configs) - 1; i >= 0; i-- {
		for k, v := range configs[i] {
			merged[k] = v
		}
	}

	return settingsFromMap(merged)
}

// parseEditorConfig reads an .editorconfig file and returns the merged
// properties for sections matching fileName. Returns (nil, false) if the
// file doesn't exist. The bool indicates whether root = true was found.
func parseEditorConfig(path, fileName string) (map[string]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	props := make(map[string]string)
	isRoot := false
	inMatchingSection := false
	inPreamble := true // before any section header

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		// Section header
		if line[0] == '[' && line[len(line)-1] == ']' {
			inPreamble = false
			pattern := line[1 : len(line)-1]
			inMatchingSection = matchPattern(pattern, fileName)
			continue
		}

		// Key = value
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		value := strings.TrimSpace(line[eqIdx+1:])
		key = strings.ToLower(key)
		value = strings.ToLower(value)

		if inPreamble && key == "root" && value == "true" {
			isRoot = true
			continue
		}

		if inMatchingSection {
			props[key] = value
		}
	}

	if len(props) == 0 {
		return nil, isRoot
	}
	return props, isRoot
}

// matchPattern checks if fileName matches an editorconfig glob pattern.
// Handles {a,b,c} brace expansion by expanding into multiple patterns
// and checking each with filepath.Match.
func matchPattern(pattern, fileName string) bool {
	patterns := expandBraces(pattern)
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, fileName); matched {
			return true
		}
	}
	return false
}

// expandBraces expands brace expressions like "*.{js,ts}" into
// ["*.js", "*.ts"]. Handles one level of brace expansion.
func expandBraces(pattern string) []string {
	braceStart := strings.IndexByte(pattern, '{')
	if braceStart < 0 {
		return []string{pattern}
	}

	// Find the matching closing brace
	braceEnd := -1
	depth := 0
	for i := braceStart; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				braceEnd = i
			}
		}
		if braceEnd >= 0 {
			break
		}
	}

	if braceEnd < 0 {
		return []string{pattern}
	}

	prefix := pattern[:braceStart]
	suffix := pattern[braceEnd+1:]
	alternatives := splitBraceAlternatives(pattern[braceStart+1 : braceEnd])

	var results []string
	for _, alt := range alternatives {
		// Recursively expand in case suffix has more braces
		expanded := expandBraces(prefix + alt + suffix)
		results = append(results, expanded...)
	}
	return results
}

// splitBraceAlternatives splits "a,b,c" respecting nested braces.
func splitBraceAlternatives(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func settingsFromMap(m map[string]string) *EditorConfigSettings {
	s := &EditorConfigSettings{}
	hasAny := false

	if v, ok := m["indent_style"]; ok {
		s.IndentStyle = v
		hasAny = true
	}
	if v, ok := m["indent_size"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.IndentSize = n
			hasAny = true
		}
	}
	if v, ok := m["tab_width"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.TabWidth = n
			hasAny = true
		}
	}
	if v, ok := m["end_of_line"]; ok {
		s.EndOfLine = v
		hasAny = true
	}
	if v, ok := m["trim_trailing_whitespace"]; ok {
		s.TrimTrailingWhitespace = v == "true"
		hasAny = true
	}
	if v, ok := m["insert_final_newline"]; ok {
		s.InsertFinalNewline = v == "true"
		hasAny = true
	}
	if v, ok := m["charset"]; ok {
		s.Charset = v
		hasAny = true
	}

	if !hasAny {
		return nil
	}
	return s
}
