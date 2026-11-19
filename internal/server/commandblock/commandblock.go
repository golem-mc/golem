// Package commandblock is vanilla's BaseCommandBlock: the command, its last
// result and output, the saved form and the gate and bookkeeping around one
// run. A command block and a command block minecart each hold a State; the
// caller owns the locking and the command's execution.
package commandblock

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// State is a command block's BaseCommandBlock fields.
type State struct {
	Command      string
	SuccessCount int
	TrackOutput  bool
	// LastOutput is kept only while TrackOutput is set.
	LastOutput          *protocol.Text
	UpdateLastExecution bool
	// LastExecution is the game time of the last run, -1 for none.
	LastExecution int64
}

// New is a fresh state: output tracked, the same-tick gate on, no run yet.
func New() State {
	return State{TrackOutput: true, UpdateLastExecution: true, LastExecution: -1}
}

// SetCommand replaces the command and clears the success count.
// vanilla: BaseCommandBlock.setCommand
func (s *State) SetCommand(line string) {
	s.Command = line
	s.SuccessCount = 0
}

// Program is the edit screen's Done: the command, and whether output is
// tracked, which drops the output when it is not.
func (s *State) Program(line string, trackOutput bool) {
	s.SetCommand(line)
	s.TrackOutput = trackOutput
	if !trackOutput {
		s.LastOutput = nil
	}
}

// Step is what Begin decided about a run.
type Step int

const (
	// Gated: the state already ran this game tick; nothing changed.
	Gated Step = iota
	// Answered: the Searge easter egg set the result itself; no End follows.
	Answered
	// Idle: nothing to run (no command, or command blocks are off); End
	// follows with ran false.
	Idle
	// Run: the caller runs Command and then calls End with its result.
	Run
)

// Begin opens one run at gameTime. commandsWork is the command_blocks_work
// rule.
// vanilla: BaseCommandBlock.performCommand
func (s *State) Begin(gameTime int64, commandsWork bool) Step {
	if s.UpdateLastExecution && s.LastExecution == gameTime {
		return Gated
	}
	if strings.EqualFold(s.Command, "Searge") {
		out := protocol.Text{Text: "#itzlipofutzli"}
		s.LastOutput = &out
		s.SuccessCount = 1
		return Answered
	}
	s.SuccessCount = 0
	if s.Command != "" && commandsWork {
		// The output is cleared only when the command runs, so a disabled or
		// empty state keeps its line.
		s.LastOutput = nil
		return Run
	}
	return Idle
}

// End closes a run Begin opened. After a Run step, success and output are
// the command's result.
func (s *State) End(gameTime int64, step Step, success int, output *protocol.Text) {
	if step == Run {
		s.SuccessCount = success
		s.LastOutput = output
	}
	if s.UpdateLastExecution {
		s.LastExecution = gameTime
	} else {
		s.LastExecution = -1
	}
}

// Save writes the fields under vanilla's names; LastOutput only while
// tracked and LastExecution only while the gate is on and it is not stale.
// vanilla: BaseCommandBlock.save
func (s *State) Save(tag nbt.CompoundTag) {
	tag.Set("Command", nbt.String(s.Command))
	tag.Set("SuccessCount", nbt.Int(int32(s.SuccessCount)))
	tag.Set("TrackOutput", flag(s.TrackOutput))
	if s.TrackOutput && s.LastOutput != nil {
		tag.Set("LastOutput", protocol.TextTag(*s.LastOutput))
	}
	tag.Set("UpdateLastExecution", flag(s.UpdateLastExecution))
	if s.UpdateLastExecution && s.LastExecution != -1 {
		tag.Set("LastExecution", nbt.Long(s.LastExecution))
	}
}

// Load reads the fields Save writes; a load with a flag off takes the fresh
// value of what the flag guards.
// vanilla: BaseCommandBlock.load
func (s *State) Load(tag nbt.Compound) {
	s.Command = ""
	if v, ok := nbt.Get[nbt.String](tag, "Command"); ok {
		s.Command = string(v)
	}
	count, _ := nbt.IntField(tag, "SuccessCount")
	s.SuccessCount = int(count)
	s.TrackOutput = true
	if _, ok := tag["TrackOutput"]; ok {
		s.TrackOutput = nbt.BoolField(tag, "TrackOutput")
	}
	s.LastOutput = nil
	if text, ok := protocol.StyledTextOf(tag["LastOutput"]); ok && s.TrackOutput {
		s.LastOutput = &text
	}
	s.UpdateLastExecution = true
	if _, ok := tag["UpdateLastExecution"]; ok {
		s.UpdateLastExecution = nbt.BoolField(tag, "UpdateLastExecution")
	}
	s.LastExecution = -1
	if v, ok := nbt.LongValue(tag["LastExecution"]); ok && s.UpdateLastExecution {
		s.LastExecution = v
	}
}

func flag(v bool) nbt.Tag {
	if v {
		return nbt.Byte(1)
	}
	return nbt.Byte(0)
}
