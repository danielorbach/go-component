package fileloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/uuid"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/kafkalinker"
	"github.com/danielorbach/go-component/loader"
)

type FootprintJSON struct {
	Name       string // human-readable name (does not need to be unique)
	Metadata   string // human-readable description/summary/notes/comments
	Solution   string // attribute this footprint to a solution
	Identifier uuid.UUID
	Revision   int
	Locations  []string // IGNORED
	Components map[string]ComponentJSON
}

type ComponentJSON struct {
	Location  string            // IGNORED
	Options   json.RawMessage   // unmarshal into an appropriate Options type
	Aspects   map[string]string // map[aspect]topic
	Interests map[string]string // map[interest]topic
}

// A JSONLoader implements component.Procedure loading
// its json-encoded blob of a loader.Footprint.
type JSONLoader []byte

func (x JSONLoader) Exec(l *component.L) {
	fp, err := UnmarshalFootprintJSON(x)
	if err != nil {
		l.Fatalf("json: %w", err)
	}
	loader.Load(fp)
}

func UnmarshalFootprintJSON(data []byte) (loader.Footprint, error) {
	components := make(map[string]*component.Descriptor, len(loader.Descriptors))
	for _, d := range loader.Descriptors {
		components[d.Name] = d
	}

	var msg FootprintJSON
	if err := json.Unmarshal(data, &msg); err != nil {
		return loader.Footprint{}, fmt.Errorf("unmarshal: %w", err)
	}

	fp := loader.Footprint{
		Name:        msg.Name,
		Metadata:    msg.Metadata,
		Solution:    msg.Solution,
		Identifier:  msg.Identifier,
		Revision:    msg.Revision,
		Locations:   msg.Locations,
		Allocations: make([]*loader.Claim, 0, len(msg.Components)),
	}
	for name, req := range msg.Components {
		d, ok := components[name]
		if !ok {
			continue // ignore unknown (or disabled) components
		}

		if err := validateTargets(d, req.Aspects, req.Interests); err != nil {
			return loader.Footprint{}, fmt.Errorf("component %q: %w", name, err)
		}

		options, err := unmarshalOptionsJSON(d.OptionsType, req.Options)
		if err != nil {
			return loader.Footprint{}, fmt.Errorf("component %q options: %w", name, err)
		}

		id := fmt.Sprintf("%s/%s", fp.Identifier, d.Name)
		binding, err := kafkalinker.New(id, req.Aspects, req.Interests)
		if err != nil {
			return loader.Footprint{}, fmt.Errorf("component %q binding: %w", name, err)
		}

		fp.Allocations = append(fp.Allocations, &loader.Claim{
			Component: d,
			Options:   options,
			Binding:   binding,
		})
	}

	return fp, nil

}

func unmarshalOptionsJSON(rt reflect.Type, data json.RawMessage) (any, error) {
	// when a component has no options type, the data provided should be empty as well
	if rt == nil {
		if len(data) > 0 && !bytes.EqualFold(data, []byte("null")) {
			return nil, fmt.Errorf("options type is nil but data is not empty")
		}
		return nil, nil
	}

	// must not dereference the allocated pointer, otherwise json.Unmarshal confuses
	// &reflect.New(d.OptionsType).Elem().Interface() with a interface{} - which is
	// unmarshalled as map[string]interface{}. yes - this really changes the concrete
	// value pointed to by a variable of type interface{}.
	options := reflect.New(rt)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // strict mode
	if err := dec.Decode(options.Interface()); err != nil {
		return nil, err
	}
	// now it is safe to dereference the pointer and access the already unmarshalled
	// value within
	return options.Elem().Interface(), nil
}
