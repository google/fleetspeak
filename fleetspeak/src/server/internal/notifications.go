package internal

import (
	"context"

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

type NoopListener struct {
	c chan common.ClientID
}

func (l *NoopListener) Start() (<-chan common.ClientID, error) {
	l.c = make(chan common.ClientID)
	return l.c, nil
}
func (l *NoopListener) Stop() {
	close(l.c)
}
func (l *NoopListener) Address() string {
	return ""
}

type NoopNotifier struct{}

func (n NoopNotifier) NewMessageForClient(ctx context.Context, target string, id common.ClientID) error {
	return nil
}
