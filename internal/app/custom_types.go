package app

import (
	"encoding/json"
	"reflect"
	"strconv"
)

var (
	True  = Boolean{HasValue: true, Value: true}
	False = Boolean{HasValue: true, Value: false}
)

type Boolean struct {
	Value    bool
	HasValue bool // true if bool is not null
}

func (b Boolean) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Value)
}

func (b *Boolean) UnmarshalJSON(data []byte) error {
	var unmarshalledJson bool

	err := json.Unmarshal(data, &unmarshalledJson)
	if err != nil {
		return err
	}

	b.Value = unmarshalledJson
	b.HasValue = true

	return nil
}

func (b *Boolean) UnmarshalText(text []byte) error {
	str := string(text)
	if len(str) < 1 {
		return nil
	}

	value, err := strconv.ParseBool(str)
	if err != nil {
		return err
	}

	b.HasValue = true
	b.Value = value

	return nil
}

type MergoTransformer func(typ reflect.Type) func(dst, src reflect.Value) error

func (m MergoTransformer) Transformer(typ reflect.Type) func(dst, src reflect.Value) error {
	return m(typ)
}

func BoolTransformer(typ reflect.Type) func(dst, src reflect.Value) error {
	if typ != reflect.TypeOf(Boolean{}) {
		return nil
	}

	return func(dst, src reflect.Value) error {
		if src.FieldByName("HasValue").Bool() {
			dst.Set(src)
		}
		return nil
	}
}
