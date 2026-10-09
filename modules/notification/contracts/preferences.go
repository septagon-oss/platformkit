package contracts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// minuteday is how many minutes a day has, which is the whole of what a quiet
// hour is measured in.
const minuteday = 24 * 60

// Preference is one person's answer about one channel, in one tenant. The row
// that does not exist is the answer too: no row means the channel is on, which
// is why a person who never opened the settings page has no rows and why this
// table stays the size of the people who chose something.
//
// Intent "" is the blanket choice — "never mail me" — and a row for a specific
// intent wins over the blanket one, so "no mail, but mail me when my password
// changed" is two rows and no rule nobody can read.
type Preference struct {
	crud.Base

	RecipientID uuid.UUID `json:"recipientId" gorm:"type:uuid;not null" validate:"required" format:"uuid" doc:"The person these are the settings of"`
	// Intent is the notice this choice is about, "" for every notice. It is the
	// same string Notice.Intent is, and nothing maps between them.
	Intent string `json:"intent,omitempty" gorm:"type:text;not null;default:''" maxLength:"100" doc:"The notice this choice is about; empty for every notice"`
	// Channel is never in_app: a person cannot mute their own inbox, because the
	// in-app row is the notice, and an opt-out that deleted it would be a notice
	// silently not written rather than an opt-out.
	Channel Channel `json:"channel" gorm:"type:text;not null" validate:"required" doc:"The channel this choice is about" example:"email"`
	// Enabled is the choice itself. Turning a channel off suppresses it with a
	// ledger row saying so; it never deletes a notice.
	Enabled bool `json:"enabled" gorm:"type:boolean;not null" doc:"Whether this channel may be used"`
}

// TableName pins the table, so the entity and migrations/000028 agree.
func (Preference) TableName() string { return "notification_preferences" }

// Validate is the entity's own check, run by kit/crud on every write.
func (p *Preference) Validate(context.Context) error {
	p.Intent = trimASCII(p.Intent)
	switch {
	case p.RecipientID == uuid.Nil:
		return fmt.Errorf("a preference is somebody's own")
	case p.Channel == "":
		return fmt.Errorf("a preference is about a channel")
	case p.Channel == ChannelInApp:
		return fmt.Errorf("in-app is not a preference: the row is the notice")
	}
	return p.Channel.Validate()
}

// QuietHours is one person's window in which nothing but a security notice is
// sent. Minutes from midnight in the named zone, and a window whose start equals
// its end is no window at all.
//
// The zone is an IANA name and not an offset, because quiet hours are the same
// hours in March and in September and an offset is not. PutSender validates it
// the same way: an unknown zone is refused when it is written, so a row that
// exists is a row the runtime can read unless the deployment ships no tz database
// — see Active.
type QuietHours struct {
	crud.Base

	RecipientID uuid.UUID `json:"recipientId" gorm:"type:uuid;not null" validate:"required" format:"uuid" doc:"The person this window belongs to"`
	// StartMinute and EndMinute are minutes from midnight in TimeZone; the
	// window includes its start and excludes its end, and start > end means it
	// runs past midnight (1320 to 420 is 22:00 to 07:00).
	StartMinute int    `json:"startMinute" gorm:"column:start_minute;not null;check:start_minute >= 0 AND start_minute < 1440" minimum:"0" maximum:"1439" doc:"Minutes from midnight the window starts"`
	EndMinute   int    `json:"endMinute" gorm:"column:end_minute;not null;check:end_minute >= 0 AND end_minute < 1440" minimum:"0" maximum:"1439" doc:"Minutes from midnight the window ends"`
	TimeZone    string `json:"timeZone" gorm:"type:text;not null" validate:"required" maxLength:"64" doc:"IANA zone these minutes are in" example:"Europe/Lisbon"`
}

// TableName pins the table, so the entity and migrations/000028 agree.
func (QuietHours) TableName() string { return "notification_quiet_hours" }

// Validate is the entity's own check, run by kit/crud on every write: an offset,
// a nonsense name or a minute outside the day is refused here, in the request
// that got it wrong, rather than at the next notice.
func (q *QuietHours) Validate(context.Context) error {
	q.TimeZone = trimASCII(q.TimeZone)
	switch {
	case q.RecipientID == uuid.Nil:
		return fmt.Errorf("quiet hours are somebody's own")
	case q.StartMinute < 0 || q.StartMinute >= minuteday:
		return fmt.Errorf("a quiet window starts within the day")
	case q.EndMinute < 0 || q.EndMinute >= minuteday:
		return fmt.Errorf("a quiet window ends within the day")
	case q.TimeZone == "":
		return fmt.Errorf("quiet hours are in a place")
	}
	if _, err := time.LoadLocation(q.TimeZone); err != nil {
		return fmt.Errorf("quiet hours name %q, which is not a time zone the application knows", q.TimeZone)
	}
	return nil
}

// Active reports whether at falls inside the window.
//
// A window whose minutes are equal is no window: the row says "quiet all day"
// would mean "nothing is ever sent", and that is what turning channels off is
// for. When the zone cannot be loaded — a scratch container with no tz database
// and no embedded one — the window is treated as UTC and the notice goes out:
// a wrong delivery costs somebody a notification at a dull hour, and a wrong
// suppression costs them the notification, because nothing in this module
// resends a suppressed channel later.
func (q *QuietHours) Active(at time.Time) bool {
	if q == nil || q.StartMinute == q.EndMinute {
		return false
	}
	loc, err := time.LoadLocation(q.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	min := func(t time.Time) int {
		h, m, _ := t.In(loc).Clock()
		return h*60 + m
	}
	start, end, now := q.StartMinute, q.EndMinute, min(at)
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

// String is the window in the form a settings page and a ledger reason both
// show: "22:00–07:00 Europe/Lisbon".
func (q *QuietHours) String() string {
	if q == nil {
		return ""
	}
	fmtMin := func(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
	return fmtMin(q.StartMinute) + "–" + fmtMin(q.EndMinute) + " " + q.TimeZone
}

// Preferences is how this module reads one person's answer for one notice. The
// implementation is this module's own, over its own tables, behind the same
// transaction the notice is written in: a preference a notice could not see in
// its own transaction would be a preference that decides the next notice
// instead of this one.
type Preferences interface {
	// Settings is the person's rows for this intent and the blanket ones, the
	// intent-specific first. No rows is the normal answer and means "on".
	Settings(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, intent string) ([]Preference, error)

	// Quiet is the person's window, or nil when they never set one.
	Quiet(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID) (*QuietHours, error)
}

// PreferenceService is what the person does with their own settings. Every
// command takes the caller's transaction, and every method — the reads as much as
// the writes — refuses a caller whose credential names somebody other than the
// person the call names. There is no route in this module that reaches another
// person's preferences, which is the same fact as ListFor scoping by the caller.
//
// It is a separate interface rather than more methods on Service because a
// caller that raises notices should not have to hold a way to change what the
// recipient chose, and because the fake a consumer wires for Notify is then not
// also a fake for opt-out.
type PreferenceService interface {
	// SetChannel writes one choice. An unknown channel, in-app, or a row that
	// already says this changes nothing and publishes nothing.
	SetChannel(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, intent string, channel Channel, enabled bool) (*Preference, error)

	// SetQuietHours writes the person's one window, and refuses a zone or a
	// minute it cannot read back (QuietHours.Validate).
	SetQuietHours(ctx context.Context, tx db.Tx[db.Tenant], q QuietHours) (*QuietHours, error)

	// ClearQuietHours removes the window. Nobody's window being gone is not an
	// error, and nothing is published when there was nothing to clear.
	ClearQuietHours(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID) error

	// Mine is the person's own rows, blanket first, then by channel. The recipient
	// is the person the caller's credential names: a principal that is not them is
	// refused rather than answered.
	Mine(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*Preference, int64, error)
}

// Enabled is the person's own answer about one channel and one intent: a row for
// this intent, else a blanket row, else on. It is exported because the answer
// has to be identical in the service, in the fake and in a settings page that
// shows what the row that does not exist means.
func Enabled(prefs []Preference, intent string, channel Channel) bool {
	on := true
	for _, p := range prefs { // an intent row wins wherever it sits; a blanket row only when none does
		if p.Channel != channel {
			continue
		}
		if p.Intent == intent {
			return p.Enabled
		}
		if p.Intent == "" {
			on = p.Enabled
		}
	}
	return on
}

// Correction names whose decision would make this suppression stop happening.
// It is on the record rather than in prose because "why is nothing arriving" has
// two different answers — the person turned it off, or this deployment does not
// send that channel — and an operator reading the ledger should not have to know
// which table to open.
type Correction string

const (
	// CorrectionRecipient is the person's own to make: a channel they turned
	// off, a quiet window they set.
	CorrectionRecipient Correction = "recipient"
	// CorrectionTenant is an administrator of the tenant's to make: a switched
	// channel, an unverified sender.
	CorrectionTenant Correction = "tenant"
	// CorrectionDeployment is whoever runs this installation's to make: no
	// provider wired, no DKIM key configured, no VAPID keys.
	CorrectionDeployment Correction = "deployment"
)

// Suppression is one channel that was asked for and will not happen, with the
// sentence the ledger keeps and the name of who could change it.
type Suppression struct {
	Channel  Channel
	Reason   string
	Corrects Correction
}

// Scope is what Decide knows about the world that is not the person: when it is,
// which channels this deployment has a provider for, and which channels this
// tenant has switched off.
//
// Disabled is read from nothing today, and that is stated rather than papered
// over: the tenant-wide switch is the product's screen (its table would be
// notification_channel_settings, one row per (tenant, channel), no row meaning
// on), and the decision the module runs is already shaped for it so that adding
// it is one query in the composition and no change here.
type Scope struct {
	Now       time.Time
	Available []Channel
	Disabled  []Channel
	// SenderOptional says that this installation may mail a person without the
	// tenant having its own verified sender. It is the deployment's answer and
	// not the tenant's: an installation that speaks as its own configured address
	// (config's mail.from, whose domain the operators vouched for when they wrote
	// it) sends for every tenant it serves, while an installation that put
	// per-tenant identity in place refuses mail for a tenant that has not proved
	// a domain, because the address in the header is the one thing that cannot be
	// corrected after the mail left. Its zero value is the strict answer, so a
	// scope built without thinking about it refuses rather than mails.
	SenderOptional bool
}

func (s Scope) available(c Channel) bool {
	for _, a := range s.Available {
		if a == c {
			return true
		}
	}
	return false
}

func (s Scope) disabled(c Channel) bool {
	for _, d := range s.Disabled {
		if d == c {
			return true
		}
	}
	return false
}

// Decision is Decide's answer: what to send, and what not to with the reason.
type Decision struct {
	Chosen     []Channel
	Suppressed []Suppression
}

// Decide is the whole of "the tenant's preferences and the person's decide the
// channels", as a pure function so that the fake a consumer tests against and
// the service that runs against Postgres cannot drift apart.
//
// It is total, which is the property the ledger depends on: every channel in
// wants appears exactly once in Chosen or in Suppressed, so every requested
// channel has an answer the same moment the notice is written — the chosen ones
// get a terminal row when their provider answers, the suppressed ones get one
// here. A channel Decide forgets is a requested row with no terminal row, which
// is the one thing delivery_ledger_coverage measures against.
//
// The order of the rules is the order the reasons are given in, and the first
// refusal wins: a channel this deployment does not send is not also reported as
// the person's choice, because that would tell them to look at a settings page
// that would change nothing.
//
// Two things are deliberately not here. in-app is never suppressed — it is the
// notice, and a preference that could drop it would be a notice silently not
// written — and ClassSecurity bypasses the person's choices: the reason
// opt-out exists is not to be told about a password change. Whether an intent is
// a security one is said by the caller on the notice, never guessed from its
// name, because the name is the caller's vocabulary.
//
// Whether the person has an address, a device or an endpoint at all is also not
// here: that is a lookup, Decide takes no transaction, and the worker suppresses
// what it cannot address in its own. Neither is the deployment's own mail
// identity, which is SenderOptional above rather than a row in a table.
func Decide(wants Wants, class, intent string, prefs []Preference, quiet *QuietHours, sender *Sender, s Scope) Decision {
	d := Decision{}
	for _, c := range wants.AsChannels() {
		switch {
		case c == ChannelInApp:
			d.Chosen = append(d.Chosen, c)
		case !s.available(c):
			d.Suppressed = append(d.Suppressed, Suppression{c, string(c) + " is not sent by this deployment", CorrectionDeployment})
		case s.disabled(c):
			d.Suppressed = append(d.Suppressed, Suppression{c, "this tenant switched " + string(c) + " off", CorrectionTenant})
		case c == ChannelEmail && !s.SenderOptional && sender == nil:
			d.Suppressed = append(d.Suppressed, Suppression{c,
				"this tenant has no sender: mail is refused until its administrator sets one", CorrectionTenant})
		case c == ChannelEmail && !s.SenderOptional && sender.Status != SenderVerified:
			d.Suppressed = append(d.Suppressed, Suppression{c,
				"this tenant's sender " + sender.FromAddress + " has no verified DKIM record for " + sender.DKIMName() + " (status " + sender.Status + ")", CorrectionTenant})
		case c == ChannelEmail && !s.SenderOptional && len(sender.Key) == 0:
			d.Suppressed = append(d.Suppressed, Suppression{c,
				"this deployment holds no DKIM key to sign as " + sender.DKIMName(), CorrectionDeployment})
		case class != ClassSecurity && !Enabled(prefs, intent, c):
			d.Suppressed = append(d.Suppressed, Suppression{c, string(c) + " is off for " + intentName(intent), CorrectionRecipient})
		case class != ClassSecurity && quiet.Active(s.Now):
			d.Suppressed = append(d.Suppressed, Suppression{c, "quiet hours until " + quietEnd(quiet), CorrectionRecipient})
		default:
			d.Chosen = append(d.Chosen, c)
		}
	}
	return d
}

func intentName(intent string) string {
	if intent == "" {
		return "every notice"
	}
	return intent
}

// quietEnd is when the window closes, in the words of a notice somebody will
// read: "07:00 Europe/Lisbon".
func quietEnd(q *QuietHours) string {
	if q == nil {
		return ""
	}
	return fmt.Sprintf("%02d:%02d %s", q.EndMinute/60, q.EndMinute%60, q.TimeZone)
}

// trimASCII is what every text field on this module's entities does before
// checking it: an entity that stores " email " would be a row nobody finds with
// the channel name it holds.
func trimASCII(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
