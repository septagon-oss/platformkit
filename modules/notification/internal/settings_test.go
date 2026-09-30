package internal_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// settings is the service the way a deployment that has preferences wires it:
// the module's own store for the person's answers, and no tenant sender, so the
// installation speaks as the address it configured for itself.
func settings() *internal.Service {
	return internal.NewService(directory{}, internal.WithPreferences(internal.Prefs{}))
}

func reason(t *testing.T, tx db.Tx[db.Tenant], id uuid.UUID, channel, outcome string) string {
	t.Helper()
	rows, err := internal.Deliveries(tx, id)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	for _, r := range rows {
		if r.Channel == channel && r.Outcome == outcome {
			return r.Reason
		}
	}
	t.Fatalf("no %s %s row in %v", channel, outcome, rows)
	return ""
}

func at(day time.Time) func() time.Time { return func() time.Time { return day } }

// // TestTurningAChannelOffSuppressesItWithItsReason is item 2's promise: a person
// who turned mail off is still told in the application, still has the notice, and
// the ledger says the channel was refused, why, and not in words a reader has to
// look up in another table.
func TestTurningAChannelOffSuppressesItWithItsReason(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc, prefs := settings(), internal.Prefs{}
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false); err != nil {
			return err
		}
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "newsletter", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		id = row.ID
		if got, want := ledger(t, tx, id), []string{"in_app requested", "in_app sent", "email requested", "email suppressed"}; !slices.Equal(got, want) {
			t.Errorf("the ledger of somebody who opted out is %v, want %v", got, want)
		}
		if r := reason(t, tx, id, "email", "suppressed"); !strings.Contains(r, "email is off for every notice") {
			t.Errorf("the ledger says %q, want the sentence about her own choice", r)
		}
		// A security notice about her own account crosses her choice: opt-out
		// exists so people are not marketed to, not so that a takeover is quiet.
		secure, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "password changed", Wants: contracts.WantsEmail,
			Class: contracts.ClassSecurity,
		})
		if err != nil {
			return err
		}
		if got, want := ledger(t, tx, secure.ID), []string{"in_app requested", "in_app sent", "email requested"}; !slices.Equal(got, want) {
			t.Errorf("a security notice's ledger is %v, want %v", got, want)
		}
		requested, terminal, err := internal.Coverage(tx)
		if err != nil {
			return err
		}
		if requested != 4 || terminal != 3 {
			t.Errorf("coverage is %d terminal of %d requested, want 3 of 4 (the mail still out there is not unaccounted for)", terminal, requested)
		}
		return errRollback
	})
	if err != nil && err != errRollback {
		t.Fatalf("settings: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("the notice that opted out of mail did not exist")
	}
}

// TestAnIntentRowWinsOverTheBlanketOne is the shape opt-out has to have: "no
// mail, but mail me when my password changed" is two rows, and the specific one
// answers for its intent.
func TestAnIntentRowWinsOverTheBlanketOne(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc, prefs := settings(), internal.Prefs{}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false); err != nil {
			return err
		}
		if _, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "task.assigned", contracts.ChannelEmail, true); err != nil {
			return err
		}
		off, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "newsletter", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		on, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "assigned", Intent: "task.assigned", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		if reason(t, tx, off.ID, "email", "suppressed") == "" {
			t.Error("the blanket no did not suppress the generic notice")
		}
		if got := ledger(t, tx, on.ID); slices.Contains(got, "email suppressed") {
			t.Errorf("the intent's own yes was not taken: %v", got)
		}
		// Setting the same answer again is not two rows and not two events.
		before := prefsRow(t, tx, notificationtest.Ada, "", "email")
		again, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false)
		if err != nil {
			return err
		}
		if again.ID != before.ID {
			t.Errorf("setting the same answer wrote a new row: %s, was %s", again.ID, before.ID)
		}
		if got := outbox(t, tx); !slices.Equal(got, []string{contracts.EventPreferenceSet, contracts.EventPreferenceSet,
			contracts.EventCreated, contracts.EventCreated, contracts.EventEmailRequested}) {
			t.Errorf("the events of two settings, two notices and one no-op are %v", got)
		}
		return errRollback
	})
	if err != nil && err != errRollback {
		t.Fatalf("settings: %v", err)
	}
}

func prefsRow(t *testing.T, tx db.Tx[db.Tenant], recipient uuid.UUID, intent, channel string) *contracts.Preference {
	t.Helper()
	var row contracts.Preference
	if err := tx.DB().Where("recipient_id = ? AND intent = ? AND channel = ? AND deleted_at IS NULL", recipient, intent, channel).Take(&row).Error; err != nil {
		t.Fatalf("read the preference row: %v", err)
	}
	return &row
}

// TestQuietHoursAreInTheZoneTheyName is why the column is an IANA name and not an
// offset. Lisbon is UTC+0 in March and UTC+1 in September, so 21:00 UTC is inside
// a 22:00–07:00 window in September and outside it in March — and a window stored
// as an offset would be one hour wrong for half the year. The three other cases
// are the window itself: its start, its end, and the morning side of a window that
// ran past midnight.
func TestQuietHoursAreInTheZoneTheyName(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	september, march := time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC), time.Date(2026, 3, 12, 21, 0, 0, 0, time.UTC)
	prefs := internal.Prefs{}
	for _, c := range []struct {
		what   string
		moment func() time.Time
		quiet  bool
	}{
		{"21:00 UTC on a September day, which is 22:00 in Lisbon", at(september), true},
		{"the same clock time in March, which is 21:00 in Lisbon", at(march), false},
		{"20:30 UTC in September, which is 21:30 in Lisbon, before the window", at(september.Add(-30 * time.Minute)), false},
		{"03:00 UTC in September, the morning side of a window that ran past midnight", at(september.Add(6 * time.Hour)), true},
	} {
		t.Run(c.what, func(t *testing.T) {
			wired := internal.NewService(directory{}, internal.WithPreferences(prefs), internal.WithClock(c.moment))
			err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if _, err := prefs.SetQuietHours(ctx, tx, contracts.QuietHours{
					RecipientID: notificationtest.Ada, StartMinute: 22 * 60, EndMinute: 7 * 60, TimeZone: "Europe/Lisbon",
				}); err != nil {
					return err
				}
				row, err := wired.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "newsletter", Wants: contracts.WantsEmail})
				if err != nil {
					return err
				}
				got := ledger(t, tx, row.ID)
				if quiet := slices.Contains(got, "email suppressed"); quiet != c.quiet {
					t.Errorf("%v: suppressed=%v, want %v", got, quiet, c.quiet)
				}
				if c.quiet {
					if r := reason(t, tx, row.ID, "email", "suppressed"); !strings.Contains(r, "quiet hours until 07:00 Europe/Lisbon") {
						t.Errorf("the ledger says %q, want the window that is open", r)
					}
				}
				if err := prefs.ClearQuietHours(ctx, tx, notificationtest.Ada); err != nil {
					return err
				}
				if again, err := prefs.Quiet(ctx, tx, notificationtest.Ada); err != nil || again != nil {
					t.Errorf("after clearing, the window is %+v (%v)", again, err)
				}
				return errRollback
			})
			if err != nil && err != errRollback {
				t.Fatalf("quiet hours: %v", err)
			}
		})
	}
}

// TestSettingsAreSomebodyElseInvisible is row-level security on the three tables
// this commit adds, one subtest each: another tenant's transaction cannot read
// the choice, the window or the sender, and cannot be answered by them either.
func TestSettingsAreSomebodyElseInvisible(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := internal.Prefs{}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := store.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false); err != nil {
			return err
		}
		if _, err := store.SetQuietHours(ctx, tx, contracts.QuietHours{
			RecipientID: notificationtest.Ada, StartMinute: 22 * 60, EndMinute: 7 * 60, TimeZone: "Europe/Lisbon",
		}); err != nil {
			return err
		}
		_, err := senders().Put(ctx, tx, sender())
		return err
	})
	if err != nil {
		t.Fatalf("acme's settings: %v", err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		prefs, err := store.Settings(ctx, tx, notificationtest.Ada, "")
		if err != nil || len(prefs) != 0 {
			t.Errorf("globex reads acme's preferences: %v (%v)", prefs, err)
		}
		quiet, err := store.Quiet(ctx, tx, notificationtest.Ada)
		if err != nil || quiet != nil {
			t.Errorf("globex reads acme's quiet hours: %+v (%v)", quiet, err)
		}
		row, err := senders().For(ctx, tx)
		if err != nil || row != nil {
			t.Errorf("globex reads acme's sender: %+v (%v)", row, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read in globex: %v", err)
	}
}

// verified is the composition's answer to "does this domain say so". What a proof
// is belongs to the product (contracts.SenderVerifier); this is one that always
// says yes and says what it saw, which is what the module's own tests need.
type verified struct{ proof string }

func (v verified) Verify(context.Context, contracts.Sender) (string, error) { return v.proof, nil }

// keys is a deployment that holds one key, for the domain it was configured with.
type keys struct{ b []byte }

func (k keys) KeyFor(context.Context, db.Tx[db.Tenant], contracts.Sender) []byte { return k.b }

func senders() *internal.Senders {
	return &internal.Senders{Verifier: verified{"TXT _platformkit-verify.acme.example.com = ok"}, Keys: keys{[]byte("pem")}}
}

func sender() contracts.Sender {
	return contracts.Sender{
		Domain: "acme.example.com", Selector: "selector1", FromName: "Acme",
		FromAddress: "notifications@acme.example.com",
	}
}

// TestAConversationWithTheSenderCommands is item 4's four refusals: the status is
// not the caller's to post, verification needs a proof it can show afterwards, a
// verified sender cannot be deleted out from under the tenant, and none of the
// refusals wrote a row or published an event.
func TestAConversationWithTheSenderCommands(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		in := sender()
		in.Status = contracts.SenderVerified // the caller's claim, refused
		row, err := senders().Put(ctx, tx, in)
		if err != nil {
			return err
		}
		if row.Status != contracts.SenderPending || row.Token == "" {
			t.Errorf("a new sender is %+v, want pending with a token to publish", row)
		}
		id = row.ID

		// Until it is verified, every mail this tenant would send is refused with
		// a reason that names the tenant — and the notice still exists.
		wired := internal.NewService(directory{}, internal.WithPreferences(internal.Prefs{}), internal.WithSenders(senders()))
		notice, err := wired.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "newsletter", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		if r := reason(t, tx, notice.ID, "email", "suppressed"); !strings.Contains(r, "has no verified DKIM record") {
			t.Errorf("an unverified sender's mail says %q", r)
		}

		if _, err := senders().Verify(ctx, tx, uuid.New()); err == nil {
			t.Error("verifying a sender that does not exist succeeded")
		}
		verifiedRow, err := senders().Verify(ctx, tx, id)
		if err != nil {
			return err
		}
		if verifiedRow.Status != contracts.SenderVerified || verifiedRow.Proof == "" || verifiedRow.VerifiedAt == nil {
			t.Errorf("the verified sender is %+v, want believed, with the proof and the moment", verifiedRow)
		}
		mailed, err := wired.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "now", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		if got := ledger(t, tx, mailed.ID); slices.Contains(got, "email suppressed") {
			t.Errorf("mail was still refused after verification: %v", got)
		}
		if err := senders().Delete(ctx, tx, id); err == nil {
			t.Error("deleting the tenant's only verified sender succeeded")
		}
		if got := outbox(t, tx); !slices.Equal(got, []string{contracts.EventSenderSet, contracts.EventCreated,
			contracts.EventSenderVerified, contracts.EventCreated, contracts.EventEmailRequested}) {
			t.Errorf("the events are %v", got)
		}
		return errRollback
	})
	if err != nil && err != errRollback {
		t.Fatalf("senders: %v", err)
	}

	// A deployment that holds no key cannot sign as the tenant's own sender, and
	// says whose problem that is.
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		noKeys := &internal.Senders{Verifier: verified{"ok"}, Keys: keys{}}
		row, err := noKeys.Put(ctx, tx, sender())
		if err != nil {
			return err
		}
		if row, err = noKeys.Verify(ctx, tx, row.ID); err != nil {
			return err
		}
		wired := internal.NewService(directory{}, internal.WithPreferences(internal.Prefs{}), internal.WithSenders(noKeys))
		mailed, err := wired.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "unsigned", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		if r := reason(t, tx, mailed.ID, "email", "suppressed"); !strings.Contains(r, "holds no DKIM key") {
			t.Errorf("a sender whose key the deployment lacks says %q", r)
		}
		return errRollback
	})
	if err != nil && err != errRollback {
		t.Fatalf("unsigned sender: %v", err)
	}
}

// TestARedeliveredSendWritesOneSentRow is why 000030 exists: the outbox
// redelivers what it did not see acknowledged, and a provider that answered on
// both attempts must leave one sent row and a worker that can acknowledge.
func TestARedeliveredSendWritesOneSentRow(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := settings()
	send := internal.SendMail(notification.NewMailbox(), directory{}, hosts{}, nil, true)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "twice", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		for i := 0; i < 2; i++ {
			if err := send.Handler(ctx, tx, request(row.ID, row.RecipientID)); err != nil {
				t.Fatalf("delivery attempt %d: %v", i+1, err)
			}
		}
		var sent int64
		if err := tx.DB().Raw(`SELECT count(*) FROM notification_deliveries WHERE notification_id = ? AND outcome = 'sent' AND channel = 'email'`, row.ID).Scan(&sent).Error; err != nil {
			return err
		}
		if sent != 1 {
			t.Errorf("a redelivered send wrote %d sent rows, want 1", sent)
		}
		if requested, terminal, err := internal.Coverage(tx); err != nil || requested != terminal {
			t.Errorf("coverage is %d of %d after the redelivery, want the two equal (%v)", terminal, requested, err)
		}
		return errRollback
	})
	if err != nil && err != errRollback {
		t.Fatalf("redelivery: %v", err)
	}
}

// TestAChoiceThatIsNotOneIsRefused: the entity's own Validate is the door, so a
// person cannot mute their own inbox, a quiet window cannot name an offset, and
// nothing was written or published for either.
func TestAChoiceThatIsNotOneIsRefused(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	prefs := internal.Prefs{}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelInApp, false); err == nil {
			t.Error("muteing one's own inbox succeeded")
		}
		if _, err := prefs.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.Channel("carrier-pigeon"), true); err == nil {
			t.Error("a channel the ledger does not know was accepted")
		}
		if _, err := prefs.SetQuietHours(ctx, tx, contracts.QuietHours{
			RecipientID: notificationtest.Ada, StartMinute: 100, EndMinute: 200, TimeZone: "UTC+1",
		}); err == nil {
			t.Error("quiet hours naming an offset rather than a zone were accepted")
		}
		if _, err := senders().Put(ctx, tx, contracts.Sender{
			Domain: "acme.example.com", Selector: "selector1", FromName: "Acme", FromAddress: "notifications@other.example",
		}); err == nil {
			t.Error("an address in another domain than the one that signs was accepted")
		}
		if got := outbox(t, tx); len(got) != 0 {
			t.Errorf("four refusals published %v", got)
		}
		_, total, err := prefs.Mine(ctx, tx, notificationtest.Ada, crud.Query{})
		if err != nil || total != 0 {
			t.Errorf("four refusals wrote %d preference rows", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("refusals: %v", err)
	}
}
