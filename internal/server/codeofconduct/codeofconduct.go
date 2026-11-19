// Package codeofconduct holds the server's Code of Conduct texts: one file per
// language that the configuration phase shows a joining client.
package codeofconduct

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultDir is the texts' folder, relative to the working directory.
// vanilla: DedicatedServer.readCodeOfConducts
const DefaultDir = "codeofconduct"

// Texts maps a lowercase language code (file name minus ".txt") to its text.
type Texts map[string]string

// colorCodes matches a section-sign formatting code.
// vanilla: StringUtil.stripColor
var colorCodes = regexp.MustCompile(`(?i)\x{00A7}[0-9A-FK-OR]`)

// Load reads every .txt file of dir. The folder must exist, and a file that is
// a link out of it is refused.
// vanilla: DedicatedServer.readCodeOfConducts
func Load(dir string) (Texts, error) {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("code of conduct folder does not exist: %s", dir)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read code of conduct folder: %w", err)
	}
	texts := Texts{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".txt") {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("failed to read code of conduct file %s: %w", name, err)
		}
		real, err = filepath.Abs(real)
		if err != nil {
			return nil, err
		}
		if filepath.Dir(real) != root {
			return nil, fmt.Errorf("failed to read code of conduct file %q because it links to a file outside the allowed directory", name)
		}
		data, err := os.ReadFile(real)
		if err != nil {
			return nil, fmt.Errorf("failed to read code of conduct file %s: %w", name, err)
		}
		lang := strings.ToLower(strings.TrimSuffix(name, ".txt"))
		texts[lang] = colorCodes.ReplaceAllString(joinLines(string(data)), "")
	}
	return texts, nil
}

// joinLines re-joins text with "\n", dropping the final line break.
// vanilla: Files.readAllLines
func joinLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSuffix(s, "\n")
}

// Pick returns the text for language, else en_us, else any; false for none.
// vanilla: ServerConfigurationPacketListenerImpl.addOptionalTasks
func (t Texts) Pick(language string) (string, bool) {
	if text, ok := t[strings.ToLower(language)]; ok {
		return text, true
	}
	if text, ok := t["en_us"]; ok {
		return text, true
	}
	// Vanilla takes its map's first entry; the smallest code keeps this stable.
	var first string
	for lang := range t {
		if first == "" || lang < first {
			first = lang
		}
	}
	if first == "" {
		return "", false
	}
	return t[first], true
}
