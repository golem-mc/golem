package registry

import "testing"

func TestGameEventNamesRoundTrip(t *testing.T) {
	for id := 1; id < GameEventCount; id++ {
		event := GameEvent(id)
		if name := event.Name(); name == "" || GameEventByName(name) != event {
			t.Errorf("game event %d: name %q resolves to %d", id, name, GameEventByName(name))
		}
	}
	if GameEventByName("minecraft:unknown") != GameEventNone || GameEventByName("") != GameEventNone ||
		GameEventNone.Name() != "" || GameEvent(255).Name() != "" {
		t.Fatal("unregistered game event did not resolve to none")
	}
}
