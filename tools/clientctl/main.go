// Command clientctl drives a Minecraft client running the clientctl agent over HTTP
// (see tools/internal/clientctl for what the agent answers).
//
// Start the client with the agent, then send commands:
//
//	CLIENTCTL=1 mise run client:run 127.0.0.1:25565
//	go run ./tools/clientctl state
//	go run ./tools/clientctl chat "/time set noon"
//	go run ./tools/clientctl input -forward -ticks 20
//	go run ./tools/clientctl wait -kind death
//
// The agent's port is CLIENTCTL_PORT (default 25700); CLIENTCTL_HOST names another
// machine.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golem-mc/golem/tools/internal/clientctl"
)

func main() {
	os.Exit(run(os.Args[1:], clientctl.FromEnv(), os.Stdout, os.Stderr))
}

const usage = `usage: clientctl command [arguments]

commands:
  state                       print the full state snapshot
  chat TEXT                   send a chat line or /command
  input [-forward -back -left -right -jump -sneak -sprint] [-ticks N] [-yaw D] [-pitch D]
                              hold movement keys for a number of ticks
  use | attack                use or attack what the crosshair points at
  slot N                      select a hotbar slot
  drop [-all]                 drop the held stack
  click SLOT [-button N] [-mode pickup|quick_move|swap|throw]
  respawn | disconnect
  connect ADDRESS
  screenshot PATH
  wait (-world | -kind SCREEN) [-timeout SECONDS]`

func run(args []string, c *clientctl.Client, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	result, err := dispatch(args[0], args[1:], c, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "clientctl:", err)
		return 2
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(stdout, string(out))
	if !result.OK() {
		return 1
	}
	return 0
}

func dispatch(command string, args []string, c *clientctl.Client, stderr io.Writer) (clientctl.Result, error) {
	fl := flag.NewFlagSet(command, flag.ContinueOnError)
	fl.SetOutput(stderr)
	positional := func(n int, what string) ([]string, error) { return parseArgs(fl, args, n, command+" needs "+what) }
	switch command {
	case "state", "use", "attack", "respawn", "disconnect":
		if _, err := positional(0, ""); err != nil {
			return nil, err
		}
	}
	switch command {
	case "state":
		return c.State()
	case "use":
		return c.Use()
	case "attack":
		return c.Attack()
	case "respawn":
		return c.Respawn()
	case "disconnect":
		return c.Disconnect()
	case "chat":
		pos, err := positional(1, "the text")
		if err != nil {
			return nil, err
		}
		return c.Chat(pos[0])
	case "connect":
		pos, err := positional(1, "an address")
		if err != nil {
			return nil, err
		}
		return c.Connect(pos[0])
	case "screenshot":
		pos, err := positional(1, "a path")
		if err != nil {
			return nil, err
		}
		return c.Screenshot(pos[0])
	case "slot":
		pos, err := positional(1, "a slot number")
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(pos[0])
		if err != nil {
			return nil, fmt.Errorf("slot %q is not a number", pos[0])
		}
		return c.Slot(n)
	case "drop":
		all := fl.Bool("all", false, "the whole stack")
		if _, err := positional(0, ""); err != nil {
			return nil, err
		}
		return c.Drop(*all)
	case "click":
		button := fl.Int("button", 0, "mouse button")
		mode := fl.String("mode", "pickup", "pickup, quick_move, swap or throw")
		pos, err := positional(1, "a slot number")
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(pos[0])
		if err != nil {
			return nil, fmt.Errorf("slot %q is not a number", pos[0])
		}
		switch *mode {
		case "pickup", "quick_move", "swap", "throw":
		default:
			return nil, fmt.Errorf("mode %q is not pickup, quick_move, swap or throw", *mode)
		}
		return c.Click(n, *button, *mode)
	case "input":
		var k clientctl.Keys
		for name, p := range map[string]*bool{"forward": &k.Forward, "back": &k.Back, "left": &k.Left, "right": &k.Right,
			"jump": &k.Jump, "sneak": &k.Sneak, "sprint": &k.Sprint} {
			fl.BoolVar(p, name, false, "hold "+name)
		}
		fl.IntVar(&k.Ticks, "ticks", 20, "how long to hold, in ticks")
		yaw, pitch := fl.Float64("yaw", 0, "turn the view"), fl.Float64("pitch", 0, "turn the view")
		if _, err := positional(0, ""); err != nil {
			return nil, err
		}
		fl.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "yaw":
				k.Yaw = yaw
			case "pitch":
				k.Pitch = pitch
			}
		})
		return c.Input(k)
	case "wait":
		world := fl.Bool("world", false, "wait until in a world")
		kind := fl.String("kind", "", "the screen kind to wait for")
		seconds := fl.Float64("timeout", 60, "seconds")
		if _, err := positional(0, ""); err != nil {
			return nil, err
		}
		timeout := time.Duration(*seconds * float64(time.Second))
		switch {
		case *world:
			return c.WaitWorld(timeout)
		case *kind != "":
			return c.WaitScreen(*kind, timeout)
		}
		return nil, fmt.Errorf("wait needs -world or -kind")
	}
	return nil, fmt.Errorf("no command %q\n%s", command, usage)
}

// parseArgs reads the flags of fl and exactly n positional arguments, in any
// order (`click 5 -button 1` and `click -button 1 5` alike, as argparse did).
// A negative number is a positional (`click -999` clicks outside the window),
// and everything after -- is. missing is the error when there are fewer.
func parseArgs(fl *flag.FlagSet, args []string, n int, missing string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if _, err := strconv.ParseFloat(tok, 64); err == nil || !strings.HasPrefix(tok, "-") || tok == "-" {
			pos = append(pos, tok)
			continue
		}
		if tok == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		// A flag goes to the flag set with its value, which is the next
		// argument unless the flag is a boolean or has an =.
		take := 1
		if name, _, hasValue := strings.Cut(strings.TrimLeft(tok, "-"), "="); !hasValue && i+1 < len(args) {
			if f := fl.Lookup(name); f != nil && !isBoolFlag(f) {
				take = 2
			}
		}
		if err := fl.Parse(args[i : i+take]); err != nil {
			return nil, err
		}
		i += take - 1
	}
	if len(pos) < n {
		return nil, errors.New(missing)
	}
	if len(pos) > n {
		return nil, fmt.Errorf("unexpected argument %q", pos[n])
	}
	return pos, nil
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
