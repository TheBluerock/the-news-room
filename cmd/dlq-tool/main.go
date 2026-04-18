// dlq-tool — inspect, replay, and discard messages from RedPanda DLQ topics.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	brokers := envOr("REDPANDA_BROKERS", "localhost:9092")

	switch os.Args[1] {
	case "list":
		cmdList(brokers)
	case "replay":
		fs := flag.NewFlagSet("replay", flag.ExitOnError)
		topic := fs.String("topic", "", "DLQ topic name (e.g. article.generated.dlq)")
		limit := fs.Int("limit", 0, "max messages to replay (0 = all)")
		fs.Parse(os.Args[2:])
		if *topic == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --topic is required")
			os.Exit(1)
		}
		cmdReplay(brokers, *topic, *limit)
	case "inspect":
		fs := flag.NewFlagSet("inspect", flag.ExitOnError)
		topic := fs.String("topic", "", "DLQ topic name")
		limit := fs.Int("limit", 10, "messages to print")
		fs.Parse(os.Args[2:])
		if *topic == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --topic is required")
			os.Exit(1)
		}
		cmdInspect(brokers, *topic, *limit)
	case "discard":
		fs := flag.NewFlagSet("discard", flag.ExitOnError)
		topic := fs.String("topic", "", "DLQ topic name")
		fs.Parse(os.Args[2:])
		if *topic == "" {
			fmt.Fprintln(os.Stderr, "ERROR: --topic is required")
			os.Exit(1)
		}
		cmdDiscard(brokers, *topic)
	default:
		usage()
		os.Exit(1)
	}
}

func cmdList(brokers string) {
	// TODO: connect to RedPanda Admin API and list all *.dlq topics with message counts
	fmt.Printf("Connecting to brokers: %s\n", brokers)
	fmt.Println("TODO: list DLQ topics and pending message counts")
}

func cmdReplay(brokers, topic string, limit int) {
	// TODO: consume messages from <topic>, produce to original topic (strip .dlq suffix)
	// Preserve original headers including W3C TraceContext
	fmt.Printf("Replaying from %s → %s (limit=%d)\n", topic, originalTopic(topic), limit)
}

func cmdInspect(brokers, topic string, limit int) {
	// TODO: consume up to limit messages, print JSON to stdout, do NOT commit offsets
	fmt.Printf("Inspecting %d messages from %s\n", limit, topic)
}

func cmdDiscard(brokers, topic string) {
	// TODO: advance consumer group offset to end of topic (effectively discarding)
	// Requires explicit confirmation prompt
	fmt.Printf("WARNING: this will discard all messages in %s. Type 'yes' to confirm: ", topic)
	var confirm string
	fmt.Scan(&confirm)
	if confirm != "yes" {
		fmt.Println("Aborted.")
		return
	}
	fmt.Printf("TODO: discard all messages in %s\n", topic)
}

func originalTopic(dlqTopic string) string {
	n := len(dlqTopic)
	if n > 4 && dlqTopic[n-4:] == ".dlq" {
		return dlqTopic[:n-4]
	}
	return dlqTopic
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func usage() {
	fmt.Println(`dlq-tool — RedPanda dead letter queue CLI

Usage:
  dlq-tool list
  dlq-tool inspect --topic <dlq-topic> [--limit N]
  dlq-tool replay  --topic <dlq-topic> [--limit N]
  dlq-tool discard --topic <dlq-topic>

Environment:
  REDPANDA_BROKERS  comma-separated broker addresses (default: localhost:9092)

Runbook: ops/DLQ-handling.md`)
}
