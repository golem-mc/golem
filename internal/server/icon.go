package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
)

// IconFile is the server icon path, relative to the working directory.
const IconFile = "server-icon.png"

// iconSize is the icon dimension the Minecraft client expects.
const iconSize = 64

// loadFavicon reads a 64x64 PNG as the status response's data URI. A
// missing file is no error and gives an empty favicon.
func loadFavicon(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", path, err)
	}
	if cfg.Width != iconSize || cfg.Height != iconSize {
		return "", fmt.Errorf("%s must be %dx%d, got %dx%d", path, iconSize, iconSize, cfg.Width, cfg.Height)
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}
