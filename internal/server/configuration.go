package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/resourcepack"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// configure runs the configuration phase: the client is told the server's
// data packs and, once it reports which it knows, gets the registries and
// tags it needs.
//
// It runs the play phase's keep-alive exchange (vanilla's
// ServerConfigurationPacketListenerImpl is ticked by keepConnectionAlive
// too), so a lingering client is pinged and dropped for not answering
// rather than held only by a fixed deadline.
func (c *client) configure() error {
	c.conn.State = protocol.StateConfiguration
	if c.srv.plugins != nil {
		c.setPluginConnectionPhase(protocol.StateConfiguration, true)
	}
	c.keepAlive = &keepAliveTicker{}
	c.keepAlive.start(c.srv.keepAliveClock())

	if err := c.sendBrand(); err != nil {
		return err
	}
	if c.srv.plugins != nil {
		if c.srv.plugins.Has(pluginruntime.PlayerConfigure) {
			if !c.srv.plugins.Call(func() { c.srv.plugins.Fire(&plugin.PlayerConfigureEvent{Player: c.connectionPlayer()}) }) {
				return net.ErrClosed
			}
		}
	}
	if err := c.sendServerLinks(); err != nil {
		return err
	}
	if err := c.sendKnownPacks(); err != nil {
		return err
	}

	stopKeepAlives := c.startConfigKeepAlives()
	defer stopKeepAlives()

	var sentRegistries bool
	for {
		if err := c.keepAlive.armReadDeadline(c.conn.SetReadDeadline, c.srv.keepAliveClock(),
			time.Duration(c.srv.keepAliveTimeout.Load())); err != nil {
			return err
		}
		packetID, data, err := c.conn.ReadPacket()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				// Only the outstanding ping's timeout is armed, so the deadline firing means
				// no answer came.
				reason := fmt.Errorf("client did not answer keep-alive within %s",
					time.Duration(c.srv.keepAliveTimeout.Load()))
				c.keepAlive.setFailure(reason)
				if err := c.disconnect(reason, protocol.Text{Translate: "disconnect.timeout"}); err != nil {
					return err
				}
				// Give a cancelled timeout another answer window rather than
				// repeatedly reading against the already expired deadline.
				c.keepAlive.retryTimeout(c.srv.keepAliveClock())
				continue
			}
			if reason := c.keepAlive.failure(); reason != nil {
				return reason
			}
			return fmt.Errorf("read configuration packet: %w", c.readFailure(err))
		}

		finished, err := c.handleConfigurationPacket(packetID, data, &sentRegistries)
		if err != nil || finished {
			return err
		}
	}
}

// handleConfigurationPacket keeps a cancelled disconnect in configuration;
// only a validated finish acknowledgement advances the connection to play.
func (c *client) handleConfigurationPacket(packetID int32, data []byte, sentRegistries *bool) (bool, error) {
	r := protocol.NewReader(data)
	switch packetID {
	case protocol.IDKnownPacksIn:
		if *sentRegistries {
			// The registry task is never cleared, so vanilla runs the second reply and
			// fails finishCurrentTask against the current task: the pack while pending,
			// join_world once the finish is out.
			// vanilla: ServerConfigurationPacketListenerImpl.finishCurrentTask
			current := "join_world"
			if c.configurationPackPending() {
				current = "server_resource_pack"
			}
			return false, c.disconnect(fmt.Errorf("known packs answered twice"), protocol.Text{
				Translate: "disconnect.genericReason",
				With: []protocol.Text{{Text: "Internal Exception: java.lang.IllegalStateException: " +
					"Unexpected request for task finish, current task: " + current +
					", requested: synchronize_registries"}},
			})
		}
		*sentRegistries = true
		if err := c.handleKnownPacks(r); err != nil {
			return false, err
		}

	case protocol.IDClientInformation:
		if err := c.handleClientInformation(r); err != nil {
			return false, err
		}

	case protocol.IDConfigFinishAck:
		if !*sentRegistries {
			return false, fmt.Errorf("client finished configuration before receiving it")
		}
		if c.conductPending {
			// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
			return false, c.unexpectedTask("server_code_of_conduct", "join_world")
		}
		if c.configurationPackPending() {
			// Vanilla's finishCurrentTask throws while the pack task is current, so the
			// client never joins.
			// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
			return false, fmt.Errorf("client finished configuration before answering the resource pack")
		}
		if r.Err() == nil && r.Remaining() != 0 {
			return false, c.configExtraCheck(packetID, r)
		}
		if c.srv.plugins != nil {
			c.setPluginConnectionPhase(protocol.StateHandshaking, false)
		}
		return true, nil

	case protocol.IDConfigPluginMessageIn:
		if err := c.logPluginMessage(r); err != nil {
			return false, err
		}

	case protocol.IDConfigKeepAliveIn:
		if err := c.handleKeepAliveAnswer(c.keepAlive, r); err != nil {
			return false, err
		}

	case protocol.IDConfigResourcePackIn:
		if err := c.handleResourcePackResponse(r); err != nil {
			return false, err
		}

	case protocol.IDConfigAcceptCodeOfConduct:
		if !c.conductPending {
			current := "join_world"
			switch {
			case !*sentRegistries:
				current = "synchronize_registries"
			case c.configurationPackPending():
				current = "server_resource_pack"
			}
			return false, c.unexpectedTask(current, "server_code_of_conduct")
		}
		c.conductPending = false
		if err := c.offerResourcePack(); err != nil {
			return false, err
		}

	case protocol.IDConfigPong:
		// The answered Ping's id; none is sent, but the body is read so a short one
		// still fails the packet.
		r.ReadI32()

	case protocol.IDConfigCustomClickAction:
		if err := c.handleCustomClick(r); err != nil {
			return false, err
		}
	case protocol.IDConfigCookieResponse:
		if err := c.handlePluginCookie(r); err != nil {
			return false, err
		}

	default:
		if packetID < 0 || packetID > maxServerboundConfigID {
			return false, c.disconnect(
				fmt.Errorf("configuration packet id 0x%02X is not a serverbound configuration packet", packetID),
				badPacketID(packetID))
		}
		c.log.Debug("ignoring configuration packet", "id", packetID)
		r.Rest()
	}
	return false, c.configExtraCheck(packetID, r)
}

// maxServerboundConfigID is the last id in the serverbound configuration
// range.
const maxServerboundConfigID = protocol.IDConfigAcceptCodeOfConduct

// configExtraCheck disconnects a client whose configuration packet had
// bytes past its fields. vanilla: PacketDecoder.decode
func (c *client) configExtraCheck(id int32, r *protocol.Reader) error {
	if r.Err() != nil || r.Remaining() == 0 {
		return nil
	}
	p := configPackets[id]
	return c.disconnect(
		fmt.Errorf("configuration packet 0x%02X carried %d bytes past its fields", id, r.Remaining()),
		extraBytes("configuration", p.name, p.class, r.Remaining()))
}

// configPackets maps handled serverbound configuration ids to resource name
// and vanilla simple class name, for decode messages.
var configPackets = map[int32]struct{ name, class string }{
	protocol.IDClientInformation:         {"client_information", "ServerboundClientInformationPacket"},
	protocol.IDConfigPluginMessageIn:     {"custom_payload", "ServerboundCustomPayloadPacket"},
	protocol.IDConfigFinishAck:           {"finish_configuration", "ServerboundFinishConfigurationPacket"},
	protocol.IDConfigKeepAliveIn:         {"keep_alive", "ServerboundKeepAlivePacket"},
	protocol.IDConfigAcceptCodeOfConduct: {"accept_code_of_conduct", "ServerboundAcceptCodeOfConductPacket"},
	protocol.IDConfigPong:                {"pong", "ServerboundPongPacket"},
	protocol.IDConfigResourcePackIn:      {"resource_pack", "ServerboundResourcePackPacket"},
	protocol.IDKnownPacksIn:              {"select_known_packs", "ServerboundSelectKnownPacksPacket"},
}

// brand is the server name the client shows on its debug screen and in
// crash reports.
const brand = "golem"

// sendBrand tells the client which server it is talking to.
func (c *client) sendBrand() error {
	return c.write(protocol.IDConfigPluginMessage, "brand", func(b *protocol.Buffer) {
		b.PutString("minecraft:brand")
		b.PutString(brand)
	})
}

// sendServerLinks sends the pause menu's links; nothing goes with
// bug-report-link unset.
// vanilla: ServerConfigurationPacketListenerImpl.startConfiguration
func (c *client) sendServerLinks() error {
	if c.srv.Config.BugReportLink == "" {
		return nil
	}
	return c.write(protocol.IDConfigServerLinks, "server links", func(b *protocol.Buffer) {
		b.PutVarInt(1)
		// Either label: true picks the built-in kind over a custom text component.
		b.PutBool(true)
		b.PutVarInt(protocol.ServerLinkBugReport)
		b.PutString(c.srv.Config.BugReportLink)
	})
}

// corePack is the vanilla data pack, holding the definitions behind the
// registry entries sent.
var corePack = struct{ Namespace, ID, Version string }{"minecraft", "core", registry.Version}

// sendKnownPacks tells the client which data packs the server has.
func (c *client) sendKnownPacks() error {
	return c.write(protocol.IDKnownPacks, "known packs", func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString(corePack.Namespace)
		b.PutString(corePack.ID)
		b.PutString(corePack.Version)
	})
}

// maxKnownPacks is the most packs a Known Packs reply may list, the bound
// vanilla's ByteBufCodecs.list reads with; a negative count fails the
// list's allocation. vanilla: ServerboundSelectKnownPacks.STREAM_CODEC
const maxKnownPacks = 64

// handleKnownPacks checks the client has the vanilla data pack and, if so,
// sends what it needs to finish configuration.
func (c *client) handleKnownPacks(r *protocol.Reader) error {
	count := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return fmt.Errorf("known packs: %w", err)
	}
	if count < 0 || count > maxKnownPacks {
		return c.decodeFailure("select_known_packs",
			fmt.Errorf("known packs lists %d packs, at most %d", count, maxKnownPacks))
	}
	known := false
	for range count {
		namespace := r.ReadString(32767)
		id := r.ReadString(32767)
		version := r.ReadString(32767)
		if err := r.Err(); err != nil {
			return fmt.Errorf("known packs: %w", err)
		}
		if namespace == corePack.Namespace && id == corePack.ID && version == corePack.Version {
			known = true
		}
	}
	if err := r.Err(); err != nil {
		return fmt.Errorf("known packs: %w", err)
	}

	// Registry entries go without definitions, so a client lacking the data
	// pack cannot fill them in.
	if !known {
		c.log.Warn("client does not know the vanilla data pack", "version", corePack.Version)
		return c.disconnect(
			fmt.Errorf("client does not know data pack %s:%s %s", corePack.Namespace, corePack.ID, corePack.Version),
			protocol.Text{Text: fmt.Sprintf("This server is running Minecraft %s.", protocol.GameVersion)})
	}

	if err := c.sendFeatureFlags(); err != nil {
		return err
	}
	if err := c.sendRegistries(); err != nil {
		return err
	}
	if err := c.sendTags(); err != nil {
		return err
	}
	return c.offerCodeOfConduct()
}

// offerCodeOfConduct sends the Code of Conduct, if any, holding
// configuration until the client accepts.
// vanilla: ServerCodeOfConductConfigurationTask, ServerConfigurationPacketListenerImpl.addOptionalTasks
func (c *client) offerCodeOfConduct() error {
	language := c.language
	if language == "" {
		language = "en_us"
	}
	text, ok := c.srv.conduct.Pick(language)
	if !ok {
		return c.offerResourcePack()
	}
	if err := c.write(protocol.IDConfigCodeOfConduct, "code of conduct", func(b *protocol.Buffer) {
		b.PutString(text)
	}); err != nil {
		return err
	}
	c.conductPending = true
	return nil
}

// unexpectedTask disconnects a client answering a configuration task that
// is not current. vanilla: ServerConfigurationPacketListenerImpl.finishCurrentTask
func (c *client) unexpectedTask(current, requested string) error {
	return c.disconnect(fmt.Errorf("unexpected request for task finish, current task: %s", current),
		protocol.Text{
			Translate: "disconnect.genericReason",
			With: []protocol.Text{{Text: "Internal Exception: java.lang.IllegalStateException: " +
				"Unexpected request for task finish, current task: " + current +
				", requested: " + requested}},
		})
}

// offerResourcePack sends the resource pack, if any, holding Finish
// Configuration until the client reports a final state; the keep-alive
// bounds the wait.
// vanilla: ServerResourcePackConfigurationTask, ServerConfigurationPacketListenerImpl.addOptionalTasks
func (c *client) offerResourcePack() error {
	c.pluginConnectionMu.Lock()
	defer c.pluginConnectionMu.Unlock()
	pack := c.srv.resourcePack
	if pack != nil && pack.HashTooLong() {
		// vanilla: ServerConfigurationPacketListenerImpl.startNextTask
		c.log.Error("failed to start configuration task", "task", "server_resource_pack",
			"err", "resource-pack-sha1 is longer than 40 characters")
		return c.disconnect(errors.New("resource-pack-sha1 is too long to send"),
			protocol.Text{Translate: "multiplayer.disconnect.configuration_error"})
	}
	if pack != nil {
		if err := c.write(protocol.IDConfigResourcePackPush, "add resource pack", pack.Put); err != nil {
			return err
		}
		c.configurationPacks.Offer(pack.ID)
	}
	if c.configurationPacks.Start() {
		return c.finishConfiguration()
	}
	return nil
}

func (c *client) configurationPackPending() bool {
	c.pluginConnectionMu.Lock()
	defer c.pluginConnectionMu.Unlock()
	return c.configurationPacks.Pending()
}

// resolveConfigurationPackLocked requires pluginConnectionMu so a removal's
// Pop precedes Finish and another plugin cannot offer a pack between them.
func (c *client) resolveConfigurationPackLocked(id uuid.UUID) error {
	if c.configurationPacks.Resolve(id) {
		return c.finishConfiguration()
	}
	return nil
}

// handleResourcePackResponse reads the pack state and kicks a client that
// declined a required pack.
// vanilla: ServerCommonPacketListenerImpl.handleResourcePackResponse
func (c *client) handleResourcePackResponse(r *protocol.Reader) error {
	id := r.ReadUUID()
	action := resourcepack.Action(r.ReadVarInt())
	if err := r.Err(); err != nil {
		return c.decodeFailure("resource_pack", err)
	}
	if !action.Valid() {
		return c.decodeFailure("resource_pack", fmt.Errorf("resource pack action %d is out of range", action))
	}
	var required bool
	if c.srv.plugins != nil {
		c.pluginMu.Lock()
		required = c.pluginPacks[id]
		if action.Terminal() {
			delete(c.pluginPacks, id)
		}
		c.pluginMu.Unlock()
	}
	if c.srv.plugins != nil && c.srv.plugins.Has(pluginruntime.PlayerResourcePackStatus) {
		event := &plugin.PlayerResourcePackStatusEvent{Player: c.connectionPlayer(), ID: id, Status: plugin.PackStatus(action)}
		if action == resourcepack.Declined && (required || c.srv.resourcePack != nil && c.srv.resourcePack.Required) {
			c.srv.plugins.Call(func() { c.srv.plugins.Fire(event) })
		} else {
			c.enqueuePluginNotification(event)
		}
	}
	if action == resourcepack.Declined && (required || c.srv.resourcePack != nil && c.srv.resourcePack.Required) {
		c.log.Info("disconnecting due to resource pack rejection", "pack", id)
		return c.disconnect(errors.New("required resource pack declined"),
			protocol.Text{Translate: "multiplayer.requiredTexturePrompt.disconnect"})
	}
	if action.Terminal() {
		c.pluginConnectionMu.Lock()
		defer c.pluginConnectionMu.Unlock()
		return c.resolveConfigurationPackLocked(id)
	}
	return nil
}

// sendFeatureFlags enables the ordinary set of game features.
func (c *client) sendFeatureFlags() error {
	return c.write(protocol.IDFeatureFlags, "feature flags", func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft:vanilla")
	})
}

// sendRegistries sends one packet per synchronized registry listing its
// entries. A data pack's entry carries its data; a built-in one none, the
// client taking that from the agreed core pack.
// vanilla: RegistrySynchronization.packRegistry
func (c *client) sendRegistries() error {
	for _, reg := range registry.Current() {
		var b protocol.Buffer
		b.PutString(reg.Name)
		b.PutVarInt(int32(len(reg.Entries)))
		for _, entry := range reg.Entries {
			b.PutString(entry)
			data, ok := reg.Data[entry]
			b.PutBool(ok)
			if ok {
				enc, err := nbt.EncodeNetwork(data)
				if err != nil {
					return fmt.Errorf("registry %s entry %s: %w", reg.Name, entry, err)
				}
				b.PutRaw(enc)
			}
		}
		if err := c.writePacket(protocol.IDRegistryData, "registry "+reg.Name, b.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// sendTags sends the tags the client needs. Unlike registry entries, tags
// never come from known packs, so they are sent in full; the client's own
// pack entries refer to them (a dimension type names
// minecraft:infiniburn_overworld, and the registry fails to load without
// it).
func (c *client) sendTags() error {
	payload, err := tagsPayload()
	if err != nil {
		return err
	}
	return c.writePacket(protocol.IDConfigUpdateTags, "update tags", payload)
}

// tagsPayload is Update Tags' body, the same in configuration and play.
func tagsPayload() ([]byte, error) {
	syncTags, builtinTags := registry.CurrentTags()
	var b protocol.Buffer
	b.PutVarInt(int32(len(syncTags) + len(builtinTags)))

	// Tags of synchronized registries, whose entries we know by name.
	for _, tagged := range syncTags {
		reg, ok := registry.Get(tagged.Registry)
		if !ok {
			return nil, fmt.Errorf("tags for unknown registry %s", tagged.Registry)
		}
		b.PutString(tagged.Registry)
		b.PutVarInt(int32(len(tagged.Tags)))
		for _, tag := range tagged.Tags {
			b.PutString(tag.Name)
			b.PutVarInt(int32(len(tag.Entries)))
			for _, entry := range tag.Entries {
				id := reg.ID(entry)
				if id < 0 {
					return nil, fmt.Errorf("tag %s refers to unknown entry %s", tag.Name, entry)
				}
				b.PutVarInt(id)
			}
		}
	}

	// Tags of built-in registries, which only exist as numeric IDs here.
	for _, tagged := range builtinTags {
		b.PutString(tagged.Registry)
		b.PutVarInt(int32(len(tagged.Tags)))
		for _, tag := range tagged.Tags {
			b.PutString(tag.Name)
			b.PutVarInt(int32(len(tag.IDs)))
			for _, id := range tag.IDs {
				b.PutVarInt(id)
			}
		}
	}

	return b.Bytes(), nil
}

// finishConfiguration asks the client to validate what it got and move to
// play.
func (c *client) finishConfiguration() error {
	return c.writePacket(protocol.IDConfigFinish, "finish configuration", nil)
}
