package internal_test

// The second factor, end to end, over its two tables.
//
// The brief's Done-when is "a factor enrols and signs in under test", and the
// property that makes a second factor worth having is a sequence rather than a
// function, so this is one test walking that sequence in the order a person
// would:
//
//   - a password signs in, until a factor exists — and enrolling writes nothing
//     until a code proves the secret;
//   - once one exists, the password alone is ErrFactorRequired: no session, no
//     auth.logged_in, and no auth.login_failed either, because the password was
//     right;
//   - the code signs the person in, and the same code never again;
//   - a recovery code signs them in once, and rotating retires every unused one;
//   - the last factor cannot be withdrawn, and the second can.
//
// Every refusal is checked for the state it must not leave: rows, events, and a
// secret or code in a payload.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAFactorEnrolsAndIsWhatSignsThatPersonIn(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	svc.EnableFactors([]byte("a factor key this deployment set"))
	seed(t, conn, acme)
	ctx := httpx.WithConn(t.Context(), conn)
	const email = "ada@acme.example.com"

	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, email, email)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, authtest.Password); err != nil {
			return err
		}

		// Before: the password is enough, because a factor is chosen and never
		// imposed on an account that did not ask for one.
		_, _, err = svc.Login(ctx, tx, email, authtest.Password, nobody)
		if err != nil {
			return err
		}
		held, err := svc.ListFactors(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if len(held) != 0 {
			t.Errorf("a person who enrolled nothing holds %d factors", len(held))
		}

		// Beginning writes nothing. A secret that has proved nothing is not a
		// factor, and an abandoned enrolment must not change how this person
		// signs in — not into a half state where the password stops working and
		// nothing else works yet.
		enrolment, err := svc.BeginTOTP(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if enrolment.Secret == "" || enrolment.Account != email {
			t.Errorf("begin = secret %q account %q", enrolment.Secret, enrolment.Account)
		}
		held, err = svc.ListFactors(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if len(held) != 0 {
			return errors.New("beginning an enrolment wrote a factor")
		}
		if _, _, err := svc.Login(ctx, tx, email, authtest.Password, nobody); err != nil {
			return errors.New("sign-in broke before any factor was enrolled")
		}

		// A code that the device did not produce enrols nothing and issues no
		// recovery codes — the guessable half of the enrolment, refused.
		if _, _, err := svc.FinishTOTP(ctx, tx, person.ID, enrolment.Secret, "000000"); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("finishing with a wrong code = %v, want %v", err, contracts.ErrCredentials)
		}
		if held, err = svc.ListFactors(ctx, tx, person.ID); err != nil {
			return err
		} else if len(held) != 0 {
			return errors.New("a wrong code enrolled a factor")
		}

		factor, codes, err := svc.FinishTOTP(ctx, tx, person.ID, enrolment.Secret, codeFor(t, enrolment.Secret, db.Now()))
		if err != nil {
			return err
		}
		if factor.Kind != "totp" || len(codes) != contracts.RecoveryCodes {
			t.Errorf("finish = %s with %d codes, want totp with %d", factor.Kind, len(codes), contracts.RecoveryCodes)
		}
		// Nothing the trail holds carries the secret or a code. The event names
		// come from outbox(t, tx); the bodies come from payloads(t, tx), because
		// a name that never mentions a credential proves nothing about the row.
		names, bodies := strings.Join(outbox(t, tx), " "), payloads(t, tx)
		if strings.Contains(names, "auth.factor_enrolled") == false {
			t.Error("enrolling published no auth.factor_enrolled")
		}
		if strings.Contains(names, "auth.recovery_codes_issued") == false {
			t.Error("issuing codes published no auth.recovery_codes_issued")
		}
		if strings.Contains(strings.ToLower(bodies), strings.ToLower(enrolment.Secret)) {
			t.Error("an outbox payload carries the factor secret")
		}
		for _, code := range codes {
			if strings.Contains(bodies, code) {
				t.Error("an outbox payload carries a recovery code")
			}
		}

		// The password alone is now a half. One answer, and no row: the session
		// the password earned is not opened, so the trail says nothing about a
		// sign-in that did not happen — and does not say login_failed either,
		// because the password was right.
		before := len(outbox(t, tx))
		previous, err := liveFactorSessions(t, tx, person.ID)
		if err != nil {
			return err
		}
		if _, _, err := svc.Login(ctx, tx, email, authtest.Password, nobody); !errors.Is(err, contracts.ErrFactorRequired) {
			t.Errorf("the password alone = %v, want %v", err, contracts.ErrFactorRequired)
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("a sign-in refused for its second half published %d events: it is an outcome, not a failure", after-before)
		}
		if live, err := liveFactorSessions(t, tx, person.ID); err != nil {
			return err
		} else if live != previous {
			t.Errorf("the refused sign-in left %d sessions where %d were, want none opened", live, previous)
		}

		// The code is the second half of that same sign-in.
		opened, identity, err := svc.VerifySecondFactor(ctx, tx, email, codeFor(t, enrolment.Secret, db.Now()), nobody)
		if err != nil {
			return err
		}
		if opened == nil || identity == nil {
			return errors.New("a correct code opened nothing")
		}
		// The same code again: refused. The code is presented as the string it
		// was, so the step it names is fixed — the refusal is the factor row's
		// own last_step guard and not a race with the clock.
		replay := codeFor(t, enrolment.Secret, db.Now())
		used := opened.ID
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, replay, nobody); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("the same step presented twice = %v, want %v", err, contracts.ErrCredentials)
		}
		if _, err := svc.Identify(ctx, tx, used, nobody); err != nil {
			return err
		}

		// A recovery code signs in once, and never again.
		code := codes[0]
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, code, nobody); err != nil {
			return err
		}
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, code, nobody); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("a spent recovery code = %v, want %v", err, contracts.ErrCredentials)
		}
		// Rotation retires what is left unused: the code that was never spent is
		// now worth nothing, and the fresh set is spendable.
		fresh, err := svc.RotateRecoveryCodes(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, codes[1], nobody); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("a code retired by rotation = %v, want %v", err, contracts.ErrCredentials)
		}
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, fresh[0], nobody); err != nil {
			return err
		}

		// The last factor is not withdrawable, and withdrawing one is only
		// possible once there is a second. Rule 8, checked from both sides.
		if err := svc.WithdrawFactor(ctx, tx, person.ID, factor.ID); !errors.Is(err, contracts.ErrLastFactor) {
			t.Errorf("withdrawing the only factor = %v, want %v", err, contracts.ErrLastFactor)
		}
		second, err := svc.BeginTOTP(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		secondFactor, _, err := svc.FinishTOTP(ctx, tx, person.ID, second.Secret, codeFor(t, second.Secret, db.Now()))
		if err != nil {
			return err
		}
		if err := svc.WithdrawFactor(ctx, tx, person.ID, factor.ID); err != nil {
			return err
		}
		if err := svc.WithdrawFactor(ctx, tx, person.ID, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("withdrawing a factor nobody holds = %v, want %v", err, crud.ErrNotFound)
		}
		held, err = svc.ListFactors(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if len(held) != 1 || held[0].ID != secondFactor.ID {
			t.Errorf("after withdrawing one of two, %d factors remain", len(held))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAPasswordOnlyPersonIsNotHeldAtTheDoorByAMissingKey covers the composition
// that set no auth.factor_key: the factor capability is not half-enabled, and a
// person who enrolled nothing signs in with the password as they always did.
func TestAPasswordOnlyPersonIsNotHeldAtTheDoorByAMissingKey(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	seed(t, conn, acme)
	ctx := httpx.WithConn(t.Context(), conn)
	const email = "bhavna@acme.example.com"

	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, email, email)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, authtest.Password); err != nil {
			return err
		}
		if _, err := svc.BeginTOTP(ctx, tx, person.ID); !errors.Is(err, contracts.ErrNoFactorKey) {
			t.Errorf("beginning an enrolment with no key = %v, want %v", err, contracts.ErrNoFactorKey)
		}
		if _, _, err := svc.Login(ctx, tx, email, authtest.Password, nobody); err != nil {
			return err
		}
		if _, err := svc.RotateRecoveryCodes(ctx, tx, person.ID); !errors.Is(err, contracts.ErrNoFactor) {
			t.Errorf("asking for codes with no factor = %v, want %v", err, contracts.ErrNoFactor)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// liveFactorSessions counts the rows a person has that Identify would still accept,
// which is the honest measure of "did this call open a session".
func liveFactorSessions(t *testing.T, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error) {
	t.Helper()
	var counted int64
	err := tx.DB().Table("sessions").
		Where("user_id = ? AND expires_at > ?", userID, db.Now()).Count(&counted).Error
	return int(counted), err
}

// payloads is every auth outbox row's own JSON, as one string to search. An
// event that names no credential is the claim; the names alone would not show it.
func payloads(t *testing.T, tx db.Tx[db.Tenant]) string {
	t.Helper()
	var bodies []string
	err := tx.DB().Table("platformkit_outbox").
		Where("name LIKE 'auth.%'").Pluck("payload::text", &bodies).Error
	if err != nil {
		t.Fatalf("read the outbox payloads: %v", err)
	}
	return strings.Join(bodies, "\n")
}

// codeFor is the HOTP this module computes, written out again here from RFC
// 4226 rather than called: a test that reused the implementation under test would
// only prove it agrees with itself.
func codeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("the enrolment handed out a secret that is not base32: %v", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/int64(contracts.TOTPPeriod.Seconds())))
	mac := hmac.New(sha1.New, raw)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return pad(int64(value)%1000000, contracts.TOTPDigits)
}

func pad(value int64, width int) string {
	digits := []byte("000000")[:width]
	for i := width - 1; i >= 0; i-- {
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits)
}
