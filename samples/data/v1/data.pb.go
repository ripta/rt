// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.28.1
// 	protoc        v3.21.12
// source: samples/data/v1/data.proto

package v1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	anypb "google.golang.org/protobuf/types/known/anypb"
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

type Envelope struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message   *anypb.Any             `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
	KeyId     string                 `protobuf:"bytes,3,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	Signature string                 `protobuf:"bytes,4,opt,name=signature,proto3" json:"signature,omitempty"`
	SignedAt  *timestamppb.Timestamp `protobuf:"bytes,5,opt,name=signed_at,json=signedAt,proto3" json:"signed_at,omitempty"`
}

func (x *Envelope) Reset() {
	*x = Envelope{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Envelope) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Envelope) ProtoMessage() {}

func (x *Envelope) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Envelope.ProtoReflect.Descriptor instead.
func (*Envelope) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{0}
}

func (x *Envelope) GetMessage() *anypb.Any {
	if x != nil {
		return x.Message
	}
	return nil
}

func (x *Envelope) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

func (x *Envelope) GetSignature() string {
	if x != nil {
		return x.Signature
	}
	return ""
}

func (x *Envelope) GetSignedAt() *timestamppb.Timestamp {
	if x != nil {
		return x.SignedAt
	}
	return nil
}

type Bytes struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Content []byte `protobuf:"bytes,1,opt,name=content,proto3" json:"content,omitempty"`
}

func (x *Bytes) Reset() {
	*x = Bytes{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Bytes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Bytes) ProtoMessage() {}

func (x *Bytes) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Bytes.ProtoReflect.Descriptor instead.
func (*Bytes) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{1}
}

func (x *Bytes) GetContent() []byte {
	if x != nil {
		return x.Content
	}
	return nil
}

type MultipartBytes struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Parts []*MultipartBytes_Part `protobuf:"bytes,1,rep,name=parts,proto3" json:"parts,omitempty"`
}

func (x *MultipartBytes) Reset() {
	*x = MultipartBytes{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MultipartBytes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MultipartBytes) ProtoMessage() {}

func (x *MultipartBytes) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MultipartBytes.ProtoReflect.Descriptor instead.
func (*MultipartBytes) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{2}
}

func (x *MultipartBytes) GetParts() []*MultipartBytes_Part {
	if x != nil {
		return x.Parts
	}
	return nil
}

type UnixMeta struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Permission string                            `protobuf:"bytes,1,opt,name=permission,proto3" json:"permission,omitempty"`
	OwnerName  string                            `protobuf:"bytes,2,opt,name=owner_name,json=ownerName,proto3" json:"owner_name,omitempty"`
	OwnerId    int32                             `protobuf:"varint,3,opt,name=owner_id,json=ownerId,proto3" json:"owner_id,omitempty"`
	GroupName  string                            `protobuf:"bytes,4,opt,name=group_name,json=groupName,proto3" json:"group_name,omitempty"`
	GroupId    int32                             `protobuf:"varint,5,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	Attrs      []*UnixMeta_UnixExtendedAttribute `protobuf:"bytes,6,rep,name=attrs,proto3" json:"attrs,omitempty"`
	Times      *UnixMeta_UnixExtendedTimestamps  `protobuf:"bytes,7,opt,name=times,proto3" json:"times,omitempty"`
}

func (x *UnixMeta) Reset() {
	*x = UnixMeta{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *UnixMeta) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UnixMeta) ProtoMessage() {}

func (x *UnixMeta) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use UnixMeta.ProtoReflect.Descriptor instead.
func (*UnixMeta) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{3}
}

func (x *UnixMeta) GetPermission() string {
	if x != nil {
		return x.Permission
	}
	return ""
}

func (x *UnixMeta) GetOwnerName() string {
	if x != nil {
		return x.OwnerName
	}
	return ""
}

func (x *UnixMeta) GetOwnerId() int32 {
	if x != nil {
		return x.OwnerId
	}
	return 0
}

func (x *UnixMeta) GetGroupName() string {
	if x != nil {
		return x.GroupName
	}
	return ""
}

func (x *UnixMeta) GetGroupId() int32 {
	if x != nil {
		return x.GroupId
	}
	return 0
}

func (x *UnixMeta) GetAttrs() []*UnixMeta_UnixExtendedAttribute {
	if x != nil {
		return x.Attrs
	}
	return nil
}

func (x *UnixMeta) GetTimes() *UnixMeta_UnixExtendedTimestamps {
	if x != nil {
		return x.Times
	}
	return nil
}

type File struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name      string          `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Metadata  *UnixMeta       `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
	Multipart *MultipartBytes `protobuf:"bytes,3,opt,name=multipart,proto3" json:"multipart,omitempty"`
}

func (x *File) Reset() {
	*x = File{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *File) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*File) ProtoMessage() {}

func (x *File) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use File.ProtoReflect.Descriptor instead.
func (*File) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{4}
}

func (x *File) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *File) GetMetadata() *UnixMeta {
	if x != nil {
		return x.Metadata
	}
	return nil
}

func (x *File) GetMultipart() *MultipartBytes {
	if x != nil {
		return x.Multipart
	}
	return nil
}

type Directory struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name        string       `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Metadata    *UnixMeta    `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
	Directories []*Directory `protobuf:"bytes,3,rep,name=directories,proto3" json:"directories,omitempty"`
	Files       []*File      `protobuf:"bytes,4,rep,name=files,proto3" json:"files,omitempty"`
}

func (x *Directory) Reset() {
	*x = Directory{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Directory) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Directory) ProtoMessage() {}

func (x *Directory) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Directory.ProtoReflect.Descriptor instead.
func (*Directory) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{5}
}

func (x *Directory) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Directory) GetMetadata() *UnixMeta {
	if x != nil {
		return x.Metadata
	}
	return nil
}

func (x *Directory) GetDirectories() []*Directory {
	if x != nil {
		return x.Directories
	}
	return nil
}

func (x *Directory) GetFiles() []*File {
	if x != nil {
		return x.Files
	}
	return nil
}

type MultipartBytes_Part struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	BlobRef string `protobuf:"bytes,1,opt,name=blob_ref,json=blobRef,proto3" json:"blob_ref,omitempty"`
	Offset  int64  `protobuf:"varint,2,opt,name=offset,proto3" json:"offset,omitempty"`
	Size    int32  `protobuf:"varint,3,opt,name=size,proto3" json:"size,omitempty"`
}

func (x *MultipartBytes_Part) Reset() {
	*x = MultipartBytes_Part{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[6]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MultipartBytes_Part) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MultipartBytes_Part) ProtoMessage() {}

func (x *MultipartBytes_Part) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[6]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MultipartBytes_Part.ProtoReflect.Descriptor instead.
func (*MultipartBytes_Part) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{2, 0}
}

func (x *MultipartBytes_Part) GetBlobRef() string {
	if x != nil {
		return x.BlobRef
	}
	return ""
}

func (x *MultipartBytes_Part) GetOffset() int64 {
	if x != nil {
		return x.Offset
	}
	return 0
}

func (x *MultipartBytes_Part) GetSize() int32 {
	if x != nil {
		return x.Size
	}
	return 0
}

type UnixMeta_UnixExtendedAttribute struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

func (x *UnixMeta_UnixExtendedAttribute) Reset() {
	*x = UnixMeta_UnixExtendedAttribute{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[7]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *UnixMeta_UnixExtendedAttribute) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UnixMeta_UnixExtendedAttribute) ProtoMessage() {}

func (x *UnixMeta_UnixExtendedAttribute) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[7]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use UnixMeta_UnixExtendedAttribute.ProtoReflect.Descriptor instead.
func (*UnixMeta_UnixExtendedAttribute) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{3, 0}
}

func (x *UnixMeta_UnixExtendedAttribute) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *UnixMeta_UnixExtendedAttribute) GetValue() string {
	if x != nil {
		return x.Value
	}
	return ""
}

type UnixMeta_UnixExtendedTimestamps struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	CreateTime *timestamppb.Timestamp `protobuf:"bytes,1,opt,name=create_time,json=createTime,proto3" json:"create_time,omitempty"`
	ModifyTime *timestamppb.Timestamp `protobuf:"bytes,2,opt,name=modify_time,json=modifyTime,proto3" json:"modify_time,omitempty"`
	AccessTime *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=access_time,json=accessTime,proto3" json:"access_time,omitempty"`
}

func (x *UnixMeta_UnixExtendedTimestamps) Reset() {
	*x = UnixMeta_UnixExtendedTimestamps{}
	if protoimpl.UnsafeEnabled {
		mi := &file_samples_data_v1_data_proto_msgTypes[8]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *UnixMeta_UnixExtendedTimestamps) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UnixMeta_UnixExtendedTimestamps) ProtoMessage() {}

func (x *UnixMeta_UnixExtendedTimestamps) ProtoReflect() protoreflect.Message {
	mi := &file_samples_data_v1_data_proto_msgTypes[8]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use UnixMeta_UnixExtendedTimestamps.ProtoReflect.Descriptor instead.
func (*UnixMeta_UnixExtendedTimestamps) Descriptor() ([]byte, []int) {
	return file_samples_data_v1_data_proto_rawDescGZIP(), []int{3, 1}
}

func (x *UnixMeta_UnixExtendedTimestamps) GetCreateTime() *timestamppb.Timestamp {
	if x != nil {
		return x.CreateTime
	}
	return nil
}

func (x *UnixMeta_UnixExtendedTimestamps) GetModifyTime() *timestamppb.Timestamp {
	if x != nil {
		return x.ModifyTime
	}
	return nil
}

func (x *UnixMeta_UnixExtendedTimestamps) GetAccessTime() *timestamppb.Timestamp {
	if x != nil {
		return x.AccessTime
	}
	return nil
}

var File_samples_data_v1_data_proto protoreflect.FileDescriptor

var file_samples_data_v1_data_proto_rawDesc = []byte{
	0x0a, 0x1a, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2f, 0x64, 0x61, 0x74, 0x61, 0x2f, 0x76,
	0x31, 0x2f, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x0f, 0x73, 0x61,
	0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76, 0x31, 0x1a, 0x19, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x61,
	0x6e, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x1f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x74,
	0x61, 0x6d, 0x70, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xae, 0x01, 0x0a, 0x08, 0x45, 0x6e,
	0x76, 0x65, 0x6c, 0x6f, 0x70, 0x65, 0x12, 0x2e, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e, 0x79, 0x52, 0x07, 0x6d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x15, 0x0a, 0x06, 0x6b, 0x65, 0x79, 0x5f, 0x69, 0x64,
	0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x6b, 0x65, 0x79, 0x49, 0x64, 0x12, 0x1c, 0x0a,
	0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x12, 0x37, 0x0a, 0x09, 0x73,
	0x69, 0x67, 0x6e, 0x65, 0x64, 0x5f, 0x61, 0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a,
	0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66,
	0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x08, 0x73, 0x69, 0x67, 0x6e,
	0x65, 0x64, 0x41, 0x74, 0x4a, 0x04, 0x08, 0x01, 0x10, 0x02, 0x22, 0x21, 0x0a, 0x05, 0x42, 0x79,
	0x74, 0x65, 0x73, 0x12, 0x18, 0x0a, 0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74, 0x22, 0x9b, 0x01,
	0x0a, 0x0e, 0x4d, 0x75, 0x6c, 0x74, 0x69, 0x70, 0x61, 0x72, 0x74, 0x42, 0x79, 0x74, 0x65, 0x73,
	0x12, 0x3a, 0x0a, 0x05, 0x70, 0x61, 0x72, 0x74, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32,
	0x24, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76,
	0x31, 0x2e, 0x4d, 0x75, 0x6c, 0x74, 0x69, 0x70, 0x61, 0x72, 0x74, 0x42, 0x79, 0x74, 0x65, 0x73,
	0x2e, 0x50, 0x61, 0x72, 0x74, 0x52, 0x05, 0x70, 0x61, 0x72, 0x74, 0x73, 0x1a, 0x4d, 0x0a, 0x04,
	0x50, 0x61, 0x72, 0x74, 0x12, 0x19, 0x0a, 0x08, 0x62, 0x6c, 0x6f, 0x62, 0x5f, 0x72, 0x65, 0x66,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x62, 0x6c, 0x6f, 0x62, 0x52, 0x65, 0x66, 0x12,
	0x16, 0x0a, 0x06, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x03, 0x52,
	0x06, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x73, 0x69, 0x7a, 0x65, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x04, 0x73, 0x69, 0x7a, 0x65, 0x22, 0xc2, 0x04, 0x0a, 0x08,
	0x55, 0x6e, 0x69, 0x78, 0x4d, 0x65, 0x74, 0x61, 0x12, 0x1e, 0x0a, 0x0a, 0x70, 0x65, 0x72, 0x6d,
	0x69, 0x73, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a, 0x70, 0x65,
	0x72, 0x6d, 0x69, 0x73, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x1d, 0x0a, 0x0a, 0x6f, 0x77, 0x6e, 0x65,
	0x72, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x6f, 0x77,
	0x6e, 0x65, 0x72, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x19, 0x0a, 0x08, 0x6f, 0x77, 0x6e, 0x65, 0x72,
	0x5f, 0x69, 0x64, 0x18, 0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x07, 0x6f, 0x77, 0x6e, 0x65, 0x72,
	0x49, 0x64, 0x12, 0x1d, 0x0a, 0x0a, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65,
	0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x4e, 0x61, 0x6d,
	0x65, 0x12, 0x19, 0x0a, 0x08, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x5f, 0x69, 0x64, 0x18, 0x05, 0x20,
	0x01, 0x28, 0x05, 0x52, 0x07, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x49, 0x64, 0x12, 0x45, 0x0a, 0x05,
	0x61, 0x74, 0x74, 0x72, 0x73, 0x18, 0x06, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2f, 0x2e, 0x73, 0x61,
	0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76, 0x31, 0x2e, 0x55, 0x6e,
	0x69, 0x78, 0x4d, 0x65, 0x74, 0x61, 0x2e, 0x55, 0x6e, 0x69, 0x78, 0x45, 0x78, 0x74, 0x65, 0x6e,
	0x64, 0x65, 0x64, 0x41, 0x74, 0x74, 0x72, 0x69, 0x62, 0x75, 0x74, 0x65, 0x52, 0x05, 0x61, 0x74,
	0x74, 0x72, 0x73, 0x12, 0x46, 0x0a, 0x05, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x18, 0x07, 0x20, 0x01,
	0x28, 0x0b, 0x32, 0x30, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74,
	0x61, 0x2e, 0x76, 0x31, 0x2e, 0x55, 0x6e, 0x69, 0x78, 0x4d, 0x65, 0x74, 0x61, 0x2e, 0x55, 0x6e,
	0x69, 0x78, 0x45, 0x78, 0x74, 0x65, 0x6e, 0x64, 0x65, 0x64, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74,
	0x61, 0x6d, 0x70, 0x73, 0x52, 0x05, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x1a, 0x41, 0x0a, 0x15, 0x55,
	0x6e, 0x69, 0x78, 0x45, 0x78, 0x74, 0x65, 0x6e, 0x64, 0x65, 0x64, 0x41, 0x74, 0x74, 0x72, 0x69,
	0x62, 0x75, 0x74, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x1a, 0xcf,
	0x01, 0x0a, 0x16, 0x55, 0x6e, 0x69, 0x78, 0x45, 0x78, 0x74, 0x65, 0x6e, 0x64, 0x65, 0x64, 0x54,
	0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x73, 0x12, 0x3b, 0x0a, 0x0b, 0x63, 0x72, 0x65,
	0x61, 0x74, 0x65, 0x5f, 0x74, 0x69, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a,
	0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66,
	0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x63, 0x72, 0x65, 0x61,
	0x74, 0x65, 0x54, 0x69, 0x6d, 0x65, 0x12, 0x3b, 0x0a, 0x0b, 0x6d, 0x6f, 0x64, 0x69, 0x66, 0x79,
	0x5f, 0x74, 0x69, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f,
	0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69,
	0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x6d, 0x6f, 0x64, 0x69, 0x66, 0x79, 0x54,
	0x69, 0x6d, 0x65, 0x12, 0x3b, 0x0a, 0x0b, 0x61, 0x63, 0x63, 0x65, 0x73, 0x73, 0x5f, 0x74, 0x69,
	0x6d, 0x65, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73,
	0x74, 0x61, 0x6d, 0x70, 0x52, 0x0a, 0x61, 0x63, 0x63, 0x65, 0x73, 0x73, 0x54, 0x69, 0x6d, 0x65,
	0x22, 0x90, 0x01, 0x0a, 0x04, 0x46, 0x69, 0x6c, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d,
	0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x35, 0x0a,
	0x08, 0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32,
	0x19, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76,
	0x31, 0x2e, 0x55, 0x6e, 0x69, 0x78, 0x4d, 0x65, 0x74, 0x61, 0x52, 0x08, 0x6d, 0x65, 0x74, 0x61,
	0x64, 0x61, 0x74, 0x61, 0x12, 0x3d, 0x0a, 0x09, 0x6d, 0x75, 0x6c, 0x74, 0x69, 0x70, 0x61, 0x72,
	0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1f, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65,
	0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76, 0x31, 0x2e, 0x4d, 0x75, 0x6c, 0x74, 0x69, 0x70,
	0x61, 0x72, 0x74, 0x42, 0x79, 0x74, 0x65, 0x73, 0x52, 0x09, 0x6d, 0x75, 0x6c, 0x74, 0x69, 0x70,
	0x61, 0x72, 0x74, 0x22, 0xc1, 0x01, 0x0a, 0x09, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72,
	0x79, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x35, 0x0a, 0x08, 0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74,
	0x61, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x19, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65,
	0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76, 0x31, 0x2e, 0x55, 0x6e, 0x69, 0x78, 0x4d, 0x65,
	0x74, 0x61, 0x52, 0x08, 0x6d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74, 0x61, 0x12, 0x3c, 0x0a, 0x0b,
	0x64, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x69, 0x65, 0x73, 0x18, 0x03, 0x20, 0x03, 0x28,
	0x0b, 0x32, 0x1a, 0x2e, 0x73, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61,
	0x2e, 0x76, 0x31, 0x2e, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x79, 0x52, 0x0b, 0x64,
	0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x69, 0x65, 0x73, 0x12, 0x2b, 0x0a, 0x05, 0x66, 0x69,
	0x6c, 0x65, 0x73, 0x18, 0x04, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x15, 0x2e, 0x73, 0x61, 0x6d, 0x70,
	0x6c, 0x65, 0x73, 0x2e, 0x64, 0x61, 0x74, 0x61, 0x2e, 0x76, 0x31, 0x2e, 0x46, 0x69, 0x6c, 0x65,
	0x52, 0x05, 0x66, 0x69, 0x6c, 0x65, 0x73, 0x42, 0x25, 0x5a, 0x23, 0x67, 0x69, 0x74, 0x68, 0x75,
	0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x72, 0x69, 0x70, 0x74, 0x61, 0x2f, 0x72, 0x74, 0x2f, 0x73,
	0x61, 0x6d, 0x70, 0x6c, 0x65, 0x73, 0x2f, 0x64, 0x61, 0x74, 0x61, 0x2f, 0x76, 0x31, 0x62, 0x06,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_samples_data_v1_data_proto_rawDescOnce sync.Once
	file_samples_data_v1_data_proto_rawDescData = file_samples_data_v1_data_proto_rawDesc
)

func file_samples_data_v1_data_proto_rawDescGZIP() []byte {
	file_samples_data_v1_data_proto_rawDescOnce.Do(func() {
		file_samples_data_v1_data_proto_rawDescData = protoimpl.X.CompressGZIP(file_samples_data_v1_data_proto_rawDescData)
	})
	return file_samples_data_v1_data_proto_rawDescData
}

var file_samples_data_v1_data_proto_msgTypes = make([]protoimpl.MessageInfo, 9)
var file_samples_data_v1_data_proto_goTypes = []interface{}{
	(*Envelope)(nil),                        // 0: samples.data.v1.Envelope
	(*Bytes)(nil),                           // 1: samples.data.v1.Bytes
	(*MultipartBytes)(nil),                  // 2: samples.data.v1.MultipartBytes
	(*UnixMeta)(nil),                        // 3: samples.data.v1.UnixMeta
	(*File)(nil),                            // 4: samples.data.v1.File
	(*Directory)(nil),                       // 5: samples.data.v1.Directory
	(*MultipartBytes_Part)(nil),             // 6: samples.data.v1.MultipartBytes.Part
	(*UnixMeta_UnixExtendedAttribute)(nil),  // 7: samples.data.v1.UnixMeta.UnixExtendedAttribute
	(*UnixMeta_UnixExtendedTimestamps)(nil), // 8: samples.data.v1.UnixMeta.UnixExtendedTimestamps
	(*anypb.Any)(nil),                       // 9: google.protobuf.Any
	(*timestamppb.Timestamp)(nil),           // 10: google.protobuf.Timestamp
}
var file_samples_data_v1_data_proto_depIdxs = []int32{
	9,  // 0: samples.data.v1.Envelope.message:type_name -> google.protobuf.Any
	10, // 1: samples.data.v1.Envelope.signed_at:type_name -> google.protobuf.Timestamp
	6,  // 2: samples.data.v1.MultipartBytes.parts:type_name -> samples.data.v1.MultipartBytes.Part
	7,  // 3: samples.data.v1.UnixMeta.attrs:type_name -> samples.data.v1.UnixMeta.UnixExtendedAttribute
	8,  // 4: samples.data.v1.UnixMeta.times:type_name -> samples.data.v1.UnixMeta.UnixExtendedTimestamps
	3,  // 5: samples.data.v1.File.metadata:type_name -> samples.data.v1.UnixMeta
	2,  // 6: samples.data.v1.File.multipart:type_name -> samples.data.v1.MultipartBytes
	3,  // 7: samples.data.v1.Directory.metadata:type_name -> samples.data.v1.UnixMeta
	5,  // 8: samples.data.v1.Directory.directories:type_name -> samples.data.v1.Directory
	4,  // 9: samples.data.v1.Directory.files:type_name -> samples.data.v1.File
	10, // 10: samples.data.v1.UnixMeta.UnixExtendedTimestamps.create_time:type_name -> google.protobuf.Timestamp
	10, // 11: samples.data.v1.UnixMeta.UnixExtendedTimestamps.modify_time:type_name -> google.protobuf.Timestamp
	10, // 12: samples.data.v1.UnixMeta.UnixExtendedTimestamps.access_time:type_name -> google.protobuf.Timestamp
	13, // [13:13] is the sub-list for method output_type
	13, // [13:13] is the sub-list for method input_type
	13, // [13:13] is the sub-list for extension type_name
	13, // [13:13] is the sub-list for extension extendee
	0,  // [0:13] is the sub-list for field type_name
}

func init() { file_samples_data_v1_data_proto_init() }
func file_samples_data_v1_data_proto_init() {
	if File_samples_data_v1_data_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_samples_data_v1_data_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Envelope); i {
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
		file_samples_data_v1_data_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Bytes); i {
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
		file_samples_data_v1_data_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MultipartBytes); i {
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
		file_samples_data_v1_data_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*UnixMeta); i {
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
		file_samples_data_v1_data_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*File); i {
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
		file_samples_data_v1_data_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Directory); i {
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
		file_samples_data_v1_data_proto_msgTypes[6].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MultipartBytes_Part); i {
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
		file_samples_data_v1_data_proto_msgTypes[7].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*UnixMeta_UnixExtendedAttribute); i {
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
		file_samples_data_v1_data_proto_msgTypes[8].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*UnixMeta_UnixExtendedTimestamps); i {
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
			RawDescriptor: file_samples_data_v1_data_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   9,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_samples_data_v1_data_proto_goTypes,
		DependencyIndexes: file_samples_data_v1_data_proto_depIdxs,
		MessageInfos:      file_samples_data_v1_data_proto_msgTypes,
	}.Build()
	File_samples_data_v1_data_proto = out.File
	file_samples_data_v1_data_proto_rawDesc = nil
	file_samples_data_v1_data_proto_goTypes = nil
	file_samples_data_v1_data_proto_depIdxs = nil
}
