// Package terrain holds exact ports of 26.3's terrain engine: the float noise
// and density samplers, the routers that wire them, the noise chunk that
// interpolates them, the aquifer and the fill. Results must match the
// reference server bit for bit, so the arithmetic keeps Java's float
// promotions: a float times a double is a double, and a (float) cast rounds.
//
// Go may fuse a multiply and an add into one FMA instruction, which rounds
// once where Java rounds twice. An explicit float32(...) or float64(...)
// conversion forbids that fusion (Go spec, "Floating-point operators"), so
// every product that feeds an addition here is wrapped in one.
package terrain
