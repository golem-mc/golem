package vanilla

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ManifestURL lists every Minecraft version and where its metadata is.
const ManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

// Download describes one file the version metadata links to.
type Download struct {
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

// Library is one jar of the client's classpath.
type Library struct {
	Downloads struct {
		Artifact *struct {
			Download
			Path string `json:"path"`
		} `json:"artifact"`
	} `json:"downloads"`
	Rules []Rule `json:"rules"`
}

// Rule allows or disallows a library on some platforms.
type Rule struct {
	Action   string                `json:"action"`
	OS       struct{ Name string } `json:"os"`
	Features map[string]bool       `json:"features"`
}

// Allowed evaluates a library's rules for Linux with no features.
func (l Library) Allowed() bool {
	if len(l.Rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range l.Rules {
		if r.OS.Name != "" && r.OS.Name != "linux" {
			continue
		}
		if len(r.Features) > 0 {
			continue
		}
		allowed = r.Action == "allow"
	}
	return allowed
}

// VersionMeta is the metadata of one version, as Mojang publishes it.
type VersionMeta struct {
	ID        string `json:"id"`
	MainClass string `json:"mainClass"`
	Downloads struct {
		Client Download `json:"client"`
		Server Download `json:"server"`
	} `json:"downloads"`
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Libraries  []Library `json:"libraries"`
	AssetIndex struct {
		ID string `json:"id"`
		Download
	} `json:"assetIndex"`
	// Raw is the metadata as downloaded, for keeping beside the files.
	Raw []byte `json:"-"`
}

// Fetcher downloads the reference files. Every file is checked against the
// SHA-1 the metadata gives for it.
type Fetcher struct {
	HTTP        *http.Client
	ManifestURL string
	// UserAgent is the one Mojang's CDN expects; a plain default is served 403.
	UserAgent string
	// Log receives progress lines.
	Log io.Writer
}

// NewFetcher is a Fetcher for the real servers.
func NewFetcher(log io.Writer) *Fetcher {
	return &Fetcher{
		HTTP:        &http.Client{Timeout: 10 * time.Minute},
		ManifestURL: ManifestURL,
		UserAgent:   "golem reference fetch",
		Log:         log,
	}
}

func (f *Fetcher) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func sum(data []byte) string {
	h := sha1.Sum(data)
	return hex.EncodeToString(h[:])
}

// VersionMeta resolves a version through the manifest rather than a hardcoded
// URL, so it keeps working when a file moves.
func (f *Fetcher) VersionMeta(version string) (*VersionMeta, error) {
	raw, err := f.get(f.ManifestURL)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Versions []struct {
			ID   string `json:"id"`
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("the version manifest: %w", err)
	}
	for _, v := range manifest.Versions {
		if v.ID != version {
			continue
		}
		data, err := f.get(v.URL)
		if err != nil {
			return nil, err
		}
		if v.SHA1 != "" && sum(data) != v.SHA1 {
			return nil, fmt.Errorf("sha1 mismatch for the metadata of %s", version)
		}
		meta := &VersionMeta{Raw: data}
		if err := json.Unmarshal(data, meta); err != nil {
			return nil, fmt.Errorf("the metadata of %s: %w", version, err)
		}
		return meta, nil
	}
	return nil, fmt.Errorf("no such Minecraft version: %s", version)
}

// Fetch downloads url to path unless a file with the right SHA-1 is already
// there. The file lands under a temporary name first, so an interrupted
// download is never mistaken for a finished one.
func (f *Fetcher) Fetch(url, sha, path string) error {
	if have, err := os.ReadFile(path); err == nil && sum(have) == sha {
		return nil
	}
	data, err := f.get(url)
	if err != nil {
		return err
	}
	if got := sum(data); got != sha {
		return fmt.Errorf("sha1 mismatch for %s: got %s, want %s", url, got, sha)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".part", data, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".part", path)
}

// Logf writes a progress line.
func (f *Fetcher) Logf(format string, args ...any) {
	if f.Log != nil {
		fmt.Fprintf(f.Log, format+"\n", args...)
	}
}
