package notifications

import (
	"context"
	"reflect"
	"testing"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/notifications"
)

func TestListenNotify(t *testing.T) {
	var l notifications.Listener
	l = &HttpListener{
		BindAddress: "localhost:",
	}
	c, err := l.Start()
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer l.Stop()
	log.Infof("Started [locahost:] listener, reports address: %v", l.Address())

	var gotIDs []common.ClientID
	go func() {
		for id := range c {
			gotIDs = append(gotIDs, id)
		}
	}()

	n := HttpNotifier{}
	id1, _ := common.StringToClientID("0000000000000001")
	id2, _ := common.StringToClientID("0000000000000002")

	for _, id := range []common.ClientID{id1, id2} {
		if err := n.NewMessageForClient(context.Background(), l.Address(), id); err != nil {
			t.Errorf("Unable to send notification for client: %v", err)
		}
	}

	if !reflect.DeepEqual(gotIDs, []common.ClientID{id1, id2}) {
		t.Errorf("Unexpected ids received got: %v want: %v", gotIDs, []common.ClientID{id1, id2})
	}
}
