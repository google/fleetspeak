// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package frr implements the "Fake Rapid Response" service. It contains
// Fleetspeak modules that can be used to simulate GRR traffic through the
// Fleetspeak system for integration and load testing.
package frr

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"sync"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	anypb "google.golang.org/protobuf/types/known/anypb"

	cservice "github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	srpb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

const retryDelay = 15 * time.Second

// ClientServiceFactory is a client.ServiceFactory which produces a frr client
// component.
func ClientServiceFactory(conf *fspb.ClientServiceConfig) (cservice.Service, error) {
	return &frrClientService{}, nil
}

type frrClientService struct {
	sc     cservice.Context
	w      sync.WaitGroup
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *frrClientService) Start(sc cservice.Context) error {
	s.sc = sc
	s.done = make(chan struct{})
	return nil
}

func (s *frrClientService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	// Currently, all messages require data.
	if m.Data == nil {
		log.Fatalf("Received message with nil Data: %v", m)
	}

	switch m.MessageType {
	case "TrafficRequest":
		return s.processTrafficRequest(m)
	case "FileRequest":
		return s.processFileRequest(ctx, m)
	default:
		return fmt.Errorf("unknown message_type: %v", m.MessageType)
	}
}

func (s *frrClientService) processTrafficRequest(m *fspb.Message) error {
	rd := &fpb.TrafficRequestData{}
	if err := m.Data.UnmarshalTo(rd); err != nil {
		return fmt.Errorf("unable to parse data as TrafficRequestData: %v", err)
	}
	if rd.NumMessages == 0 {
		rd.NumMessages = 1
	}
	if rd.MessageSize == 0 {
		rd.MessageSize = 1024
	}
	s.w.Add(1)
	dataBuf := bytes.Repeat([]byte{42}, int(float32(rd.MessageSize)*(1.0+rd.Jitter))+1)
	go func() {
		defer s.w.Done()

		cnt := jitter(rd.NumMessages, rd.Jitter)
		log.V(1).Infof("%v: creating %v responses for request %v", s.sc.GetLocalInfo().ClientID, cnt, rd.RequestId)
		for i := int64(0); i < cnt; i++ {
			delay := time.Millisecond * time.Duration(jitter(rd.MessageDelayMs, rd.Jitter))
			t := time.NewTimer(delay)
			select {
			case <-s.done:
				return
			case <-t.C:
			}

			res := &fpb.TrafficResponseData{
				MasterId:      rd.MasterId,
				RequestId:     rd.RequestId,
				ResponseIndex: i,
				Data:          dataBuf[:jitter(rd.MessageSize, rd.Jitter)],
				Fin:           i == cnt-1,
			}
			d, err := anypb.New(res)
			if err != nil {
				log.Fatalf("Failed to marshal TrafficResponseData: %v", err)
			}
			m := &fspb.Message{
				Destination: &fspb.Address{ServiceName: "FRR"},
				Data:        d,
				MessageType: "TrafficResponse",
			}
			for {
				ctx, c := context.WithTimeout(context.Background(), time.Second)
				err := s.sc.Send(ctx, cservice.AckMessage{M: m})
				c()
				if err == nil {
					break
				}
				select {
				case <-s.done:
					return
				default:
				}
				if ctx.Err() == nil {
					log.Fatalf("Unexpected error sending message: %v", err)
				}
			}
		}
	}()
	return nil
}

func (s *frrClientService) processFileRequest(ctx context.Context, m *fspb.Message) error {
	rd := &fpb.FileRequestData{}
	if err := m.Data.UnmarshalTo(rd); err != nil {
		return fmt.Errorf("unable to parse data as TrafficRequestData: %v", err)
	}
	data, _, err := s.sc.GetFileIfModified(ctx, rd.Name, time.Time{})
	if err != nil {
		return fmt.Errorf("unable to get file [%v]: %v", rd.Name, err)
	}
	defer data.Close()
	b, err := ioutil.ReadAll(data)
	if err != nil {
		return fmt.Errorf("unable to read file body [%v]: %v", rd.Name, err)
	}
	res := &fpb.FileResponseData{
		MasterId: rd.MasterId,
		Name:     rd.Name,
		Size:     uint64(len(b)),
	}
	d, err := anypb.New(res)
	if err != nil {
		log.Fatalf("Failed to marshal FileResponseData: %v", err)
	}
	return s.sc.Send(ctx, cservice.AckMessage{M: &fspb.Message{
		Destination: &fspb.Address{ServiceName: "FRR"},
		Data:        d,
		MessageType: "FileResponse",
	}})
}

func (s *frrClientService) Stop() error {
	close(s.done)
	s.w.Wait()
	return nil
}

func jitter(base int64, j float32) int64 {
	if j == 0.0 || base == 0 {
		return base
	}
	return int64(float64(base) * (1.0 + rand.Float64()*float64(j)))
}

// DefaultFRRMaster sets a default connection to the FRR master server.  This
// default will be used by the FRR ServerService if a configuration is not
// provided. It exists to allow special connection types.
var DefaultFRRMaster fgrpc.MasterClient

type frrServerService struct {
	conn *grpc.ClientConn
	m    fgrpc.MasterClient
	sc   service.Context
}

// ServerServiceFactory is a server.ServiceFactory which produces a FRR server
// component. This component receives messages from clients and forwards them
// (via grpc calls) to a MasterServer.
func ServerServiceFactory(sc *srpb.ServiceConfig) (service.Service, error) {
	var r frrServerService

	if sc.Config == nil && DefaultFRRMaster == nil {
		return nil, fmt.Errorf("FRR server component requires a Config attribute, got: %v", sc)
	}
	if sc.Config == nil {
		r.m = DefaultFRRMaster
	} else {
		c := &fpb.Config{}
		if err := sc.Config.UnmarshalTo(c); err != nil {
			return nil, fmt.Errorf("Unable to parse Config attribute as frr.Config: %v", err)
		}
		conn, err := grpc.Dial(c.MasterServer, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("Unable to connect to master server[%v]: %v", c.MasterServer, err)
		}
		r.conn = conn
		r.m = fgrpc.NewMasterClient(conn)
	}

	return &r, nil
}

func (s *frrServerService) Start(sc service.Context) error {
	s.sc = sc
	return nil
}

func (s *frrServerService) Stop() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *frrServerService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	// This is an integration/performance testing tool. Just fail hard if we
	// get really bad data.
	switch m.MessageType {
	case "TrafficResponse":
		rd := &fpb.TrafficResponseData{}
		if err := m.Data.UnmarshalTo(rd); err != nil {
			log.Fatalf("Unable to parse data as TrafficResponseData: %v", err)
		}
		// Zero the data field - save bandwidth and cpu communicating with master.
		rd.Data = nil
		if _, err := s.m.RecordTrafficResponse(ctx, &fpb.MessageInfo{ClientId: m.Source.ClientId, Data: rd}); err != nil {
			return service.TemporaryError{E: fmt.Errorf("failed to reach FRR master server: %v", err)}
		}
	case "FileResponse":
		rd := &fpb.FileResponseData{}
		if err := m.Data.UnmarshalTo(rd); err != nil {
			log.Fatalf("Unable to parse data as FileResponseData: %v", err)
		}
		if _, err := s.m.RecordFileResponse(ctx, &fpb.FileResponseInfo{ClientId: m.Source.ClientId, Data: rd}); err != nil {
			return service.TemporaryError{E: fmt.Errorf("failed to reach FRR master server: %v", err)}
		}
	default:
		log.Fatalf("Unknown message type: [%v]", m.MessageType)
	}

	return nil
}

// A MasterServer implements fgrpc.MasterServer which records (in ram)
// metadata about received messages.  It also provides methods to examine this
// metadata, check it for consistency and trigger FRR operations.
type MasterServer struct {
	fgrpc.UnimplementedMasterServer

	clients   map[common.ClientID]*clientInfo
	lock      sync.RWMutex // protects clients
	completed chan common.ClientID
	admin     sgrpc.AdminClient
	masterID  int64
}

// NewMasterServer returns MasterServer object.
func NewMasterServer(admin sgrpc.AdminClient) *MasterServer {
	id := rand.Int63()
	log.Infof("Creating master server with id: %v", id)
	return &MasterServer{
		clients:   make(map[common.ClientID]*clientInfo),
		completed: nil,
		admin:     admin,
		masterID:  id,
	}
}

type clientInfo struct {
	requests      map[int64]*requestInfo
	fileDownloads map[string]uint64
	lock          sync.Mutex // protects everything in clientInfo
}

type requestInfo struct {
	responses map[int64]bool
	fin       int64
}

func (i *requestInfo) completed() bool {
	return i.fin != -1 && len(i.responses) == int(i.fin+1)
}

func (s *MasterServer) getClientInfo(id common.ClientID) *clientInfo {
	s.lock.RLock()
	ci := s.clients[id]
	s.lock.RUnlock()

	if ci == nil {
		s.lock.Lock()
		ci = s.clients[id]
		if ci == nil {
			ci = &clientInfo{
				requests:      make(map[int64]*requestInfo),
				fileDownloads: make(map[string]uint64),
			}
			s.clients[id] = ci
		}
		s.lock.Unlock()
	}
	return ci
}

// RecordTrafficResponse implements fgrpc.MasterServer and records that the message
// was received.
func (s *MasterServer) RecordTrafficResponse(ctx context.Context, i *fpb.MessageInfo) (*fspb.EmptyMessage, error) {
	id, err := common.BytesToClientID(i.ClientId)
	if err != nil {
		log.Fatalf("Received message with invalid ClientId[%v]: %v", i.ClientId, err)
	}
	if i.Data == nil {
		log.Fatalf("Received MessageInfo without Data")
	}
	if i.Data.MasterId != s.masterID {
		return &fspb.EmptyMessage{}, nil
	}
	log.V(2).Infof("%v: processing message: %v, %v, %v", id, i.Data.RequestId, i.Data.ResponseIndex, i.Data.Fin)

	ci := s.getClientInfo(id)
	ci.lock.Lock()
	defer ci.lock.Unlock()

	ri := ci.requests[i.Data.RequestId]
	if ri == nil {
		ri = &requestInfo{responses: make(map[int64]bool), fin: -1}
		ci.requests[i.Data.RequestId] = ri
	}
	ri.responses[i.Data.ResponseIndex] = true
	if i.Data.Fin {
		ri.fin = i.Data.ResponseIndex
	}

	if ri.completed() {
		log.V(1).Infof("%v: completed request %v", id, i.Data.RequestId)
		if s.completed != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case s.completed <- id:
			}
		}
	}

	return &fspb.EmptyMessage{}, nil
}

// RecordFileResponse implements fgrpc.MasterServer and records that the message
// was received.
func (s *MasterServer) RecordFileResponse(ctx context.Context, i *fpb.FileResponseInfo) (*fspb.EmptyMessage, error) {
	id, err := common.BytesToClientID(i.ClientId)
	if err != nil {
		log.Fatalf("Received message with invalid ClientId[%v]: %v", i.ClientId, err)
	}
	if i.Data == nil {
		log.Fatalf("Received FileResponseInfo without Data")
	}
	if i.Data.MasterId != s.masterID {
		return &fspb.EmptyMessage{}, nil
	}
	if i.Data.Name == "" {
		log.Fatalf("Received FileResponseInfo without Name")
	}

	ci := s.getClientInfo(id)
	ci.lock.Lock()
	defer ci.lock.Unlock()

	ci.fileDownloads[i.Data.Name] = i.Data.Size
	if s.completed != nil {
		s.completed <- id
	}
	return &fspb.EmptyMessage{}, nil
}

// SetAdminClient changes the stub that is used to contact the FS server.
func (s *MasterServer) SetAdminClient(admin sgrpc.AdminClient) {
	s.admin = admin
}

// AllRequests returns a list of all requests for the client which we've
// received any data for.
func (s *MasterServer) AllRequests(id common.ClientID) []int64 {
	var r []int64

	s.lock.RLock()
	ci := s.clients[id]
	s.lock.RUnlock()

	if ci == nil {
		return r
	}

	ci.lock.Lock()
	defer ci.lock.Unlock()

	for id := range ci.requests {
		r = append(r, id)
	}
	return r
}

// GetCompletedRequests returns a list of requests made to a client which have been
// completed.
func (s *MasterServer) GetCompletedRequests(id common.ClientID) []int64 {
	var r []int64

	s.lock.RLock()
	ci := s.clients[id]
	s.lock.RUnlock()

	if ci == nil {
		return r
	}

	ci.lock.Lock()
	defer ci.lock.Unlock()

	for id, info := range ci.requests {
		if info.completed() {
			r = append(r, id)
		}
	}
	return r
}

// CompletedRequests implements fgrpc.MasterServer and returns a list of
// requests made to a client which have been completed.
func (s *MasterServer) CompletedRequests(ctx context.Context, id *fpb.CompletedRequestsRequest) (*fpb.CompletedRequestsResponse, error) {
	var r fpb.CompletedRequestsResponse

	cid, err := common.StringToClientID(id.ClientId)
	if err != nil {
		return &r, err
	}

	r.RequestIds = s.GetCompletedRequests(cid)
	return &r, nil
}

// WatchCompleted creates, and returns a channel which notifies when a request
// to a client is completed. Repeated calls return the same channel. Should only
// be called before the server is exported. (i.e. when it is idle)
func (s *MasterServer) WatchCompleted() <-chan common.ClientID {
	if s.completed == nil {
		// Use a large capacity - ids are small and we want to minimize
		// any blocking caused by instrumentation.
		s.completed = make(chan common.ClientID, 1000)
	}
	return s.completed
}

// CreateBroadcastRequest initiates a hunt which sends the provided TrafficRequestData to
// every client, up to limit.
func (s *MasterServer) CreateBroadcastRequest(ctx context.Context, rd *fpb.TrafficRequestData, limit uint64) error {
	rd.MasterId = s.masterID
	d, err := anypb.New(rd)
	if err != nil {
		return fmt.Errorf("unable to marshal TrafficRequestData: %v", err)
	}
	bid, err := ids.RandomBroadcastID()
	if err != nil {
		return fmt.Errorf("unable to create BroadcastID: %v", err)
	}
	req := srpb.CreateBroadcastRequest{
		Broadcast: &srpb.Broadcast{
			BroadcastId: bid.Bytes(),
			Source:      &fspb.Address{ServiceName: "FRR"},
			MessageType: "TrafficRequest",
			Data:        d,
		},
		Limit: limit,
	}
	for {
		_, err := s.admin.CreateBroadcast(ctx, &req)
		if err == nil {
			break
		}
		if grpc.Code(err) == codes.Unavailable {
			log.Warningf("FS server unavailable, retrying in %v", retryDelay)
			time.Sleep(retryDelay)
			continue
		}
		return fmt.Errorf("CreateBroadcast(%v) failed: %v", req.String(), err)
	}
	return nil
}

func (s *MasterServer) createUnicastRequest(ctx context.Context, rd *fpb.TrafficRequestData, clientIDs []string) error {
	rd.MasterId = s.masterID
	dat, err := anypb.New(rd)
	if err != nil {
		return fmt.Errorf("unable to marshal TrafficRequestData: %v", err)
	}

	for _, cid := range clientIDs {
		bcid, err := hex.DecodeString(cid)
		if err != nil {
			return fmt.Errorf("failed to decode client id: %v", err)
		}
		m := &fspb.Message{
			Destination: &fspb.Address{ServiceName: "FRR", ClientId: bcid},
			Source:      &fspb.Address{ServiceName: "FRR"},
			Data:        dat,
			MessageType: "TrafficRequest",
		}
		_, err = s.admin.InsertMessage(ctx, m)
		if err != nil {
			return fmt.Errorf("failed to Insert message: %v", err)
		}
	}
	return nil
}

// CreateHunt implements fgrpc.MasterServer and initiates a hunt which sends the provided
// TrafficRequestData to provided clients, or to every client, up to limit, if ClientIds is empty
func (s *MasterServer) CreateHunt(ctx context.Context, hr *fpb.CreateHuntRequest) (*fpb.CreateHuntResponse, error) {
	if len(hr.ClientIds) == 0 {
		return &fpb.CreateHuntResponse{}, s.CreateBroadcastRequest(ctx, hr.Data, hr.Limit)
	}
	return &fpb.CreateHuntResponse{}, s.createUnicastRequest(ctx, hr.Data, hr.ClientIds)
}

// CreateFileDownloadHunt initiates a hunt which requests that up to limit clients download
// the file identified by name.
func (s *MasterServer) CreateFileDownloadHunt(ctx context.Context, name string, limit uint64) error {
	rd := &fpb.FileRequestData{
		MasterId: s.masterID,
		Name:     name,
	}
	d, err := anypb.New(rd)
	if err != nil {
		return fmt.Errorf("unable to marshal FileRequestData: %v", err)
	}

	bid, err := ids.RandomBroadcastID()
	if err != nil {
		return fmt.Errorf("unable to create BroadcastID: %v", err)
	}
	req := srpb.CreateBroadcastRequest{
		Broadcast: &srpb.Broadcast{
			BroadcastId: bid.Bytes(),
			Source:      &fspb.Address{ServiceName: "FRR"},
			MessageType: "FileRequest",
			Data:        d,
		},
		Limit: limit,
	}
	for {
		_, err := s.admin.CreateBroadcast(ctx, &req)
		if err == nil {
			break
		}
		if grpc.Code(err) == codes.Unavailable {
			log.Warningf("FS server unavailable, retrying in %v", retryDelay)
			time.Sleep(retryDelay)
			continue
		}
		return fmt.Errorf("CreateBroadcast(%v) failed: %v", req.String(), err)
	}
	return nil
}
