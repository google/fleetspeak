// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.1
// 	protoc        v5.26.1
// source: fleetspeak/src/server/proto/fleetspeak_server/server.proto

package fleetspeak_server

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// Describes a server's configuration. If unset, all values default to values
// reasonable for a unit test or small installation. Larger installations may
// need to tune these.
type ServerConfig struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The collection of services that this server should include.
	Services []*ServiceConfig `protobuf:"bytes,1,rep,name=services,proto3" json:"services,omitempty"`
	// The approximate time to wait between checking for new broadcasts. If unset,
	// a default of 1 minute is used.
	BroadcastPollTime *durationpb.Duration `protobuf:"bytes,2,opt,name=broadcast_poll_time,json=broadcastPollTime,proto3" json:"broadcast_poll_time,omitempty"`
}

func (x *ServerConfig) Reset() {
	*x = ServerConfig{}
	mi := &file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServerConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServerConfig) ProtoMessage() {}

func (x *ServerConfig) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServerConfig.ProtoReflect.Descriptor instead.
func (*ServerConfig) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescGZIP(), []int{0}
}

func (x *ServerConfig) GetServices() []*ServiceConfig {
	if x != nil {
		return x.Services
	}
	return nil
}

func (x *ServerConfig) GetBroadcastPollTime() *durationpb.Duration {
	if x != nil {
		return x.BroadcastPollTime
	}
	return nil
}

var File_fleetspeak_src_server_proto_fleetspeak_server_server_proto protoreflect.FileDescriptor

var file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDesc = []byte{
	0x0a, 0x3a, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f,
	0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x11, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x1a,
	0x3c, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63, 0x2f,
	0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c, 0x65,
	0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f, 0x73,
	0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x1e, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x64,
	0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x97, 0x01,
	0x0a, 0x0c, 0x53, 0x65, 0x72, 0x76, 0x65, 0x72, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x12, 0x3c,
	0x0a, 0x08, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b,
	0x32, 0x20, 0x2e, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x73, 0x65,
	0x72, 0x76, 0x65, 0x72, 0x2e, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x43, 0x6f, 0x6e, 0x66,
	0x69, 0x67, 0x52, 0x08, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x73, 0x12, 0x49, 0x0a, 0x13,
	0x62, 0x72, 0x6f, 0x61, 0x64, 0x63, 0x61, 0x73, 0x74, 0x5f, 0x70, 0x6f, 0x6c, 0x6c, 0x5f, 0x74,
	0x69, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x19, 0x2e, 0x67, 0x6f, 0x6f, 0x67,
	0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x44, 0x75, 0x72, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x52, 0x11, 0x62, 0x72, 0x6f, 0x61, 0x64, 0x63, 0x61, 0x73, 0x74, 0x50,
	0x6f, 0x6c, 0x6c, 0x54, 0x69, 0x6d, 0x65, 0x42, 0x4c, 0x5a, 0x4a, 0x67, 0x69, 0x74, 0x68, 0x75,
	0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x66, 0x6c, 0x65,
	0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65,
	0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63, 0x2f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x73,
	0x65, 0x72, 0x76, 0x65, 0x72, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescOnce sync.Once
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescData = file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDesc
)

func file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescGZIP() []byte {
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescOnce.Do(func() {
		file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescData = protoimpl.X.CompressGZIP(file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescData)
	})
	return file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDescData
}

var file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_goTypes = []any{
	(*ServerConfig)(nil),        // 0: fleetspeak.server.ServerConfig
	(*ServiceConfig)(nil),       // 1: fleetspeak.server.ServiceConfig
	(*durationpb.Duration)(nil), // 2: google.protobuf.Duration
}
var file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_depIdxs = []int32{
	1, // 0: fleetspeak.server.ServerConfig.services:type_name -> fleetspeak.server.ServiceConfig
	2, // 1: fleetspeak.server.ServerConfig.broadcast_poll_time:type_name -> google.protobuf.Duration
	2, // [2:2] is the sub-list for method output_type
	2, // [2:2] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_init() }
func file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_init() {
	if File_fleetspeak_src_server_proto_fleetspeak_server_server_proto != nil {
		return
	}
	file_fleetspeak_src_server_proto_fleetspeak_server_services_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_goTypes,
		DependencyIndexes: file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_depIdxs,
		MessageInfos:      file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_msgTypes,
	}.Build()
	File_fleetspeak_src_server_proto_fleetspeak_server_server_proto = out.File
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_rawDesc = nil
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_goTypes = nil
	file_fleetspeak_src_server_proto_fleetspeak_server_server_proto_depIdxs = nil
}
