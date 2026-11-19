package server

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Each kind's interface methods sit on a small type of their own, so a handle
// has exactly the interfaces of its kind (see pluginKindHandle). The state
// they read is the saved compound's, and writes merge over it.
type (
	standOps     struct{ e pluginEntity }
	frameOps     struct{ e pluginEntity }
	hangOps      struct{ e pluginEntity }
	tntOps       struct{ e pluginEntity }
	fallOps      struct{ e pluginEntity }
	dispOps      struct{ e pluginEntity }
	blockDispOps struct{ e pluginEntity }
	itemDispOps  struct{ e pluginEntity }
	textDispOps  struct{ e pluginEntity }
	interOps     struct{ e pluginEntity }
	projOps      struct{ e pluginEntity }
	arrowOps     struct{ e pluginEntity }

	pluginStandEntity struct {
		pluginEntity
		livingOps
		standOps
	}
	pluginFrameEntity struct {
		pluginEntity
		hangOps
		frameOps
	}
	pluginHangingEntity struct {
		pluginEntity
		hangOps
	}
	pluginTNTEntity struct {
		pluginEntity
		tntOps
	}
	pluginFallingEntity struct {
		pluginEntity
		fallOps
	}
	pluginBlockDisplayEntity struct {
		pluginEntity
		dispOps
		blockDispOps
	}
	pluginItemDisplayEntity struct {
		pluginEntity
		dispOps
		itemDispOps
	}
	pluginTextDisplayEntity struct {
		pluginEntity
		dispOps
		textDispOps
	}
	pluginInteractionEntity struct {
		pluginEntity
		interOps
	}
	pluginProjectileEntity struct {
		pluginEntity
		projOps
	}
	pluginArrowEntity struct {
		pluginEntity
		projOps
		arrowOps
	}
)

// pluginTypedHandle answers the handle of the kinds whose interfaces follow
// from the entity type, nil for the rest.
func pluginTypedHandle(base pluginEntity, t registry.EntityType, saved nbt.Compound) plugin.Entity {
	e := base
	switch t {
	case registry.EntityTypeArmorStand:
		return pluginStandEntity{pluginEntity: e, livingOps: livingOps{e}, standOps: standOps{e}}
	case registry.EntityTypeItemFrame, registry.EntityTypeGlowItemFrame:
		return pluginFrameEntity{pluginEntity: e, hangOps: hangOps{e}, frameOps: frameOps{e}}
	case registry.EntityTypePainting:
		return pluginHangingEntity{pluginEntity: e, hangOps: hangOps{e}}
	case registry.EntityTypeTnt:
		return pluginTNTEntity{pluginEntity: e, tntOps: tntOps{e}}
	case registry.EntityTypeFallingBlock:
		return pluginFallingEntity{pluginEntity: e, fallOps: fallOps{e}}
	case registry.EntityTypeBlockDisplay:
		return pluginBlockDisplayEntity{pluginEntity: e, dispOps: dispOps{e}, blockDispOps: blockDispOps{e}}
	case registry.EntityTypeItemDisplay:
		return pluginItemDisplayEntity{pluginEntity: e, dispOps: dispOps{e}, itemDispOps: itemDispOps{e}}
	case registry.EntityTypeTextDisplay:
		return pluginTextDisplayEntity{pluginEntity: e, dispOps: dispOps{e}, textDispOps: textDispOps{e}}
	case registry.EntityTypeFireworkRocket:
		return pluginFireworkEntity{pluginEntity: e, projOps: projOps{e}, fireworkOps: fireworkOps{e}}
	case registry.EntityTypeInteraction:
		return pluginInteractionEntity{pluginEntity: e, interOps: interOps{e}}
	}
	// Projectile.addAdditionalSaveData always writes HasBeenShot; an arrow
	// is the one with an inGround flag.
	if _, ok := saved["HasBeenShot"]; ok {
		if _, arrow := saved["inGround"]; arrow {
			return pluginArrowEntity{pluginEntity: e, projOps: projOps{e}, arrowOps: arrowOps{e}}
		}
		return pluginProjectileEntity{pluginEntity: e, projOps: projOps{e}}
	}
	return nil
}

func floatsOf(c nbt.Compound, key string, n int, def ...float32) []float32 {
	out := make([]float32, n)
	copy(out, def)
	list, _ := nbt.Get[nbt.List](c, key)
	for i := 0; i < n && i < len(list.Items); i++ {
		if f, ok := list.Items[i].(nbt.Float); ok {
			out[i] = float32(f)
		}
	}
	return out
}

func floatList(v ...float32) nbt.List {
	items := make([]nbt.Tag, len(v))
	for i, f := range v {
		items[i] = nbt.Float(f)
	}
	return nbt.NewList(items)
}

// withEntry merges one nested compound's key over the saved one, keeping its siblings.
func (e pluginEntity) mergeInto(key, field string, v nbt.Tag) {
	inner, _ := nbt.CompoundView(e.saved()[key])
	c := maps.Clone(inner)
	if c == nil {
		c = nbt.Compound{}
	}
	c[field] = v
	e.merge(nbt.Compound{key: c})
}

func (e pluginEntity) boolField(key string, def bool) bool {
	if _, ok := e.saved()[key]; !ok {
		return def
	}
	return e.flag(key)
}
func (e pluginEntity) floatField(key string, def float32) float32 {
	if f, ok := nbt.FloatField(e.saved(), key); ok {
		return f
	}
	return def
}
func (e pluginEntity) intField(key string) int { n, _ := nbt.IntField(e.saved(), key); return int(n) }

// Armor stand.
var standPoseKeys = [...]string{"Head", "Body", "LeftArm", "RightArm", "LeftLeg", "RightLeg"}

// vanilla: ArmorStand.DEFAULT_*_POSE
var standPoseDefaults = [...][3]float32{{}, {}, {-10, 0, -10}, {-15, 0, 10}, {-1, 0, -1}, {1, 0, 1}}

func (o standOps) Small() bool         { return o.e.flag("Small") }
func (o standOps) SetSmall(v bool)     { o.e.setFlag("Small", v) }
func (o standOps) Arms() bool          { return o.e.flag("ShowArms") }
func (o standOps) SetArms(v bool)      { o.e.setFlag("ShowArms", v) }
func (o standOps) BasePlate() bool     { return !o.e.flag("NoBasePlate") }
func (o standOps) SetBasePlate(v bool) { o.e.setFlag("NoBasePlate", !v) }
func (o standOps) Marker() bool        { return o.e.flag("Marker") }
func (o standOps) SetMarker(v bool)    { o.e.setFlag("Marker", v) }
func (o standOps) Visible() bool       { return !o.e.flag("Invisible") }
func (o standOps) SetVisible(v bool)   { o.e.setFlag("Invisible", !v) }
func (o standOps) Pose(p plugin.ArmorStandPart) plugin.Vec3 {
	pose, _ := nbt.CompoundView(o.e.saved()["Pose"])
	f := floatsOf(pose, standPoseKeys[p], 3, standPoseDefaults[p][:]...)
	if _, set := pose[standPoseKeys[p]]; !set {
		f = standPoseDefaults[p][:]
	}
	return plugin.Vec3{X: float64(f[0]), Y: float64(f[1]), Z: float64(f[2])}
}
func (o standOps) SetPose(p plugin.ArmorStandPart, r plugin.Vec3) {
	o.e.mergeInto("Pose", standPoseKeys[p], floatList(float32(r.X), float32(r.Y), float32(r.Z)))
}

// Hanging entities and item frames.
func (o hangOps) Facing() plugin.BlockFace {
	saved := o.e.saved()
	if n, ok := nbt.ByteField(saved, "Facing"); ok {
		return plugin.BlockFace(n) % 6
	}
	// A painting's facing is the 2D id: south, west, north, east.
	n, _ := nbt.ByteField(saved, "facing")
	return [...]plugin.BlockFace{plugin.South, plugin.West, plugin.North, plugin.East}[n&3]
}

// SetFacing turns a frame or painting on the block it hangs from; a vertical
// painting is refused. force skips nothing here: the anchor cell does not
// move, so there is no support to check.
// vanilla: HangingEntity.setDirection
func (o hangOps) SetFacing(f plugin.BlockFace, force bool) bool {
	if f > plugin.East {
		return false
	}
	if _, frame := o.e.saved()["Facing"]; frame {
		o.e.merge(nbt.Compound{"Facing": nbt.Byte(int8(f))})
		return true
	}
	id := map[plugin.BlockFace]int8{plugin.South: 0, plugin.West: 1, plugin.North: 2, plugin.East: 3}
	n, ok := id[f]
	if ok {
		o.e.merge(nbt.Compound{"facing": nbt.Byte(n)})
	}
	return ok
}
func (o frameOps) Item() plugin.ItemStack {
	c, ok := nbt.Get[nbt.CompoundTag](o.e.saved(), "Item")
	if !ok {
		return plugin.ItemStack{}
	}
	st, _ := registry.StoredStack(c)
	return pluginStack(st)
}
func (o frameOps) SetItem(s plugin.ItemStack) {
	if s.Empty() {
		o.e.drop("Item")
	} else if st, ok := pluginProtocolStack(s); ok {
		o.e.merge(nbt.Compound{"Item": registry.StackTag(st)})
	}
}
func (o frameOps) Rotation() int { n, _ := nbt.ByteField(o.e.saved(), "ItemRotation"); return int(n) }
func (o frameOps) SetRotation(n int) {
	o.e.merge(nbt.Compound{"ItemRotation": nbt.Byte(int8(((n % 8) + 8) % 8))})
}
func (o frameOps) Fixed() bool       { return o.e.flag("Fixed") }
func (o frameOps) SetFixed(v bool)   { o.e.setFlag("Fixed", v) }
func (o frameOps) Visible() bool     { return !o.e.flag("Invisible") }
func (o frameOps) SetVisible(v bool) { o.e.setFlag("Invisible", !v) }

// Primed TNT.
func (o tntOps) Fuse() int { n, _ := nbt.ShortField(o.e.saved(), "fuse"); return int(n) }
func (o tntOps) SetFuse(n int) {
	o.e.merge(nbt.Compound{"fuse": nbt.Short(clampInt16(n))})
}
func (o tntOps) Source() (plugin.Entity, bool) {
	id, ok := o.e.uuidField("owner")
	if !ok {
		return nil, false
	}
	b, found := o.e.host.entityByUUID(uuidOf(id), "")
	if !found {
		return nil, false
	}
	return o.e.host.pluginEntityOf(b), true
}

// Yield is the explosion power, 4 when unset.
// vanilla: PrimedTnt.DEFAULT_EXPLOSION_POWER
func (o tntOps) Yield() float32     { return o.e.floatField("explosion_power", 4) }
func (o tntOps) SetYield(v float32) { o.e.merge(nbt.Compound{"explosion_power": nbt.Float(max(v, 0))}) }

// Falling block.
func blockStateOfTag(tag nbt.Tag) plugin.BlockState {
	st, _ := registry.StateOfTag(tag)
	return plugin.BlockState(st)
}
func blockStateTag(s plugin.BlockState) nbt.Tag {
	name, props, ok := registry.StateNameProps(uint16(s))
	if !ok {
		name, props, _ = registry.StateNameProps(0)
	}
	c := nbt.Compound{"Name": nbt.String(name)}
	if len(props) != 0 {
		p := nbt.Compound{}
		for k, v := range props {
			p[k] = nbt.String(v)
		}
		c["Properties"] = p
	}
	return c
}

func (o fallOps) State() plugin.BlockState { return blockStateOfTag(o.e.saved()["BlockState"]) }
func (o fallOps) DropItem() bool           { return o.e.boolField("DropItem", true) }
func (o fallOps) SetDropItem(v bool)       { o.e.setFlag("DropItem", v) }
func (o fallOps) HurtEntities() bool       { return o.e.flag("HurtEntities") }
func (o fallOps) SetHurtEntities(v bool)   { o.e.setFlag("HurtEntities", v) }

// Displays.
func (o dispOps) Transform() plugin.Transform {
	t, _ := nbt.CompoundView(o.e.saved()["transformation"])
	out := plugin.Transform{Translation: [3]float32(floatsOf(t, "translation", 3)),
		Scale: [3]float32(floatsOf(t, "scale", 3, 1, 1, 1)), LeftRotation: [4]float32(floatsOf(t, "left_rotation", 4, 0, 0, 0, 1)),
		RightRotation: [4]float32(floatsOf(t, "right_rotation", 4, 0, 0, 0, 1))}
	return out
}
func (o dispOps) SetTransform(t plugin.Transform) {
	o.e.merge(nbt.Compound{"transformation": nbt.Compound{"translation": floatList(t.Translation[:]...), "scale": floatList(t.Scale[:]...),
		"left_rotation": floatList(t.LeftRotation[:]...), "right_rotation": floatList(t.RightRotation[:]...)}})
}
func (o dispOps) SetInterpolation(delay, duration int) {
	o.e.merge(nbt.Compound{"start_interpolation": nbt.Int(int32(delay)), "interpolation_duration": nbt.Int(int32(max(duration, 0)))})
}

var billboardNames = [...]string{"fixed", "vertical", "horizontal", "center"}

func (o dispOps) Billboard() plugin.Billboard {
	s, _ := nbt.Get[nbt.String](o.e.saved(), "billboard")
	for i, n := range billboardNames {
		if string(s) == n {
			return plugin.Billboard(i)
		}
	}
	return plugin.BillboardFixed
}
func (o dispOps) SetBillboard(b plugin.Billboard) {
	if int(b) < len(billboardNames) {
		o.e.merge(nbt.Compound{"billboard": nbt.String(billboardNames[b])})
	}
}
func (o dispOps) Brightness() *[2]int {
	c, ok := nbt.CompoundView(o.e.saved()["brightness"])
	if !ok {
		return nil
	}
	sky, _ := nbt.IntField(c, "sky")
	block, _ := nbt.IntField(c, "block")
	return &[2]int{int(block), int(sky)}
}

// SetBrightness takes [block, sky] light, each 0 to 15, as Brightness answers.
func (o dispOps) SetBrightness(b *[2]int) {
	if b == nil {
		o.e.drop("brightness")
		return
	}
	o.e.merge(nbt.Compound{"brightness": nbt.Compound{"block": nbt.Int(int32(min(max(b[0], 0), 15))), "sky": nbt.Int(int32(min(max(b[1], 0), 15)))}})
}
func (o dispOps) ViewRange() float32     { return o.e.floatField("view_range", 1) }
func (o dispOps) SetViewRange(v float32) { o.e.merge(nbt.Compound{"view_range": nbt.Float(v)}) }
func (o dispOps) Shadow() (float32, float32) {
	return o.e.floatField("shadow_radius", 0), o.e.floatField("shadow_strength", 1)
}
func (o dispOps) SetShadow(radius, strength float32) {
	o.e.merge(nbt.Compound{"shadow_radius": nbt.Float(radius), "shadow_strength": nbt.Float(strength)})
}
func (o dispOps) GlowColor() int32 {
	if n, ok := nbt.IntField(o.e.saved(), "glow_color_override"); ok {
		return n
	}
	return -1
}
func (o dispOps) SetGlowColor(c int32) { o.e.merge(nbt.Compound{"glow_color_override": nbt.Int(c)}) }

func (o blockDispOps) State() plugin.BlockState { return blockStateOfTag(o.e.saved()["block_state"]) }
func (o blockDispOps) SetState(s plugin.BlockState) {
	o.e.merge(nbt.Compound{"block_state": blockStateTag(s)})
}

func (o itemDispOps) Item() plugin.ItemStack {
	c, ok := nbt.Get[nbt.CompoundTag](o.e.saved(), "item")
	if !ok {
		return plugin.ItemStack{}
	}
	st, _ := registry.StoredStack(c)
	return pluginStack(st)
}
func (o itemDispOps) SetItem(s plugin.ItemStack) {
	if s.Empty() {
		o.e.drop("item")
	} else if st, ok := pluginProtocolStack(s); ok {
		o.e.merge(nbt.Compound{"item": registry.StackTag(st)})
	}
}

func (o textDispOps) Text() plugin.Text {
	if tag, ok := o.e.saved()["text"]; ok {
		if t, ok := pluginTextOf(tag); ok {
			return t
		}
	}
	return plugin.Text{}
}
func (o textDispOps) SetText(t plugin.Text) {
	o.e.merge(nbt.Compound{"text": protocol.TextTag(pluginProtocolText(t))})
}

// LineWidth is 200 when unset.
// vanilla: Display.TextDisplay.readAdditionalSaveData
func (o textDispOps) LineWidth() int {
	if n, ok := nbt.IntField(o.e.saved(), "line_width"); ok {
		return int(n)
	}
	return 200
}
func (o textDispOps) SetLineWidth(n int) { o.e.merge(nbt.Compound{"line_width": nbt.Int(int32(n))}) }

// Background is the explicit ARGB colour, the default 0x40000000 when the
// saved form leaves it unset.
func (o textDispOps) Background() uint32 {
	if n, ok := nbt.IntField(o.e.saved(), "background"); ok {
		return uint32(n)
	}
	return 0x40000000
}
func (o textDispOps) SetBackground(c uint32) {
	o.e.merge(nbt.Compound{"background": nbt.Int(int32(c)), "default_background": nbt.Byte(0)})
}
func (o textDispOps) SeeThrough() bool     { return o.e.flag("see_through") }
func (o textDispOps) SetSeeThrough(v bool) { o.e.setFlag("see_through", v) }

// Interaction.
func (o interOps) Size() (float32, float32) {
	return o.e.floatField("width", 1), o.e.floatField("height", 1)
}
func (o interOps) SetSize(w, h float32) {
	o.e.merge(nbt.Compound{"width": nbt.Float(w), "height": nbt.Float(h)})
}
func (o interOps) Responsive() bool     { return o.e.flag("response") }
func (o interOps) SetResponsive(v bool) { o.e.setFlag("response", v) }

// Projectiles and arrows.
func (o projOps) Shooter() (plugin.Entity, bool) {
	id, ok := o.e.uuidField("Owner")
	if !ok {
		return nil, false
	}
	b, found := o.e.host.entityByUUID(uuidOf(id), "")
	if !found {
		return nil, false
	}
	return o.e.host.pluginEntityOf(b), true
}
func (o projOps) SetShooter(s plugin.Entity) {
	if s == nil {
		o.e.setUUID("Owner", plugin.UUID{})
		return
	}
	o.e.setUUID("Owner", s.UUID())
	// A player's arrow is no longer disallowed.
	// vanilla: AbstractArrow.setOwner
	if _, ok := s.(plugin.Player); ok {
		if n, ok := nbt.ByteField(o.e.saved(), "pickup"); ok && plugin.ArrowPickup(n) == plugin.PickupDisallowed {
			o.e.merge(nbt.Compound{"pickup": nbt.Byte(int8(plugin.PickupAllowed))})
		}
	}
}
func (o arrowOps) BaseDamage() float64 { return nbt.DoubleFieldOr(o.e.saved(), "damage", 2) }
func (o arrowOps) SetBaseDamage(v float64) {
	o.e.merge(nbt.Compound{"damage": nbt.Double(v)})
}
func (o arrowOps) Critical() bool     { return o.e.flag("crit") }
func (o arrowOps) SetCritical(v bool) { o.e.setFlag("crit", v) }
func (o arrowOps) PierceLevel() int {
	n, _ := nbt.ByteField(o.e.saved(), "PierceLevel")
	return int(n)
}
func (o arrowOps) SetPierceLevel(n int) {
	o.e.merge(nbt.Compound{"PierceLevel": nbt.Byte(int8(min(max(n, 0), 127)))})
}
func (o arrowOps) Pickup() plugin.ArrowPickup {
	n, _ := nbt.ByteField(o.e.saved(), "pickup")
	return plugin.ArrowPickup(n)
}
func (o arrowOps) SetPickup(p plugin.ArrowPickup) {
	if p <= plugin.PickupCreativeOnly {
		o.e.merge(nbt.Compound{"pickup": nbt.Byte(int8(p))})
	}
}
func (o arrowOps) InGround() bool { return o.e.flag("inGround") }
