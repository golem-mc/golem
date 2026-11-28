package command

import "github.com/golem-mc/golem/internal/protocol"

// Ride is the /ride command.
var Ride = Command{
	Name:  "ride",
	Level: PermLevelGamemasters,
	Build: rideNode,
	Run:   runRide,
}

// The translation keys /ride answers with.
// vanilla: RideCommand
const (
	rideNotRidingKey      = "commands.ride.not_riding"
	rideAlreadyRidingKey  = "commands.ride.already_riding"
	rideMountFailedKey    = "commands.ride.mount.failure.generic"
	rideCantRidePlayers   = "commands.ride.mount.failure.cant_ride_players"
	rideLoopKey           = "commands.ride.mount.failure.loop"
	rideWrongDimensionKey = "commands.ride.mount.failure.wrong_dimension"
	rideMountSuccessKey   = "commands.ride.mount.success"
	rideDismountDoneKey   = "commands.ride.dismount.success"
)

// Rider is an Entity that can sit on another.
type Rider interface {
	Entity

	// Vehicle is the entity this one rides.
	Vehicle() (Entity, bool)

	// StartRiding seats the entity on the vehicle without asking its
	// capacity, reporting false for a vehicle it cannot sit on.
	// vanilla: Entity.startRiding
	StartRiding(vehicle Entity) bool

	// HasIndirectPassenger reports whether the entity rides this one, at any
	// depth of the stack.
	// vanilla: Entity.getSelfAndPassengers
	HasIndirectPassenger(other Entity) bool

	// StopRiding takes the entity off its vehicle.
	// vanilla: Entity.stopRiding
	StopRiding()
}

// Related is an Entity that knows the entities it stands in relation to.
type Related interface {
	Entity

	// Relation is the entities in the named /execute on relation (owner,
	// leasher, target, attacker, vehicle, controller, origin, passengers),
	// none for a relation the entity does not have.
	// vanilla: ExecuteCommand.createRelationOperations
	Relation(name string) []Entity
}

// rideEntityNode is a single-entity argument of the /ride tree.
func rideEntityNode(name string, executable bool, children ...*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       name,
		Executable: executable,
		Parser:     ParserEntity,
		// 0x01: a single entity, of any kind.
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) },
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: children,
	}
}

// rideNode builds the /ride tree, matching the vanilla report.
func rideNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "ride",
		Required: PermLevelGamemasters,
		Children: []*Node{
			rideEntityNode("target", false,
				&Node{Type: NodeLiteral, Name: "mount", Children: []*Node{
					rideEntityNode("vehicle", true),
				}},
				&Node{Type: NodeLiteral, Name: "dismount", Executable: true},
			),
		},
	}
}

// runRide runs /ride <target> mount <vehicle> and /ride <target> dismount.
// The refusals come in vanilla's order.
// vanilla: RideCommand.mount, RideCommand.dismount
func runRide(src Source, input string, args []string) error {
	word, rest := splitTargets(input, args)
	if word == "" || len(rest) == 0 {
		return nil
	}
	found, err := resolveEntities(src, word)
	if err != nil {
		return err
	}
	target := found[0]
	rider, _ := target.(Rider)

	if rest[0] == "dismount" {
		var vehicle Entity
		if rider != nil {
			vehicle, _ = rider.Vehicle()
		}
		if vehicle == nil {
			return refuse(src, rideNotRidingKey, target.DisplayName())
		}
		rider.StopRiding()
		if !wantsFeedback(src) {
			return nil
		}
		return feedbackToAdmins(src, rideDismountDoneKey, target.DisplayName(), vehicle.DisplayName())
	}

	vehicleWord, _ := splitTargets(input, rest[1:])
	if vehicleWord == "" {
		return nil
	}
	vehicles, err := resolveEntities(src, vehicleWord)
	if err != nil {
		return err
	}
	vehicle := vehicles[0]
	if rider != nil {
		if current, ok := rider.Vehicle(); ok {
			return refuse(src, rideAlreadyRidingKey, target.DisplayName(), current.DisplayName())
		}
	}
	switch {
	case vehicle.EntityType() == "minecraft:player":
		return refuse(src, rideCantRidePlayers)
	case target.EntityID() == vehicle.EntityID() || rider != nil && rider.HasIndirectPassenger(vehicle):
		return refuse(src, rideLoopKey)
	case target.DimensionName() != vehicle.DimensionName():
		return refuse(src, rideWrongDimensionKey)
	case rider == nil || !rider.StartRiding(vehicle):
		return refuse(src, rideMountFailedKey, target.DisplayName(), vehicle.DisplayName())
	}
	if !wantsFeedback(src) {
		return nil
	}
	return feedbackToAdmins(src, rideMountSuccessKey, target.DisplayName(), vehicle.DisplayName())
}
