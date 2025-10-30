package kafkalinker

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/go-cmp/cmp"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader"
	"github.com/danielorbach/go-component/loader/loaderflags"
)

// TestSettingConfig tests the ConfigFlag.Set() method for setting
// configuration values via key=value string pairs. It verifies correct behavior
// for valid keys (happy paths) and for invalid inputs.
func TestSettingConfig(t *testing.T) {
	type testCase struct {
		label  string // Pretty test label, used to identify the test-case in logs.
		kvPair string // A key=value string pair.
	}

	happyTests := []testCase{
		{label: "a string field", kvPair: "ClientID=client"},
		{label: "an int field", kvPair: "ChannelBufferSize=1"},
		{label: "a bool field", kvPair: "Metadata.Full=true"},
		{label: "a bytes field", kvPair: "Consumer.Group.Member.UserData=some\x20bytes"},
		{label: "an enum field", kvPair: "Net.SASL.Mechanism=PLAIN"},
		{label: "a multilevel int field", kvPair: "Consumer.Offsets.Retry.Max=1"},
		{label: "a duration field", kvPair: "Consumer.Offsets.AutoCommit.Interval=3s"},
		{label: "an exported struct value", kvPair: "Version=2.2.2"},
	}

	got := MinimalConfig()

	for _, tc := range happyTests {
		t.Logf("Setting %v...", tc.label)
		err := (*ConfigFlag)(got).Set(tc.kvPair)

		if err != nil {
			t.Errorf("Set(%q) failed with error: %v", tc.kvPair, err)
		}
	}
	// The test cases modify a minimal config one by one, eventually culminating in
	// the following configuration:
	want := MinimalConfig()
	want.ClientID = "client"
	want.ChannelBufferSize = 1
	want.Metadata.Full = true
	want.Consumer.Group.Member.UserData = []byte("some\x20bytes")
	want.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	want.Consumer.Offsets.Retry.Max = 1
	want.Consumer.Offsets.AutoCommit.Interval = 3 * time.Second
	want.Version = sarama.V2_2_2_0

	if diff := diffConfig(want, got); diff != "" {
		t.Fatalf("Configs are different (-want +got):\n%s", diff)
	}

	sadTests := []testCase{
		{label: "a field with missing value", kvPair: "ClientID"},
		{label: "an invalid int type", kvPair: "ChannelBufferSize=notInt"},
		{label: "an invalid bool type", kvPair: "Metadata.Full=notABool"},
		{label: "an unknown field", kvPair: "UnknownField=value"},
		{label: "a pointer field", kvPair: "Net.TLS.Config=value"},
		{label: "a slice field with unsupported type", kvPair: "Producer.Interceptors=value"},
		{label: "a duration field with non-duration value", kvPair: "Metadata.Retry.Backoff=fail"},
		{label: "an unknown nested field", kvPair: "Metadata.Retry.Backoff.UnknownField=irrelevant"},
	}

	got = MinimalConfig()
	for _, tc := range sadTests {
		t.Logf("Setting %v should fail...", tc.label)
		err := (*ConfigFlag)(got).Set(tc.kvPair)

		if err == nil {
			t.Errorf("Set(%q) returned nil error", tc.kvPair)
		} else {
			t.Logf("Set(%q) failed as expected with error: %v", tc.kvPair, err)
		}
	}
}

// TestParsingBrokers tests the flag parsing behavior if command-line arguments
// used to configure Kafka brokers list. It verifies that the resulting brokers
// configuration matches expectations.
//
// The test is structured around test cases that define specific command-line
// inputs along with the expected final configuration.
//
// It uses the testing framework to execute and log each test scenario, ensuring
// isolation by resetting configurations between runs.
func TestBrokersFlagSet(t *testing.T) {
	testCases := []struct {
		label           string
		input           string
		expectedBrokers []string
	}{
		{
			label:           "single broker",
			input:           "broker1:9092",
			expectedBrokers: []string{"broker1:9092"},
		},
		{
			label:           "multiple brokers",
			input:           "broker1:9092,broker2:9093,broker3:9094",
			expectedBrokers: []string{"broker1:9092", "broker2:9093", "broker3:9094"},
		},
		{
			label:           "trailing comma",
			input:           "broker1:9092,",
			expectedBrokers: []string{"broker1:9092", ""},
		},
		{
			label:           "empty input",
			input:           "",
			expectedBrokers: []string{""},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			var brokersFlag BrokersFlag

			t.Logf("Setting %v...", tc.label)
			err := brokersFlag.Set(tc.input)
			if err != nil {
				t.Fatalf("BrokersFlag.Set(%q) failed with error: %v", tc.input, err)
			}

			// Convert brokersFlag to a regular []string for comparison.
			actualBrokers := []string(brokersFlag)

			if diff := cmp.Diff(tc.expectedBrokers, actualBrokers); diff != "" {
				t.Errorf("Brokers are different (-expected +got):\n%s", diff)
			}
		})
	}
}

// TestParsingConfig tests the flag parsing behavior when command-line arguments
// and env vars are used to configure Kafka settings. It verifies that the
// resulting configuration matches expectations.

// The test is structured around test cases that define specific command-line
// inputs and env var configuration along with the expected final configuration.

// It uses the testing framework to execute and log each test scenario, ensuring
// isolation by resetting configurations between runs.
func TestParsingConfig(t *testing.T) {
	type testCase struct {
		label          string // Pretty test label, used to identify the test-case in logs.
		cmd            []string
		env            map[string]string
		expectedConfig func() *sarama.Config
	}

	testCases := []testCase{
		{
			label: "multiple fields from flags",
			cmd: []string{
				"-kafka-config", "ClientID=TestClient",
				"-kafka-config", "Consumer.Offsets.AutoCommit.Interval=3s",
				"-kafka-config", "Net.SASL.Mechanism=PLAIN",
			},
			expectedConfig: func() *sarama.Config {
				cfg := MinimalConfig()
				cfg.ClientID = "TestClient"
				cfg.Consumer.Offsets.AutoCommit.Interval = 3 * time.Second
				cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
				return cfg
			},
		},
		{
			label: "multiple fields from env vars",
			env: map[string]string{
				"KAFKA_CONFIG": "Consumer.Offsets.Retry.Max=1;ClientID=TestClient;Metadata.Full=true",
			},
			expectedConfig: func() *sarama.Config {
				cfg := MinimalConfig()
				cfg.Consumer.Offsets.Retry.Max = 1
				cfg.ClientID = "TestClient"
				cfg.Metadata.Full = true
				return cfg
			},
		},
		{
			label: "the same field twice in repeated flags",
			cmd: []string{
				"-kafka-config", "ClientID=FirstClient",
				"-kafka-config", "ClientID=SecondClient",
				"-kafka-config", "Consumer.Offsets.AutoCommit.Interval=3s",
				"-kafka-config", "Consumer.Offsets.AutoCommit.Interval=5s",
				"-kafka-config", "Net.SASL.Mechanism=PLAIN",
				"-kafka-config", "Net.SASL.Mechanism=GSSAPI",
			},
			expectedConfig: func() *sarama.Config {
				cfg := MinimalConfig()
				cfg.ClientID = "SecondClient"
				cfg.Consumer.Offsets.AutoCommit.Interval = 5 * time.Second
				cfg.Net.SASL.Mechanism = sarama.SASLTypeGSSAPI
				return cfg
			},
		},
		{
			label: "the same field twice in repeated env vars",
			env: map[string]string{
				"KAFKA_CONFIG": "ClientID=firstEnvClient;ClientID=SecondCmdClient;Net.SASL.Mechanism=GSSAPI;Net.SASL.Mechanism=PLAIN;Version=1.1.1;Version=2.2.2",
			},
			expectedConfig: func() *sarama.Config {
				cfg := MinimalConfig()
				cfg.ClientID = "SecondCmdClient"
				cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
				cfg.Version = sarama.V2_2_2_0
				return cfg
			},
		},
		{
			label: "the same field twice in a flag and in an env var",
			// When a flag is in both cmd and env vars- cmd takes precedence and env vars are
			// ignored.
			cmd: []string{
				"-kafka-config", "ClientID=FirstClient",
			},
			env: map[string]string{
				"KAFKA_CONFIG": "ClientID=firstEnvClient;ClientID=secondEnvClient;Version=1.1.1",
			},
			expectedConfig: func() *sarama.Config {
				cfg := MinimalConfig()
				cfg.ClientID = "FirstClient"
				return cfg
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			t.Logf("Parsing %v...", tc.label)
			// Initialize a clean Kafka configuration object.
			want := tc.expectedConfig()
			got := MinimalConfig()

			// Init env vars.
			for v := range tc.env {
				t.Setenv(v, tc.env[v])
			}
			// Backup original os.Args and set command-line arguments
			// for loaderflags.ParseFlags().
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{loader.ProgramName()}, tc.cmd...)

			// Initialize Kafka's flagset.
			fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
			fs.Var((*ConfigFlag)(got), "kafka-config", "key value pairs of kafka configuration as in sarama.Config type")

			if _, err := loaderflags.ParseFlags(fs, []*component.Descriptor{{Name: "test-comp"}}, true); err != nil {
				t.Fatalf("loaderflags.ParseFlags failed with error: %v", err)
			}

			if diff := diffConfig(want, got); diff != "" {
				t.Fatalf("Configs are different (-want +got):\n%s", diff)
			}
		})
	}
}
