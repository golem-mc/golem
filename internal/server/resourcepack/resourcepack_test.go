package resourcepack

import (
	"strings"
	"testing"
)

const testSHA1 = "0123456789abcdef0123456789abcdef01234567"

func TestResolve(t *testing.T) {
	if p, _ := Resolve("", "", "", "", "", true); p != nil {
		t.Error("an empty url offers a pack")
	}
	if p, _ := Resolve("https://x", "not-a-uuid", "", "", "", false); p != nil {
		t.Error("an invalid id offers a pack")
	}
	p, warnings := Resolve("https://x", "", "zz", "", `{"text":"a"}`, true)
	if p == nil || !p.Required || p.Prompt == nil {
		t.Fatalf("pack = %+v", p)
	}
	if len(warnings) != 2 {
		t.Errorf("warnings = %q, want the invalid sha1 and the missing id", warnings)
	}
	p, warnings = Resolve("https://x", "11111111-2222-3333-4444-555555555555", "", "", "nope", false)
	if p == nil || p.Prompt != nil || len(warnings) != 2 {
		t.Errorf("pack = %+v warnings = %q, want no prompt and two warnings", p, warnings)
	}
	// An invalid colour drops the whole prompt; a click event drops only
	// itself, with a warning.
	p, warnings = Resolve("https://x", "11111111-2222-3333-4444-555555555555", testSHA1, "", `{"text":"a","color":"notacolor"}`, false)
	if p == nil || p.Prompt != nil || len(warnings) != 1 {
		t.Errorf("pack = %+v warnings = %q, want no prompt and one warning", p, warnings)
	}
	p, warnings = Resolve("https://x", "11111111-2222-3333-4444-555555555555", testSHA1, "", `{"text":"a","click_event":{}}`, false)
	if p == nil || p.Prompt == nil || len(warnings) != 1 {
		t.Errorf("pack = %+v warnings = %q, want a prompt and one warning", p, warnings)
	}
	long := strings.Repeat("a", 41)
	if p, _ := Resolve("https://x", "11111111-2222-3333-4444-555555555555", long, "", "", false); p.Hash != long || !p.HashTooLong() {
		t.Errorf("hash = %q, too long = %v, want the 41 characters kept and too long", p.Hash, p.HashTooLong())
	}
	if p, _ := Resolve("https://x", "11111111-2222-3333-4444-555555555555", long[:40], "", "", false); p.HashTooLong() {
		t.Error("a 40-character hash is too long")
	}
}

func TestActionTerminal(t *testing.T) {
	for a, want := range map[Action]bool{SuccessfullyLoaded: true, Declined: true, FailedDownload: true,
		Accepted: false, Downloaded: false, InvalidURL: true, FailedReload: true, Discarded: true} {
		if a.Terminal() != want || !a.Valid() {
			t.Errorf("action %d terminal=%v valid=%v", a, a.Terminal(), a.Valid())
		}
	}
	if Action(8).Valid() || Action(-1).Valid() {
		t.Error("out-of-range action is valid")
	}
}

// TestResolveLegacyHash checks resource-pack-hash stands in for an empty
// resource-pack-sha1 and is ignored beside one, each with its warning.
func TestResolveLegacyHash(t *testing.T) {
	p, warnings := Resolve("https://x", "", "", testSHA1, "", false)
	if p == nil || p.Hash != testSHA1 || len(warnings) < 1 || !strings.Contains(warnings[0], "resource-pack-hash is deprecated. Please use") {
		t.Errorf("legacy only = %+v %q", p, warnings)
	}
	other := strings.Repeat("a", 40)
	p, warnings = Resolve("https://x", "", other, testSHA1, "", false)
	if p == nil || p.Hash != other || len(warnings) < 1 || !strings.Contains(warnings[0], "will be ignored") {
		t.Errorf("both = %+v %q", p, warnings)
	}
}
