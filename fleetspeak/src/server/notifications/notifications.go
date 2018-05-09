package notifications

import (
	"context"

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

type Listener interface {
	// Start causes the Listener to begin listening for notifications. Once
	// started, should write to ids every time it is notified that there may be
	// new messages for a client.
	Start() (<-chan common.ClientID, error)

	// Stop causes the Listener to stop listening for notifications and close the
	// ids channel.
	Stop()

	// Address returns the address of this listener. These addresses must be
	// suitable to pass as a target to any Notifier compatible with this Listener.
	// Will only be called after Start().
	Address() string
}

type Notifier interface {
	// NewMessageForClient indicates that one or more messages have been written
	// and notifies the provided target of this.
	NewMessageForClient(ctx context.Context, target string, id common.ClientID) error
}
