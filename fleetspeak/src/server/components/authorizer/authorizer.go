package authorizer

import (
	"net"

	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// LabelFilter is an authorizer.Authorizer which refuses connections from
// clients that do not have a specific label.
type LabelFilter struct {
	Label string
}

// Allow1 implements Authorizer.
func (f LabelFilter) Allow1(net.Addr) bool { return true }

// Allow2 implements Authorizer.
func (f LabelFilter) Allow2(_ net.Addr, i authorizer.ContactInfo) bool {
	if f.Label == "" {
		return true
	}
	for _, l := range i.ClientLabels {
		if l == f.Label {
			return true
		}
	}
	return false
}

// Allow3 implements Authorizer.
func (f LabelFilter) Allow3(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo) bool { return true }

// Allow4 implements Authorizer.
func (f LabelFilter) Allow4(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo, []authorizer.SignatureInfo) (bool, *fspb.ValidationInfo) {
	return true, nil
}
