package lsp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Known language server commands
var languageServers = map[string][]string{
	"Go":         {"gopls"},
	"Python":     {"pyright-langserver", "--stdio"},
	"TypeScript": {"typescript-language-server", "--stdio"},
	"JavaScript": {"typescript-language-server", "--stdio"},
	"Rust":       {"rust-analyzer"},
	"C":          {"clangd"},
	"C++":        {"clangd"},
}

// languageIDs maps editor language names to LSP language identifiers.
var languageIDs = map[string]string{
	"Go":         "go",
	"Python":     "python",
	"TypeScript": "typescript",
	"JavaScript": "javascript",
	"Rust":       "rust",
	"C":          "c",
	"C++":        "cpp",
	"Java":       "java",
	"HTML":       "html",
	"CSS":        "css",
	"JSON":       "json",
	"YAML":       "yaml",
}

type Manager struct {
	clients     map[string]*Client   // language -> client
	diagnostics map[string][]Diagnostic // URI -> diagnostics
	rootURI     string
}

func NewManager(workDir string) *Manager {
	return &Manager{
		clients:     make(map[string]*Client),
		diagnostics: make(map[string][]Diagnostic),
		rootURI:     FileURI(workDir),
	}
}

// FileURI converts a file path to a file:// URI.
func FileURI(path string) string {
	absPath, _ := filepath.Abs(path)
	return "file://" + absPath
}

// URIToPath converts a file:// URI back to a file path.
func URIToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

// EnsureServer starts a language server for the given language if available
// and not already running.
func (m *Manager) EnsureServer(language string) *Client {
	if client, ok := m.clients[language]; ok {
		return client
	}

	serverCmd, ok := languageServers[language]
	if !ok {
		return nil
	}

	if _, err := exec.LookPath(serverCmd[0]); err != nil {
		return nil
	}

	client, err := NewClient(serverCmd[0], serverCmd[1:]...)
	if err != nil {
		return nil
	}

	client.OnDiagnostics = func(params PublishDiagnosticsParams) {
		m.diagnostics[params.URI] = params.Diagnostics
	}

	initParams := map[string]interface{}{
		"processId": nil,
		"rootUri":   m.rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"completion": map[string]interface{}{
					"completionItem": map[string]interface{}{
						"snippetSupport": false,
					},
				},
				"hover": map[string]interface{}{
					"contentFormat": []string{"plaintext"},
				},
				"publishDiagnostics": map[string]interface{}{},
			},
		},
	}

	_, err = client.sendRequest("initialize", initParams)
	if err != nil {
		client.Close()
		return nil
	}

	client.sendNotification("initialized", map[string]interface{}{})

	m.clients[language] = client
	return client
}

// DidOpen notifies the language server that a file was opened.
func (m *Manager) DidOpen(language, path, content string) {
	client := m.EnsureServer(language)
	if client == nil {
		return
	}

	langID := languageIDs[language]
	if langID == "" {
		langID = strings.ToLower(language)
	}

	client.sendNotification("textDocument/didOpen", map[string]interface{}{
		"textDocument": TextDocumentItem{
			URI:        FileURI(path),
			LanguageID: langID,
			Version:    1,
			Text:       content,
		},
	})
}

// DidChange notifies servers of a full document change.
func (m *Manager) DidChange(path, content string, version int) {
	for _, client := range m.clients {
		client.sendNotification("textDocument/didChange", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     FileURI(path),
				"version": version,
			},
			"contentChanges": []map[string]interface{}{
				{"text": content},
			},
		})
	}
}

// DidSave notifies servers that a file was saved.
func (m *Manager) DidSave(path string) {
	for _, client := range m.clients {
		client.sendNotification("textDocument/didSave", map[string]interface{}{
			"textDocument": TextDocumentIdentifier{URI: FileURI(path)},
		})
	}
}

// Completion requests completions at the given position.
func (m *Manager) Completion(language, path string, line, col int) []CompletionItem {
	client := m.EnsureServer(language)
	if client == nil {
		return nil
	}

	result, err := client.sendRequest("textDocument/completion", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: FileURI(path)},
		Position:     Position{Line: line, Character: col},
	})
	if err != nil || result == nil {
		return nil
	}

	var list CompletionList
	if err := json.Unmarshal(result, &list); err == nil {
		return list.Items
	}

	var items []CompletionItem
	json.Unmarshal(result, &items)
	return items
}

// Hover gets hover info at the given position.
func (m *Manager) Hover(language, path string, line, col int) string {
	client := m.EnsureServer(language)
	if client == nil {
		return ""
	}

	result, err := client.sendRequest("textDocument/hover", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: FileURI(path)},
		Position:     Position{Line: line, Character: col},
	})
	if err != nil || result == nil {
		return ""
	}

	var hover Hover
	if err := json.Unmarshal(result, &hover); err != nil {
		return ""
	}

	switch v := hover.Contents.(type) {
	case string:
		return v
	case map[string]interface{}:
		if val, ok := v["value"]; ok {
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

// Definition goes to the definition of the symbol at the given position.
func (m *Manager) Definition(language, path string, line, col int) *Location {
	client := m.EnsureServer(language)
	if client == nil {
		return nil
	}

	result, err := client.sendRequest("textDocument/definition", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: FileURI(path)},
		Position:     Position{Line: line, Character: col},
	})
	if err != nil || result == nil {
		return nil
	}

	var loc Location
	if err := json.Unmarshal(result, &loc); err == nil && loc.URI != "" {
		return &loc
	}

	var locs []Location
	if err := json.Unmarshal(result, &locs); err == nil && len(locs) > 0 {
		return &locs[0]
	}

	return nil
}

// Rename renames the symbol at the given position across the workspace.
func (m *Manager) Rename(language, path string, line, col int, newName string) *WorkspaceEdit {
	client := m.EnsureServer(language)
	if client == nil {
		return nil
	}

	result, err := client.sendRequest("textDocument/rename", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: FileURI(path)},
		"position":     Position{Line: line, Character: col},
		"newName":      newName,
	})
	if err != nil || result == nil {
		return nil
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(result, &edit); err != nil {
		return nil
	}
	return &edit
}

// GetDiagnostics returns diagnostics for a file.
func (m *Manager) GetDiagnostics(path string) []Diagnostic {
	return m.diagnostics[FileURI(path)]
}

// Close shuts down all language servers.
func (m *Manager) Close() {
	for _, client := range m.clients {
		client.Close()
	}
	m.clients = make(map[string]*Client)
}
