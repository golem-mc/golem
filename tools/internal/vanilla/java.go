package vanilla

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// quickJVM are the flags for a JVM that lives a few seconds: the C1 compiler
// alone and one GC thread, so that several of them can start side by side
// without fighting over the cores.
var quickJVM = []string{"-XX:TieredStopAtLevel=1", "-XX:+UseSerialGC", "-XX:-UsePerfData"}

// JavaOptions tune one run of a Java helper.
type JavaOptions struct {
	// Xmx is the heap limit, such as "1g"; empty leaves the JVM's default.
	Xmx string
	// Args follow the source file on the command line.
	Args []string
}

// Classpath is the server jar followed by the library jars, as absolute paths.
func (r Ref) Classpath() (string, error) {
	jar, err := filepath.Abs(r.ServerJar())
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(jar); err != nil {
		return "", fmt.Errorf("%s is missing; run `mise run reference:setup` and `mise run reference:run` once so the server unpacks it", r.ServerJar())
	}
	libs, err := filepath.Abs(r.Libraries())
	if err != nil {
		return "", err
	}
	var jars []string
	err = filepath.WalkDir(libs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jar") {
			jars = append(jars, p)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	slices.Sort(jars)
	return strings.Join(append([]string{jar}, jars...), string(os.PathListSeparator)), nil
}

// RunJava runs a single-file Java program against the server's classes and
// returns what it printed. The server's bootstrap writes a logs/ directory
// into wherever it runs, so the program runs in a throwaway one; its output
// reaches standard output wrapped by the server's logger, as
// "[time] [main/INFO]: [STDOUT]: <line>".
func (r Ref) RunJava(source string, o JavaOptions) (string, error) {
	if _, err := exec.LookPath("java"); err != nil {
		return "", errors.New("java is not on the PATH; the gen tasks provide it through mise (java = temurin-25)")
	}
	classpath, err := r.Classpath()
	if err != nil {
		return "", err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	cwd, err := os.MkdirTemp("", "gen-java-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cwd)
	args := slices.Clone(quickJVM)
	if o.Xmx != "" {
		args = append(args, "-Xmx"+o.Xmx)
	}
	args = append(args, "-cp", classpath, source)
	args = append(args, o.Args...)
	cmd := exec.Command("java", args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w\n%s%s", filepath.Base(source), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}
