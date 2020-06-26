package endtoendtests

import (
	"context"
	"errors"
	"fmt"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	"google.golang.org/grpc"
	"math/rand"
	"time"
)

// RunTest creates a hunt for 1 FS client and checks that the client responds
func RunTest(msPort int, clientID string) error {
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%v", msPort), grpc.WithInsecure(), grpc.WithBlock())
	defer conn.Close()
	if err != nil {
		return fmt.Errorf("Failed to connect to master server: %v", err)
	}

	client := fgrpc.NewMasterClient(conn)
	ctx := context.Background()

	endTime := time.Now().Add(time.Second * 10)
	requestID := rand.Int63()

	_, err = client.CreateHunt(ctx, &fgrpc.CreateHuntRequest{Limit: 1, Data: &fgrpc.TrafficRequestData{
		MasterId:       0,
		RequestId:      requestID,
		NumMessages:    1,
		MessageSize:    1,
		MessageDelayMs: 1000,
	}})
	if err != nil {
		return fmt.Errorf("Unable to create hunt: %v", err)
	}

	for {
		response, err := client.CompletedRequests(ctx, &fgrpc.CompletedRequestsRequest{ClientId: clientID})
		if err != nil {
			return fmt.Errorf("Failed to get completed requests: %v", err)
		}
		if len(response.RequestIds) == 1 {
			if response.RequestIds[0] != requestID {
				return fmt.Errorf("Invalid request id (expected: %v, received: %v)", requestID, response.RequestIds[0])
			}
			return nil
		}
		if len(response.RequestIds) != 0 {
			return fmt.Errorf("Invalid length of response. Expected 0 or 1, got: %v", len(response.RequestIds))
		}
		if time.Now().After(endTime) {
			break
		}
	}
	return errors.New("Not all responses were received")
}
