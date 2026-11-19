//go:build race

package servertest

import "time"

// ChunksSettledTimeout is stretched under the race detector, which slows a
// seeded overworld's chunk generation and the loading it waits on several
// times over.
const ChunksSettledTimeout = 4 * time.Minute
