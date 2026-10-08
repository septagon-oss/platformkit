package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	wiregate "github.com/septagon-oss/platformkit/kit/wire"
)

// TestEveryPublishedEventMemberIsGatedByTheWireRules removes each payload member
// of each event the checked-in AsyncAPI document publishes, one at a time, and
// requires the wire gate to refuse it as B3 at the event's own address. The
// operation-to-message projection is what makes a payload reachable; a document
// whose events stopped being linked from an operation would pass every rename.
func TestEveryPublishedEventMemberIsGatedByTheWireRules(t *testing.T) {
	golden, err := os.ReadFile(asyncapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v", asyncapiGolden, err)
	}
	if breaks := wiregate.Compare(golden, golden); len(breaks) > 0 {
		t.Fatalf("the checked-in document does not compare equal to itself: %v", breaks)
	}
	var doc map[string]any
	if err := json.Unmarshal(golden, &doc); err != nil {
		t.Fatalf("%s is not JSON: %v", asyncapiGolden, err)
	}
	checked := 0
	for channelName, rawChannel := range wireMap(doc["channels"]) {
		channel := wireMap(rawChannel)
		address, _ := channel["address"].(string)
		for messageName, rawMessage := range wireMap(channel["messages"]) {
			for member := range wireMap(wireMap(wireMap(rawMessage)["payload"])["properties"]) {
				var fresh map[string]any
				if err := json.Unmarshal(golden, &fresh); err != nil {
					t.Fatal(err)
				}
				payload := wireMap(wireMap(wireMap(wireMap(wireMap(fresh["channels"])[channelName])["messages"])[messageName])["payload"])
				delete(wireMap(payload["properties"]), member)
				current, err := json.Marshal(fresh)
				if err != nil {
					t.Fatal(err)
				}
				breaks := wiregate.Compare(golden, current)
				if !slices.ContainsFunc(breaks, func(b wiregate.Break) bool {
					return b.Rule == "B3" && b.Path == "SEND "+address
				}) {
					t.Errorf("removing %s/%s payload .%s is not refused as B3 at SEND %s: %v", channelName, messageName, member, address, breaks)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatalf("%s publishes no event member, so this case checked nothing", asyncapiGolden)
	}
}
