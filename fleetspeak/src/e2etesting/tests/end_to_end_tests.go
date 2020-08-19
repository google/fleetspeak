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

func broadcastRequestTest(msAddress string, clientIDs []string) error {
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

func unicastMessagesTest(msAddress string, clientIDs []string) error {
	conn, err := grpc.Dial(msAddress, grpc.WithInsecure(), grpc.WithBlock())
	defer conn.Close()
	if err != nil {
		return fmt.Errorf("Failed to connect to master server: %v", err)
	}

	client := fgrpc.NewMasterClient(conn)
	ctx := context.Background()

	rand.Seed(int64(time.Now().Nanosecond()))
	requestID := rand.Int63()

	clientID := clientIDs[0]

	_, err = client.CreateHunt(ctx, &fpb.CreateHuntRequest{ClientIds: []string{clientID}, Data: &fpb.TrafficRequestData{
		MasterId:       0,
		RequestId:      requestID,
		NumMessages:    1,
		MessageSize:    1,
		MessageDelayMs: 0,
	}})

	if err != nil {
		return fmt.Errorf("Unable to create hunt: %v", err)
	}

	for i := 0; i < 20; i++ {
		response, err := client.CompletedRequests(ctx, &fpb.CompletedRequestsRequest{ClientId: clientID})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for _, reqID := range response.RequestIds {
			if reqID == requestID {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("No response from client (id %v)", clientID)
}

// RunTests runs all end-to-end tests
func RunTests(msAddress string, clientIDs []string) error {
	err := broadcastRequestTest(msAddress, clientIDs)
	if err != nil {
		return fmt.Errorf("FAIL BroadcastRequestTest: %v", err)
	}
	err = unicastMessagesTest(msAddress, clientIDs)
	if err != nil {
		return fmt.Errorf("FAIL UnicastMessagesTest: %v", err)
	}
	return nil
}
