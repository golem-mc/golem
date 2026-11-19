//go:build !race

package servertest

import (
	"time"
)

// ChunksSettledTimeout is how long AwaitChunksSettled waits for a join's
// background chunk loading to settle one more chunk.
const ChunksSettledTimeout = 30 * time.Second
