// Code generated by protoc-gen-go. DO NOT EDIT.
// source: fleetspeak/src/config/proto/fleetspeak_config/config.proto

package fleetspeak_config

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	fleetspeak_components "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

// The configuration parameters needed by the configuration tool in order to
// create the artifacts needed to run a fleetspeak installation.
type Config struct {
	// An name for this installation, e.g. "Nascent" or "Nascent Staging".
	// Required.
	ConfigurationName string `protobuf:"bytes,1,opt,name=configuration_name,json=configurationName,proto3" json:"configuration_name,omitempty"`
	// A template for the components configuration file that will be generated.
	// The configuration tool will populate the https_config.key and
	// https_config.certificates fields based on the parameters below.
	ComponentsConfig *fleetspeak_components.Config `protobuf:"bytes,2,opt,name=components_config,json=componentsConfig,proto3" json:"components_config,omitempty"`
	// A file containing a PEM encoded certificate that clients should be
	// configured to trust. Typically a CA cert. If this file is not already
	// present, a 10 year self-signed CA certificate and associated private key
	// will be created.
	TrustedCertFile string `protobuf:"bytes,3,opt,name=trusted_cert_file,json=trustedCertFile,proto3" json:"trusted_cert_file,omitempty"`
	// A file containing the private key associated with trusted_cert_file, only
	// required if it is necessary to create server certificates.
	//
	// NOTE: Contains private key material. Only needs to be online when creating new
	// server certificates.
	TrustedCertKeyFile string `protobuf:"bytes,4,opt,name=trusted_cert_key_file,json=trustedCertKeyFile,proto3" json:"trusted_cert_key_file,omitempty"`
	// A file containing a PEM encoded certificate that the Fleetspeak server
	// should use to identify itself. If this file is not already present, a 1
	// year certificate signed directly using the contents of
	// trusted_cert_?(key_)file will be created.
	ServerCertFile string `protobuf:"bytes,5,opt,name=server_cert_file,json=serverCertFile,proto3" json:"server_cert_file,omitempty"`
	// A file containing the private key associated with
	// server_cert_file. Required.
	//
	// NOTE: Contains private key material.
	ServerCertKeyFile string `protobuf:"bytes,6,opt,name=server_cert_key_file,json=serverCertKeyFile,proto3" json:"server_cert_key_file,omitempty"`
	// Where to write the fleetspeak server component configuration file.
	//
	// NOTE: Result will contain private key material. Will only be needed by
	// fleetspeak servers.
	ServerComponentConfigurationFile string `protobuf:"bytes,7,opt,name=server_component_configuration_file,json=serverComponentConfigurationFile,proto3" json:"server_component_configuration_file,omitempty"`
	// How clients should find the fleetspeak server(s) used by this installation.
	//
	// Each entry should be of the form "<ip/hostname>:<port>". Note the clients
	// will not perform any intelligent load balancing, rather they will continue
	// to use the first option which works for them.
	//
	// If you are creating your own server certificates, they will need to cover
	// these addresses.
	PublicHostPort []string `protobuf:"bytes,8,rep,name=public_host_port,json=publicHostPort,proto3" json:"public_host_port,omitempty"`
	// If set, write a linux client configuration file for this installation.
	LinuxClientConfigurationFile string `protobuf:"bytes,9,opt,name=linux_client_configuration_file,json=linuxClientConfigurationFile,proto3" json:"linux_client_configuration_file,omitempty"`
	// If set, write a linux client configuration file for this installation.
	DarwinClientConfigurationFile string `protobuf:"bytes,10,opt,name=darwin_client_configuration_file,json=darwinClientConfigurationFile,proto3" json:"darwin_client_configuration_file,omitempty"`
	// If set, write a linux client configuration file for this installation.
	WindowsClientConfigurationFile string   `protobuf:"bytes,11,opt,name=windows_client_configuration_file,json=windowsClientConfigurationFile,proto3" json:"windows_client_configuration_file,omitempty"`
	XXX_NoUnkeyedLiteral           struct{} `json:"-"`
	XXX_unrecognized               []byte   `json:"-"`
	XXX_sizecache                  int32    `json:"-"`
}

func (m *Config) Reset()         { *m = Config{} }
func (m *Config) String() string { return proto.CompactTextString(m) }
func (*Config) ProtoMessage()    {}
func (*Config) Descriptor() ([]byte, []int) {
	return fileDescriptor_6f66be8faebf9149, []int{0}
}

func (m *Config) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Config.Unmarshal(m, b)
}
func (m *Config) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Config.Marshal(b, m, deterministic)
}
func (m *Config) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Config.Merge(m, src)
}
func (m *Config) XXX_Size() int {
	return xxx_messageInfo_Config.Size(m)
}
func (m *Config) XXX_DiscardUnknown() {
	xxx_messageInfo_Config.DiscardUnknown(m)
}

var xxx_messageInfo_Config proto.InternalMessageInfo

func (m *Config) GetConfigurationName() string {
	if m != nil {
		return m.ConfigurationName
	}
	return ""
}

func (m *Config) GetComponentsConfig() *fleetspeak_components.Config {
	if m != nil {
		return m.ComponentsConfig
	}
	return nil
}

func (m *Config) GetTrustedCertFile() string {
	if m != nil {
		return m.TrustedCertFile
	}
	return ""
}

func (m *Config) GetTrustedCertKeyFile() string {
	if m != nil {
		return m.TrustedCertKeyFile
	}
	return ""
}

func (m *Config) GetServerCertFile() string {
	if m != nil {
		return m.ServerCertFile
	}
	return ""
}

func (m *Config) GetServerCertKeyFile() string {
	if m != nil {
		return m.ServerCertKeyFile
	}
	return ""
}

func (m *Config) GetServerComponentConfigurationFile() string {
	if m != nil {
		return m.ServerComponentConfigurationFile
	}
	return ""
}

func (m *Config) GetPublicHostPort() []string {
	if m != nil {
		return m.PublicHostPort
	}
	return nil
}

func (m *Config) GetLinuxClientConfigurationFile() string {
	if m != nil {
		return m.LinuxClientConfigurationFile
	}
	return ""
}

func (m *Config) GetDarwinClientConfigurationFile() string {
	if m != nil {
		return m.DarwinClientConfigurationFile
	}
	return ""
}

func (m *Config) GetWindowsClientConfigurationFile() string {
	if m != nil {
		return m.WindowsClientConfigurationFile
	}
	return ""
}

func init() {
	proto.RegisterType((*Config)(nil), "fleetspeak.config.Config")
}

func init() {
	proto.RegisterFile("fleetspeak/src/config/proto/fleetspeak_config/config.proto", fileDescriptor_6f66be8faebf9149)
}

var fileDescriptor_6f66be8faebf9149 = []byte{
	// 364 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x7c, 0x92, 0xdf, 0x4e, 0xea, 0x40,
	0x10, 0xc6, 0xc3, 0xe1, 0xc0, 0x39, 0x2c, 0x89, 0xd2, 0x8d, 0x26, 0x8d, 0x11, 0xad, 0x7a, 0x43,
	0x4c, 0x6c, 0xa3, 0xde, 0x79, 0xdb, 0xf8, 0x07, 0x8d, 0xc6, 0xf0, 0x02, 0x9b, 0x52, 0x06, 0xdd,
	0x50, 0x76, 0x9b, 0xdd, 0xad, 0xc8, 0x63, 0xf8, 0xc6, 0xc6, 0xd9, 0x96, 0xb6, 0x2a, 0x5c, 0x91,
	0xcc, 0xf7, 0x9b, 0xdf, 0x37, 0x6c, 0x4a, 0xae, 0xa6, 0x09, 0x80, 0xd1, 0x29, 0x44, 0xb3, 0x40,
	0xab, 0x38, 0x88, 0xa5, 0x98, 0xf2, 0x97, 0x20, 0x55, 0xd2, 0xc8, 0xa0, 0xcc, 0x58, 0x3e, 0xb7,
	0x3f, 0x3e, 0xc6, 0xd4, 0x29, 0x73, 0xdf, 0x06, 0x7b, 0xc3, 0x6f, 0x3a, 0x0d, 0xea, 0x0d, 0x54,
	0x10, 0xcb, 0x79, 0x2a, 0x05, 0x08, 0xa3, 0x7f, 0x33, 0xaf, 0xb2, 0xaa, 0xfd, 0xf8, 0xa3, 0x45,
	0xda, 0x21, 0x0e, 0xe8, 0x19, 0xa1, 0x36, 0xca, 0x54, 0x64, 0xb8, 0x14, 0x4c, 0x44, 0x73, 0x70,
	0x1b, 0x5e, 0x63, 0xd0, 0x19, 0x39, 0xb5, 0xe4, 0x29, 0x9a, 0x03, 0xbd, 0x27, 0x4e, 0x29, 0xcd,
	0x2f, 0x77, 0xff, 0x78, 0x8d, 0x41, 0xf7, 0xa2, 0xef, 0xd7, 0x6e, 0x2e, 0x20, 0xdf, 0x16, 0x8d,
	0x7a, 0xe5, 0x28, 0xaf, 0x3e, 0x25, 0x8e, 0x51, 0x99, 0x36, 0x30, 0x61, 0x31, 0x28, 0xc3, 0xa6,
	0x3c, 0x01, 0xb7, 0x89, 0xcd, 0xdb, 0x79, 0x10, 0x82, 0x32, 0x37, 0x3c, 0x01, 0x7a, 0x4e, 0x76,
	0x6b, 0xec, 0x0c, 0x96, 0x96, 0xff, 0x8b, 0x3c, 0xad, 0xf0, 0x0f, 0xb0, 0xc4, 0x95, 0x01, 0xe9,
	0xd9, 0x27, 0xaa, 0xd8, 0x5b, 0x48, 0x6f, 0xd9, 0xf9, 0x4a, 0x1e, 0x90, 0x9d, 0x2a, 0xb9, 0x72,
	0xb7, 0xed, 0x2b, 0x94, 0x74, 0xa1, 0x7e, 0x24, 0x27, 0xc5, 0x42, 0xf1, 0xa7, 0x58, 0xfd, 0x15,
	0x71, 0xff, 0x1f, 0xee, 0x7b, 0xf9, 0x7e, 0x41, 0x86, 0x55, 0xb0, 0xb8, 0x34, 0xcd, 0xc6, 0x09,
	0x8f, 0xd9, 0xab, 0xd4, 0x86, 0xa5, 0x52, 0x19, 0xf7, 0xbf, 0xd7, 0xfc, 0xba, 0xd4, 0xce, 0xef,
	0xa4, 0x36, 0xcf, 0x52, 0x19, 0x7a, 0x4d, 0x0e, 0x13, 0x2e, 0xb2, 0x77, 0x16, 0x27, 0x7c, 0x4d,
	0x69, 0x07, 0x4b, 0xf7, 0x11, 0x0b, 0x91, 0xfa, 0x59, 0x78, 0x4b, 0xbc, 0x49, 0xa4, 0x16, 0x5c,
	0x6c, 0xf0, 0x10, 0xf4, 0xf4, 0x2d, 0xb7, 0x4e, 0x34, 0x24, 0x47, 0x0b, 0x2e, 0x26, 0x72, 0xa1,
	0x37, 0x98, 0xba, 0x68, 0x3a, 0xc8, 0xc1, 0x35, 0xaa, 0x71, 0x1b, 0x3f, 0xcd, 0xcb, 0xcf, 0x00,
	0x00, 0x00, 0xff, 0xff, 0xda, 0x4e, 0x88, 0xe0, 0x36, 0x03, 0x00, 0x00,
}
