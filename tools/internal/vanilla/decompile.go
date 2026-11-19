package vanilla

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// CFR is the decompiler a few generators read the client's and the server's
// classes through, because what they need (the sounds, the light levels, the
// fuel times) is set in code that no report or data pack file carries.
const (
	CFRVersion = "0.152"
	CFRURL     = "https://github.com/leibnitz27/cfr/releases/download/0.152/cfr-0.152.jar"
	CFRSHA1    = "48ef4892cfe8feffddbbd0ff077735140557db74"
)

// CFRJar is where the decompiler is kept.
func (r Ref) CFRJar() string {
	return filepath.Join(r.Dir, "cfr-"+CFRVersion+".jar")
}

// ClientJar is the vanilla client jar: the one `mise run client:setup`
// fetches, or the one `mise run reference:setup` keeps beside the server's.
func (r Ref) ClientJar() (string, error) {
	for _, p := range []string{
		filepath.Join(r.Dir, "client", "client.jar"),
		filepath.Join(r.Dir, "minecraft_client."+r.Version+".jar"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no client jar in %s; run `mise run reference:setup`", r.Dir)
}

// Decompile returns CFR's source for one class of a jar, such as
// "net/minecraft/world/level/block/Blocks.class".
func (r Ref) Decompile(jar, class string) (string, error) {
	return r.decompile(jar, class, false)
}

// DecompileNested is Decompile with the class's nested and anonymous classes
// beside it: without them CFR prints enum constants that have a body as
// "Unavailable Anonymous Inner Class". It changes how other constructs print
// (switches over enums), so a caller opts in.
func (r Ref) DecompileNested(jar, class string) (string, error) {
	return r.decompile(jar, class, true)
}

func (r Ref) decompile(jar, class string, nested bool) (string, error) {
	cfr := r.CFRJar()
	if _, err := os.Stat(cfr); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s is missing; run `mise run reference:setup`", cfr)
	}
	if _, err := exec.LookPath("java"); err != nil {
		return "", errors.New("java is not on the PATH; the gen tasks provide it through mise (java = temurin-25)")
	}
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	entry, err := zr.Open(class)
	if err != nil {
		return "", fmt.Errorf("%s: %w", jar, err)
	}
	data, err := io.ReadAll(entry)
	entry.Close()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "gen-cfr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, filepath.Base(class))
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return "", err
	}
	prefix := strings.TrimSuffix(class, ".class") + "$"
	for _, zf := range zr.File {
		if !nested || !strings.HasPrefix(zf.Name, prefix) || !strings.HasSuffix(zf.Name, ".class") {
			continue
		}
		if err := copyZipEntry(zf, filepath.Join(dir, filepath.Base(zf.Name))); err != nil {
			return "", err
		}
	}
	cmd := exec.Command("java", append(slices.Clone(quickJVM), "-jar", cfr, file)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("decompiling %s failed: %w\n%s", class, err, stderr.String())
	}
	return stdout.String(), nil
}

func copyZipEntry(zf *zip.File, dst string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
