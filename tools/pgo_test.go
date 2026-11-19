package tools_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPGORecordRejectsInvalidInput(t *testing.T) {
	script, err := os.ReadFile("pgo.sh")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile("../default.pgo")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"missing", "malformed"} {
		for _, invalidFirst := range []bool{false, true} {
			name := invalid + "_last"
			if invalidFirst {
				name = invalid + "_first"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.Mkdir(filepath.Join(dir, "tools"), 0o755); err != nil {
					t.Fatal(err)
				}
				previous := []byte("previous build profile\n")
				files := map[string][]byte{
					"tools/pgo.sh":        script,
					"valid profile.pprof": profile,
					"malformed":           []byte("not a profile\n"),
					"default.pgo":         previous,
				}
				for name, data := range files {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"valid profile.pprof", invalid}
				if invalidFirst {
					args[0], args[1] = args[1], args[0]
				}
				cmd := exec.Command("bash", append([]string{filepath.Join(dir, "tools/pgo.sh")}, args...)...)
				if out, err := cmd.CombinedOutput(); err == nil {
					t.Errorf("mixed valid/invalid profiles succeeded:\n%s", out)
				}
				got, err := os.ReadFile(filepath.Join(dir, "default.pgo"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, previous) {
					t.Error("failed refresh replaced the previous profile")
				}
			})
		}
	}
}
