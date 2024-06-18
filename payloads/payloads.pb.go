// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.34.1
// 	protoc        v5.26.1
// source: payloads/payloads.proto

package payloads

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

type PayloadType int32

const (
	PayloadType_BINARY  PayloadType = 0
	PayloadType_TEXT    PayloadType = 1
	PayloadType_ACK     PayloadType = 2
	PayloadType_SESSION PayloadType = 3
)

// Enum value maps for PayloadType.
var (
	PayloadType_name = map[int32]string{
		0: "BINARY",
		1: "TEXT",
		2: "ACK",
		3: "SESSION",
	}
	PayloadType_value = map[string]int32{
		"BINARY":  0,
		"TEXT":    1,
		"ACK":     2,
		"SESSION": 3,
	}
)

func (x PayloadType) Enum() *PayloadType {
	p := new(PayloadType)
	*p = x
	return p
}

func (x PayloadType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (PayloadType) Descriptor() protoreflect.EnumDescriptor {
	return file_payloads_payloads_proto_enumTypes[0].Descriptor()
}

func (PayloadType) Type() protoreflect.EnumType {
	return &file_payloads_payloads_proto_enumTypes[0]
}

func (x PayloadType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use PayloadType.Descriptor instead.
func (PayloadType) EnumDescriptor() ([]byte, []int) {
	return file_payloads_payloads_proto_rawDescGZIP(), []int{0}
}

type Message struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Payload      []byte `protobuf:"bytes,1,opt,name=payload,proto3" json:"payload,omitempty"`
	Encrypted    bool   `protobuf:"varint,2,opt,name=encrypted,proto3" json:"encrypted,omitempty"`
	Nonce        []byte `protobuf:"bytes,3,opt,name=nonce,proto3" json:"nonce,omitempty"`
	EncryptedKey []byte `protobuf:"bytes,4,opt,name=encrypted_key,json=encryptedKey,proto3" json:"encrypted_key,omitempty"`
}

func (x *Message) Reset() {
	*x = Message{}
	if protoimpl.UnsafeEnabled {
		mi := &file_payloads_payloads_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Message) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Message) ProtoMessage() {}

func (x *Message) ProtoReflect() protoreflect.Message {
	mi := &file_payloads_payloads_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Message.ProtoReflect.Descriptor instead.
func (*Message) Descriptor() ([]byte, []int) {
	return file_payloads_payloads_proto_rawDescGZIP(), []int{0}
}

func (x *Message) GetPayload() []byte {
	if x != nil {
		return x.Payload
	}
	return nil
}

func (x *Message) GetEncrypted() bool {
	if x != nil {
		return x.Encrypted
	}
	return false
}

func (x *Message) GetNonce() []byte {
	if x != nil {
		return x.Nonce
	}
	return nil
}

func (x *Message) GetEncryptedKey() []byte {
	if x != nil {
		return x.EncryptedKey
	}
	return nil
}

type Payload struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Type      PayloadType `protobuf:"varint,1,opt,name=type,proto3,enum=payloads.PayloadType" json:"type,omitempty"`
	MessageId []byte      `protobuf:"bytes,2,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	Data      []byte      `protobuf:"bytes,3,opt,name=data,proto3" json:"data,omitempty"`
	ReplyToId []byte      `protobuf:"bytes,4,opt,name=reply_to_id,json=replyToId,proto3" json:"reply_to_id,omitempty"`
	NoReply   bool        `protobuf:"varint,5,opt,name=no_reply,json=noReply,proto3" json:"no_reply,omitempty"`
}

func (x *Payload) Reset() {
	*x = Payload{}
	if protoimpl.UnsafeEnabled {
		mi := &file_payloads_payloads_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Payload) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Payload) ProtoMessage() {}

func (x *Payload) ProtoReflect() protoreflect.Message {
	mi := &file_payloads_payloads_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Payload.ProtoReflect.Descriptor instead.
func (*Payload) Descriptor() ([]byte, []int) {
	return file_payloads_payloads_proto_rawDescGZIP(), []int{1}
}

func (x *Payload) GetType() PayloadType {
	if x != nil {
		return x.Type
	}
	return PayloadType_BINARY
}

func (x *Payload) GetMessageId() []byte {
	if x != nil {
		return x.MessageId
	}
	return nil
}

func (x *Payload) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

func (x *Payload) GetReplyToId() []byte {
	if x != nil {
		return x.ReplyToId
	}
	return nil
}

func (x *Payload) GetNoReply() bool {
	if x != nil {
		return x.NoReply
	}
	return false
}

type TextData struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Text string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
}

func (x *TextData) Reset() {
	*x = TextData{}
	if protoimpl.UnsafeEnabled {
		mi := &file_payloads_payloads_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TextData) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TextData) ProtoMessage() {}

func (x *TextData) ProtoReflect() protoreflect.Message {
	mi := &file_payloads_payloads_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TextData.ProtoReflect.Descriptor instead.
func (*TextData) Descriptor() ([]byte, []int) {
	return file_payloads_payloads_proto_rawDescGZIP(), []int{2}
}

func (x *TextData) GetText() string {
	if x != nil {
		return x.Text
	}
	return ""
}

var File_payloads_payloads_proto protoreflect.FileDescriptor

var file_payloads_payloads_proto_rawDesc = []byte{
	0x0a, 0x17, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x73, 0x2f, 0x70, 0x61, 0x79, 0x6c, 0x6f,
	0x61, 0x64, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x08, 0x70, 0x61, 0x79, 0x6c, 0x6f,
	0x61, 0x64, 0x73, 0x22, 0x7c, 0x0a, 0x07, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x18,
	0x0a, 0x07, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52,
	0x07, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x12, 0x1c, 0x0a, 0x09, 0x65, 0x6e, 0x63, 0x72,
	0x79, 0x70, 0x74, 0x65, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x08, 0x52, 0x09, 0x65, 0x6e, 0x63,
	0x72, 0x79, 0x70, 0x74, 0x65, 0x64, 0x12, 0x14, 0x0a, 0x05, 0x6e, 0x6f, 0x6e, 0x63, 0x65, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x6e, 0x6f, 0x6e, 0x63, 0x65, 0x12, 0x23, 0x0a, 0x0d,
	0x65, 0x6e, 0x63, 0x72, 0x79, 0x70, 0x74, 0x65, 0x64, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x04, 0x20,
	0x01, 0x28, 0x0c, 0x52, 0x0c, 0x65, 0x6e, 0x63, 0x72, 0x79, 0x70, 0x74, 0x65, 0x64, 0x4b, 0x65,
	0x79, 0x22, 0xa2, 0x01, 0x0a, 0x07, 0x50, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x12, 0x29, 0x0a,
	0x04, 0x74, 0x79, 0x70, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x15, 0x2e, 0x70, 0x61,
	0x79, 0x6c, 0x6f, 0x61, 0x64, 0x73, 0x2e, 0x50, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x54, 0x79,
	0x70, 0x65, 0x52, 0x04, 0x74, 0x79, 0x70, 0x65, 0x12, 0x1d, 0x0a, 0x0a, 0x6d, 0x65, 0x73, 0x73,
	0x61, 0x67, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x6d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x49, 0x64, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x12, 0x1e, 0x0a, 0x0b, 0x72,
	0x65, 0x70, 0x6c, 0x79, 0x5f, 0x74, 0x6f, 0x5f, 0x69, 0x64, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0c,
	0x52, 0x09, 0x72, 0x65, 0x70, 0x6c, 0x79, 0x54, 0x6f, 0x49, 0x64, 0x12, 0x19, 0x0a, 0x08, 0x6e,
	0x6f, 0x5f, 0x72, 0x65, 0x70, 0x6c, 0x79, 0x18, 0x05, 0x20, 0x01, 0x28, 0x08, 0x52, 0x07, 0x6e,
	0x6f, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x1e, 0x0a, 0x08, 0x54, 0x65, 0x78, 0x74, 0x44, 0x61,
	0x74, 0x61, 0x12, 0x12, 0x0a, 0x04, 0x74, 0x65, 0x78, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x04, 0x74, 0x65, 0x78, 0x74, 0x2a, 0x39, 0x0a, 0x0b, 0x50, 0x61, 0x79, 0x6c, 0x6f, 0x61,
	0x64, 0x54, 0x79, 0x70, 0x65, 0x12, 0x0a, 0x0a, 0x06, 0x42, 0x49, 0x4e, 0x41, 0x52, 0x59, 0x10,
	0x00, 0x12, 0x08, 0x0a, 0x04, 0x54, 0x45, 0x58, 0x54, 0x10, 0x01, 0x12, 0x07, 0x0a, 0x03, 0x41,
	0x43, 0x4b, 0x10, 0x02, 0x12, 0x0b, 0x0a, 0x07, 0x53, 0x45, 0x53, 0x53, 0x49, 0x4f, 0x4e, 0x10,
	0x03, 0x42, 0x0c, 0x5a, 0x0a, 0x2e, 0x2f, 0x70, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x73, 0x62,
	0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_payloads_payloads_proto_rawDescOnce sync.Once
	file_payloads_payloads_proto_rawDescData = file_payloads_payloads_proto_rawDesc
)

func file_payloads_payloads_proto_rawDescGZIP() []byte {
	file_payloads_payloads_proto_rawDescOnce.Do(func() {
		file_payloads_payloads_proto_rawDescData = protoimpl.X.CompressGZIP(file_payloads_payloads_proto_rawDescData)
	})
	return file_payloads_payloads_proto_rawDescData
}

var file_payloads_payloads_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_payloads_payloads_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_payloads_payloads_proto_goTypes = []interface{}{
	(PayloadType)(0), // 0: payloads.PayloadType
	(*Message)(nil),  // 1: payloads.Message
	(*Payload)(nil),  // 2: payloads.Payload
	(*TextData)(nil), // 3: payloads.TextData
}
var file_payloads_payloads_proto_depIdxs = []int32{
	0, // 0: payloads.Payload.type:type_name -> payloads.PayloadType
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_payloads_payloads_proto_init() }
func file_payloads_payloads_proto_init() {
	if File_payloads_payloads_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_payloads_payloads_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Message); i {
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
		file_payloads_payloads_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Payload); i {
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
		file_payloads_payloads_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TextData); i {
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
			RawDescriptor: file_payloads_payloads_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_payloads_payloads_proto_goTypes,
		DependencyIndexes: file_payloads_payloads_proto_depIdxs,
		EnumInfos:         file_payloads_payloads_proto_enumTypes,
		MessageInfos:      file_payloads_payloads_proto_msgTypes,
	}.Build()
	File_payloads_payloads_proto = out.File
	file_payloads_payloads_proto_rawDesc = nil
	file_payloads_payloads_proto_goTypes = nil
	file_payloads_payloads_proto_depIdxs = nil
}
