package tests

import (
	"context"
	"fmt"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	"google.golang.org/grpc"
	"math/rand"
	"time"
)

// RunTest creates a hunt for FS clients and checks that all of them respond
func RunTest(msAddress string, clientIDs []string) error {
	conn, err := grpc.Dial(msAddress, grpc.WithInsecure(), grpc.WithBlock())
	defer conn.Close()
	if err != nil {
		return fmt.Errorf("Failed to connect to master server: %v", err)
	}

	client := fgrpc.NewMasterClient(conn)
	ctx := context.Background()

	rand.Seed(int64(time.Now().Nanosecond()))
	requestID := rand.Int63()

	_, err = client.CreateHunt(ctx, &fpb.CreateHuntRequest{Limit: uint64(len(clientIDs)), Data: &fpb.TrafficRequestData{
		MasterId:       0,
		RequestId:      requestID,
		NumMessages:    1,
		MessageSize:    1,
		MessageDelayMs: 0,
	}})

	if err != nil {
		return fmt.Errorf("Unable to create hunt: %v", err)
	}

	respondedClients := make(map[string]bool)

	for i := 0; i < 150; i++ {
		for _, clientID := range clientIDs {
			if _, ok := respondedClients[clientID]; ok {
				continue
			}
			response, err := client.CompletedRequests(ctx, &fpb.CompletedRequestsRequest{ClientId: clientID})
			if err != nil {
				continue
			}
			for _, reqID := range response.RequestIds {
				if reqID == requestID {
					respondedClients[clientID] = true
					break
				}
			}
		}

		if len(respondedClients) == len(clientIDs) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("Received responses from %v clients out of %v", len(respondedClients), len(clientIDs))
}
