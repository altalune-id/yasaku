package gen

import (
	"bytes"
	"encoding/json"
	"slices"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	timestampName = protoreflect.FullName("google.protobuf.Timestamp")
	int64Pattern  = `^-?[0-9]+$`
)

type jsonObject struct {
	keys []string
	vals []any
}

func newObject() *jsonObject { return &jsonObject{} }

func (o *jsonObject) set(key string, val any) *jsonObject {
	o.keys = append(o.keys, key)
	o.vals = append(o.vals, val)
	return o
}

func (o *jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		b.Write(encoded)
		b.WriteByte(':')
		if encoded, err = json.Marshal(o.vals[i]); err != nil {
			return nil, err
		}
		b.Write(encoded)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func inputSchema(msg *protogen.Message) (json.RawMessage, error) {
	return json.Marshal(messageSchema(msg, []protoreflect.FullName{msg.Desc.FullName()}))
}

func messageSchema(msg *protogen.Message, stack []protoreflect.FullName) *jsonObject {
	props := newObject()
	for _, field := range msg.Fields {
		props.set(field.Desc.JSONName(), fieldSchema(field, stack))
	}
	return newObject().
		set("type", "object").
		set("properties", props).
		set("additionalProperties", false)
}

func fieldSchema(field *protogen.Field, stack []protoreflect.FullName) *jsonObject {
	var schema *jsonObject
	switch {
	case field.Desc.IsMap():
		schema = newObject().
			set("type", "object").
			set("additionalProperties", valueSchema(field.Message.Fields[1], stack))
	case field.Desc.IsList():
		schema = newObject().
			set("type", "array").
			set("items", valueSchema(field, stack))
	default:
		schema = valueSchema(field, stack)
	}
	if description := comment(field.Comments.Leading); description != "" {
		schema.set("description", description)
	}
	return schema
}

func valueSchema(field *protogen.Field, stack []protoreflect.FullName) *jsonObject {
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return newObject().set("type", "boolean")
	case protoreflect.StringKind:
		return newObject().set("type", "string")
	case protoreflect.BytesKind:
		return newObject().set("type", "string").set("format", "byte")
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return newObject().set("type", "number")
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return newObject().set("type", "integer")
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return newObject().set("type", "string").set("pattern", int64Pattern)
	case protoreflect.EnumKind:
		return enumSchema(field.Enum)
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return nestedSchema(field.Message, stack)
	default:
		return newObject().set("type", "string")
	}
}

func enumSchema(enum *protogen.Enum) *jsonObject {
	if enum == nil {
		return newObject().set("type", "string")
	}
	values := make([]string, 0, len(enum.Values))
	for _, value := range enum.Values {
		values = append(values, string(value.Desc.Name()))
	}
	return newObject().set("type", "string").set("enum", values)
}

func nestedSchema(msg *protogen.Message, stack []protoreflect.FullName) *jsonObject {
	if msg == nil {
		return newObject().set("type", "object")
	}
	name := msg.Desc.FullName()
	if name == timestampName {
		return newObject().set("type", "string").set("format", "date-time")
	}
	if slices.Contains(stack, name) {
		return newObject().set("type", "object")
	}
	return messageSchema(msg, append(slices.Clone(stack), name))
}
