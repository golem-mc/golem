package server

// The two streams retain their protocol order; fixtures are initial-join only.
var initialEntitySenders = []func(*client) error{
	itemTracker.sendJoin, arrowTracker.sendJoin, thrownTracker.sendJoin,
	bobberTracker.sendJoin, cloudTracker.sendJoin, orbTracker.sendJoin,
	tntTracker.sendJoin, fireworkTracker.sendJoin, (*client).sendBoatEntities,
	fallingTracker.sendJoin, paintingTracker.sendJoin, frameTracker.sendJoin,
	endCrystalTracker.sendJoin, fangsTracker.sendJoin, eyeTracker.sendJoin, fixtureTracker.sendJoin,
	standTracker.sendJoin, mannequinTracker.sendJoin, mobTracker.sendJoin,
}

var crossingEntitySenders = []func(*client) error{
	itemTracker.sendJoin, arrowTracker.sendJoin, thrownTracker.sendJoin,
	cloudTracker.sendJoin, bobberTracker.sendJoin, orbTracker.sendJoin,
	tntTracker.sendJoin, fireworkTracker.sendJoin, (*client).sendBoatEntities,
	fallingTracker.sendJoin, paintingTracker.sendJoin, frameTracker.sendJoin,
	endCrystalTracker.sendJoin, fangsTracker.sendJoin, eyeTracker.sendJoin, standTracker.sendJoin,
	mannequinTracker.sendJoin, mobTracker.sendJoin,
}

func (c *client) sendWorldEntities(initial bool) error {
	senders := crossingEntitySenders
	if initial {
		senders = initialEntitySenders
	}
	for _, send := range senders {
		if err := send(c); err != nil {
			return err
		}
	}
	return nil
}
