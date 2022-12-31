package toto

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

type TypedDescriptor interface {
	Enums() protoreflect.EnumDescriptors
	Extensions() protoreflect.ExtensionDescriptors
	Messages() protoreflect.MessageDescriptors
}

func ConvertFilesToTypes(files *protoregistry.Files) (*protoregistry.Types, error) {
	types := &protoregistry.Types{}

	var outerErr error
	files.RangeFiles(func(desc protoreflect.FileDescriptor) bool {
		if err := typedConverter(types, desc); err != nil {
			outerErr = err
			return false
		}
		return true
	})

	if outerErr != nil {
		return nil, outerErr
	}
	return types, nil
}

func typedConverter(types *protoregistry.Types, desc TypedDescriptor) error {
	// register enums
	es := desc.Enums()
	for i := 0; i < es.Len(); i++ {
		e := es.Get(i)
		if err := types.RegisterEnum(dynamicpb.NewEnumType(e)); err != nil {
			return err
		}
	}

	// register extensions
	xs := desc.Extensions()
	for i := 0; i < xs.Len(); i++ {
		x := xs.Get(i)
		if err := types.RegisterExtension(dynamicpb.NewExtensionType(x)); err != nil {
			return err
		}
	}

	// register messages
	ms := desc.Messages()
	for i := 0; i < ms.Len(); i++ {
		m := ms.Get(i)
		if err := types.RegisterMessage(dynamicpb.NewMessageType(m)); err != nil {
			return err
		}

		if err := typedConverter(types, m); err != nil {
			return err
		}
	}

	return nil
}
