//go:build !windows

package ui

import "os"

// enableVT answers true: terminals outside Windows take escape codes.
func enableVT(*os.File) bool { return true }
