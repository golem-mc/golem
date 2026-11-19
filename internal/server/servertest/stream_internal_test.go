package servertest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// awaitNeverChild marks the child process TestAwaitPacketFailsOnAPacketThatNeverComes runs.
const awaitNeverChild = "SERVERTEST_AWAIT_NEVER_CHILD"

// TestAwaitPacketFailsOnAPacketThatNeverComes awaits a Respawn no one sends
// on a free loop, whose time syncs keep the connection busy: the wait has
// to fail at its deadline instead of hanging until go test's timeout. A
// failing test cannot be observed from inside, so it runs in a child.
func TestAwaitPacketFailsOnAPacketThatNeverComes(t *testing.T) {
	if os.Getenv(awaitNeverChild) != "" {
		cfg := config.Default()
		cfg.OnlineMode = false
		_, c := Join(t, cfg, "Testificate")
		c.streamTimeout = 3 * time.Second
		c.AwaitPacket(protocol.IDPlayRespawn)
		return
	}
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAwaitPacketFailsOnAPacketThatNeverComes$", "-test.timeout=45s")
	cmd.Env = append(os.Environ(), awaitNeverChild+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the child passed, want it failed:\n%s", out)
	}
	if !strings.Contains(string(out), "the awaited packet did not come within 3s") {
		t.Fatalf("the child did not fail at the Stream deadline:\n%s", out)
	}
}
