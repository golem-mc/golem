// Package resourcepack holds the server resource pack and the client's answers.
package resourcepack

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Pack is the resource pack the server offers every client.
type Pack struct {
	ID  uuid.UUID
	URL string
	// Hash is the SHA-1 as 40 hex characters, or empty for none; it is kept
	// as configured, so it may be too long to send (HashTooLong).
	Hash string
	// Required kicks a client that declines the pack.
	Required bool
	// Prompt is the accept-or-decline screen text as network NBT; nil for default.
	Prompt []byte
}

// maxHash is the longest hash the Add Resource Pack packet takes.
// vanilla: ClientboundResourcePackPushPacket.MAX_HASH_LENGTH
const maxHash = 40

var sha1Pattern = regexp.MustCompile(`(?i)^[a-f0-9]{40}$`)

// Resolve builds the pack from the resource-pack keys: nil when there is none,
// plus a warning for every key vanilla logs about.
// vanilla: DedicatedServerProperties.getServerPackInfo
func Resolve(url, id, sha1, legacyHash, prompt string, required bool) (*Pack, []string) {
	if url == "" {
		return nil, nil
	}
	var warnings []string
	if legacyHash != "" {
		if sha1 == "" {
			warnings = append(warnings, "resource-pack-hash is deprecated. Please use resource-pack-sha1 instead.")
			sha1 = legacyHash
		} else {
			warnings = append(warnings, "resource-pack-hash is deprecated and found along side resource-pack-sha1. resource-pack-hash will be ignored.")
		}
	}
	switch {
	case sha1 == "":
		warnings = append(warnings, "You specified a resource pack without providing a sha1 hash. "+
			"Pack will be updated on the client only if you change the name of the pack.")
	case !sha1Pattern.MatchString(sha1):
		warnings = append(warnings, "Invalid sha1 for resource-pack-sha1")
	}
	p := &Pack{URL: url, Hash: sha1, Required: required}
	if prompt != "" {
		p.Prompt, warnings = parsePrompt(prompt, warnings)
	}
	if id == "" {
		// Java's UUID.nameUUIDFromBytes: an MD5 tagged as version 3.
		u := uuid.UUID(md5.Sum([]byte(url)))
		u[6] = u[6]&0x0F | 0x30
		u[8] = u[8]&0x3F | 0x80
		p.ID = u
		warnings = append(warnings, fmt.Sprintf("resource-pack-id missing, using default of %s", u))
		return p, warnings
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("Failed to parse '%s' into UUID", id))
	}
	p.ID = u
	return p, warnings
}

// parsePrompt reads resource-pack-prompt into network NBT, or nil with a
// warning when it does not parse.
// vanilla: DedicatedServerProperties.parseResourcePackPrompt
func parsePrompt(prompt string, warnings []string) ([]byte, []string) {
	tag, dropped, err := protocol.ParseTextJSON(prompt)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("Failed to parse resource pack prompt '%s': %v", prompt, err))
	}
	if len(dropped) > 0 {
		verb := "is"
		if len(dropped) > 1 {
			verb = "are"
		}
		warnings = append(warnings, fmt.Sprintf("Resource pack prompt '%s': %s %s not sent",
			prompt, strings.Join(dropped, " and "), verb))
	}
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("Failed to parse resource pack prompt '%s': %v", prompt, err))
	}
	return data, warnings
}

// HashTooLong reports whether Hash is too long for the packet, which fails
// every client's configuration.
// vanilla: ClientboundResourcePackPushPacket (constructor)
func (p *Pack) HashTooLong() bool {
	return len(utf16.Encode([]rune(p.Hash))) > maxHash
}

// Put writes the Add Resource Pack body.
// vanilla: ClientboundResourcePackPushPacket.STREAM_CODEC
func (p *Pack) Put(b *protocol.Buffer) {
	b.PutUUID(p.ID)
	b.PutString(p.URL)
	b.PutString(p.Hash)
	b.PutBool(p.Required)
	b.PutBool(p.Prompt != nil)
	b.PutRaw(p.Prompt)
}

// Action is the state a client reports for a pack.
// vanilla: ServerboundResourcePackPacket.Action
type Action int32

const (
	SuccessfullyLoaded Action = iota
	Declined
	FailedDownload
	Accepted
	Downloaded
	InvalidURL
	FailedReload
	Discarded
	actionCount
)

// Valid reports whether a is one of the eight actions.
func (a Action) Valid() bool { return a >= 0 && a < actionCount }

// Terminal reports whether the client is done with the pack, either way.
func (a Action) Terminal() bool { return a != Accepted && a != Downloaded }
