package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// FeedSound is one sound played by a successful food interaction.
type FeedSound struct {
	Event         registry.Sound
	Volume, Pitch float32
}

// FeedSounds returns the sounds of entering love mode or growing from food.
// remaining is the held stack after consumption.
// vanilla: Animal.mobInteract, Animal.playEatingSound
func (m *Mob) FeedSounds(remaining protocol.ItemStack) []FeedSound {
	if m.Silent {
		return nil
	}
	switch m.Type {
	case TypeFox:
		// vanilla: Fox.playEatingSound
		return []FeedSound{{registry.SoundEntityFoxEat, 1, 1}}
	case TypeCat:
		// vanilla: Cat.playEatingSound
		return []FeedSound{{m.Sounds().Eat, 1, 1}}
	case TypeFrog:
		// vanilla: Frog.playEatingSound
		return []FeedSound{{registry.SoundEntityFrogEat, 2, 1}}
	case TypeGoat, TypeSniffer:
		// Goat checks food after consumption; Sniffer captures it beforehand.
		// vanilla: Goat.mobInteract, Sniffer.mobInteract
		event := m.Sounds().Eat
		sounds := []FeedSound{{event, 1, 0.8 + m.Rand.Float32()*0.4}}
		if m.Type == TypeSniffer || m.FedBy(remaining) {
			sounds = append(sounds, FeedSound{event, 1, 0.8 + m.Rand.Float32()*0.4})
		}
		return sounds
	case TypeStrider:
		// vanilla: Strider.mobInteract
		return []FeedSound{{registry.SoundEntityStriderEat, 1, 1 + (m.Rand.Float32()-m.Rand.Float32())*0.2}}
	case TypePig, TypeArmadillo, TypeNautilus, TypeZombieNautilus, TypeSulfurCube:
		// These overrides use makeSound's volume and voice pitch.
		// vanilla: Pig.playEatingSound, Armadillo.playEatingSound,
		// Nautilus.playEatingSound, ZombieNautilus.playEatingSound, SulfurCube.playEatingSound
		return []FeedSound{{m.Sounds().Eat, m.SoundVolume(), m.VoicePitch()}}
	default:
		return nil
	}
}
