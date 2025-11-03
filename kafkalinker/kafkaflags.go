package kafkalinker

import (
	"encoding"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/rcrowley/go-metrics"
)

// ConfigFlag enhances the Sarama Config by integrating with command-line tools
// to dynamically set Kafka configuration options. This type allows flexible and
// reflective manipulation of configuration fields.
type ConfigFlag sarama.Config

// The func implements the flag.Value interface, returning a string
// representation of the flag's default value.
//
// If the config differs from the minimal config, it indicates that.
// Otherwise, it confirms that the configuration is indeed the default.
func (f *ConfigFlag) String() string {
	res := diffConfig(MinimalConfig(), (*sarama.Config)(f))
	if res != "" {
		return "modified config"
	}
	return "minimal config"
}

// Set assigns a key=value pair to the pointer [sarama.Config] using reflection.
//
// It supports fields with basic types: string, bool, signed and unsigned
// integers, floats, []byte, time.Duration, and types that implement
// encoding.TextUnmarshaler. The [sarama.Config.Version] field is uniquely
// supported by parsing according to [sarama.KafkaVersion].
//
// The key input must adhere to these rules:
//   - Each segment of the path should refer to a field within a struct at the
//     corresponding level.
//   - Fields that are not exported cannot be accessed.
//   - Pointers must be allocated (non-nil) to allow dereferencing. If a pointer is
//     nil, it indicates the absence of the underlying struct, and the function
//     will not be able to find and set the field.
//
// Attempting to set unsupported types will result in a non-nil error.
//
// If the value "nil" is provided, the field will be set to the zero value for
// its type, effectively clearing or resetting the field. The function returns
// an error if the key is not found, is not settable, or the value type is
// unsupported.
func (f *ConfigFlag) Set(s string) error {
	key, value, found := strings.Cut(s, "=")
	if !found {
		return fmt.Errorf("missing value for key %q", key)
	}
	slog.Debug("Setting Kafka config...", "key", key, "value", value)

	// Attempt to identify the field by name in the sarama.Config struct.
	field, ok := f.findFieldByName(key)
	if !ok {
		return fmt.Errorf("unknown field %q", key)
	}

	// Special handling for setting to nil. If the value is explicitly "nil", we set
	// the field to its zero value.
	if value == "nil" {
		field.Set(reflect.Zero(field.Type()))
		slog.Info("Kafka config field has been zeroed", "field", key)
		return nil
	}

	err := f.setField(field, value)
	if err != nil {
		return fmt.Errorf("field %v: %w", key, err)
	}

	slog.Info("Kafka config field has been set", "field", key, "value", value)
	return nil
}

// The func locates a nested field within the pointed [sarama.Config] value
// instance using reflection. The provided key is a string that represents the
// path to the field, with each component of the path separated by a dot ('.').
//
// The key input must adhere to these rules:
//   - Each segment of the path should refer to a field within a struct at the
//     corresponding level.
//   - Fields that are not exported cannot be accessed. This will result in
//     a not-found return value.
//   - Pointers must be allocated (non-nil) to allow dereferencing. If a pointer is
//     nil, it indicates the absence of the underlying struct, and the function
//     will not be able to find the field.
//
// The function returns the found field's reflect.Value and a boolean that is
// true if the field is found and valid, false otherwise.
func (f *ConfigFlag) findFieldByName(key string) (field reflect.Value, ok bool) {
	value := reflect.ValueOf(f).Elem() // Start with the ConfigFlag underlying value.

	for _, name := range strings.Split(key, ".") {
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				// Can't dereference empty pointer, returning empty field.
				return reflect.Value{}, false
			}
			value = value.Elem()
		}

		// At this point, the user has indicated a desire to select a field of a struct.
		// This is not possible if the currently selected value (a nested field of
		// sarama.Config) is not a struct.
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}

		value = value.FieldByName(name)
		if !value.IsValid() {
			// FieldByName returns an invalid reflect.Value when the desired field cannot be
			// found. In which case, the function returns false to indicate the given path does
			// not lead to any field.
			return reflect.Value{}, false
		}
	}

	return value, true
}

// The func converts the given string value to the most appropriate type
// according to the type of the given nested field of the [sarama.Config] pointed
// by this flag. This function is called by [Set] to modify the configuration of
// Kafka clients using reflection.
//
// The function returns an error if the string cannot be converted to the
// field's type, or if the field type is unsupported.
func (f *ConfigFlag) setField(field reflect.Value, value string) error {
	slog.Debug("Setting Kafka config field", "field", field, "value", value)
	// The function first tries to unmarshal the given text value into the given
	// field, if that field implements text unmarshalling.
	if x, ok := field.Interface().(encoding.TextUnmarshaler); ok {
		if err := x.UnmarshalText([]byte(value)); err != nil {
			return fmt.Errorf("unmarshal text: %w", err)
		}
		return nil
	}

	// Check if field is bytes array.
	if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8 {
		field.Set(reflect.ValueOf([]byte(value)))
		return nil
	}

	// Sometimes, types implement an interface only on a pointer receiver.
	//
	// Addr is safe to call without panic because the field reflect.Value is always
	// a field of a struct, which is addressable.
	if x, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok {
		if err := x.UnmarshalText([]byte(value)); err != nil {
			return fmt.Errorf("unmarshal text: %w", err)
		}
		return nil
	}

	// Duration fields require special parsing because humans use many ways to
	// express time durations. Go supports a standard format (e.g. 1h2m3s), which we
	// adopt for predictability and uniformity.
	if field.Type() == reflect.TypeFor[time.Duration]() {
		v, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		field.Set(reflect.ValueOf(v))
		return nil
	}

	// Fields of type sarama.KafkaVersion require parsing for consistent version
	// management. Using sarama.ParseKafkaVersion converts version strings (e.g.
	// "2.2.0.0") into structured sarama.KafkaVersion objects, ensuring compatibility
	// and preventing configuration errors.
	if _, ok := field.Interface().(sarama.KafkaVersion); ok {
		version, err := sarama.ParseKafkaVersion(value)
		if err != nil {
			return fmt.Errorf("parse version: %w", err)
		}
		field.Set(reflect.ValueOf(version))
		return nil
	}

	// Fields of primitive types are trivial to set from string values. More complex
	// fields (e.g. collections) are not supported at the moment.
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool: %w", err)
		}
		field.SetBool(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, err := strconv.ParseInt(value, 10, field.Type().Bits()); err == nil {
			field.SetInt(v)
		} else {
			return fmt.Errorf("invalid int: %w", err)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, err := strconv.ParseUint(value, 10, field.Type().Bits()); err == nil {
			field.SetUint(v)
		} else {
			return fmt.Errorf("invalid uint: %w", err)
		}
	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(value, field.Type().Bits()); err == nil {
			field.SetFloat(v)
		} else {
			return fmt.Errorf("invalid float: %w", err)
		}
	default:
		return fmt.Errorf("unsupported type %v", field.Type())
	}
	return nil
}

// A diffConfig compares two *sarama.Config values and returns a string
// describing the differences. It uses custom comparison rules to account
// for certain fields that are not directly comparable or are intended
// to be ignored in configuration comparison.
//
// Specifically, the function:
//   - Considers two sarama.KafkaVersion instances with default values
//     as equivalent.
//   - Compares sarama.BalanceStrategy instances based on their strategy
//     names, ignoring differences if both are nil or if their names match.
//   - Ignores metrics.Registry implementations and the Partitioner field
//     within the Producer struct to exclude runtime-specific or variable
//     implementation details.
//
// This function is useful for determining configuration mismatches that
// affect the behavior of Kafka clients, while ignoring elements that are
// not relevant to configuration comparisons.
func diffConfig(want, got *sarama.Config) string {
	var opts = []cmp.Option{
		// Values of type sarama.KafkaVersion are comparable, so we instruct cmp.Diff to
		// compare them using the builtin == operator instead of the default reflection
		// algorithm.
		cmpopts.EquateComparable(sarama.KafkaVersion{}),
		// A field of type sarama.BalanceStrategy is an interface field, which is unsafe
		// to compare using the builtin equality operator. Instead, for
		// sarama.BalanceStrategy, we can rely on the Name method.
		cmp.Comparer(func(want, got sarama.BalanceStrategy) bool {
			// In Go, two interface variables pointing to a nil value are always equal, so
			// this custom comparer must respect that logic.
			if want == nil && got == nil {
				return true
			}
			// Otherwise, we rely on proper implementations of the interface and compare
			// strategies based on their name. We cannot simply compare with == because using
			// the builtin equality operator is unsafe as it may panic depending on the
			// concrete types.
			if want != nil && got != nil {
				return want.Name() == got.Name()
			}
			// If one is nil and the other is not, they are not equal.
			return false
		}),
		// If the configurations were instantiated or modified separately, they might
		// point to different registry instances, especially if they are initialized at
		// different times or under different conditions.
		cmpopts.IgnoreInterfaces(struct{ metrics.Registry }{}),
		// Ignore the sarama.Config.Producer.Partitioner field since it, often being a
		// function pointer, varies by implementation context.
		cmpopts.IgnoreFields(sarama.Config{}.Producer, "Partitioner"),
	}
	return cmp.Diff(want, got, opts...)
}

// BrokersFlag implements flag.Value for a comma-separated list of Kafka brokers.
type BrokersFlag []string

// Get returns the wrapped list of brokers as a []string.
func (f *BrokersFlag) Get() any {
	return *f
}

// String returns a comma-separated string representation of the list of brokers.
func (f *BrokersFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

// Set splits a comma-separated value string into individual broker addresses,
// updating the wrapped list of brokers.
func (f *BrokersFlag) Set(s string) error {
	brokers := strings.Split(s, ",")
	*f = brokers
	slog.Info("A flag has modified a list of Kafka brokers", "brokers", brokers)
	return nil
}
