package chatsecurity

import "time"

// InvalidCommandSignature identifies mismatched argument names.
const InvalidCommandSignature Failure = "chat.disabled.invalid_command_signature"

// Argument is a parsed signable argument, sliced from the original command.
type Argument struct {
	Name, Value string
}

// ArgumentSignature is one wire entry; its order determines the chain index.
type ArgumentSignature struct {
	Name      string
	Signature Signature
}

// Arguments holds immutable verified messages scoped to one command invocation.
// Copies can be shared by execution forks without advancing the decoder again.
type Arguments struct {
	messages map[string]Message
}

// Message returns the verified argument, including its original link/signature.
func (a Arguments) Message(name string) (Message, bool) {
	message, ok := a.messages[name]
	return message, ok
}

// CollectArguments verifies entries in packet order against parsed names using
// the sender's shared decoder. On failure the command must not execute, even
// though earlier entries may have advanced the chain.
// vanilla: ServerGamePacketListenerImpl.collectSignedArguments, collectUnsignedArguments
func (d *Decoder) CollectArguments(parsed []Argument, entries []ArgumentSignature, timestamp, salt int64, lastSeen []Signature, now time.Time) (Arguments, error) {
	messages := make(map[string]Message, len(parsed))
	if len(entries) == 0 {
		for _, argument := range parsed {
			message, err := d.Decode(nil, NewBody(argument.Value, now.UnixMilli(), 0, nil), now)
			if err != nil {
				return Arguments{}, err
			}
			messages[argument.Name] = message
		}
		return Arguments{messages}, nil
	}
	for _, entry := range entries {
		var expected *Argument
		for i := range parsed {
			if parsed[i].Name == entry.Name {
				expected = &parsed[i]
				break
			}
		}
		if expected == nil {
			d.BreakChain()
			return Arguments{}, InvalidCommandSignature
		}
		message, err := d.Decode(&entry.Signature, NewBody(expected.Value, timestamp, salt, lastSeen), now)
		if err != nil {
			return Arguments{}, err
		}
		messages[expected.Name] = message
	}
	for _, argument := range parsed {
		if _, ok := messages[argument.Name]; !ok {
			// Missing names refuse this command but do not break the chain.
			return Arguments{}, InvalidCommandSignature
		}
	}
	return Arguments{messages}, nil
}
