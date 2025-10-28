package loaderflags

import (
	"flag"
	"strconv"
	"testing"

	"github.com/peterbourgon/ff/v3"
)

// Test_ffEnvName fails when ffEnvName drifts from the internal implementation of
// github.com/peterbourgon/ff/v3.
func Test_ffEnvName(t *testing.T) {
	// interesting environment-variable names
	testNames := []string{
		"foo",
		"foo_bar",
		"foo_bar-baz",
		"foo_bar-baz.qux",
		"foo_bar-baz.qux/quux",
		"foo_bar-baz.qux/quux!@#$%^&*()+corge",
	}

	// define interesting variables on the command line
	fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	flags := make(map[string]*string, len(testNames)) // map[variableName]value
	const indicator = "success"
	for _, name := range testNames {
		flags[name] = fs.String(name, "", "testing: "+strconv.Quote(name))
		env := ffEnvName(name, "")
		t.Logf("Flag %q is set via environment variable %q", name, env)
		t.Setenv(env, indicator)
	}

	// parse the command line
	err := ff.Parse(fs, []string{}, ff.WithEnvVars())
	if err != nil {
		t.Fatal("Parse flags:", err)
	}

	// check that the environment variables were set
	for name, value := range flags {
		if *value != indicator {
			t.Errorf("Flag %q was not set", name)
		}
	}
}

// Test_boolEnviron asserts github.com/peterbourgon/ff/v3 parses boolean flags
// from environment variables correctly.
func Test_boolEnviron(t *testing.T) {
	fs := flag.NewFlagSet(t.Name(), flag.ContinueOnError)

	// check the flavour of exporting an environment variable with "true" string
	envTrue := fs.Bool("true", false, "testing -flag=true")
	t.Setenv("TEST_TRUE", "true")

	// check the flavour of exporting an environment variable with "false" string
	envFalse := fs.Bool("false", true, "testing -flag=false")
	t.Setenv("TEST_FALSE", "false")

	// check the flavour of exporting an environment variable with no value
	envEmpty := fs.Bool("empty", false, "testing -flag")
	t.Setenv("TEST_EMPTY", "")

	// parse the command line
	err := ff.Parse(fs, []string{}, ff.WithEnvVarPrefix("TEST"))
	if err != nil {
		t.Fatal("Parse flags:", err)
	}

	// setting the environment variable to "true" should set the flag to true
	if !*envTrue {
		t.Errorf("Flag pattern %q was not set, or set incorrectly", "true")
	}
	// setting the environment variable to "false" should set the flag to false
	if *envFalse {
		t.Errorf("Flag pattern %q was not set, or set incorrectly", "false")
	}
	// setting the environment variable to "" should not have any effect
	//
	// personally, I would prefer this to set the flag to true, but that's not how
	// github.com/peterbourgon/ff/v3 works.
	if *envEmpty {
		t.Errorf("Flag pattern %q was not set, or set incorrectly", "empty")
	}
}
