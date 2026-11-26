package server

import (
	"github.com/golem-mc/golem/internal/server/advancement"
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The *client command.Source methods beyond its embedded worldScope
// (command_scope.go). Target methods are in command_targets.go.

func (c *client) Name() string { return c.player.Name }

// Debug and Info log on the connection's logger, which names the player.
func (c *client) Debug(msg string, args ...any) { c.log.Debug(msg, args...) }

func (c *client) Info(msg string, args ...any) { c.log.Info(msg, args...) }

// Feedback is vanilla's source.sendSuccess: a translated System Chat line,
// dropped with send_command_feedback off (acceptsSuccess). It goes via the
// mailbox like the command's effects, so it cannot overtake packets already
// queued, e.g. a teleport's.
func (c *client) Feedback(key string, with ...protocol.Text) error {
	return c.FeedbackText(protocol.Text{Translate: key, With: with})
}

func (c *client) FeedbackText(text protocol.Text) error {
	if !c.theWorld().BoolRule(world.RuleSendFeedback) {
		return nil
	}
	var b protocol.Buffer
	b.PutText(text)
	b.PutBool(false) // in the chat window, not over the hotbar
	c.send(protocol.IDPlaySystemChat, b.Bytes())
	return nil
}

// InformAdmins echoes the player's command under their name to other
// operators in game and the console log.
func (c *client) InformAdmins(message protocol.Text) { c.informAdminsAs(c.DisplayName(), message) }

// adminEchoer is a source that can echo its command to operators under
// another name, as execute as does with the executor's.
type adminEchoer interface {
	informAdminsAs(name, message protocol.Text)
}

func (c *client) informAdminsAs(name, message protocol.Text) {
	c.srv.informAdmins(c.player.UUID, true, name, message)
}

// informAdmins sends a successful command's gray italic "[Name: message]"
// to every other operator with send_command_feedback on, and logs it with
// log_admin_commands on. runner is the source's UUID, excluded; logAdmin is
// false for the console, whose commands vanilla does not log this way.
// vanilla: CommandSourceStack.broadcastToAdmins
func (s *Server) informAdmins(runner uuid.UUID, logAdmin bool, name, message protocol.Text) {
	echo := protocol.Text{
		Translate: "chat.type.admin",
		With:      []protocol.Text{name, message},
		Color:     protocol.ColorGray,
		Italic:    true,
	}
	send, _, logAdminRule := s.world.FeedbackRules()
	if send {
		for _, other := range s.playerClients() {
			// Read the op list, not the cached level: /op and /deop change another
			// connection's level, and only the list is written under a lock.
			if other.player.UUID == runner || s.ops.Level(other.player.UUID) == 0 {
				continue
			}
			other.Notify(echo)
		}
	}
	if !logAdmin || !logAdminRule {
		return
	}
	slog.Info(echo.Plain(), protocol.LogComponentKey, echo)
}

// Error is vanilla's source.sendFailure without parse context.
func (c *client) Error(key string) error {
	c.sendFailure(protocol.Text{Translate: key})
	return nil
}

// Message sends a finished System Chat line (as /help does), via the
// mailbox with the command's other output.
func (c *client) Message(text protocol.Text) error {
	var b protocol.Buffer
	b.PutText(text)
	b.PutBool(false) // in the chat window, not over the hotbar
	c.send(protocol.IDPlaySystemChat, b.Bytes())
	return nil
}

// CommandTree is the tree for the player's permission level, from which
// their Commands packet was built.
func (c *client) CommandTree() *command.Node {
	return c.srv.pluginCommandTree(c, c.PermLevel())
}

// SelfEntity is the client itself, what @s names.
func (c *client) SelfEntity() command.Entity { return c }

// DataTag is the player's saved compound, read by /data get and the nbt
// selector option: saveWithoutId with the held stack as SelectedItem. A
// joining player has none yet.
// vanilla: PlayerList.save, NbtPredicate.getEntityTagToCompare
func (c *client) DataTag() (nbt.CompoundTag, bool) {
	state, ok := c.savedTag()
	if !ok {
		return nil, false
	}
	c.addSelectedItem(state)
	return state, true
}

func (c *client) addSelectedItem(state *nbt.OrderedCompound) {
	if held := c.heldItem(protocol.HandMain); !held.Empty() {
		state.Set("SelectedItem", registry.StackTag(held))
	}
}

// savedTag is the player's saveWithoutId compound, without SelectedItem.
func (c *client) savedTag() (*nbt.OrderedCompound, bool) {
	if c.player == nil {
		return nil, false
	}
	return c.playerSaveState(c.savedVehicle(false), c.srv.savedPearlTags(c.player.UUID)), true
}

func (c *client) playerSaveState(vehicle nbt.Compound, pearls []nbt.Tag) *nbt.OrderedCompound {
	c.srv.wardenMu.Lock()
	tracker := c.wardenTracker
	c.srv.wardenMu.Unlock()
	extra := playerSaveExtras{warden: tracker.Tag()}
	if at, ok := c.srv.raidOmenPosition(c.player.UUID); ok {
		extra.raid = nbt.IntArray{int32(at.X), int32(at.Y), int32(at.Z)}
	}
	post := nbt.Compound{}
	c.storePostEffects(post)
	extra.post = post["post_effects"]
	if st := c.abilities.Load(); st != nil {
		extra.speeds = st
	}
	return orderedPlayerState(c.saved, c.player, c.flying.Load(), c.dimension().name, c.xpSeed.Load(), c.spawnPoint.Load(), vehicle, pearls, c.recipeBook.Load(), c.portalCooldown.Load(), extra)
}

// ChatArgument is empty outside a signed command invocation.
func (c *client) ChatArgument(string) *chatsecurity.Message { return nil }

// BroadcastSay and BroadcastMe relay a player's /say and /me: signed names
// them by profile, unsigned uses the system form. vanilla: SayCommand, MeCommand
func (c *client) BroadcastSay(message *chatsecurity.Message, resolved protocol.Text) {
	c.srv.broadcastCommandChat("minecraft:say_command", c.DisplayName(), message, resolved, c.enqueueChatRelay)
}

func (c *client) BroadcastMe(message *chatsecurity.Message, resolved protocol.Text) {
	c.srv.broadcastCommandChat("minecraft:emote_command", c.DisplayName(), message, resolved, c.enqueueChatRelay)
}

// OutgoingMessage echoes a sent direct message to the player under its
// attributed name. vanilla: MsgCommand, CommandSourceStack.sendChatMessage
func (c *client) OutgoingMessage(from protocol.Text, message *chatsecurity.Message, to protocol.Text, resolved protocol.Text) {
	c.commandChat("minecraft:msg_command_outgoing", from, message, &to, resolved)
}

// TeamMessage shows a /teammsg in the team chat type's outgoing or incoming
// form. vanilla: TeamMsgCommand.sendMessage
func (c *client) TeamMessage(outgoing bool, from protocol.Text, message *chatsecurity.Message, team protocol.Text, resolved protocol.Text) {
	chatType := "minecraft:team_msg_command_incoming"
	if outgoing {
		chatType = "minecraft:team_msg_command_outgoing"
	}
	c.commandChat(chatType, from, message, &team, resolved)
}

// ErrorWith is Error with the values its %s take.
// vanilla: CommandSourceStack.sendFailure
func (c *client) ErrorWith(key string, with ...protocol.Text) error {
	c.sendFailure(protocol.Text{Translate: key, With: with})
	return nil
}

// Advancements and FlushAdvancements are the player's progress and the
// send of its changes.
func (c *client) Advancements() *advancement.Progress { return c.advancements.Load() }

func (c *client) FlushAdvancements(show bool) {
	if progress := c.advancements.Load(); progress != nil {
		c.sendAdvancementUpdate(progress, show)
	}
}

// GiveExperiencePoints and GiveExperienceLevels add experience as an orb
// pickup does, with vanilla's level-up sound.
func (c *client) GiveExperiencePoints(points int) {
	points = c.pluginExpChange(points)
	c.giveExperience(func(v *player.Vitals) *rules.LevelUp { return rules.GiveExperiencePoints(v, points) })
}

func (c *client) GiveExperienceLevels(levels int) {
	c.giveExperience(func(v *player.Vitals) *rules.LevelUp { return rules.GiveExperienceLevels(v, levels) })
}

func (c *client) giveExperience(give func(*player.Vitals) *rules.LevelUp) {
	var levelUp *rules.LevelUp
	var was, now int
	c.player.UpdateVitals(func(v *player.Vitals) {
		was = v.ExperienceLevel
		levelUp = give(v)
		now = v.ExperienceLevel
	})
	defer c.pluginLevelChange(was, now)
	if err := c.sendExperience(); err != nil {
		c.log.Debug("could not send experience", "error", err)
	}
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, scoreMetadata(c.player))
	if levelUp != nil {
		pos := c.player.Position()
		c.srv.broadcastSoundID(c.theWorld(),
			registry.SoundEntityPlayerLevelup, protocol.SoundCategoryPlayer,
			pos.X, pos.Y, pos.Z,
			levelUp.Volume, levelUp.Pitch)

	}
}

// SetExperiencePoints sets points within the level, refusing the level's
// full requirement or more.
func (c *client) SetExperiencePoints(points int) bool {
	var accepted, changed bool
	c.player.UpdateVitals(func(v *player.Vitals) {
		accepted, changed = rules.SetExperiencePoints(v, points)
	})
	if changed {
		if err := c.sendExperience(); err != nil {
			c.log.Debug("could not send experience", "error", err)
		}
	}
	return accepted
}

func (c *client) SetExperienceLevels(levels int) bool {
	var changed bool
	var was, now int
	c.player.UpdateVitals(func(v *player.Vitals) {
		was = v.ExperienceLevel
		changed = rules.SetExperienceLevels(v, levels)
		now = v.ExperienceLevel
	})
	if changed {
		defer c.pluginLevelChange(was, now)
		if err := c.sendExperience(); err != nil {
			c.log.Debug("could not send experience", "error", err)
		}
	}
	return changed
}

// ExperiencePoints is the points into the current level.
func (c *client) ExperiencePoints() int {
	vitals := c.player.Vitals()
	return rules.ExperiencePointsWithinLevel(&vitals)
}

func (c *client) ExperienceLevels() int {
	return c.player.Vitals().ExperienceLevel
}

// Silent reports whether nothing reaches the source's output.
func (c *client) Silent() bool { return false }
