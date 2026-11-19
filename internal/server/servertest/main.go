package servertest

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"testing"
)

// testsPerCPU is how many of a package's tests run at once per CPU when
// -parallel is not given. They spend most of their time waiting on game
// ticks, so Go's default of one per CPU leaves the machine mostly idle; at
// twelve a 4-CPU machine runs about two thirds busy and its tick loops keep
// time.
const testsPerCPU = 12

// maxParallel caps that on a large machine: the tests share one process and
// its heap, and twelve per CPU of a CI runner's CPUs failed timing-sensitive
// tests that held at 48.
const maxParallel = 48

// RunMain scales -parallel with the machine and runs the tests; every server
// test package's TestMain calls it. GOMAXPROCS follows a container's CPU
// limit where the host's CPU count would not.
func RunMain(m *testing.M) int {
	flag.Parse()
	given := false
	flag.Visit(func(f *flag.Flag) { given = given || f.Name == "test.parallel" })
	if !given {
		n := min(testsPerCPU*runtime.GOMAXPROCS(0), maxParallel)
		if err := flag.Set("test.parallel", strconv.Itoa(n)); err != nil {
			panic(err)
		}
	}
	var err error
	regionFixtureDir, err = os.MkdirTemp("", "servertest-regions-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(regionFixtureDir)
	return m.Run()
}
