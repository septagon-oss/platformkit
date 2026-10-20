package module

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

// streaming is the composition every case below starts from: one module that
// emits two events, defines the permission one stream asks for, and streams both
// events. Each refusal case changes one declaration, so the sentence Validate
// returns is about that change and nothing else.
func streaming(mutate func(m *Module)) Module {
	m := Module{
		Name:        "billing",
		Permissions: []Permission{{Key: "invoice:read", Label: "read invoices"}},
		Declared:    []events.Declared{{Name: "billing.invoice_issued"}, {Name: "billing.invoice_paid"}},
		Streams: []Stream{
			{Event: "billing.invoice_issued", Policy: StreamPermission("invoice:read")},
			{Event: "billing.invoice_paid", Policy: StreamAnyPrincipal()},
		},
	}
	if mutate != nil {
		mutate(&m)
	}
	return m
}

// TestAStreamNamingAnEventNobodyEmitsOrNoHolderFailsValidation is the table of
// refusals a stream declaration can earn. A stream is a door onto an event, and
// each row is one way the door opens on nothing or on everybody.
func TestAStreamNamingAnEventNobodyEmitsOrNoHolderFailsValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(m *Module)
		want   string
	}{
		{
			name: "an event the module does not emit",
			mutate: func(m *Module) {
				m.Streams = append(m.Streams, Stream{Event: "billing.invoice_voided", Policy: StreamAnyPrincipal()})
			},
			want: `module "billing": streams "billing.invoice_voided", which it does not emit`,
		},
		{
			name: "an event streamed twice",
			mutate: func(m *Module) {
				m.Streams = append(m.Streams, Stream{Event: "billing.invoice_paid", Policy: StreamAnyPrincipal()})
			},
			want: `module "billing": stream "billing.invoice_paid" is declared twice`,
		},
		{
			name:   "no policy at all",
			mutate: func(m *Module) { m.Streams[1] = Stream{Event: "billing.invoice_paid"} },
			want:   `module "billing": stream "billing.invoice_paid" declares no policy; every stream names who may watch it`,
		},
		{
			name: "a grant no module defines",
			mutate: func(m *Module) {
				m.Streams = []Stream{{Event: "billing.invoice_issued", Policy: StreamPermission("invoice:delete")}}
			},
			want: `module "billing": stream "billing.invoice_issued" requires permission "invoice:delete", which no module defines`,
		},
		{
			name: "a grant that is not a permission",
			mutate: func(m *Module) {
				m.Streams = []Stream{{Event: "billing.invoice_issued", Policy: StreamPermission("invoice_read")}}
			},
			want: `module "billing": stream "billing.invoice_issued" requires "invoice_read"; a permission is "<resource>:<action>", both lower-case identifiers`,
		},
		{
			name: "a recipient field that is not a payload member",
			mutate: func(m *Module) {
				m.Streams = []Stream{{Event: "billing.invoice_issued", Policy: StreamRecipient("Paid By")}}
			},
			want: `module "billing": stream "billing.invoice_issued" names recipient field "Paid By"; a payload member is an identifier that starts lower-case`,
		},
		{
			name: "a policy built outside the three constructors",
			mutate: func(m *Module) {
				m.Streams = []Stream{{Event: "billing.invoice_issued", Policy: StreamPolicy{kind: "everyone"}}}
			},
			want: `module "billing": stream "billing.invoice_issued" declares policy "everyone", which is not one of the three constructors`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Validate([]Module{streaming(test.mutate)})
			if err == nil {
				t.Fatalf("Validate accepted a stream with %s; the refusals are what keeps a stream from opening on everybody", test.name)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error does not name the problem\nwant: %s\ngot:\n%v", test.want, err)
			}
		})
	}
}

// TestAStreamDeclaredByTheModuleThatEmitsItIsValid: the three policies, and a
// stream on an event this module emits but no subscription consumes — the durable
// path and the live one are asked separately, and a module may answer one and not
// the other.
func TestAStreamDeclaredByTheModuleThatEmitsItIsValid(t *testing.T) {
	mods := []Module{
		{
			Name:        "billing",
			Permissions: []Permission{{Key: "invoice:read", Label: "read invoices"}},
			Declared:    []events.Declared{{Name: "billing.invoice_issued"}},
			Streams: []Stream{
				{Event: "billing.invoice_issued", Policy: StreamAnyPrincipal()},
			},
		},
		{
			Name:     "work",
			Declared: []events.Declared{{Name: "work.task_assigned"}},
			Streams: []Stream{
				{Event: "work.task_assigned", Policy: StreamRecipient("assignedTo")},
			},
		},
	}
	if err := Validate(mods); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
