package main

import "github.com/golem-mc/golem/tools/internal/vanilla"

type soundHolders map[string]string

func (sounds soundHolders) holder(name string) string { return sounds.sound(name) + ".Holder()" }

func (sounds soundHolders) sound(name string) string {
	if sound, ok := sounds[qualified(name)]; ok {
		return sound
	}
	fail("unregistered component sound %q", name)
	return ""
}

// registeredSoundHolders keeps generated component defaults free of name lookups.
func registeredSoundHolders(registries vanilla.Registries) soundHolders {
	names, err := registries.NamesByID("minecraft:sound_event")
	if err != nil {
		fail("%v", err)
	}
	symbols, err := idents("Sound", names)
	if err != nil {
		fail("%v", err)
	}
	holders := make(soundHolders, len(names))
	for i, name := range names {
		holders[name] = symbols[i]
	}
	return holders
}
