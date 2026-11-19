package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// releaseKey is the base64 Ed25519 public key release SHA256SUMS files are
// signed with (tools/release keygen; the private key is the repository's
// RELEASE_SIGNING_KEY secret). Empty, no release is trusted and every version
// is built from source.
var releaseKey = "4+7jY2i57NaUEZLzai5fhiP7Jmhf8Rqqi96YPuLbKww="

// errNoRelease is a version with no signed release for this platform; the
// launcher builds it instead. A release that fails its check is an error,
// never a reason to fall back.
var errNoRelease = errors.New("no signed release")

// releaseAsset is the file name .goreleaser.yaml gives a binary.
func releaseAsset(name, version, goos, goarch string) string {
	s := fmt.Sprintf("%s_%s_%s_%s", name, version, goos, goarch)
	if goos == "windows" {
		s += ".exe"
	}
	return s
}

// release answers the cached release binary of version, downloading and
// checking it on first use.
func (l *launcher) release(ctx context.Context, version string) (string, error) {
	// A pseudo-version or a commit is never released.
	if releaseKey == "" || !semver.IsValid(version) || module.IsPseudoVersion(version) {
		return "", errNoRelease
	}
	asset := releaseAsset("golem", version, runtime.GOOS, runtime.GOARCH)
	bin := filepath.Join(l.cache, "releases", version, asset)
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	assets, err := l.releaseAssets(ctx, version)
	if err != nil {
		return "", err
	}
	if assets[asset] == "" || assets["SHA256SUMS"] == "" || assets["SHA256SUMS.sig"] == "" {
		return "", errNoRelease
	}
	sums, err := l.fetch(ctx, assets["SHA256SUMS"], true)
	if err != nil {
		return "", err
	}
	sig, err := l.fetch(ctx, assets["SHA256SUMS.sig"], true)
	if err != nil {
		return "", err
	}
	logger.Printf("downloading %s", asset)
	data, err := l.fetch(ctx, assets[asset], true)
	if err != nil {
		return "", err
	}
	if err := verifyRelease(releaseKey, sums, sig, asset, data); err != nil {
		return "", fmt.Errorf("release %s: %w", version, err)
	}
	return bin, writeExecutable(bin, data)
}

// verifyRelease checks sig over sums with the base64 public key key, then
// data's hash against the line sums gives name.
func verifyRelease(key string, sums, sig []byte, name string, data []byte) error {
	pub, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("the launcher's release key is malformed")
	}
	rawSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(pub, sums, rawSig) {
		return errors.New("SHA256SUMS is not signed with the release key")
	}
	sum := sha256.Sum256(data)
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		// sha256sum's form: the hex hash, two spaces, the name.
		h, file, ok := strings.Cut(sc.Text(), "  ")
		if ok && file == name {
			if h != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("%s does not match its checksum", name)
			}
			return nil
		}
	}
	return fmt.Errorf("SHA256SUMS does not list %s", name)
}

// releaseAssets answers the API URLs of a release's files by name.
func (l *launcher) releaseAssets(ctx context.Context, version string) (map[string]string, error) {
	repo := strings.TrimPrefix(serverModule, "github.com/")
	data, err := l.fetch(ctx, "https://api.github.com/repos/"+repo+"/releases/tags/"+version, false)
	if errors.Is(err, errNotFound) {
		return nil, errNoRelease
	}
	if err != nil {
		return nil, err
	}
	var rel struct {
		Assets []struct{ Name, URL string }
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("read release %s: %w", version, err)
	}
	assets := map[string]string{}
	for _, a := range rel.Assets {
		assets[a.Name] = a.URL
	}
	return assets, nil
}

var errNotFound = errors.New("not found")

// fetch GETs url; binary asks the GitHub API for an asset's bytes rather
// than its description. GITHUB_TOKEN, when set, goes only to GitHub's API:
// the asset download it redirects to drops the header.
func (l *launcher) fetch(ctx context.Context, url string, binary bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		if binary {
			req.Header.Set("Accept", "application/octet-stream")
		}
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("%s: %w", url, errNotFound)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// writeExecutable writes data to path through a temporary file, so a
// launcher stopped halfway leaves no partial binary behind.
func writeExecutable(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// latestRelease answers the tag of the newest published release.
func (l *launcher) latestRelease(ctx context.Context) (string, error) {
	repo := strings.TrimPrefix(serverModule, "github.com/")
	data, err := l.fetch(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", false)
	if errors.Is(err, errNotFound) {
		return "", errors.New("there is no release yet")
	}
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", fmt.Errorf("read the latest release: %w", err)
	}
	return rel.TagName, nil
}
