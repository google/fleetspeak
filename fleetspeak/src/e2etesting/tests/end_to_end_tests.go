package tests

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	masterClient fgrpc.MasterClient
	msAddress    string
	clientIDs    []string
)

func waitForClientResponses(client fgrpc.MasterClient, clientIDs []string, requestID int64) error {
	respondedClients := make(map[string]bool)

	for range 150 {
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

func broadcastRequestTest(t *testing.T) {
	requestID := rand.Int63()
	_, err := masterClient.CreateHunt(context.Background(), &fpb.CreateHuntRequest{
		Limit: uint64(len(clientIDs)),
		Data: &fpb.TrafficRequestData{
			MasterId:       0,
			RequestId:      requestID,
			NumMessages:    1,
			MessageSize:    1,
			MessageDelayMs: 0,
		}})
	if err != nil {
		t.Fatalf("Unable to create hunt: %v", err)
	}
	err = waitForClientResponses(masterClient, clientIDs, requestID)
	if err != nil {
		t.Fatal(err)
	}
}

func unicastMessagesTest(t *testing.T) {
	requestID := rand.Int63()
	_, err := masterClient.CreateHunt(context.Background(), &fpb.CreateHuntRequest{
		ClientIds: clientIDs,
		Data: &fpb.TrafficRequestData{
			MasterId:       0,
			RequestId:      requestID,
			NumMessages:    1,
			MessageSize:    1,
			MessageDelayMs: 0,
		}})
	if err != nil {
		t.Fatalf("Unable to create hunt: %v", err)
	}
	err = waitForClientResponses(masterClient, clientIDs, requestID)
	if err != nil {
		t.Fatal(err)
	}
}

// RunTests runs all end-to-end tests
func RunTests(t *testing.T, msAddr string, fsClientIDs []string) {
	msAddress = msAddr
	clientIDs = fsClientIDs
	rand.Seed(int64(time.Now().Nanosecond()))
	conn, err := grpc.Dial(msAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("Failed to connect to master server: %v", err)
	}
	defer conn.Close()
	masterClient = fgrpc.NewMasterClient(conn)

	t.Run("BroadcastRequestTest", broadcastRequestTest)
	t.Run("UnicastMessagesTest", unicastMessagesTest)
}
