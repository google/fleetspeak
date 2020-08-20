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

func waitForClientResponses(client fgrpc.MasterClient, clientIDs []string, requestID int64) error {
	respondedClients := make(map[string]bool)

	for i := 0; i < 150; i++ {
		for _, clientID := range clientIDs {
			if _, ok := respondedClients[clientID]; ok {
				continue
			}
			response, err := client.CompletedRequests(context.Background(), &fpb.CompletedRequestsRequest{ClientId: clientID})
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

func broadcastRequestTest(client fgrpc.MasterClient, msAddress string, clientIDs []string) error {
	requestID := rand.Int63()
	_, err := client.CreateHunt(context.Background(), &fpb.CreateHuntRequest{
		Limit: uint64(len(clientIDs)),
		Data: &fpb.TrafficRequestData{
			MasterId:       0,
			RequestId:      requestID,
			NumMessages:    1,
			MessageSize:    1,
			MessageDelayMs: 0,
		}})
	if err != nil {
		return fmt.Errorf("Unable to create hunt: %v", err)
	}
	return waitForClientResponses(client, clientIDs, requestID)
}

func unicastMessagesTest(client fgrpc.MasterClient, msAddress string, clientIDs []string) error {
	requestID := rand.Int63()
	_, err := client.CreateHunt(context.Background(), &fpb.CreateHuntRequest{
		ClientIds: clientIDs,
		Data: &fpb.TrafficRequestData{
			MasterId:       0,
			RequestId:      requestID,
			NumMessages:    1,
			MessageSize:    1,
			MessageDelayMs: 0,
		}})
	if err != nil {
		return fmt.Errorf("Unable to create hunt: %v", err)
	}
	return waitForClientResponses(client, clientIDs, requestID)
}

// RunTests runs all end-to-end tests
func RunTests(msAddress string, clientIDs []string) error {
	rand.Seed(int64(time.Now().Nanosecond()))
	conn, err := grpc.Dial(msAddress, grpc.WithInsecure(), grpc.WithBlock())
	defer conn.Close()
	if err != nil {
		return fmt.Errorf("Failed to connect to master server: %v", err)
	}
	masterClient := fgrpc.NewMasterClient(conn)

	err = broadcastRequestTest(masterClient, msAddress, clientIDs)
	if err != nil {
		return fmt.Errorf("FAIL BroadcastRequestTest: %v", err)
	}
	err = unicastMessagesTest(masterClient, msAddress, clientIDs)
	if err != nil {
		return fmt.Errorf("FAIL UnicastMessagesTest: %v", err)
	}
	return nil
}
