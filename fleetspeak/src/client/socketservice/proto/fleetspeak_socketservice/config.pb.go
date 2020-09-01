// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.25.0
// 	protoc        v3.8.0
// source: fleetspeak/src/client/socketservice/proto/fleetspeak_socketservice/config.proto

package fleetspeak_socketservice

import (
	proto "github.com/golang/protobuf/proto"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// This is a compile-time assertion that a sufficiently up-to-date version
// of the legacy proto package is being used.
const _ = proto.ProtoPackageIsVersion4

// The configuration information expected by socketservice.Factory in
// ClientServiceConfig.config.
type Config struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The given api_proxy_path may be an arbitrary filesystem path and will be
	// used to pair the daemon service with its non-child client process.
	//
	// On Unix in particular, a Unix socket will be created at this path and used
	// for communication between FS and the client.
	//
	// Side note: FS requires the proxy's parent directory's perms to be 0700.
	// If the parent directory doesn't exist, FS will mkdir -p it with perms set
	// to 0700.
	ApiProxyPath string `protobuf:"bytes,1,opt,name=api_proxy_path,json=apiProxyPath,proto3" json:"api_proxy_path,omitempty"`
	// By default, socket services report resource usage every 10 minutes. This
	// flag disables this if set.
	DisableResourceMonitoring bool `protobuf:"varint,2,opt,name=disable_resource_monitoring,json=disableResourceMonitoring,proto3" json:"disable_resource_monitoring,omitempty"`
	// How many samples to aggregate into a report when monitoring resource usage.
	// If unset, defaults to 20.
	ResourceMonitoringSampleSize int32 `protobuf:"varint,3,opt,name=resource_monitoring_sample_size,json=resourceMonitoringSampleSize,proto3" json:"resource_monitoring_sample_size,omitempty"`
	// How long to wait between resource monitoring samples. If unset, defaults to
	// 30 seconds.
	ResourceMonitoringSamplePeriodSeconds int32 `protobuf:"varint,4,opt,name=resource_monitoring_sample_period_seconds,json=resourceMonitoringSamplePeriodSeconds,proto3" json:"resource_monitoring_sample_period_seconds,omitempty"`
}

func (x *Config) Reset() {
	*x = Config{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Config) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Config) ProtoMessage() {}

func (x *Config) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Config.ProtoReflect.Descriptor instead.
func (*Config) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescGZIP(), []int{0}
}

func (x *Config) GetApiProxyPath() string {
	if x != nil {
		return x.ApiProxyPath
	}
	return ""
}

func (x *Config) GetDisableResourceMonitoring() bool {
	if x != nil {
		return x.DisableResourceMonitoring
	}
	return false
}

func (x *Config) GetResourceMonitoringSampleSize() int32 {
	if x != nil {
		return x.ResourceMonitoringSampleSize
	}
	return 0
}

func (x *Config) GetResourceMonitoringSamplePeriodSeconds() int32 {
	if x != nil {
		return x.ResourceMonitoringSamplePeriodSeconds
	}
	return 0
}

var File_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto protoreflect.FileDescriptor

var file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDesc = []byte{
	0x0a, 0x4f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x73, 0x6f, 0x63, 0x6b, 0x65, 0x74, 0x73, 0x65,
	0x72, 0x76, 0x69, 0x63, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c, 0x65, 0x65,
	0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x73, 0x6f, 0x63, 0x6b, 0x65, 0x74, 0x73, 0x65, 0x72,
	0x76, 0x69, 0x63, 0x65, 0x2f, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x12, 0x18, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x73, 0x6f,
	0x63, 0x6b, 0x65, 0x74, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x22, 0x8f, 0x02, 0x0a, 0x06,
	0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x12, 0x24, 0x0a, 0x0e, 0x61, 0x70, 0x69, 0x5f, 0x70, 0x72,
	0x6f, 0x78, 0x79, 0x5f, 0x70, 0x61, 0x74, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0c,
	0x61, 0x70, 0x69, 0x50, 0x72, 0x6f, 0x78, 0x79, 0x50, 0x61, 0x74, 0x68, 0x12, 0x3e, 0x0a, 0x1b,
	0x64, 0x69, 0x73, 0x61, 0x62, 0x6c, 0x65, 0x5f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65,
	0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x08, 0x52, 0x19, 0x64, 0x69, 0x73, 0x61, 0x62, 0x6c, 0x65, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72,
	0x63, 0x65, 0x4d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x12, 0x45, 0x0a, 0x1f,
	0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72,
	0x69, 0x6e, 0x67, 0x5f, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x5f, 0x73, 0x69, 0x7a, 0x65, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x1c, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x4d,
	0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x53, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x53,
	0x69, 0x7a, 0x65, 0x12, 0x58, 0x0a, 0x29, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f,
	0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x5f, 0x73, 0x61, 0x6d, 0x70, 0x6c,
	0x65, 0x5f, 0x70, 0x65, 0x72, 0x69, 0x6f, 0x64, 0x5f, 0x73, 0x65, 0x63, 0x6f, 0x6e, 0x64, 0x73,
	0x18, 0x04, 0x20, 0x01, 0x28, 0x05, 0x52, 0x25, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65,
	0x4d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x53, 0x61, 0x6d, 0x70, 0x6c, 0x65,
	0x50, 0x65, 0x72, 0x69, 0x6f, 0x64, 0x53, 0x65, 0x63, 0x6f, 0x6e, 0x64, 0x73, 0x62, 0x06, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescOnce sync.Once
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescData = file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDesc
)

func file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescGZIP() []byte {
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescOnce.Do(func() {
		file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescData = protoimpl.X.CompressGZIP(file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescData)
	})
	return file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDescData
}

var file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_goTypes = []interface{}{
	(*Config)(nil), // 0: fleetspeak.socketservice.Config
}
var file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_depIdxs = []int32{
	0, // [0:0] is the sub-list for method output_type
	0, // [0:0] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() {
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_init()
}
func file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_init() {
	if File_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Config); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_goTypes,
		DependencyIndexes: file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_depIdxs,
		MessageInfos:      file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_msgTypes,
	}.Build()
	File_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto = out.File
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_rawDesc = nil
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_goTypes = nil
	file_fleetspeak_src_client_socketservice_proto_fleetspeak_socketservice_config_proto_depIdxs = nil
}
