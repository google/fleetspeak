package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/kolide/osquery-go/plugin/distributed"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	ospb "github.com/google/fleetspeak/fleetspeak/src/osquery/proto/fleetspeak_osquery"
)

// MakeDistributed returns a logger.Plugin which accepts Fleetspeak messages containing queries and returns messages containing results.
func MakeDistributed(name string, input <-chan *fspb.Message, output chan<- *fspb.Message) *distributed.Plugin {
	d := dist{
		in:      input,
		out:     output,
		running: make(map[string]*fspb.Address),
	}

	return distributed.NewPlugin(name, d.GetQueries, d.WriteResults)
}

type dist struct {
	in  <-chan *fspb.Message
	out chan<- *fspb.Message

	rl      sync.Mutex               // Protects running
	running map[string]*fspb.Address // FS return address for running queries
}

func (d *dist) GetQueries(ctx context.Context) (*distributed.GetQueriesResult, error) {
	res := distributed.GetQueriesResult{
		Queries:   make(map[string]string),
		Discovery: make(map[string]string),
	}
L:
	for {
		select {
		case m := <-d.in:
			if m.MessageType != "Queries" {
				return nil, fmt.Errorf("expected Queries message type, got %s", m.MessageType)
			}
			var q ospb.Queries
			if err := ptypes.UnmarshalAny(m.Data, &q); err != nil {
				return nil, fmt.Errorf("unable to unmarshal data: %v", err)
			}
			d.rl.Lock()
			for q, s := range q.Queries {
				res.Queries[q] = s
				d.running[q] = m.Source
			}
			d.rl.Unlock()
			for q, s := range q.Discovery {
				res.Discovery[q] = s
			}
		default:
			break L
		}
	}
	return &res, nil
}

func (d *dist) WriteResults(ctx context.Context, results []distributed.Result) error {
	for _, r := range results {
		if err := d.writeResult(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func (d *dist) writeResult(ctx context.Context, result distributed.Result) error {
	d.rl.Lock()
	dest := d.running[result.QueryName]
	delete(d.running, result.QueryName)
	d.rl.Unlock()

	rows := ospb.Rows{
		Rows: make([]*ospb.Row, len(result.Rows)),
	}
	for i, r := range result.Rows {
		rows.Rows[i] = &ospb.Row{Row: r}
	}
	b, err := proto.Marshal(&rows)
	if err != nil {
		return err
	}
	c, b := encodeResult(b)

	res := ospb.QueryResults{
		QueryName: result.QueryName,
		Status:    int64(result.Status),
		Compress:  c,
		Rows:      b,
	}
	data, err := ptypes.MarshalAny(&res)
	if err != nil {
		return err
	}

	m := fspb.Message{
		Destination: dest,
		MessageType: "DistributedResult",
		Data:        data,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case d.out <- &m:
	}
	return nil
}
