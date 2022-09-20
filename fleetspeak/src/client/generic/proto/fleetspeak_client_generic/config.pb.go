// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.28.1
// 	protoc        v3.19.4
// source: fleetspeak/src/client/generic/proto/fleetspeak_client_generic/config.proto

package fleetspeak_client_generic

import (
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

type Config struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// One or more PEM encoded certificates that the client should trust,
	// typically a CA certificate specific to the installation.
	TrustedCerts string `protobuf:"bytes,1,opt,name=trusted_certs,json=trustedCerts,proto3" json:"trusted_certs,omitempty"`
	// The servers that the client should attempt to connect to in <host>:<port>
	// format. E.g. "lazy.com:443", "10.0.0.5:1234"
	Server []string `protobuf:"bytes,2,rep,name=server,proto3" json:"server,omitempty"`
	// The client labels that this client should present to the server. Labels
	// indicating the client architecture and OS are automatically included.
	ClientLabel []string `protobuf:"bytes,3,rep,name=client_label,json=clientLabel,proto3" json:"client_label,omitempty"`
	// Types that are assignable to PersistenceHandler:
	//
	//	*Config_FilesystemHandler
	//	*Config_RegistryHandler
	PersistenceHandler isConfig_PersistenceHandler `protobuf_oneof:"persistence_handler"`
	// If set, the client will use long running persistent connections, otherwise
	// it will make regular short lived polls to the server. Recommended.
	Streaming bool `protobuf:"varint,6,opt,name=streaming,proto3" json:"streaming,omitempty"`
	// If provided, proxy used for connecting to the server.
	// The format is a URL.
	// See https://golang.org/pkg/net/http/#Transport.Proxy for details.
	Proxy string `protobuf:"bytes,7,opt,name=proxy,proto3" json:"proxy,omitempty"`
}

func (x *Config) Reset() {
	*x = Config{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Config) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Config) ProtoMessage() {}

func (x *Config) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[0]
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
	return file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescGZIP(), []int{0}
}

func (x *Config) GetTrustedCerts() string {
	if x != nil {
		return x.TrustedCerts
	}
	return ""
}

func (x *Config) GetServer() []string {
	if x != nil {
		return x.Server
	}
	return nil
}

func (x *Config) GetClientLabel() []string {
	if x != nil {
		return x.ClientLabel
	}
	return nil
}

func (m *Config) GetPersistenceHandler() isConfig_PersistenceHandler {
	if m != nil {
		return m.PersistenceHandler
	}
	return nil
}

func (x *Config) GetFilesystemHandler() *FilesystemHandler {
	if x, ok := x.GetPersistenceHandler().(*Config_FilesystemHandler); ok {
		return x.FilesystemHandler
	}
	return nil
}

func (x *Config) GetRegistryHandler() *RegistryHandler {
	if x, ok := x.GetPersistenceHandler().(*Config_RegistryHandler); ok {
		return x.RegistryHandler
	}
	return nil
}

func (x *Config) GetStreaming() bool {
	if x != nil {
		return x.Streaming
	}
	return false
}

func (x *Config) GetProxy() string {
	if x != nil {
		return x.Proxy
	}
	return ""
}

type isConfig_PersistenceHandler interface {
	isConfig_PersistenceHandler()
}

type Config_FilesystemHandler struct {
	FilesystemHandler *FilesystemHandler `protobuf:"bytes,4,opt,name=filesystem_handler,json=filesystemHandler,proto3,oneof"`
}

type Config_RegistryHandler struct {
	RegistryHandler *RegistryHandler `protobuf:"bytes,5,opt,name=registry_handler,json=registryHandler,proto3,oneof"`
}

func (*Config_FilesystemHandler) isConfig_PersistenceHandler() {}

func (*Config_RegistryHandler) isConfig_PersistenceHandler() {}

type FilesystemHandler struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Where to persist client state, see NewFilesystemPersistenceHandler for
	// details:
	//
	// https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/config#FilesystemPersistenceHandler
	ConfigurationDirectory string `protobuf:"bytes,1,opt,name=configuration_directory,json=configurationDirectory,proto3" json:"configuration_directory,omitempty"`
	StateFile              string `protobuf:"bytes,2,opt,name=state_file,json=stateFile,proto3" json:"state_file,omitempty"`
}

func (x *FilesystemHandler) Reset() {
	*x = FilesystemHandler{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *FilesystemHandler) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FilesystemHandler) ProtoMessage() {}

func (x *FilesystemHandler) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FilesystemHandler.ProtoReflect.Descriptor instead.
func (*FilesystemHandler) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescGZIP(), []int{1}
}

func (x *FilesystemHandler) GetConfigurationDirectory() string {
	if x != nil {
		return x.ConfigurationDirectory
	}
	return ""
}

func (x *FilesystemHandler) GetStateFile() string {
	if x != nil {
		return x.StateFile
	}
	return ""
}

type RegistryHandler struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Where to persist client state, see NewWindowsRegistryPersistenceHandler
	// for details:
	//
	// https://github.com/google/fleetspeak/blob/master/fleetspeak/src/client/config/windows_registry_persistence_handler.go
	ConfigurationKey string `protobuf:"bytes,1,opt,name=configuration_key,json=configurationKey,proto3" json:"configuration_key,omitempty"`
}

func (x *RegistryHandler) Reset() {
	*x = RegistryHandler{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *RegistryHandler) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RegistryHandler) ProtoMessage() {}

func (x *RegistryHandler) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RegistryHandler.ProtoReflect.Descriptor instead.
func (*RegistryHandler) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescGZIP(), []int{2}
}

func (x *RegistryHandler) GetConfigurationKey() string {
	if x != nil {
		return x.ConfigurationKey
	}
	return ""
}

var File_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto protoreflect.FileDescriptor

var file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDesc = []byte{
	0x0a, 0x4a, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63, 0x2f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b,
	0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x5f, 0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63, 0x2f,
	0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x19, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2e,
	0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63, 0x22, 0xeb, 0x02, 0x0a, 0x06, 0x43, 0x6f, 0x6e, 0x66,
	0x69, 0x67, 0x12, 0x23, 0x0a, 0x0d, 0x74, 0x72, 0x75, 0x73, 0x74, 0x65, 0x64, 0x5f, 0x63, 0x65,
	0x72, 0x74, 0x73, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0c, 0x74, 0x72, 0x75, 0x73, 0x74,
	0x65, 0x64, 0x43, 0x65, 0x72, 0x74, 0x73, 0x12, 0x16, 0x0a, 0x06, 0x73, 0x65, 0x72, 0x76, 0x65,
	0x72, 0x18, 0x02, 0x20, 0x03, 0x28, 0x09, 0x52, 0x06, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x12,
	0x21, 0x0a, 0x0c, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x5f, 0x6c, 0x61, 0x62, 0x65, 0x6c, 0x18,
	0x03, 0x20, 0x03, 0x28, 0x09, 0x52, 0x0b, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x4c, 0x61, 0x62,
	0x65, 0x6c, 0x12, 0x5d, 0x0a, 0x12, 0x66, 0x69, 0x6c, 0x65, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d,
	0x5f, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x72, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2c,
	0x2e, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x63, 0x6c, 0x69, 0x65,
	0x6e, 0x74, 0x2e, 0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63, 0x2e, 0x46, 0x69, 0x6c, 0x65, 0x73,
	0x79, 0x73, 0x74, 0x65, 0x6d, 0x48, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x72, 0x48, 0x00, 0x52, 0x11,
	0x66, 0x69, 0x6c, 0x65, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x48, 0x61, 0x6e, 0x64, 0x6c, 0x65,
	0x72, 0x12, 0x57, 0x0a, 0x10, 0x72, 0x65, 0x67, 0x69, 0x73, 0x74, 0x72, 0x79, 0x5f, 0x68, 0x61,
	0x6e, 0x64, 0x6c, 0x65, 0x72, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x2a, 0x2e, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2e,
	0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63, 0x2e, 0x52, 0x65, 0x67, 0x69, 0x73, 0x74, 0x72, 0x79,
	0x48, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x72, 0x48, 0x00, 0x52, 0x0f, 0x72, 0x65, 0x67, 0x69, 0x73,
	0x74, 0x72, 0x79, 0x48, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x72, 0x12, 0x1c, 0x0a, 0x09, 0x73, 0x74,
	0x72, 0x65, 0x61, 0x6d, 0x69, 0x6e, 0x67, 0x18, 0x06, 0x20, 0x01, 0x28, 0x08, 0x52, 0x09, 0x73,
	0x74, 0x72, 0x65, 0x61, 0x6d, 0x69, 0x6e, 0x67, 0x12, 0x14, 0x0a, 0x05, 0x70, 0x72, 0x6f, 0x78,
	0x79, 0x18, 0x07, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x42, 0x15,
	0x0a, 0x13, 0x70, 0x65, 0x72, 0x73, 0x69, 0x73, 0x74, 0x65, 0x6e, 0x63, 0x65, 0x5f, 0x68, 0x61,
	0x6e, 0x64, 0x6c, 0x65, 0x72, 0x22, 0x6b, 0x0a, 0x11, 0x46, 0x69, 0x6c, 0x65, 0x73, 0x79, 0x73,
	0x74, 0x65, 0x6d, 0x48, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x72, 0x12, 0x37, 0x0a, 0x17, 0x63, 0x6f,
	0x6e, 0x66, 0x69, 0x67, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x64, 0x69, 0x72, 0x65,
	0x63, 0x74, 0x6f, 0x72, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x16, 0x63, 0x6f, 0x6e,
	0x66, 0x69, 0x67, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74,
	0x6f, 0x72, 0x79, 0x12, 0x1d, 0x0a, 0x0a, 0x73, 0x74, 0x61, 0x74, 0x65, 0x5f, 0x66, 0x69, 0x6c,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x73, 0x74, 0x61, 0x74, 0x65, 0x46, 0x69,
	0x6c, 0x65, 0x22, 0x3e, 0x0a, 0x0f, 0x52, 0x65, 0x67, 0x69, 0x73, 0x74, 0x72, 0x79, 0x48, 0x61,
	0x6e, 0x64, 0x6c, 0x65, 0x72, 0x12, 0x2b, 0x0a, 0x11, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75,
	0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x10, 0x63, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x4b,
	0x65, 0x79, 0x42, 0x5c, 0x5a, 0x5a, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d,
	0x2f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65,
	0x61, 0x6b, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72,
	0x63, 0x2f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63,
	0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61,
	0x6b, 0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x5f, 0x67, 0x65, 0x6e, 0x65, 0x72, 0x69, 0x63,
	0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescOnce sync.Once
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescData = file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDesc
)

func file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescGZIP() []byte {
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescOnce.Do(func() {
		file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescData = protoimpl.X.CompressGZIP(file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescData)
	})
	return file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDescData
}

var file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_goTypes = []interface{}{
	(*Config)(nil),            // 0: fleetspeak.client.generic.Config
	(*FilesystemHandler)(nil), // 1: fleetspeak.client.generic.FilesystemHandler
	(*RegistryHandler)(nil),   // 2: fleetspeak.client.generic.RegistryHandler
}
var file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_depIdxs = []int32{
	1, // 0: fleetspeak.client.generic.Config.filesystem_handler:type_name -> fleetspeak.client.generic.FilesystemHandler
	2, // 1: fleetspeak.client.generic.Config.registry_handler:type_name -> fleetspeak.client.generic.RegistryHandler
	2, // [2:2] is the sub-list for method output_type
	2, // [2:2] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_init() }
func file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_init() {
	if File_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
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
		file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*FilesystemHandler); i {
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
		file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*RegistryHandler); i {
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
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes[0].OneofWrappers = []interface{}{
		(*Config_FilesystemHandler)(nil),
		(*Config_RegistryHandler)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_goTypes,
		DependencyIndexes: file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_depIdxs,
		MessageInfos:      file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_msgTypes,
	}.Build()
	File_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto = out.File
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_rawDesc = nil
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_goTypes = nil
	file_fleetspeak_src_client_generic_proto_fleetspeak_client_generic_config_proto_depIdxs = nil
}
