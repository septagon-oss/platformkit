package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

const (
	PermissionDeliveryRead = "delivery:read"
	DeliveryPath           = adminRoot + "/delivery"
	deliveryLimit          = 50
)

// DeliveryGrant also supplies navigation before the shell's own route is mounted.
// The manifest and the eventual route are checked against this operator scope.
func DeliveryGrant() tenancy.Grant {
	return tenancy.Grant{Permission: PermissionDeliveryRead, Operator: true}
}

func (p pages) mountDelivery(api *httpx.API) {
	page.Serve(api, p.shell, page.Route{ID: "admin-delivery", Method: http.MethodGet, Path: DeliveryPath,
		Summary: "Inspect event delivery", Errors: []int{http.StatusServiceUnavailable}},
		httpx.OperatorPermission(PermissionDeliveryRead), func(ctx context.Context, _ page.Request, _ *page.Empty) (page.View, error) {
			conn, ok := httpx.ConnFrom(ctx)
			if !ok {
				return page.View{}, deliveryUnavailable()
			}
			ctx, cancel := context.WithTimeout(db.Detached(ctx), 2*time.Second)
			defer cancel()
			var report events.DeliveryInspection
			err := db.RunSystem(ctx, conn, p.Token, func(ctx context.Context, tx db.Tx[db.System]) error {
				var err error
				report, err = events.InspectDelivery(ctx, tx, deliveryLimit)
				return err
			})
			if err != nil {
				return page.View{}, deliveryUnavailable()
			}
			return deliveryPage(report, time.Now()), nil
		})
}

func deliveryUnavailable() error {
	return problem.New(http.StatusServiceUnavailable, "Event delivery records are unavailable. Try again.")
}

func deliveryPage(report events.DeliveryInspection, observed time.Time) page.View {
	columns := []components.TableColumn{{Key: "event", Label: "Event", Primary: true},
		{Key: "tenant", Label: "Tenant ID"}, {Key: "id", Label: "Event ID"}, {Key: "at", Label: "Recorded at (UTC)"}}
	pending := make([]components.TableRow, 0, len(report.Pending))
	for _, row := range report.Pending {
		pending = append(pending, components.TableRow{ID: row.EventID.String(), Cells: map[string]any{
			"event": row.Name, "tenant": row.TenantID.String(), "id": row.EventID.String(), "at": row.CreatedAt.UTC().Format(time.RFC3339)}})
	}
	failed := make([]components.TableRow, 0, len(report.Failures))
	for _, row := range report.Failures {
		failed = append(failed, components.TableRow{ID: row.EventID.String() + "/" + row.Durable, Cells: map[string]any{
			"event": row.Name, "tenant": row.TenantID.String(), "id": row.EventID.String(), "at": row.FailedAt.UTC().Format(time.RFC3339), "durable": row.Durable}})
	}
	section := func(id, title, description, empty string, columns []components.TableColumn, rows []components.TableRow, more bool) g.Node {
		return components.Stack(components.StackProps{Gap: "4"},
			components.Heading(components.HeadingProps{ComponentProps: components.ComponentProps{ID: id}, Level: 2, Text: title}),
			components.Text(components.TextProps{Content: description}),
			g.If(more, components.Text(components.TextProps{Content: "Showing 50 records. More records exist."})),
			components.Table(components.TableProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"role": "region", "aria-labelledby": id, "tabindex": "0"}},
				Columns: columns, Rows: rows, EmptyText: empty}))
	}
	return page.View{Title: "Event delivery", Sensitive: true, Body: []g.Node{
		components.Toolbar(components.ToolbarProps{Title: "Event delivery", Subtitle: "Committed delivery records for this installation."},
			components.Button(components.ButtonProps{Label: "Refresh records", Href: DeliveryPath, Variant: "secondary"})),
		components.Text(components.TextProps{Content: "Observed at " + observed.UTC().Format(time.RFC3339) + ". Records can change between reads."}),
		section("delivery-pending", "Pending publication", "Oldest first. These events await a relay publication stamp; transport acceptance does not prove subscriber completion.",
			"No pending publication records.", columns, pending, report.PendingMore),
		section("delivery-failures", "Terminal failures", "Latest first. Retained subscription failures need investigation using the event ID and durable name. Refresh only reads these records.",
			"No terminal failure records.", append(columns, components.TableColumn{Key: "durable", Label: "Durable"}), failed, report.FailuresMore),
	}}
}
