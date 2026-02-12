package highlight

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"
)

type Token struct {
	Text  string
	Style tcell.Style
}

type StyledLine struct {
	Tokens []Token
}

type Highlighter struct {
	cache map[string][]StyledLine
}

func New() *Highlighter {
	return &Highlighter{
		cache: make(map[string][]StyledLine),
	}
}

func (h *Highlighter) InvalidateCache(path string) {
	delete(h.cache, path)
}

func (h *Highlighter) HighlightLines(code string, lang string, startLine, endLine int) []StyledLine {
	lines := strings.Split(code, "\n")
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine < 0 {
		startLine = 0
	}

	key := fmt.Sprintf("%s:%d:%d:%x", lang, startLine, endLine, sha256.Sum256([]byte(code)))
	if cached, ok := h.cache[key]; ok {
		return cached
	}

	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Tokenize the subset of lines we need with a bit of context
	contextStart := startLine - 50
	if contextStart < 0 {
		contextStart = 0
	}
	subset := strings.Join(lines[contextStart:endLine], "\n")

	iter, err := lexer.Tokenise(nil, subset)
	if err != nil {
		// Fallback: return unstyled lines
		result := make([]StyledLine, endLine-startLine)
		for i := startLine; i < endLine; i++ {
			result[i-startLine] = StyledLine{
				Tokens: []Token{{Text: lines[i], Style: tcell.StyleDefault}},
			}
		}
		return result
	}

	// Build styled lines from tokens
	styledLines := make([]StyledLine, endLine-contextStart)
	for i := range styledLines {
		styledLines[i] = StyledLine{}
	}

	currentLine := 0
	for _, tok := range iter.Tokens() {
		style := tokenStyle(tok.Type)
		parts := strings.Split(tok.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				currentLine++
			}
			if currentLine >= len(styledLines) {
				break
			}
			if part != "" {
				styledLines[currentLine].Tokens = append(styledLines[currentLine].Tokens, Token{
					Text:  part,
					Style: style,
				})
			}
		}
	}

	// Extract just the lines we need
	offset := startLine - contextStart
	if offset < 0 {
		offset = 0
	}
	end := offset + (endLine - startLine)
	if end > len(styledLines) {
		end = len(styledLines)
	}
	result := styledLines[offset:end]

	h.cache[key] = result
	return result
}

func DetectLanguage(filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		return ""
	}
	config := lexer.Config()
	if config == nil {
		return ""
	}
	return config.Name
}

func tokenStyle(t chroma.TokenType) tcell.Style {
	base := tcell.StyleDefault

	switch {
	case t == chroma.Keyword || t == chroma.KeywordConstant || t == chroma.KeywordDeclaration ||
		t == chroma.KeywordNamespace || t == chroma.KeywordReserved || t == chroma.KeywordType:
		return base.Foreground(tcell.ColorBlue).Bold(true)

	case t == chroma.NameBuiltin || t == chroma.NameBuiltinPseudo:
		return base.Foreground(tcell.ColorBlue)

	case t == chroma.LiteralString || t == chroma.LiteralStringAffix || t == chroma.LiteralStringBacktick ||
		t == chroma.LiteralStringChar || t == chroma.LiteralStringDouble || t == chroma.LiteralStringSingle ||
		t == chroma.LiteralStringHeredoc || t == chroma.LiteralStringInterpol ||
		t == chroma.LiteralStringOther || t == chroma.LiteralStringRegex:
		return base.Foreground(tcell.ColorGreen)

	case t == chroma.Comment || t == chroma.CommentMultiline || t == chroma.CommentSingle ||
		t == chroma.CommentSpecial || t == chroma.CommentPreproc || t == chroma.CommentPreprocFile:
		return base.Foreground(tcell.ColorGray).Italic(true)

	case t == chroma.LiteralNumber || t == chroma.LiteralNumberBin || t == chroma.LiteralNumberFloat ||
		t == chroma.LiteralNumberHex || t == chroma.LiteralNumberInteger || t == chroma.LiteralNumberIntegerLong ||
		t == chroma.LiteralNumberOct:
		return base.Foreground(tcell.ColorDarkCyan)

	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return base.Foreground(tcell.ColorYellow)

	case t == chroma.NameClass || t == chroma.NameException || t == chroma.NameDecorator:
		return base.Foreground(tcell.ColorFuchsia)

	case t == chroma.Operator || t == chroma.OperatorWord:
		return base.Foreground(tcell.ColorWhite)

	case t == chroma.Punctuation:
		return base.Foreground(tcell.ColorWhite)

	default:
		return base.Foreground(tcell.ColorWhite)
	}
}
