package fileloader_test

import (
	"github.com/MakeNowJust/heredoc"

	"github.com/danielorbach/go-component/fileloader"
)

func ExampleJSONLoader() {
	var json = heredoc.Doc(`
	{
		"name": "Static ping-pong-probe",
		"metadata": "This is a static footprint loading three components: ping, pong, and probe.\n",
		"identifier": "0abcdef0-b00b-0000-b00b-000000000000",
		"locations": null,
		"components": {
			"ping": {
				"location": null,
				"options": {
					"data": "Hello, world"
				},
				"aspects": {
					"ping": "ping-topic"
				},
				"interests": null
			},
			"pong": {
				"location": "",
				"aspects": {
					"pong": "pong-topic"
				},
				"interests": {
					"ping": "ping-topic"
				}
			},
			"probe": {
				"aspects": null,
				"interests": {
					"pong": "pong-topic"
				},
				"options": {
					"timeout": "2100ms"
				}
			}
		}
	}
	`)
	fileloader.Load([]byte(json), fileloader.FormatJSON)
}
