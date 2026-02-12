package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
)

type Config struct {
	TabSize            int     `json:"tab_size"`
	Shell              string  `json:"shell"`
	TreeWidth          int     `json:"tree_width"`
	TermRatio          float64 `json:"terminal_ratio"`
	Theme              string  `json:"theme"`
	WordWrap           bool    `json:"word_wrap"`
	AutoClose          bool    `json:"auto_close"`
	QuoteWrapSelection bool    `json:"quote_wrap_selection"`
	TrimTrailingSpace  bool    `json:"trim_trailing_whitespace"`
	InsertFinalNewline bool    `json:"insert_final_newline"`
}

// LanguageTabSize returns the appropriate tab size for a given language.
// Returns the per-language default or the user's configured tab size.
func (c *Config) LanguageTabSize(language string) int {
	switch language {
	case "JavaScript", "TypeScript", "JSON", "HTML", "CSS", "SCSS",
		"YAML", "Vue", "Svelte", "JSX", "TSX", "TOML":
		return 2
	case "Go", "Python", "Java", "C", "C++", "Rust", "C#", "PHP":
		return 4
	case "Makefile":
		return 8 // Makefiles use real tabs, but this sets the visual width
	default:
		return c.TabSize
	}
}

// LanguageUseTabs returns whether a language should use real tabs vs spaces.
func (c *Config) LanguageUseTabs(language string) bool {
	switch language {
	case "Go", "Makefile":
		return true
	default:
		return false
	}
}

type ColorScheme struct {
	Name             string
	Background       tcell.Color
	Foreground       tcell.Color
	Selection        tcell.Color
	LineNumber       tcell.Color
	LineNumberActive tcell.Color
	StatusBarBg      tcell.Color
	StatusBarFg      tcell.Color
	StatusBarModeBg  tcell.Color
	TabBarBg         tcell.Color
	TabBarFg         tcell.Color
	TabBarActiveBg   tcell.Color
	TabBarActiveFg   tcell.Color
	TreeHeaderFg     tcell.Color
	TreeDirFg        tcell.Color
	TreeFileFg       tcell.Color
	TreeSelectionBg  tcell.Color
	TreeBorder       tcell.Color
	DialogBg         tcell.Color
	DialogFg         tcell.Color
	DialogInputBg    tcell.Color
	IndentGuide      tcell.Color
}

var Themes = map[string]*ColorScheme{
	"dark": {
		Name:             "Dark",
		Background:       tcell.ColorBlack,
		Foreground:       tcell.ColorWhite,
		Selection:        tcell.ColorDarkBlue,
		LineNumber:       tcell.ColorGray,
		LineNumberActive: tcell.ColorWhite,
		StatusBarBg:      tcell.ColorDarkBlue,
		StatusBarFg:      tcell.ColorWhite,
		StatusBarModeBg:  tcell.ColorBlue,
		TabBarBg:         tcell.ColorBlack,
		TabBarFg:         tcell.ColorGray,
		TabBarActiveBg:   tcell.ColorDarkBlue,
		TabBarActiveFg:   tcell.ColorWhite,
		TreeHeaderFg:     tcell.ColorYellow,
		TreeDirFg:        tcell.ColorBlue,
		TreeFileFg:       tcell.ColorWhite,
		TreeSelectionBg:  tcell.ColorDarkBlue,
		TreeBorder:       tcell.ColorGray,
		DialogBg:         tcell.ColorBlack,
		DialogFg:         tcell.ColorWhite,
		DialogInputBg:    tcell.ColorDarkBlue,
		IndentGuide:      tcell.ColorDimGray,
	},
	"light": {
		Name:             "Light",
		Background:       tcell.ColorWhite,
		Foreground:       tcell.ColorBlack,
		Selection:        tcell.ColorLightBlue,
		LineNumber:       tcell.ColorGray,
		LineNumberActive: tcell.ColorBlack,
		StatusBarBg:      tcell.ColorLightBlue,
		StatusBarFg:      tcell.ColorBlack,
		StatusBarModeBg:  tcell.ColorBlue,
		TabBarBg:         tcell.ColorWhite,
		TabBarFg:         tcell.ColorGray,
		TabBarActiveBg:   tcell.ColorLightBlue,
		TabBarActiveFg:   tcell.ColorBlack,
		TreeHeaderFg:     tcell.ColorBlue,
		TreeDirFg:        tcell.ColorBlue,
		TreeFileFg:       tcell.ColorBlack,
		TreeSelectionBg:  tcell.ColorLightBlue,
		TreeBorder:       tcell.ColorGray,
		DialogBg:         tcell.ColorWhite,
		DialogFg:         tcell.ColorBlack,
		DialogInputBg:    tcell.ColorLightGray,
		IndentGuide:      tcell.ColorLightGray,
	},
	"monokai": {
		Name:             "Monokai",
		Background:       tcell.NewRGBColor(39, 40, 34),
		Foreground:       tcell.NewRGBColor(248, 248, 242),
		Selection:        tcell.NewRGBColor(73, 72, 62),
		LineNumber:       tcell.NewRGBColor(144, 144, 128),
		LineNumberActive: tcell.NewRGBColor(248, 248, 242),
		StatusBarBg:      tcell.NewRGBColor(73, 72, 62),
		StatusBarFg:      tcell.NewRGBColor(248, 248, 242),
		StatusBarModeBg:  tcell.NewRGBColor(102, 217, 239),
		TabBarBg:         tcell.NewRGBColor(39, 40, 34),
		TabBarFg:         tcell.NewRGBColor(144, 144, 128),
		TabBarActiveBg:   tcell.NewRGBColor(73, 72, 62),
		TabBarActiveFg:   tcell.NewRGBColor(248, 248, 242),
		TreeHeaderFg:     tcell.NewRGBColor(249, 38, 114),
		TreeDirFg:        tcell.NewRGBColor(102, 217, 239),
		TreeFileFg:       tcell.NewRGBColor(248, 248, 242),
		TreeSelectionBg:  tcell.NewRGBColor(73, 72, 62),
		TreeBorder:       tcell.NewRGBColor(144, 144, 128),
		DialogBg:         tcell.NewRGBColor(39, 40, 34),
		DialogFg:         tcell.NewRGBColor(248, 248, 242),
		DialogInputBg:    tcell.NewRGBColor(73, 72, 62),
		IndentGuide:      tcell.NewRGBColor(70, 71, 60),
	},
	"nord": {
		Name:             "Nord",
		Background:       tcell.NewRGBColor(46, 52, 64),
		Foreground:       tcell.NewRGBColor(236, 239, 244),
		Selection:        tcell.NewRGBColor(67, 76, 94),
		LineNumber:       tcell.NewRGBColor(76, 86, 106),
		LineNumberActive: tcell.NewRGBColor(236, 239, 244),
		StatusBarBg:      tcell.NewRGBColor(67, 76, 94),
		StatusBarFg:      tcell.NewRGBColor(236, 239, 244),
		StatusBarModeBg:  tcell.NewRGBColor(136, 192, 208),
		TabBarBg:         tcell.NewRGBColor(46, 52, 64),
		TabBarFg:         tcell.NewRGBColor(76, 86, 106),
		TabBarActiveBg:   tcell.NewRGBColor(67, 76, 94),
		TabBarActiveFg:   tcell.NewRGBColor(236, 239, 244),
		TreeHeaderFg:     tcell.NewRGBColor(136, 192, 208),
		TreeDirFg:        tcell.NewRGBColor(136, 192, 208),
		TreeFileFg:       tcell.NewRGBColor(236, 239, 244),
		TreeSelectionBg:  tcell.NewRGBColor(67, 76, 94),
		TreeBorder:       tcell.NewRGBColor(76, 86, 106),
		DialogBg:         tcell.NewRGBColor(46, 52, 64),
		DialogFg:         tcell.NewRGBColor(236, 239, 244),
		DialogInputBg:    tcell.NewRGBColor(67, 76, 94),
		IndentGuide:      tcell.NewRGBColor(59, 66, 82),
	},
	"solarized-dark": {
		Name:             "Solarized Dark",
		Background:       tcell.NewRGBColor(0, 43, 54),
		Foreground:       tcell.NewRGBColor(131, 148, 150),
		Selection:        tcell.NewRGBColor(7, 54, 66),
		LineNumber:       tcell.NewRGBColor(88, 110, 117),
		LineNumberActive: tcell.NewRGBColor(147, 161, 161),
		StatusBarBg:      tcell.NewRGBColor(7, 54, 66),
		StatusBarFg:      tcell.NewRGBColor(147, 161, 161),
		StatusBarModeBg:  tcell.NewRGBColor(38, 139, 210),
		TabBarBg:         tcell.NewRGBColor(0, 43, 54),
		TabBarFg:         tcell.NewRGBColor(88, 110, 117),
		TabBarActiveBg:   tcell.NewRGBColor(7, 54, 66),
		TabBarActiveFg:   tcell.NewRGBColor(147, 161, 161),
		TreeHeaderFg:     tcell.NewRGBColor(203, 75, 22),
		TreeDirFg:        tcell.NewRGBColor(38, 139, 210),
		TreeFileFg:       tcell.NewRGBColor(131, 148, 150),
		TreeSelectionBg:  tcell.NewRGBColor(7, 54, 66),
		TreeBorder:       tcell.NewRGBColor(88, 110, 117),
		DialogBg:         tcell.NewRGBColor(0, 43, 54),
		DialogFg:         tcell.NewRGBColor(131, 148, 150),
		DialogInputBg:    tcell.NewRGBColor(7, 54, 66),
		IndentGuide:      tcell.NewRGBColor(30, 65, 73),
	},
	"gruvbox": {
		Name:             "Gruvbox Dark",
		Background:       tcell.NewRGBColor(40, 40, 40),
		Foreground:       tcell.NewRGBColor(235, 219, 178),
		Selection:        tcell.NewRGBColor(60, 56, 54),
		LineNumber:       tcell.NewRGBColor(146, 131, 116),
		LineNumberActive: tcell.NewRGBColor(251, 241, 199),
		StatusBarBg:      tcell.NewRGBColor(60, 56, 54),
		StatusBarFg:      tcell.NewRGBColor(235, 219, 178),
		StatusBarModeBg:  tcell.NewRGBColor(184, 187, 38),
		TabBarBg:         tcell.NewRGBColor(40, 40, 40),
		TabBarFg:         tcell.NewRGBColor(146, 131, 116),
		TabBarActiveBg:   tcell.NewRGBColor(60, 56, 54),
		TabBarActiveFg:   tcell.NewRGBColor(235, 219, 178),
		TreeHeaderFg:     tcell.NewRGBColor(254, 128, 25),
		TreeDirFg:        tcell.NewRGBColor(131, 165, 152),
		TreeFileFg:       tcell.NewRGBColor(235, 219, 178),
		TreeSelectionBg:  tcell.NewRGBColor(60, 56, 54),
		TreeBorder:       tcell.NewRGBColor(102, 92, 84),
		DialogBg:         tcell.NewRGBColor(40, 40, 40),
		DialogFg:         tcell.NewRGBColor(235, 219, 178),
		DialogInputBg:    tcell.NewRGBColor(60, 56, 54),
		IndentGuide:      tcell.NewRGBColor(80, 73, 69),
	},
	"gruvbox-light": {
		Name:             "Gruvbox Light",
		Background:       tcell.NewRGBColor(251, 241, 199),
		Foreground:       tcell.NewRGBColor(60, 56, 54),
		Selection:        tcell.NewRGBColor(213, 196, 161),
		LineNumber:       tcell.NewRGBColor(189, 174, 147),
		LineNumberActive: tcell.NewRGBColor(60, 56, 54),
		StatusBarBg:      tcell.NewRGBColor(213, 196, 161),
		StatusBarFg:      tcell.NewRGBColor(60, 56, 54),
		StatusBarModeBg:  tcell.NewRGBColor(121, 116, 14),
		TabBarBg:         tcell.NewRGBColor(251, 241, 199),
		TabBarFg:         tcell.NewRGBColor(146, 131, 116),
		TabBarActiveBg:   tcell.NewRGBColor(213, 196, 161),
		TabBarActiveFg:   tcell.NewRGBColor(60, 56, 54),
		TreeHeaderFg:     tcell.NewRGBColor(175, 58, 3),
		TreeDirFg:        tcell.NewRGBColor(69, 133, 136),
		TreeFileFg:       tcell.NewRGBColor(60, 56, 54),
		TreeSelectionBg:  tcell.NewRGBColor(213, 196, 161),
		TreeBorder:       tcell.NewRGBColor(189, 174, 147),
		DialogBg:         tcell.NewRGBColor(251, 241, 199),
		DialogFg:         tcell.NewRGBColor(60, 56, 54),
		DialogInputBg:    tcell.NewRGBColor(213, 196, 161),
		IndentGuide:      tcell.NewRGBColor(213, 196, 161),
	},
	"dracula": {
		Name:             "Dracula",
		Background:       tcell.NewRGBColor(40, 42, 54),
		Foreground:       tcell.NewRGBColor(248, 248, 242),
		Selection:        tcell.NewRGBColor(68, 71, 90),
		LineNumber:       tcell.NewRGBColor(98, 114, 164),
		LineNumberActive: tcell.NewRGBColor(248, 248, 242),
		StatusBarBg:      tcell.NewRGBColor(68, 71, 90),
		StatusBarFg:      tcell.NewRGBColor(248, 248, 242),
		StatusBarModeBg:  tcell.NewRGBColor(189, 147, 249),
		TabBarBg:         tcell.NewRGBColor(40, 42, 54),
		TabBarFg:         tcell.NewRGBColor(98, 114, 164),
		TabBarActiveBg:   tcell.NewRGBColor(68, 71, 90),
		TabBarActiveFg:   tcell.NewRGBColor(248, 248, 242),
		TreeHeaderFg:     tcell.NewRGBColor(255, 121, 198),
		TreeDirFg:        tcell.NewRGBColor(139, 233, 253),
		TreeFileFg:       tcell.NewRGBColor(248, 248, 242),
		TreeSelectionBg:  tcell.NewRGBColor(68, 71, 90),
		TreeBorder:       tcell.NewRGBColor(98, 114, 164),
		DialogBg:         tcell.NewRGBColor(40, 42, 54),
		DialogFg:         tcell.NewRGBColor(248, 248, 242),
		DialogInputBg:    tcell.NewRGBColor(68, 71, 90),
		IndentGuide:      tcell.NewRGBColor(55, 58, 75),
	},
	"one-dark": {
		Name:             "One Dark",
		Background:       tcell.NewRGBColor(40, 44, 52),
		Foreground:       tcell.NewRGBColor(171, 178, 191),
		Selection:        tcell.NewRGBColor(61, 66, 77),
		LineNumber:       tcell.NewRGBColor(92, 99, 112),
		LineNumberActive: tcell.NewRGBColor(171, 178, 191),
		StatusBarBg:      tcell.NewRGBColor(61, 66, 77),
		StatusBarFg:      tcell.NewRGBColor(171, 178, 191),
		StatusBarModeBg:  tcell.NewRGBColor(97, 175, 239),
		TabBarBg:         tcell.NewRGBColor(40, 44, 52),
		TabBarFg:         tcell.NewRGBColor(92, 99, 112),
		TabBarActiveBg:   tcell.NewRGBColor(61, 66, 77),
		TabBarActiveFg:   tcell.NewRGBColor(171, 178, 191),
		TreeHeaderFg:     tcell.NewRGBColor(198, 120, 221),
		TreeDirFg:        tcell.NewRGBColor(97, 175, 239),
		TreeFileFg:       tcell.NewRGBColor(171, 178, 191),
		TreeSelectionBg:  tcell.NewRGBColor(61, 66, 77),
		TreeBorder:       tcell.NewRGBColor(92, 99, 112),
		DialogBg:         tcell.NewRGBColor(40, 44, 52),
		DialogFg:         tcell.NewRGBColor(171, 178, 191),
		DialogInputBg:    tcell.NewRGBColor(61, 66, 77),
		IndentGuide:      tcell.NewRGBColor(52, 56, 67),
	},
	"tokyo-night": {
		Name:             "Tokyo Night",
		Background:       tcell.NewRGBColor(26, 27, 38),
		Foreground:       tcell.NewRGBColor(169, 177, 214),
		Selection:        tcell.NewRGBColor(47, 52, 73),
		LineNumber:       tcell.NewRGBColor(86, 95, 137),
		LineNumberActive: tcell.NewRGBColor(169, 177, 214),
		StatusBarBg:      tcell.NewRGBColor(47, 52, 73),
		StatusBarFg:      tcell.NewRGBColor(169, 177, 214),
		StatusBarModeBg:  tcell.NewRGBColor(125, 207, 255),
		TabBarBg:         tcell.NewRGBColor(26, 27, 38),
		TabBarFg:         tcell.NewRGBColor(86, 95, 137),
		TabBarActiveBg:   tcell.NewRGBColor(47, 52, 73),
		TabBarActiveFg:   tcell.NewRGBColor(169, 177, 214),
		TreeHeaderFg:     tcell.NewRGBColor(187, 154, 247),
		TreeDirFg:        tcell.NewRGBColor(125, 207, 255),
		TreeFileFg:       tcell.NewRGBColor(169, 177, 214),
		TreeSelectionBg:  tcell.NewRGBColor(47, 52, 73),
		TreeBorder:       tcell.NewRGBColor(86, 95, 137),
		DialogBg:         tcell.NewRGBColor(26, 27, 38),
		DialogFg:         tcell.NewRGBColor(169, 177, 214),
		DialogInputBg:    tcell.NewRGBColor(47, 52, 73),
		IndentGuide:      tcell.NewRGBColor(40, 44, 60),
	},
	"catppuccin": {
		Name:             "Catppuccin Mocha",
		Background:       tcell.NewRGBColor(30, 30, 46),
		Foreground:       tcell.NewRGBColor(205, 214, 244),
		Selection:        tcell.NewRGBColor(69, 71, 90),
		LineNumber:       tcell.NewRGBColor(108, 112, 134),
		LineNumberActive: tcell.NewRGBColor(205, 214, 244),
		StatusBarBg:      tcell.NewRGBColor(69, 71, 90),
		StatusBarFg:      tcell.NewRGBColor(205, 214, 244),
		StatusBarModeBg:  tcell.NewRGBColor(180, 190, 254),
		TabBarBg:         tcell.NewRGBColor(30, 30, 46),
		TabBarFg:         tcell.NewRGBColor(108, 112, 134),
		TabBarActiveBg:   tcell.NewRGBColor(69, 71, 90),
		TabBarActiveFg:   tcell.NewRGBColor(205, 214, 244),
		TreeHeaderFg:     tcell.NewRGBColor(245, 194, 231),
		TreeDirFg:        tcell.NewRGBColor(137, 220, 235),
		TreeFileFg:       tcell.NewRGBColor(205, 214, 244),
		TreeSelectionBg:  tcell.NewRGBColor(69, 71, 90),
		TreeBorder:       tcell.NewRGBColor(108, 112, 134),
		DialogBg:         tcell.NewRGBColor(30, 30, 46),
		DialogFg:         tcell.NewRGBColor(205, 214, 244),
		DialogInputBg:    tcell.NewRGBColor(69, 71, 90),
		IndentGuide:      tcell.NewRGBColor(52, 53, 65),
	},
	"high-contrast": {
		Name:             "High Contrast",
		Background:       tcell.NewRGBColor(0, 0, 0),
		Foreground:       tcell.NewRGBColor(255, 255, 255),
		Selection:        tcell.NewRGBColor(0, 80, 160),
		LineNumber:       tcell.NewRGBColor(180, 180, 180),
		LineNumberActive: tcell.NewRGBColor(255, 255, 0),
		StatusBarBg:      tcell.NewRGBColor(0, 0, 200),
		StatusBarFg:      tcell.NewRGBColor(255, 255, 255),
		StatusBarModeBg:  tcell.NewRGBColor(200, 200, 0),
		TabBarBg:         tcell.NewRGBColor(0, 0, 0),
		TabBarFg:         tcell.NewRGBColor(180, 180, 180),
		TabBarActiveBg:   tcell.NewRGBColor(0, 0, 200),
		TabBarActiveFg:   tcell.NewRGBColor(255, 255, 255),
		TreeHeaderFg:     tcell.NewRGBColor(255, 255, 0),
		TreeDirFg:        tcell.NewRGBColor(100, 200, 255),
		TreeFileFg:       tcell.NewRGBColor(255, 255, 255),
		TreeSelectionBg:  tcell.NewRGBColor(0, 80, 160),
		TreeBorder:       tcell.NewRGBColor(255, 255, 255),
		DialogBg:         tcell.NewRGBColor(0, 0, 0),
		DialogFg:         tcell.NewRGBColor(255, 255, 255),
		DialogInputBg:    tcell.NewRGBColor(40, 40, 40),
		IndentGuide:      tcell.NewRGBColor(60, 60, 60),
	},
}

func Default() *Config {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return &Config{
		TabSize:            4,
		Shell:              shell,
		TreeWidth:          24,
		TermRatio:          0.30,
		Theme:              "monokai",
		AutoClose:          true,
		QuoteWrapSelection: true,
		TrimTrailingSpace:  false,
		InsertFinalNewline: true,
	}
}

func (c *Config) GetTheme() *ColorScheme {
	theme, ok := Themes[c.Theme]
	if !ok {
		return Themes["monokai"]
	}
	return theme
}

func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "aln", "settings.json")
}

func Load() (*Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist, return default config
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	path := ConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
