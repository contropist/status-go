// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        v3.20.3
// source: shard.proto

package protobuf

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

type Shard struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Cluster int32 `protobuf:"varint,1,opt,name=cluster,proto3" json:"cluster,omitempty"`
	Index   int32 `protobuf:"varint,2,opt,name=index,proto3" json:"index,omitempty"`
}

func (x *Shard) Reset() {
	*x = Shard{}
	if protoimpl.UnsafeEnabled {
		mi := &file_shard_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Shard) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Shard) ProtoMessage() {}

func (x *Shard) ProtoReflect() protoreflect.Message {
	mi := &file_shard_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Shard.ProtoReflect.Descriptor instead.
func (*Shard) Descriptor() ([]byte, []int) {
	return file_shard_proto_rawDescGZIP(), []int{0}
}

func (x *Shard) GetCluster() int32 {
	if x != nil {
		return x.Cluster
	}
	return 0
}

func (x *Shard) GetIndex() int32 {
	if x != nil {
		return x.Index
	}
	return 0
}

type PublicShardInfo struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// clock
	Clock uint64 `protobuf:"varint,1,opt,name=clock,proto3" json:"clock,omitempty"`
	// community ID
	CommunityId []byte `protobuf:"bytes,2,opt,name=community_id,json=communityId,proto3" json:"community_id,omitempty"`
	// shard information
	Shard *Shard `protobuf:"bytes,3,opt,name=shard,proto3" json:"shard,omitempty"`
	// if chainID > 0, the signer must be verified through the community contract
	ChainId uint64 `protobuf:"varint,4,opt,name=chainId,proto3" json:"chainId,omitempty"`
}

func (x *PublicShardInfo) Reset() {
	*x = PublicShardInfo{}
	if protoimpl.UnsafeEnabled {
		mi := &file_shard_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *PublicShardInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PublicShardInfo) ProtoMessage() {}

func (x *PublicShardInfo) ProtoReflect() protoreflect.Message {
	mi := &file_shard_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PublicShardInfo.ProtoReflect.Descriptor instead.
func (*PublicShardInfo) Descriptor() ([]byte, []int) {
	return file_shard_proto_rawDescGZIP(), []int{1}
}

func (x *PublicShardInfo) GetClock() uint64 {
	if x != nil {
		return x.Clock
	}
	return 0
}

func (x *PublicShardInfo) GetCommunityId() []byte {
	if x != nil {
		return x.CommunityId
	}
	return nil
}

func (x *PublicShardInfo) GetShard() *Shard {
	if x != nil {
		return x.Shard
	}
	return nil
}

func (x *PublicShardInfo) GetChainId() uint64 {
	if x != nil {
		return x.ChainId
	}
	return 0
}

type CommunityPublicShardInfo struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Signature of the payload field
	Signature []byte `protobuf:"bytes,1,opt,name=signature,proto3" json:"signature,omitempty"`
	// Marshaled PublicShardInfo
	Payload []byte `protobuf:"bytes,2,opt,name=payload,proto3" json:"payload,omitempty"`
}

func (x *CommunityPublicShardInfo) Reset() {
	*x = CommunityPublicShardInfo{}
	if protoimpl.UnsafeEnabled {
		mi := &file_shard_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *CommunityPublicShardInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CommunityPublicShardInfo) ProtoMessage() {}

func (x *CommunityPublicShardInfo) ProtoReflect() protoreflect.Message {
	mi := &file_shard_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CommunityPublicShardInfo.ProtoReflect.Descriptor instead.
func (*CommunityPublicShardInfo) Descriptor() ([]byte, []int) {
	return file_shard_proto_rawDescGZIP(), []int{2}
}

func (x *CommunityPublicShardInfo) GetSignature() []byte {
	if x != nil {
		return x.Signature
	}
	return nil
}

func (x *CommunityPublicShardInfo) GetPayload() []byte {
	if x != nil {
		return x.Payload
	}
	return nil
}

var File_shard_proto protoreflect.FileDescriptor

var file_shard_proto_rawDesc = []byte{
	0x0a, 0x0b, 0x73, 0x68, 0x61, 0x72, 0x64, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x08, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x22, 0x37, 0x0a, 0x05, 0x53, 0x68, 0x61, 0x72, 0x64,
	0x12, 0x18, 0x0a, 0x07, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x05, 0x52, 0x07, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x12, 0x14, 0x0a, 0x05, 0x69, 0x6e,
	0x64, 0x65, 0x78, 0x18, 0x02, 0x20, 0x01, 0x28, 0x05, 0x52, 0x05, 0x69, 0x6e, 0x64, 0x65, 0x78,
	0x22, 0x8b, 0x01, 0x0a, 0x0f, 0x50, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x53, 0x68, 0x61, 0x72, 0x64,
	0x49, 0x6e, 0x66, 0x6f, 0x12, 0x14, 0x0a, 0x05, 0x63, 0x6c, 0x6f, 0x63, 0x6b, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x05, 0x63, 0x6c, 0x6f, 0x63, 0x6b, 0x12, 0x21, 0x0a, 0x0c, 0x63, 0x6f,
	0x6d, 0x6d, 0x75, 0x6e, 0x69, 0x74, 0x79, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c,
	0x52, 0x0b, 0x63, 0x6f, 0x6d, 0x6d, 0x75, 0x6e, 0x69, 0x74, 0x79, 0x49, 0x64, 0x12, 0x25, 0x0a,
	0x05, 0x73, 0x68, 0x61, 0x72, 0x64, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x53, 0x68, 0x61, 0x72, 0x64, 0x52, 0x05, 0x73,
	0x68, 0x61, 0x72, 0x64, 0x12, 0x18, 0x0a, 0x07, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x18,
	0x04, 0x20, 0x01, 0x28, 0x04, 0x52, 0x07, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x22, 0x52,
	0x0a, 0x18, 0x43, 0x6f, 0x6d, 0x6d, 0x75, 0x6e, 0x69, 0x74, 0x79, 0x50, 0x75, 0x62, 0x6c, 0x69,
	0x63, 0x53, 0x68, 0x61, 0x72, 0x64, 0x49, 0x6e, 0x66, 0x6f, 0x12, 0x1c, 0x0a, 0x09, 0x73, 0x69,
	0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x73,
	0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x12, 0x18, 0x0a, 0x07, 0x70, 0x61, 0x79, 0x6c,
	0x6f, 0x61, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x07, 0x70, 0x61, 0x79, 0x6c, 0x6f,
	0x61, 0x64, 0x42, 0x0d, 0x5a, 0x0b, 0x2e, 0x2f, 0x3b, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75,
	0x66, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_shard_proto_rawDescOnce sync.Once
	file_shard_proto_rawDescData = file_shard_proto_rawDesc
)

func file_shard_proto_rawDescGZIP() []byte {
	file_shard_proto_rawDescOnce.Do(func() {
		file_shard_proto_rawDescData = protoimpl.X.CompressGZIP(file_shard_proto_rawDescData)
	})
	return file_shard_proto_rawDescData
}

var file_shard_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_shard_proto_goTypes = []interface{}{
	(*Shard)(nil),                    // 0: protobuf.Shard
	(*PublicShardInfo)(nil),          // 1: protobuf.PublicShardInfo
	(*CommunityPublicShardInfo)(nil), // 2: protobuf.CommunityPublicShardInfo
}
var file_shard_proto_depIdxs = []int32{
	0, // 0: protobuf.PublicShardInfo.shard:type_name -> protobuf.Shard
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_shard_proto_init() }
func file_shard_proto_init() {
	if File_shard_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_shard_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Shard); i {
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
		file_shard_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*PublicShardInfo); i {
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
		file_shard_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*CommunityPublicShardInfo); i {
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
			RawDescriptor: file_shard_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_shard_proto_goTypes,
		DependencyIndexes: file_shard_proto_depIdxs,
		MessageInfos:      file_shard_proto_msgTypes,
	}.Build()
	File_shard_proto = out.File
	file_shard_proto_rawDesc = nil
	file_shard_proto_goTypes = nil
	file_shard_proto_depIdxs = nil
}
