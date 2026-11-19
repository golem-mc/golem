package main

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
)

// NBT tag types, deliberately not shared with internal/protocol/text.go (nor is the
// string decoding): a mistake shared by both sides would parse our own
// packets consistently wrong and still report leftover=0.
const (
	tagEnd = iota
	tagI8
	tagI16
	tagI32
	tagI64
	tagF32
	tagF64
	tagI8Array
	tagString
	tagList
	tagCompound
	tagI32Array
	tagI64Array
)

// readComponent reads a text component, returning it rendered and as the raw
// NBT to compare with protocol.Buffer.PutText. data must be r's payload.
func readComponent(data []byte, r *protocol.Reader) (rendered string, raw []byte) {
	start := len(data) - r.Remaining()
	rendered = readNBT(r)
	if r.Err() != nil {
		return rendered, nil
	}
	return rendered, data[start : len(data)-r.Remaining()]
}

// readNBT reads a network NBT value, whose root tag carries no name, and
// renders it in a form close to the SNBT the game writes.
func readNBT(r *protocol.Reader) string {
	tag := r.ReadU8()
	if tag == tagEnd {
		return "<end>"
	}
	return readPayload(r, tag)
}

func readPayload(r *protocol.Reader, tag uint8) string {
	switch tag {
	case tagI8:
		return fmt.Sprintf("%db", r.ReadI8())
	case tagI16:
		return fmt.Sprintf("%ds", r.ReadI16())
	case tagI32:
		return fmt.Sprintf("%d", r.ReadI32())
	case tagI64:
		return fmt.Sprintf("%dL", r.ReadI64())
	case tagF32:
		return fmt.Sprintf("%gf", r.ReadF32())
	case tagF64:
		return fmt.Sprintf("%gd", r.ReadF64())
	case tagString:
		return fmt.Sprintf("%q", readNBTString(r))
	case tagI8Array, tagI32Array, tagI64Array:
		return readArray(r, tag)
	case tagList:
		return readList(r)
	case tagCompound:
		return readCompound(r)
	default:
		// An unknown tag makes the rest of the payload unreadable, which shows
		// up as leftover bytes on the line reporting the packet.
		return fmt.Sprintf("<tag %d?>", tag)
	}
}

func readArray(r *protocol.Reader, tag uint8) string {
	length := r.ReadI32()
	for range length {
		switch tag {
		case tagI8Array:
			r.ReadU8()
		case tagI32Array:
			r.ReadI32()
		case tagI64Array:
			r.ReadI64()
		}
	}
	return fmt.Sprintf("<%d values>", length)
}

func readList(r *protocol.Reader) string {
	element := r.ReadU8()
	length := r.ReadI32()
	items := make([]string, 0, max(length, 0))
	for range length {
		if r.Err() != nil {
			break
		}
		items = append(items, readPayload(r, element))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func readCompound(r *protocol.Reader) string {
	var fields []string
	for {
		tag := r.ReadU8()
		if tag == tagEnd || r.Err() != nil {
			break
		}
		name := readNBTString(r)
		fields = append(fields, name+":"+readPayload(r, tag))
	}
	return "{" + strings.Join(fields, ",") + "}"
}

// readNBTString reads Java's modified UTF-8, with runes outside the basic
// plane as two surrogates.
func readNBTString(r *protocol.Reader) string {
	length := int(r.ReadU16())
	raw := make([]byte, 0, length)
	for range length {
		raw = append(raw, r.ReadU8())
	}
	if r.Err() != nil {
		return ""
	}

	var units []uint16
	for i := 0; i < len(raw); {
		b := raw[i]
		switch {
		case b < 0x80:
			units = append(units, uint16(b))
			i++
		case b&0xE0 == 0xC0 && i+1 < len(raw):
			units = append(units, uint16(b&0x1F)<<6|uint16(raw[i+1]&0x3F))
			i += 2
		case b&0xF0 == 0xE0 && i+2 < len(raw):
			units = append(units, uint16(b&0x0F)<<12|uint16(raw[i+1]&0x3F)<<6|uint16(raw[i+2]&0x3F))
			i += 3
		default:
			return string(raw) // not modified UTF-8, show it as it came
		}
	}
	return string(utf16.Decode(units))
}
