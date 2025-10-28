package loaderflags

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/danielorbach/go-component"
)

const help = `
PROGNAME is a host for loading of system service components.

PROGNAME responds to footprints in a dedicated system deployment,
and loads the appropriate service components. It can be used to
load a single service component or a set of service components,
linked together over pub-sub. PROGNAME is designed to be used as
a Docker container in a Kubernetes cluster or run as a standalone
process. PROGNAME allows environment variable values to be split
into separate arguments at semicolons. This enables parsing of
compound environment variable values as individual configuration
parameters.

For example:

export KAFKA_CONFIG=ChannelBufferSize=100;Consumer.Offset.Initial=sarama.OffsetOldest
`

// Help implements the help subcommand for a loader main program. The optional
// args specify the services to describe. Help calls log.Fatal if no such service
// exists.
func Help(cmdline *flag.FlagSet, progname string, descriptors []*component.Descriptor) {
	args := cmdline.Args()[1:]

	// No args: show summary of all service components.
	if len(args) == 0 {
		prologue := strings.ReplaceAll(help, "PROGNAME", progname)
		fmt.Println(fillParagraphs(prologue, 69))
		fmt.Println()
		fmt.Println("Registered service components:")
		fmt.Println()
		sort.Slice(descriptors, func(i, j int) bool {
			return descriptors[i].Name < descriptors[j].Name
		})
		for _, a := range descriptors {
			title := strings.Split(a.Doc, "\n\n")[0]
			// the first paragraph ("\n\n") is the title, but it may contain
			// multiple lines, or even a very long line, so we shorten it.
			fmt.Printf("    %-12s %s\n", a.Name, shortenParagraph(title, 80))
		}
		fmt.Println("\nBy default all service components are run.")
		fmt.Println("To enabled them specifically, use the -NAME flag for each one,")
		fmt.Println(" or -NAME=false to run all those not explicitly disabled.")

		// Show only the core command-line flags.
		fmt.Println("\nCore flags:")
		fmt.Println()
		fs := flag.NewFlagSet("", flag.ExitOnError)
		cmdline.VisitAll(func(f *flag.Flag) {
			// all service-specific flags (part of Descriptor.Flags) are prefixed
			// with the service name, so we can filter them out here.
			// note: specific loader packages should not prefix their flags with
			// the loader name, so they will be shown here.
			if !strings.Contains(f.Name, ".") {
				fs.Var(f.Value, f.Name, f.Usage)
			}
		})
		fs.SetOutput(os.Stdout)
		fs.PrintDefaults()

		fmt.Printf("\nTo see details and flags of a specific service, run '%s help name'.\n", progname)

		return
	}

	// Show help on specific service(s).
outer:
	for i, arg := range args {
		if i > 0 {
			// visually separate the help for each service
			fmt.Print("\n\n")
		}

		for _, d := range descriptors {
			if d.Name == arg {
				paras := strings.Split(d.Doc, "\n\n")
				title := strings.TrimSpace(paras[0]) // uniform leading & trailing space
				fmt.Printf("%s: %s\n", d.Name, title)

				// Show only the flags relating to this analysis,
				// properly prefixed.
				first := true
				fs := flag.NewFlagSet(d.Name, flag.ExitOnError)
				d.Flags.VisitAll(func(f *flag.Flag) {
					if first {
						// not all services have flags, so only print this
						// section if there are flags to show.
						first = false
						fmt.Println("\nService flags:")
						fmt.Println()
					}
					fs.Var(f.Value, d.Name+"."+f.Name, f.Usage)
				})
				fs.SetOutput(os.Stdout)
				fs.PrintDefaults()

				if len(paras) > 1 {
					fmt.Printf("\n%s\n", strings.Join(paras[1:], "\n\n"))
				}

				continue outer
			}
		}
		log.Fatalf("Service %q not registered", arg)
	}
}

func fillParagraphs(text string, width int) string {
	paras := strings.Split(text, "\n\n")
	for i, p := range paras {
		var b strings.Builder
		b.Grow(len(p) + len(p)/width) // rough estimate of final size
		words := strings.Fields(p)

		var line int
		for _, w := range words {
			if line+len(w) > width {
				b.WriteByte('\n')
				line = 0
			}
			b.WriteString(w)
			b.WriteByte(' ')
			line += len(w) + 1
		}
		paras[i] = b.String()
	}
	return strings.Join(paras, "\n\n")
}

// TruncateParagraph returns a shortened version of the text, such that the
// result is no longer than width runes.
//
// The result is a single line, with no newlines. If the text is shortened,
// an ellipsis ("...") is appended to the end (and counted towards the width).
func shortenParagraph(text string, width int) string {
	text = strings.ReplaceAll(text, "\n", " ") // remove newlines
	if len(text) <= width {
		return text
	}
	return text[:width-3] + "..."
}
