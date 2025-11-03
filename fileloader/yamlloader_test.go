package fileloader_test

import (
	"github.com/MakeNowJust/heredoc"

	"github.com/danielorbach/go-component/fileloader"
)

func ExampleYAMLLoader() {
	var yaml = heredoc.Doc(`
		name: Static ping-pong-probe
		metadata: |
		  This is a static footprint loading three components: ping, pong, and probe.
		identifier: "0abcdef0-b00b-0000-b00b-000000000000"
		
		locations: ~ # a tilde (~) character is an alias for null
		
		components:
		  ping:
			location: null # null is a defined literal of YAML (https://yaml.org/type/null.html)
			options:
			  data: !!binary "SGVsbG8sIHdvcmxkIQ=="
			aspects:
			  ping: "ping-topic"
			interests: # omitting value equals to a null value
		
		  pong:
			location: "" # an empty string is the null value of a string variable in Go
			# options: # we can omit fields as-well
			aspects:
			  pong: "pong-topic"
			interests:
			  ping: "ping-topic"
		
		  probe:
			aspects:
			interests:
			  pong: "pong-topic"
			options:
			  timeout: 2100ms
	`)
	fileloader.Load([]byte(yaml), fileloader.FormatYAML)
}
