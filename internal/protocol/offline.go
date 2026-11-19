package protocol

import (
	"crypto/md5"

	"github.com/google/uuid"
)

// OfflineUUID returns the UUID a player gets when the server does not
// authenticate them. The vanilla server derives it from the string
// "OfflinePlayer:<name>" the way Java's UUID.nameUUIDFromBytes does: an MD5
// hash tagged as version 3.
//
// This cannot be expressed with uuid.NewMD5, which always hashes a namespace
// UUID ahead of the data and so arrives at a different value.
func OfflineUUID(name string) uuid.UUID {
	u := uuid.UUID(md5.Sum([]byte("OfflinePlayer:" + name)))
	u[6] = u[6]&0x0F | 0x30 // version 3
	u[8] = u[8]&0x3F | 0x80 // RFC 4122 variant
	return u
}
