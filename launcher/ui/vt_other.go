//go:build !windows

package ui

import "os"

// enableVT answers true: terminals outside Windows take escape codes.
func enableVT(*os.File) bool { return true }

// EnableVTInput does nothing: terminals outside Windows send escape
// sequences for the arrow keys.
func EnableVTInput(*os.File) {}
