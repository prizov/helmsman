package app

import (
	"encoding/json"
	"strconv"
)

var (
	True  = Bool{HasValue: true, Value: true}
	False = Bool{HasValue: true, Value: false}
)

type Bool struct {
	Value    bool
	HasValue bool // true if bool is not null
}

func (b Bool) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Value)
}

func (b *Bool) UnmarshalJSON(data []byte) error {
	var unmarshalledJson bool

	err := json.Unmarshal(data, &unmarshalledJson)
	if err != nil {
		return err
	}

	b.Value = unmarshalledJson
	b.HasValue = true

	return nil
}

func (b *Bool) UnmarshalText(text []byte) error {
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
