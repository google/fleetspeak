// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.34.1
// 	protoc        v5.26.1
// source: fleetspeak/src/common/proto/fleetspeak_monitoring/resource.proto

package fleetspeak_monitoring

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type KillNotification_Reason int32

const (
	KillNotification_UNSPECIFIED       KillNotification_Reason = 0
	KillNotification_HEARTBEAT_FAILURE KillNotification_Reason = 1
	KillNotification_MEMORY_EXCEEDED   KillNotification_Reason = 2
)

// Enum value maps for KillNotification_Reason.
var (
	KillNotification_Reason_name = map[int32]string{
		0: "UNSPECIFIED",
		1: "HEARTBEAT_FAILURE",
		2: "MEMORY_EXCEEDED",
	}
	KillNotification_Reason_value = map[string]int32{
		"UNSPECIFIED":       0,
		"HEARTBEAT_FAILURE": 1,
		"MEMORY_EXCEEDED":   2,
	}
)

func (x KillNotification_Reason) Enum() *KillNotification_Reason {
	p := new(KillNotification_Reason)
	*p = x
	return p
}

func (x KillNotification_Reason) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (KillNotification_Reason) Descriptor() protoreflect.EnumDescriptor {
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_enumTypes[0].Descriptor()
}

func (KillNotification_Reason) Type() protoreflect.EnumType {
	return &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_enumTypes[0]
}

func (x KillNotification_Reason) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use KillNotification_Reason.Descriptor instead.
func (KillNotification_Reason) EnumDescriptor() ([]byte, []int) {
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescGZIP(), []int{2, 0}
}

// Contains resource-usage metrics for Fleetspeak clients. The stats are
// arrived at by aggregating raw data retrieved from the OS.
// CPU-usage is in milliseconds per second, and memory usage is in bytes.
type AggregatedResourceUsage struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	MeanUserCpuRate    float64 `protobuf:"fixed64,1,opt,name=mean_user_cpu_rate,json=meanUserCpuRate,proto3" json:"mean_user_cpu_rate,omitempty"`
	MaxUserCpuRate     float64 `protobuf:"fixed64,2,opt,name=max_user_cpu_rate,json=maxUserCpuRate,proto3" json:"max_user_cpu_rate,omitempty"`
	MeanSystemCpuRate  float64 `protobuf:"fixed64,3,opt,name=mean_system_cpu_rate,json=meanSystemCpuRate,proto3" json:"mean_system_cpu_rate,omitempty"`
	MaxSystemCpuRate   float64 `protobuf:"fixed64,4,opt,name=max_system_cpu_rate,json=maxSystemCpuRate,proto3" json:"max_system_cpu_rate,omitempty"`
	MeanResidentMemory float64 `protobuf:"fixed64,5,opt,name=mean_resident_memory,json=meanResidentMemory,proto3" json:"mean_resident_memory,omitempty"`
	MaxResidentMemory  int64   `protobuf:"varint,6,opt,name=max_resident_memory,json=maxResidentMemory,proto3" json:"max_resident_memory,omitempty"`
	MaxNumFds          int32   `protobuf:"varint,7,opt,name=max_num_fds,json=maxNumFds,proto3" json:"max_num_fds,omitempty"`
	MeanNumFds         float64 `protobuf:"fixed64,8,opt,name=mean_num_fds,json=meanNumFds,proto3" json:"mean_num_fds,omitempty"`
}

func (x *AggregatedResourceUsage) Reset() {
	*x = AggregatedResourceUsage{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AggregatedResourceUsage) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AggregatedResourceUsage) ProtoMessage() {}

func (x *AggregatedResourceUsage) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AggregatedResourceUsage.ProtoReflect.Descriptor instead.
func (*AggregatedResourceUsage) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescGZIP(), []int{0}
}

func (x *AggregatedResourceUsage) GetMeanUserCpuRate() float64 {
	if x != nil {
		return x.MeanUserCpuRate
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMaxUserCpuRate() float64 {
	if x != nil {
		return x.MaxUserCpuRate
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMeanSystemCpuRate() float64 {
	if x != nil {
		return x.MeanSystemCpuRate
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMaxSystemCpuRate() float64 {
	if x != nil {
		return x.MaxSystemCpuRate
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMeanResidentMemory() float64 {
	if x != nil {
		return x.MeanResidentMemory
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMaxResidentMemory() int64 {
	if x != nil {
		return x.MaxResidentMemory
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMaxNumFds() int32 {
	if x != nil {
		return x.MaxNumFds
	}
	return 0
}

func (x *AggregatedResourceUsage) GetMeanNumFds() float64 {
	if x != nil {
		return x.MeanNumFds
	}
	return 0
}

// A fleetspeak.Message with message type "ResourceUsage" is sent regularly by
// the system and daemon services to the server, to report the performance of
// processes.
//
// Next tag: 9
type ResourceUsageData struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Name of the client service that resource usage is charged/attributed to
	// e.g 'system' for the system Fleetspeak service, or the name of a daemon
	// service as specified in its config.
	Scope string `protobuf:"bytes,1,opt,name=scope,proto3" json:"scope,omitempty"`
	Pid   int64  `protobuf:"varint,2,opt,name=pid,proto3" json:"pid,omitempty"`
	// The self reported service/service binary version.
	Version string `protobuf:"bytes,8,opt,name=version,proto3" json:"version,omitempty"`
	// Time when the process was started by Fleetspeak.
	ProcessStartTime *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=process_start_time,json=processStartTime,proto3" json:"process_start_time,omitempty"`
	// Corresponds to when computation of the resource-usage data was finalized.
	DataTimestamp *timestamppb.Timestamp   `protobuf:"bytes,4,opt,name=data_timestamp,json=dataTimestamp,proto3" json:"data_timestamp,omitempty"`
	ResourceUsage *AggregatedResourceUsage `protobuf:"bytes,5,opt,name=resource_usage,json=resourceUsage,proto3" json:"resource_usage,omitempty"`
	// Optional debug info for the process.
	DebugStatus string `protobuf:"bytes,6,opt,name=debug_status,json=debugStatus,proto3" json:"debug_status,omitempty"`
	// If true, indicates that the process has terminated, and that this is
	// the final resource-usage report for that process.
	ProcessTerminated bool `protobuf:"varint,7,opt,name=process_terminated,json=processTerminated,proto3" json:"process_terminated,omitempty"`
}

func (x *ResourceUsageData) Reset() {
	*x = ResourceUsageData{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ResourceUsageData) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ResourceUsageData) ProtoMessage() {}

func (x *ResourceUsageData) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ResourceUsageData.ProtoReflect.Descriptor instead.
func (*ResourceUsageData) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescGZIP(), []int{1}
}

func (x *ResourceUsageData) GetScope() string {
	if x != nil {
		return x.Scope
	}
	return ""
}

func (x *ResourceUsageData) GetPid() int64 {
	if x != nil {
		return x.Pid
	}
	return 0
}

func (x *ResourceUsageData) GetVersion() string {
	if x != nil {
		return x.Version
	}
	return ""
}

func (x *ResourceUsageData) GetProcessStartTime() *timestamppb.Timestamp {
	if x != nil {
		return x.ProcessStartTime
	}
	return nil
}

func (x *ResourceUsageData) GetDataTimestamp() *timestamppb.Timestamp {
	if x != nil {
		return x.DataTimestamp
	}
	return nil
}

func (x *ResourceUsageData) GetResourceUsage() *AggregatedResourceUsage {
	if x != nil {
		return x.ResourceUsage
	}
	return nil
}

func (x *ResourceUsageData) GetDebugStatus() string {
	if x != nil {
		return x.DebugStatus
	}
	return ""
}

func (x *ResourceUsageData) GetProcessTerminated() bool {
	if x != nil {
		return x.ProcessTerminated
	}
	return false
}

// Sent by clients when a service gets killed by Fleetspeak, e.g. for using
// too much memory.
type KillNotification struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Service string `protobuf:"bytes,1,opt,name=service,proto3" json:"service,omitempty"`
	Pid     int64  `protobuf:"varint,2,opt,name=pid,proto3" json:"pid,omitempty"`
	// The self-reported version of the service.
	Version string `protobuf:"bytes,3,opt,name=version,proto3" json:"version,omitempty"`
	// Time when the process was started by Fleetspeak.
	ProcessStartTime *timestamppb.Timestamp `protobuf:"bytes,4,opt,name=process_start_time,json=processStartTime,proto3" json:"process_start_time,omitempty"`
	// Time when the process was killed by Fleetspeak.
	KilledWhen *timestamppb.Timestamp  `protobuf:"bytes,5,opt,name=killed_when,json=killedWhen,proto3" json:"killed_when,omitempty"`
	Reason     KillNotification_Reason `protobuf:"varint,6,opt,name=reason,proto3,enum=fleetspeak.monitoring.KillNotification_Reason" json:"reason,omitempty"`
}

func (x *KillNotification) Reset() {
	*x = KillNotification{}
	if protoimpl.UnsafeEnabled {
		mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *KillNotification) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*KillNotification) ProtoMessage() {}

func (x *KillNotification) ProtoReflect() protoreflect.Message {
	mi := &file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use KillNotification.ProtoReflect.Descriptor instead.
func (*KillNotification) Descriptor() ([]byte, []int) {
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescGZIP(), []int{2}
}

func (x *KillNotification) GetService() string {
	if x != nil {
		return x.Service
	}
	return ""
}

func (x *KillNotification) GetPid() int64 {
	if x != nil {
		return x.Pid
	}
	return 0
}

func (x *KillNotification) GetVersion() string {
	if x != nil {
		return x.Version
	}
	return ""
}

func (x *KillNotification) GetProcessStartTime() *timestamppb.Timestamp {
	if x != nil {
		return x.ProcessStartTime
	}
	return nil
}

func (x *KillNotification) GetKilledWhen() *timestamppb.Timestamp {
	if x != nil {
		return x.KilledWhen
	}
	return nil
}

func (x *KillNotification) GetReason() KillNotification_Reason {
	if x != nil {
		return x.Reason
	}
	return KillNotification_UNSPECIFIED
}

var File_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto protoreflect.FileDescriptor

var file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDesc = []byte{
	0x0a, 0x40, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72,
	0x69, 0x6e, 0x67, 0x2f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x12, 0x15, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x6d,
	0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x1a, 0x1f, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x74, 0x69, 0x6d, 0x65, 0x73,
	0x74, 0x61, 0x6d, 0x70, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xf5, 0x02, 0x0a, 0x17, 0x41,
	0x67, 0x67, 0x72, 0x65, 0x67, 0x61, 0x74, 0x65, 0x64, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63,
	0x65, 0x55, 0x73, 0x61, 0x67, 0x65, 0x12, 0x2b, 0x0a, 0x12, 0x6d, 0x65, 0x61, 0x6e, 0x5f, 0x75,
	0x73, 0x65, 0x72, 0x5f, 0x63, 0x70, 0x75, 0x5f, 0x72, 0x61, 0x74, 0x65, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x01, 0x52, 0x0f, 0x6d, 0x65, 0x61, 0x6e, 0x55, 0x73, 0x65, 0x72, 0x43, 0x70, 0x75, 0x52,
	0x61, 0x74, 0x65, 0x12, 0x29, 0x0a, 0x11, 0x6d, 0x61, 0x78, 0x5f, 0x75, 0x73, 0x65, 0x72, 0x5f,
	0x63, 0x70, 0x75, 0x5f, 0x72, 0x61, 0x74, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x01, 0x52, 0x0e,
	0x6d, 0x61, 0x78, 0x55, 0x73, 0x65, 0x72, 0x43, 0x70, 0x75, 0x52, 0x61, 0x74, 0x65, 0x12, 0x2f,
	0x0a, 0x14, 0x6d, 0x65, 0x61, 0x6e, 0x5f, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x5f, 0x63, 0x70,
	0x75, 0x5f, 0x72, 0x61, 0x74, 0x65, 0x18, 0x03, 0x20, 0x01, 0x28, 0x01, 0x52, 0x11, 0x6d, 0x65,
	0x61, 0x6e, 0x53, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x43, 0x70, 0x75, 0x52, 0x61, 0x74, 0x65, 0x12,
	0x2d, 0x0a, 0x13, 0x6d, 0x61, 0x78, 0x5f, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x5f, 0x63, 0x70,
	0x75, 0x5f, 0x72, 0x61, 0x74, 0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x01, 0x52, 0x10, 0x6d, 0x61,
	0x78, 0x53, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x43, 0x70, 0x75, 0x52, 0x61, 0x74, 0x65, 0x12, 0x30,
	0x0a, 0x14, 0x6d, 0x65, 0x61, 0x6e, 0x5f, 0x72, 0x65, 0x73, 0x69, 0x64, 0x65, 0x6e, 0x74, 0x5f,
	0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x18, 0x05, 0x20, 0x01, 0x28, 0x01, 0x52, 0x12, 0x6d, 0x65,
	0x61, 0x6e, 0x52, 0x65, 0x73, 0x69, 0x64, 0x65, 0x6e, 0x74, 0x4d, 0x65, 0x6d, 0x6f, 0x72, 0x79,
	0x12, 0x2e, 0x0a, 0x13, 0x6d, 0x61, 0x78, 0x5f, 0x72, 0x65, 0x73, 0x69, 0x64, 0x65, 0x6e, 0x74,
	0x5f, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x18, 0x06, 0x20, 0x01, 0x28, 0x03, 0x52, 0x11, 0x6d,
	0x61, 0x78, 0x52, 0x65, 0x73, 0x69, 0x64, 0x65, 0x6e, 0x74, 0x4d, 0x65, 0x6d, 0x6f, 0x72, 0x79,
	0x12, 0x1e, 0x0a, 0x0b, 0x6d, 0x61, 0x78, 0x5f, 0x6e, 0x75, 0x6d, 0x5f, 0x66, 0x64, 0x73, 0x18,
	0x07, 0x20, 0x01, 0x28, 0x05, 0x52, 0x09, 0x6d, 0x61, 0x78, 0x4e, 0x75, 0x6d, 0x46, 0x64, 0x73,
	0x12, 0x20, 0x0a, 0x0c, 0x6d, 0x65, 0x61, 0x6e, 0x5f, 0x6e, 0x75, 0x6d, 0x5f, 0x66, 0x64, 0x73,
	0x18, 0x08, 0x20, 0x01, 0x28, 0x01, 0x52, 0x0a, 0x6d, 0x65, 0x61, 0x6e, 0x4e, 0x75, 0x6d, 0x46,
	0x64, 0x73, 0x22, 0x8b, 0x03, 0x0a, 0x11, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x55,
	0x73, 0x61, 0x67, 0x65, 0x44, 0x61, 0x74, 0x61, 0x12, 0x14, 0x0a, 0x05, 0x73, 0x63, 0x6f, 0x70,
	0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x73, 0x63, 0x6f, 0x70, 0x65, 0x12, 0x10,
	0x0a, 0x03, 0x70, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x03, 0x52, 0x03, 0x70, 0x69, 0x64,
	0x12, 0x18, 0x0a, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x08, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x48, 0x0a, 0x12, 0x70, 0x72,
	0x6f, 0x63, 0x65, 0x73, 0x73, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x74, 0x69, 0x6d, 0x65,
	0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61,
	0x6d, 0x70, 0x52, 0x10, 0x70, 0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x53, 0x74, 0x61, 0x72, 0x74,
	0x54, 0x69, 0x6d, 0x65, 0x12, 0x41, 0x0a, 0x0e, 0x64, 0x61, 0x74, 0x61, 0x5f, 0x74, 0x69, 0x6d,
	0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54,
	0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x0d, 0x64, 0x61, 0x74, 0x61, 0x54, 0x69,
	0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x12, 0x55, 0x0a, 0x0e, 0x72, 0x65, 0x73, 0x6f, 0x75,
	0x72, 0x63, 0x65, 0x5f, 0x75, 0x73, 0x61, 0x67, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32,
	0x2e, 0x2e, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e, 0x6d, 0x6f, 0x6e,
	0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x2e, 0x41, 0x67, 0x67, 0x72, 0x65, 0x67, 0x61, 0x74,
	0x65, 0x64, 0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x55, 0x73, 0x61, 0x67, 0x65, 0x52,
	0x0d, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x55, 0x73, 0x61, 0x67, 0x65, 0x12, 0x21,
	0x0a, 0x0c, 0x64, 0x65, 0x62, 0x75, 0x67, 0x5f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x18, 0x06,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x64, 0x65, 0x62, 0x75, 0x67, 0x53, 0x74, 0x61, 0x74, 0x75,
	0x73, 0x12, 0x2d, 0x0a, 0x12, 0x70, 0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x5f, 0x74, 0x65, 0x72,
	0x6d, 0x69, 0x6e, 0x61, 0x74, 0x65, 0x64, 0x18, 0x07, 0x20, 0x01, 0x28, 0x08, 0x52, 0x11, 0x70,
	0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x54, 0x65, 0x72, 0x6d, 0x69, 0x6e, 0x61, 0x74, 0x65, 0x64,
	0x22, 0xee, 0x02, 0x0a, 0x10, 0x4b, 0x69, 0x6c, 0x6c, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x18, 0x0a, 0x07, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12,
	0x10, 0x0a, 0x03, 0x70, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x03, 0x52, 0x03, 0x70, 0x69,
	0x64, 0x12, 0x18, 0x0a, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x03, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x48, 0x0a, 0x12, 0x70,
	0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x5f, 0x74, 0x69, 0x6d,
	0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74,
	0x61, 0x6d, 0x70, 0x52, 0x10, 0x70, 0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x53, 0x74, 0x61, 0x72,
	0x74, 0x54, 0x69, 0x6d, 0x65, 0x12, 0x3b, 0x0a, 0x0b, 0x6b, 0x69, 0x6c, 0x6c, 0x65, 0x64, 0x5f,
	0x77, 0x68, 0x65, 0x6e, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f,
	0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d,
	0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x6b, 0x69, 0x6c, 0x6c, 0x65, 0x64, 0x57, 0x68,
	0x65, 0x6e, 0x12, 0x46, 0x0a, 0x06, 0x72, 0x65, 0x61, 0x73, 0x6f, 0x6e, 0x18, 0x06, 0x20, 0x01,
	0x28, 0x0e, 0x32, 0x2e, 0x2e, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2e,
	0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x69, 0x6e, 0x67, 0x2e, 0x4b, 0x69, 0x6c, 0x6c, 0x4e,
	0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x52, 0x65, 0x61, 0x73,
	0x6f, 0x6e, 0x52, 0x06, 0x72, 0x65, 0x61, 0x73, 0x6f, 0x6e, 0x22, 0x45, 0x0a, 0x06, 0x52, 0x65,
	0x61, 0x73, 0x6f, 0x6e, 0x12, 0x0f, 0x0a, 0x0b, 0x55, 0x4e, 0x53, 0x50, 0x45, 0x43, 0x49, 0x46,
	0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x15, 0x0a, 0x11, 0x48, 0x45, 0x41, 0x52, 0x54, 0x42, 0x45,
	0x41, 0x54, 0x5f, 0x46, 0x41, 0x49, 0x4c, 0x55, 0x52, 0x45, 0x10, 0x01, 0x12, 0x13, 0x0a, 0x0f,
	0x4d, 0x45, 0x4d, 0x4f, 0x52, 0x59, 0x5f, 0x45, 0x58, 0x43, 0x45, 0x45, 0x44, 0x45, 0x44, 0x10,
	0x02, 0x42, 0x50, 0x5a, 0x4e, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f,
	0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61,
	0x6b, 0x2f, 0x66, 0x6c, 0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x2f, 0x73, 0x72, 0x63,
	0x2f, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x6c,
	0x65, 0x65, 0x74, 0x73, 0x70, 0x65, 0x61, 0x6b, 0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72,
	0x69, 0x6e, 0x67, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescOnce sync.Once
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescData = file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDesc
)

func file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescGZIP() []byte {
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescOnce.Do(func() {
		file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescData = protoimpl.X.CompressGZIP(file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescData)
	})
	return file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDescData
}

var file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_goTypes = []interface{}{
	(KillNotification_Reason)(0),    // 0: fleetspeak.monitoring.KillNotification.Reason
	(*AggregatedResourceUsage)(nil), // 1: fleetspeak.monitoring.AggregatedResourceUsage
	(*ResourceUsageData)(nil),       // 2: fleetspeak.monitoring.ResourceUsageData
	(*KillNotification)(nil),        // 3: fleetspeak.monitoring.KillNotification
	(*timestamppb.Timestamp)(nil),   // 4: google.protobuf.Timestamp
}
var file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_depIdxs = []int32{
	4, // 0: fleetspeak.monitoring.ResourceUsageData.process_start_time:type_name -> google.protobuf.Timestamp
	4, // 1: fleetspeak.monitoring.ResourceUsageData.data_timestamp:type_name -> google.protobuf.Timestamp
	1, // 2: fleetspeak.monitoring.ResourceUsageData.resource_usage:type_name -> fleetspeak.monitoring.AggregatedResourceUsage
	4, // 3: fleetspeak.monitoring.KillNotification.process_start_time:type_name -> google.protobuf.Timestamp
	4, // 4: fleetspeak.monitoring.KillNotification.killed_when:type_name -> google.protobuf.Timestamp
	0, // 5: fleetspeak.monitoring.KillNotification.reason:type_name -> fleetspeak.monitoring.KillNotification.Reason
	6, // [6:6] is the sub-list for method output_type
	6, // [6:6] is the sub-list for method input_type
	6, // [6:6] is the sub-list for extension type_name
	6, // [6:6] is the sub-list for extension extendee
	0, // [0:6] is the sub-list for field type_name
}

func init() { file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_init() }
func file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_init() {
	if File_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AggregatedResourceUsage); i {
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
		file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ResourceUsageData); i {
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
		file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*KillNotification); i {
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
			RawDescriptor: file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_goTypes,
		DependencyIndexes: file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_depIdxs,
		EnumInfos:         file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_enumTypes,
		MessageInfos:      file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_msgTypes,
	}.Build()
	File_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto = out.File
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_rawDesc = nil
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_goTypes = nil
	file_fleetspeak_src_common_proto_fleetspeak_monitoring_resource_proto_depIdxs = nil
}
