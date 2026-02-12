package clipboardx

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
)

var internalClipboard string

func Write(text string) bool {
	internalClipboard = text
	ok := false

	if err := clipboard.WriteAll(text); err == nil {
		ok = true
	}
	if writeWithCommands(text) {
		ok = true
	}
	if writeOSC52(text) {
		ok = true
	}

	return ok
}

func Read() string {
	if text, err := clipboard.ReadAll(); err == nil && text != "" {
		return text
	}
	if text, ok := readWithCommands(); ok && text != "" {
		return text
	}
	return internalClipboard
}

func writeWithCommands(text string) bool {
	commands := []struct {
		name string
		args []string
	}{
		{name: "wl-copy", args: []string{}},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "pbcopy", args: []string{}},
		{name: "clip.exe", args: []string{}},
	}

	ok := false
	for _, cmdCfg := range commands {
		if _, err := exec.LookPath(cmdCfg.name); err != nil {
			continue
		}
		cmd := exec.Command(cmdCfg.name, cmdCfg.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			ok = true
		}
	}
	return ok
}

func readWithCommands() (string, bool) {
	commands := []struct {
		name string
		args []string
	}{
		{name: "wl-paste", args: []string{"--no-newline"}},
		{name: "xclip", args: []string{"-o", "-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--output"}},
		{name: "pbpaste", args: []string{}},
		{name: "powershell.exe", args: []string{"-NoProfile", "-Command", "Get-Clipboard"}},
	}

	for _, cmdCfg := range commands {
		if _, err := exec.LookPath(cmdCfg.name); err != nil {
			continue
		}
		out, err := exec.Command(cmdCfg.name, cmdCfg.args...).Output()
		if err == nil && len(out) > 0 {
			return string(out), true
		}
	}
	return "", false
}

func writeOSC52(text string) bool {
	if text == "" {
		return false
	}
	if fi, err := os.Stdout.Stat(); err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return false
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
	return err == nil
}
