// Command release signs a release's SHA256SUMS for the launcher
// (launcher/release.go) and makes the signing key:
//
//	go run ./tools/release sign SHA256SUMS SHA256SUMS.sig   # RELEASE_SIGNING_KEY signs
//	go run ./tools/release keygen | gh secret set RELEASE_SIGNING_KEY
//
// GoReleaser (.goreleaser.yaml) builds the binaries and runs sign. keygen
// writes the private key to standard output only, so it can be piped straight
// into the secret, and the public key, which goes in the launcher's
// releaseKey, to standard error.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "sign":
		err = sign(os.Args[2], os.Args[3])
	case len(os.Args) == 2 && os.Args[1] == "keygen":
		err = keygen()
	default:
		err = fmt.Errorf("usage: release sign FILE SIGNATURE | release keygen")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

// sign writes the base64 Ed25519 signature of file, made with the base64 seed
// in RELEASE_SIGNING_KEY, to sigFile.
func sign(file, sigFile string) error {
	seed := os.Getenv("RELEASE_SIGNING_KEY")
	if seed == "" {
		return fmt.Errorf("RELEASE_SIGNING_KEY is not set")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seed))
	if err != nil || len(raw) != ed25519.SeedSize {
		return fmt.Errorf("RELEASE_SIGNING_KEY is not a base64 Ed25519 seed")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(raw), data)
	return os.WriteFile(sigFile, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

func keygen() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(priv.Seed()))
	fmt.Fprintln(os.Stderr, "public key for launcher/release.go releaseKey:", base64.StdEncoding.EncodeToString(pub))
	return nil
}
