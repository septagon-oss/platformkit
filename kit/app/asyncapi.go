// asyncapi.go is the composition's event half written in AsyncAPI 3.0 — the
// document an event bridge, a schema registry or a generator reads to learn who
// publishes what on which subject and what the message looks like.
//
// It is a projection of a Description and of nothing else. Every channel,
// message and operation below comes from a value app.New already checked, and
// there is no second declaration of an event here to keep in step with the
// first. Nothing serves it and no runtime path reads it, so it can be handed to
// a tool without widening what a wrong document could break: the application
// does nothing because of it. See ARCHITECTURE.md, "Start at the composition".

package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// asyncAPIVersion is the dialect AsyncAPI writes. 3.0 and not 2.x because 3.0
// makes an operation a member of the document: which module publishes an event
// and which one handles it is what the manifest says, and in 2.x a reader had to
// infer it from which channel a section named.
const asyncAPIVersion = "3.0.0"

// subjectPrefix is the subject scheme kit/events/providers/nats publishes on:
// the event named task.assigned arrives as platformkit.task.assigned. That
// package owns the scheme and kit/app cannot import it — the kernel selects a
// transport by name and links none, which scripts/check_packages.sh holds — so
// the constant is repeated here and nowhere else. It is a projection: a channel
// address that disagreed with the broker would misroute a consumer somebody
// generated from this document, and nothing at runtime would notice.
const subjectPrefix = "platformkit."

// cloudEventRef is where every message's payload points. The envelope is one
// schema, written once below and referenced by every message, because the
// context attributes are identical for every event: what differs is data.
const cloudEventRef = "#/components/schemas/cloudevent"

// AsyncAPI returns the composition as one AsyncAPI 3.0.0 document in JSON.
//
// It is a function of d and one further fact a Description deliberately does not
// carry — the broker's address, which is why natsURL is an argument and not a
// field: the description names no address, DSN or hostname, and that is what lets
// the committed document match byte for byte on every machine. The server entry is
// present when the transport the description names is JetStream and absent
// otherwise, because a document naming a broker for an application running the
// in-memory transport would describe a deployment that does not exist.
//
// Ordering is by name throughout, so the bytes are stable enough to commit and
// to diff. Keys come out in the order the dialect lists its sections, which is
// struct field order rather than encoding/json's alphabetical sort of a map.
func AsyncAPI(d Description, natsURL string) ([]byte, error) {
	doc := asyncAPIDoc{
		AsyncAPI: asyncAPIVersion,
		Info:     asyncAPIInfo{Title: "PlatformKit", Version: strconv.Itoa(d.DescribeVersion)},
		Channels: map[string]asyncAPIChannel{},
		Components: asyncAPIComponents{
			Messages: map[string]asyncAPIMessage{},
			Schemas:  map[string]any{"cloudevent": cloudEventSchema()},
		},
		Operations: map[string]asyncAPIOp{},
	}
	if d.Transport == "jetstream" {
		host, err := natsHosts(natsURL)
		if err != nil {
			return nil, err
		}
		doc.Servers = map[string]asyncAPIServer{"nats": {Host: host, Protocol: "nats"}}
	}

	for _, m := range d.Modules {
		for _, e := range m.Events {
			key := componentKey(e.Name)
			doc.Channels[e.Name] = asyncAPIChannel{
				Address:  subjectPrefix + e.Name,
				Messages: map[string]asyncAPIRef{key: {Ref: "#/components/messages/" + key}},
			}
			doc.Components.Messages[key] = asyncAPIMessage{ContentType: "application/json", Payload: envelope(e.Schema)}
			doc.Operations[m.Name+".send."+e.Name] = asyncAPIOp{
				Action: "send", Channel: channelRef(e.Name), Summary: m.Name + " publishes " + e.Name,
			}
		}
		// A subscription is the other half of the same channel: the emitter
		// declared the message, and this module says it consumes it.
		for _, s := range m.Subscriptions {
			doc.Operations[m.Name+".receive."+s] = asyncAPIOp{
				Action: "receive", Channel: channelRef(s), Summary: m.Name + " handles " + s,
			}
		}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("app: describe: the AsyncAPI document did not encode: %w", err)
	}
	return out, nil
}

// envelope is a message's payload: the CloudEvents 1.0 envelope every PlatformKit
// event arrives in, with this event's own schema pinned over the envelope's open
// data member. A module that declared no payload type leaves data open rather
// than documenting an empty object the publisher may well not send.
func envelope(data json.RawMessage) asyncAPIPayload {
	if len(data) == 0 {
		data = json.RawMessage("true")
	}
	return asyncAPIPayload{
		AllOf:      []asyncAPIRef{{Ref: cloudEventRef}},
		Properties: asyncAPIDataMember{Data: data},
	}
}

// cloudEventSchema is components.schemas.cloudevent: the members
// kit/events/transport writes on the wire, in the shape JSON gives them. data is
// open here and pinned by each message; actor and data are the two members that
// may be absent, so they are the two that are not required.
func cloudEventSchema() map[string]any {
	id := map[string]any{"type": "string", "format": "uuid"}
	return map[string]any{
		"type":     "object",
		"required": []string{"specversion", "id", "source", "type", "time", "datacontenttype", "tenantid"},
		"properties": map[string]any{
			"specversion":     map[string]any{"type": "string", "const": "1.0"},
			"id":              id,
			"source":          map[string]any{"type": "string", "description": "The emitting module as a path: /task for task.assigned"},
			"type":            map[string]any{"type": "string", "description": "The event name, <module>.<event>"},
			"time":            map[string]any{"type": "string", "format": "date-time"},
			"datacontenttype": map[string]any{"type": "string", "const": "application/json"},
			"data":            true,
			"tenantid":        id,
			"actor":           id,
		},
	}
}

// componentKey is an event name made a component key: AsyncAPI's component maps
// name members with a character set that has no dot in it, and the channel keys
// above do not share that restriction, so task.assigned is a channel and
// task-assigned the message it refers to.
func componentKey(name string) string { return strings.ReplaceAll(name, ".", "-") }

func channelRef(event string) asyncAPIRef { return asyncAPIRef{Ref: "#/channels/" + event} }

// natsHosts is nats.url written as AsyncAPI wants a server's host: host and port
// without the scheme. The configuration admits a comma-separated list because a
// NATS client reads one as several endpoints of one cluster, which is one server
// here rather than one entry each.
func natsHosts(raw string) (string, error) {
	var hosts []string
	for endpoint := range strings.SplitSeq(raw, ",") {
		u, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("app: describe: nats.url endpoint %q names no host to put in the document", endpoint)
		}
		hosts = append(hosts, u.Host)
	}
	return strings.Join(hosts, ","), nil
}

// The document types below exist for their field order. encoding/json sorts a
// map's keys, which is what makes every name-ordered list in the document
// correct by construction; what it cannot do is keep the sections in the order a
// reader expects, so each level whose key order carries meaning is a struct.

type asyncAPIDoc struct {
	AsyncAPI   string                     `json:"asyncapi"`
	Info       asyncAPIInfo               `json:"info"`
	Servers    map[string]asyncAPIServer  `json:"servers,omitempty"`
	Channels   map[string]asyncAPIChannel `json:"channels"`
	Components asyncAPIComponents         `json:"components"`
	Operations map[string]asyncAPIOp      `json:"operations"`
}

type asyncAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type asyncAPIServer struct {
	Host     string `json:"host"`
	Protocol string `json:"protocol"`
}

type asyncAPIChannel struct {
	Address  string                 `json:"address"`
	Messages map[string]asyncAPIRef `json:"messages"`
}

type asyncAPIRef struct {
	Ref string `json:"$ref"`
}

type asyncAPIComponents struct {
	Messages map[string]asyncAPIMessage `json:"messages"`
	Schemas  map[string]any             `json:"schemas"`
}

type asyncAPIMessage struct {
	ContentType string          `json:"contentType"`
	Payload     asyncAPIPayload `json:"payload"`
}

type asyncAPIPayload struct {
	AllOf      []asyncAPIRef      `json:"allOf"`
	Properties asyncAPIDataMember `json:"properties"`
}

type asyncAPIDataMember struct {
	Data json.RawMessage `json:"data"`
}

type asyncAPIOp struct {
	Action  string      `json:"action"`
	Channel asyncAPIRef `json:"channel"`
	Summary string      `json:"summary"`
}
