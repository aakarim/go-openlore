package openlore

import (
	"time"

	"github.com/aakarim/go-openlore/internal/config"
	"golang.org/x/crypto/ssh"
)

// Identity represents a connected SSH user.
type Identity struct {
	RemoteAddr  string
	User        string
	PublicKey   ssh.PublicKey
	SessionID   string
	ConnectedAt time.Time
	LoreName    string               // name of the lore spec this identity uses
	PathAccess  []config.PathMapping // resolved path mappings
}

// OnConnectFunc is called when a new SSH session is established.
type OnConnectFunc func(Identity)

// OnDisconnectFunc is called when an SSH session ends.
type OnDisconnectFunc func(Identity)
