// Package levelgen holds exact ports of vanilla's world generation
// primitives: the random sources, the climate index, structure placement and
// the structure generators. Results must match the reference server bit for
// bit, so the arithmetic keeps Java's int and long wraparound and its double
// rounding.
//
// Go may fuse a multiply and an add into one FMA instruction, which rounds
// once where Java rounds twice. An explicit float64(...) conversion forbids
// that fusion (Go spec, "Floating-point operators"), so every product that
// feeds an addition here is wrapped in one.
package levelgen
