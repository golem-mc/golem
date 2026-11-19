package codeofconduct

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndPick(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("DE_DE.txt", "a§cb\r\nc\n")
	write("fr_fr.txt", "f")
	write("notes.md", "ignored")
	texts, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || texts["de_de"] != "ab\nc" {
		t.Fatalf("texts = %q", texts)
	}
	for lang, want := range map[string]string{"de_DE": "ab\nc", "xx_xx": "ab\nc", "FR_FR": "f"} {
		if got, _ := texts.Pick(lang); got != want {
			t.Errorf("Pick(%s) = %q, want %q", lang, got, want)
		}
	}
	if _, ok := (Texts{}).Pick("en_us"); ok {
		t.Error("empty Texts picked a text")
	}
}

func TestLoadRefusals(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing folder was accepted")
	}
	dir, outside := t.TempDir(), filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "en_us.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("link out of the folder was accepted")
	}
}
