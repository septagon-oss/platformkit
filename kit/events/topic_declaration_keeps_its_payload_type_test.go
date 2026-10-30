package events_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

type declaredTopicPayload struct {
	Value string `json:"value"`
}

func TestTopicDeclarationKeepsItsPayloadType(t *testing.T) {
	topic := events.DeclareTopic[declaredTopicPayload]("work.changed")
	declaration := topic.Declaration()
	if topic.Name() != "work.changed" || declaration.Name != topic.Name() {
		t.Fatalf("topic name %q differs from declaration %q", topic.Name(), declaration.Name)
	}
	if declaration.Payload != reflect.TypeFor[declaredTopicPayload]() {
		t.Fatalf("topic payload type = %v, want %v", declaration.Payload, reflect.TypeFor[declaredTopicPayload]())
	}
}
