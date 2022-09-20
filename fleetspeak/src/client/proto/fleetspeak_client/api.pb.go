// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.28.1
// 	protoc        v3.19.4
// source: fleetspeak/src/client/proto/fleetspeak_client/api.proto

package fleetspeak_client

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	anypb "google.golang.org/protobuf/types/known/anypb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type ByteBlob struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

func (x *ByteBlob) Reset() {
	*x = ByteBlob{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ByteBlob) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ByteBlob) ProtoMessage() {}

func (x *ByteBlob) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ByteBlob.ProtoReflect.Descriptor instead.
func (*ByteBlob) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescGZIP(), []int{0}
}

func (x *ByteBlob) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

type APIMessage struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Type string     `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	Data *anypb.Any `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
}

func (x *APIMessage) Reset() {
	*x = APIMessage{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *APIMessage) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*APIMessage) ProtoMessage() {}

func (x *APIMessage) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use APIMessage.ProtoReflect.Descriptor instead.
func (*APIMessage) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescGZIP(), []int{1}
}

func (x *APIMessage) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

func (x *APIMessage) GetData() *anypb.Any {
	if x != nil {
		return x.Data
	}
	return nil
}

var File_fleetspeak_src_client_proto_fleetspeak_client_api_proto protoreflect.FileDescriptor

var file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDesc = []byte{
	0x0a, 0x37, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f,
	0x61, 0x70, 0x69, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x11, 0x66, 0x6c, 0x65, 0x65, 0x74,
	0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x1a, 0x19, 0x67, 0x6f,
	0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x61, 0x6e,
	0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x1e, 0x0a, 0x08, 0x42, 0x79, 0x74, 0x65, 0x42,
	0x6c, 0x6f, 0x62, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x22, 0x4a, 0x0a, 0x0a, 0x41, 0x50, 0x49, 0x4d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x74, 0x79, 0x70, 0x65, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x04, 0x74, 0x79, 0x70, 0x65, 0x12, 0x28, 0x0a, 0x04, 0x64, 0x61, 0x74,
	0x61, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e, 0x79, 0x52, 0x04, 0x64,
	0x61, 0x74, 0x61, 0x42, 0x4c, 0x5a, 0x4a, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f,
	0x6d, 0x2f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70,
	0x65, 0x61, 0x6b, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73,
	0x72, 0x63, 0x2f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f,
	0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e,
	0x74, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescOnce sync.Once
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescData = file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDesc
)

func file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescGZIP() []byte {
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescOnce.Do(func() {
		file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescData = protoimpl.X.CompressGZIP(file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescData)
	})
	return file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDescData
}

var file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_goTypes = []interface{}{
	(*ByteBlob)(nil),   // 0: fleetspeak.client.ByteBlob
	(*APIMessage)(nil), // 1: fleetspeak.client.APIMessage
	(*anypb.Any)(nil),  // 2: google.protobuf.Any
}
var file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_depIdxs = []int32{
	2, // 0: fleetspeak.client.APIMessage.data:type_name -> google.protobuf.Any
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_init() }
func file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_init() {
	if File_fleetspeak_src_client_proto_fleetspeak_client_api_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ByteBlob); i {
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
		file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*APIMessage); i {
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
			RawDescriptor: file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_goTypes,
		DependencyIndexes: file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_depIdxs,
		MessageInfos:      file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_msgTypes,
	}.Build()
	File_fleetspeak_src_client_proto_fleetspeak_client_api_proto = out.File
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_rawDesc = nil
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_goTypes = nil
	file_fleetspeak_src_client_proto_fleetspeak_client_api_proto_depIdxs = nil
}
