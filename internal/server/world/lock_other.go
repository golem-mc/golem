//go:build !unix

package world

import "os"

// lockSession has no source outside Unix here, so the session lock is not
// enforced on these platforms.
func lockSession(*os.File) error { return nil }

func unlockSession(*os.File) error { return nil }
