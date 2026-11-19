//go:build !unix

package world

import "math"

// usableSpace has no source outside Unix here, so the disk is never reported
// low.
func usableSpace(string) (uint64, error) { return math.MaxUint64, nil }
