package server

import (
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// testEnvironments resolves test environment holders against the
// synchronized test_environment registry and the game rule table.
func testEnvironments() datapack.TestEnvironments {
	return datapack.TestEnvironments{Entry: testEnvironmentEntry, Rule: func(id string) (bool, int32, int32, bool) {
		kind, lo, hi, ok := world.RuleBounds(id)
		return kind == world.RuleBool, lo, hi, ok
	}}
}

// testEnvironmentEntry returns the registered environment id: a data pack's
// entry, or the built-in default, which is an empty all_of.
// vanilla: GameTestEnvironments.bootstrap
func testEnvironmentEntry(id string) (nbt.Tag, bool) {
	for _, reg := range registry.Current() {
		if reg.Name != "minecraft:test_environment" {
			continue
		}
		if tag, ok := reg.Data[id]; ok {
			return tag, true
		}
		if id == "minecraft:default" && reg.ID(id) >= 0 {
			return nbt.Compound{"type": nbt.String("minecraft:all_of"), "definitions": nbt.List{}}, true
		}
	}
	return nil, false
}

// testWeatherParams are each weather's clear time, rain time, raining and
// thundering. vanilla: TestEnvironmentDefinition.Weather.Type
var testWeatherParams = [...]struct {
	clear, rain         int32
	raining, thundering bool
}{{100000, 0, false, false}, {0, 100000, true, false}, {0, 100000, true, true}}

// setTestWeather applies one of testWeatherParams; the thunder timer takes
// the rain time. vanilla: MinecraftServer.setWeatherParameters
func (s *Server) setTestWeather(weather int) {
	p := testWeatherParams[weather]
	s.world.SetWeather(p.raining, p.thundering, p.rain, p.rain, p.clear)
}

// setUpTestEnvironment applies env to the test world w and returns what
// restores the state it replaced.
// vanilla: TestEnvironmentDefinition.setup, TestEnvironmentDefinition.Activation.teardown
func (s *Server) setUpTestEnvironment(w *world.World, env datapack.TestEnvironment) (teardown func()) {
	ws := s.gameTestScope(w)
	switch env.Type {
	case "minecraft:all_of":
		teardowns := make([]func(), 0, len(env.AllOf))
		for _, def := range env.AllOf {
			teardowns = append(teardowns, s.setUpTestEnvironment(w, def))
		}
		return func() {
			for _, td := range slices.Backward(teardowns) {
				td()
			}
		}
	case "minecraft:clock_time":
		_, previous, _ := s.world.ClockTime(env.Clock)
		ws.SetClockTime(env.Clock, env.Time)
		return func() { ws.SetClockTime(env.Clock, previous) }
	case "minecraft:difficulty":
		// vanilla: MinecraftServer.setDifficulty (ignoring the lock)
		previous := s.world.Difficulty()
		s.world.SetDifficulty(env.Difficulty)
		s.broadcastDifficulty()
		return func() {
			s.world.SetDifficulty(previous)
			s.broadcastDifficulty()
		}
	case "minecraft:function":
		s.runTestEnvironmentFunction(w, env.Setup)
		return func() { s.runTestEnvironmentFunction(w, env.Teardown) }
	case "minecraft:game_rules":
		previous := make([]datapack.TestRule, len(env.Rules))
		for i, r := range env.Rules {
			previous[i] = datapack.TestRule{ID: r.ID, Value: s.gameRuleValue(r.ID)}
		}
		s.setTestRules(ws, env.Rules)
		return func() { s.setTestRules(ws, previous) }
	case "minecraft:weather":
		previous := 0
		if w.IsThundering() {
			previous = 2
		} else if w.IsRaining() {
			previous = 1
		}
		s.setTestWeather(env.Weather)
		return func() {
			s.world.ResetWeatherCycle()
			s.setTestWeather(previous)
		}
	case "minecraft:timeline_attributes":
		return w.SetTestTimelines(env.Timelines)
	}
	slog.Warn("test environment type is not applied", "type", env.Type)
	return func() {}
}

// gameRuleValue reads a rule as a TestRule holds it.
func (s *Server) gameRuleValue(id string) int32 {
	if kind, _, _, _ := world.RuleBounds(id); kind == world.RuleInt {
		return s.world.IntRule(world.RuleIndex(id))
	}
	if s.world.BoolRule(world.RuleIndex(id)) {
		return 1
	}
	return 0
}

// setTestRules sets each rule and sends the packets its change owes.
// vanilla: GameRules.setAll, GameRules.set
func (s *Server) setTestRules(ws worldScope, rules []datapack.TestRule) {
	for _, r := range rules {
		if kind, _, _, _ := world.RuleBounds(r.ID); kind == world.RuleInt {
			ws.SetIntRule(world.RuleIndex(r.ID), r.Value)
		} else {
			ws.SetBoolRule(world.RuleIndex(r.ID), r.Value != 0)
		}
		ws.RuleChanged(r.ID)
	}
}

// runTestEnvironmentFunction runs a function environment's function id, if
// any, as the server at gamemaster level in the test's world.
// vanilla: TestEnvironmentDefinition.Functions.run
func (s *Server) runTestEnvironmentFunction(w *world.World, id string) {
	if id == "" {
		return
	}
	fn, ok := s.funcs.Load().lib.Get(id)
	if !ok {
		slog.Error("Test Batch failed for non-existent function " + id)
		return
	}
	src := s.gameLoopSource()
	src.in = func() *dimension { return s.dimensionOfWorld(w) }
	command.RunFunction(src, fn)
}
