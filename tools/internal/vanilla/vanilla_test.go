package vanilla

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestDefaultVersionIsTheServers(t *testing.T) {
	if DefaultVersion != protocol.GameVersion {
		t.Errorf("DefaultVersion = %s, protocol.GameVersion = %s", DefaultVersion, protocol.GameVersion)
	}
}

func TestVersionFromEnvPrefersReferenceVersion(t *testing.T) {
	t.Setenv("REFERENCE_VERSION", "")
	t.Setenv("VERSION", "9.9")
	if got := VersionFromEnv(); got != DefaultVersion {
		t.Errorf("a stray $VERSION gave %q, want %q", got, DefaultVersion)
	}
	t.Setenv("REFERENCE_VERSION", "27.0")
	if got := VersionFromEnv(); got != "27.0" {
		t.Errorf("VersionFromEnv = %q, want 27.0", got)
	}
}

func TestOrderedKeepsFileOrder(t *testing.T) {
	var o Ordered[int]
	if err := json.Unmarshal([]byte(`{"b": 1, "a": 2, "c": 3, "b": 4}`), &o); err != nil {
		t.Fatal(err)
	}
	if got := o.Keys(); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Errorf("keys = %v, want [b a c]", got)
	}
	if v, _ := o.Get("b"); v != 4 {
		t.Errorf("a repeated key keeps its last value, got %d", v)
	}
	var seen []string
	for k := range o.All() {
		seen = append(seen, k)
	}
	if !slices.Equal(seen, o.Keys()) {
		t.Errorf("All = %v, Keys = %v", seen, o.Keys())
	}
}

func TestOrderedNests(t *testing.T) {
	var o Ordered[Ordered[int]]
	if err := json.Unmarshal([]byte(`{"y": {"q": 1, "p": 2}, "x": {"z": 3}}`), &o); err != nil {
		t.Fatal(err)
	}
	y, _ := o.Get("y")
	if !slices.Equal(o.Keys(), []string{"y", "x"}) || !slices.Equal(y.Keys(), []string{"q", "p"}) {
		t.Errorf("order lost: %v %v", o.Keys(), y.Keys())
	}
}

func TestOrderedRejectsNonObjects(t *testing.T) {
	var o Ordered[int]
	if err := json.Unmarshal([]byte(`[1]`), &o); err == nil {
		t.Error("an array was accepted")
	}
	if err := json.Unmarshal([]byte(`null`), &o); err != nil || o.Len() != 0 {
		t.Errorf("null: %v, %d keys", err, o.Len())
	}
}

func writeReports(t *testing.T, name, body string) Ref {
	t.Helper()
	dir := t.TempDir()
	ref := Ref{Dir: dir, Version: "26.3"}
	if err := os.MkdirAll(filepath.Dir(ref.ReportPath(name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.ReportPath(name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestNamesByIDSortsByProtocolID(t *testing.T) {
	ref := writeReports(t, "registries.json", `{"minecraft:item": {"entries": {
		"minecraft:c": {"protocol_id": 2}, "minecraft:a": {"protocol_id": 0}, "minecraft:b": {"protocol_id": 1}}}}`)
	rs, err := ref.Registries()
	if err != nil {
		t.Fatal(err)
	}
	got, err := rs.NamesByID("minecraft:item")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"minecraft:a", "minecraft:b", "minecraft:c"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNamesByIDRefusesAGap(t *testing.T) {
	ref := writeReports(t, "registries.json", `{"minecraft:item": {"entries": {
		"minecraft:a": {"protocol_id": 0}, "minecraft:c": {"protocol_id": 2}}}}`)
	rs, _ := ref.Registries()
	_, err := rs.NamesByID("minecraft:item")
	if err == nil || !strings.Contains(err.Error(), "minecraft:c is 2, expected 1") {
		t.Errorf("err = %v", err)
	}
	if _, err := rs.NamesByID("minecraft:nothing"); err == nil {
		t.Error("a missing registry was accepted")
	}
}

func TestMissingReportNamesTheTask(t *testing.T) {
	_, err := Ref{Dir: t.TempDir()}.Registries()
	if err == nil || !strings.Contains(err.Error(), "mise run reference:reports") {
		t.Errorf("err = %v", err)
	}
}

// fakeJava puts a java executable on the PATH that records its arguments and
// working directory and prints some lines.
func fakeJava(t *testing.T, stdout string, exit int) (record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	script := "#!/bin/sh\n{ pwd; for a in \"$@\"; do echo \"$a\"; done; } > " + record + "\n" +
		"printf '%s' '" + stdout + "'\nexit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func referenceWithJars(t *testing.T) Ref {
	t.Helper()
	ref := Ref{Dir: t.TempDir(), Version: "26.3"}
	for _, p := range []string{ref.ServerJar(), filepath.Join(ref.Libraries(), "b", "two.jar"), filepath.Join(ref.Libraries(), "a", "one.jar"), filepath.Join(ref.Libraries(), "a", "notes.txt")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ref
}

func TestRunJavaPutsTheServerOnTheClasspathAndRunsInAThrowawayDirectory(t *testing.T) {
	ref := referenceWithJars(t)
	record := fakeJava(t, "[12:00:00] [main/INFO]: [STDOUT]: minecraft:stone 1.5\n", 0)
	source := filepath.Join(t.TempDir(), "Thing.java")
	out, err := ref.RunJava(source, JavaOptions{Xmx: "1g", Args: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "minecraft:stone 1.5") {
		t.Errorf("stdout = %q", out)
	}
	rec, _ := os.ReadFile(record)
	lines := strings.Split(strings.TrimSpace(string(rec)), "\n")
	// The working directory, the quick-JVM flags, then the caller's options.
	n := len(quickJVM)
	if len(lines) != 6+n || !slices.Equal(lines[1:1+n], quickJVM) || lines[1+n] != "-Xmx1g" || lines[2+n] != "-cp" || lines[4+n] != source || lines[5+n] != "extra" {
		t.Fatalf("arguments = %q", lines)
	}
	jars := strings.Split(lines[3+n], string(os.PathListSeparator))
	if len(jars) != 3 || !strings.HasSuffix(jars[0], "server-26.3.jar") || !strings.HasSuffix(jars[1], "one.jar") || !strings.HasSuffix(jars[2], "two.jar") {
		t.Errorf("classpath = %q", jars)
	}
	if _, err := os.Stat(lines[0]); err == nil {
		t.Errorf("the working directory %s outlived the run", lines[0])
	}
	if strings.Contains(lines[0], ref.Dir) {
		t.Errorf("ran inside the reference directory: %s", lines[0])
	}
}

func TestRunJavaReportsAFailureWithItsStderr(t *testing.T) {
	ref := referenceWithJars(t)
	fakeJava(t, "", 3)
	if _, err := ref.RunJava("Thing.java", JavaOptions{}); err == nil || !strings.Contains(err.Error(), "Thing.java failed") {
		t.Errorf("err = %v", err)
	}
}

func TestRunJavaNamesTheMissingServerJar(t *testing.T) {
	fakeJava(t, "", 0)
	_, err := Ref{Dir: t.TempDir(), Version: "26.3"}.RunJava("Thing.java", JavaOptions{})
	if err == nil || !strings.Contains(err.Error(), "reference:run") {
		t.Errorf("err = %v", err)
	}
}

func TestFilesListsWholePathsInLexicalOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.json", "a/b.json", "a/c.txt", "b.json"} {
		p := filepath.Join(dir, "d", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Files(os.DirFS(dir), "d", ".json")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.json", "a/b.json", "b.json"}; !slices.Equal(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
}
