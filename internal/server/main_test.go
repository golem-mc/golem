package server_test

import (
	"os"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMain scales -parallel with the machine.
func TestMain(m *testing.M) {
	os.Exit(servertest.RunMain(m))
}
