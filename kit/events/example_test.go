package events_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func ExamplePublish() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := dbtest.WithSchema(ctx, "event delivery", func(ctx context.Context, _ *sql.DB, conn *db.Conn) error {
		type noted struct {
			Message string `json:"message"`
		}
		declaration := events.Declare[noted]("example.noted")
		release, err := events.DeclareMore([]events.Declared{declaration})
		if err != nil {
			return err
		}
		defer release()
		ctx, stop := context.WithCancel(ctx)
		defer stop()
		tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "example"}
		transport := memory.New()
		delivered := make(chan string, 1)
		err = events.Consume(ctx, conn, transport, []events.Subscription{{
			Module: "reader", Name: declaration.Name,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var payload noted
				if err := json.Unmarshal(ev.Payload, &payload); err != nil {
					return err
				}
				select {
				case delivered <- fmt.Sprint(ev.Name, " ", payload.Message, " ", db.TenantOf(tx).ID == tenant.ID):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}})
		if err != nil {
			return err
		}
		if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, declaration.Name, noted{Message: "Read the guide"})
		}); err != nil {
			return err
		}
		// Relay starts after commit and waits for this transport's handler transaction.
		if err := events.Relay(ctx, conn, transport); err != nil {
			return err
		}
		select {
		case result := <-delivered:
			fmt.Println(result)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		panic(err)
	}
	// Output: example.noted Read the guide true
}

func ExamplePublishTo() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := dbtest.WithSchema(ctx, "typed event delivery", func(ctx context.Context, _ *sql.DB, conn *db.Conn) error {
		type noted struct {
			Message string `json:"message"`
		}
		topic := events.DeclareTopic[noted]("example.noted")
		release, err := events.DeclareMore([]events.Declared{topic.Declaration()})
		if err != nil {
			return err
		}
		defer release()
		tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "example"}
		if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.PublishTo(ctx, tx, topic, noted{Message: "Read the guide"})
		}); err != nil {
			return err
		}
		fmt.Println(topic.Name())
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Output: example.noted
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExamplePublish", "ExamplePublishTo")
}
