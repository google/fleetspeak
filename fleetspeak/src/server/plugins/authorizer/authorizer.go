package main

import (
	"net"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// LabelFilter is an AuthorizerFactory which returns an authorizer which only
// allows clients reporting a specific label to connect.
func LabelFilter(l string) (authorizer.Authorizer, error) {
	return labelFilter{label: l}, nil
}

// labelFilter is an authorizer.Authorizer which refuses connections from
// clients that do not have a specific label.
type labelFilter struct {
	label string
}

// Allow1 implements Authorizer.
func (f labelFilter) Allow1(net.Addr) bool { return true }

// Allow2 implements Authorizer.
func (f labelFilter) Allow2(_ net.Addr, i authorizer.ContactInfo) bool {
	if f.label == "" {
		return true
	}
	for _, l := range i.ClientLabels {
		if l == f.label {
			return true
		}
	}
	return false
}

// Allow3 implements Authorizer.
func (f labelFilter) Allow3(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo) bool { return true }

// Allow4 implements Authorizer.
func (f labelFilter) Allow4(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo, []authorizer.SignatureInfo) (bool, *fspb.ValidationInfo) {
	return true, nil
}

func main() {
	log.Exitf("unimplemented")
}
