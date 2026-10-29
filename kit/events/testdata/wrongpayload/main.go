package main

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

type PayloadA struct{}
type PayloadB struct{}

func main() {
	var tx db.Tx[db.Tenant]
	_ = events.PublishTo(context.Background(), tx, events.DeclareTopic[PayloadA]("demo.changed"), PayloadB{})
}
