package fileloader

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/kafkalinker"
	"github.com/danielorbach/go-component/loader"
)

type FootprintYAML struct {
	Name       string // human-readable name (does not need to be unique)
	Metadata   string // human-readable description/summary/notes/comments
	Solution   string // attribute this footprint to a solution
	Identifier uuid.UUID
	Revision   int
	Locations  []string // IGNORED
	Components map[string]struct {
		Location  string
		Options   yaml.Node         // unmarshal into an appropriate Options type
		Aspects   map[string]string // map[aspect]topic
		Interests map[string]string // map[interest]topic
	}
}

// A YAMLLoader implements component.Procedure loading
// its yaml-encoded blob of a loader.Footprint.
type YAMLLoader []byte

func (x YAMLLoader) Exec(l *component.L) {
	fp, err := x.footprint()
	if err != nil {
		slog.ErrorContext(l.Context(), "load YAML footprint", "err", err)
		return
	}
	loader.Load(fp)
}

func (x YAMLLoader) footprint() (loader.Footprint, error) {
	components := make(map[string]*component.Descriptor, len(loader.Descriptors))
	for _, d := range loader.Descriptors {
		components[d.Name] = d
	}

	var msg FootprintYAML
	if err := yaml.Unmarshal(x, &msg); err != nil {
		return loader.Footprint{}, fmt.Errorf("unmarshal: %w", err)
	}

	fp := loader.Footprint{
		Name:        msg.Name,
		Metadata:    msg.Metadata,
		Identifier:  msg.Identifier,
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

		options, err := x.options(d.OptionsType, req.Options)
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

func (YAMLLoader) options(rt reflect.Type, data yaml.Node) (any, error) {
	// when a component has no options type, the data provided should be empty as well
	if rt == nil {
		// TODO: check YAML null
		//if len(data) > 0 && !bytes.Equal(data, []byte("null")) {
		//	return nil, fmt.Errorf("options type is nil but data is not empty")
		//}
		return nil, nil
	}

	// must not dereference the allocated pointer, otherwise yaml.Unmarshal confuses
	// &reflect.New(d.OptionsType).Elem().Interface() with a interface{} - which is
	// unmarshalled as map[string]interface{}. yes - this really changes the concrete
	// value pointed to by a variable of type interface{}.
	v := reflect.New(rt).Interface()
	if err := data.Decode(v); err != nil {
		return nil, err
	}
	// now it is safe to dereference the pointer and access the already unmarshalled
	// value within
	options := reflect.ValueOf(v).Elem().Interface()
	return options, nil
}
