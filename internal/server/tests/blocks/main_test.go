package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(servertest.RunMain(m)) }
