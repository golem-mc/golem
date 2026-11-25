package entity

import (
	"math/rand"
	randv2 "math/rand/v2"
)

// diceStream is the second PCG stream word; any fixed odd constant works.
const diceStream = 0x9e3779b97f4a7c15

type pcgSource struct{ pcg randv2.PCG }

func (s *pcgSource) Seed(seed int64) { s.pcg.Seed(uint64(seed), diceStream) }
func (s *pcgSource) Uint64() uint64  { return s.pcg.Uint64() }
func (s *pcgSource) Int63() int64    { return int64(s.pcg.Uint64() >> 1) }

// NewDice returns a *rand.Rand that is deterministic per seed and cheap to
// seed (math/rand's source fills a 607-word table). Its sequence is not
// math/rand's.
func NewDice(seed int64) *rand.Rand {
	s := &pcgSource{}
	s.Seed(seed)
	return rand.New(s)
}
