package redstone

import (
	"os"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMain(m *testing.M) {
	os.Exit(servertest.RunMain(m))
}
