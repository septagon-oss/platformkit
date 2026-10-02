package internal_test

// review 4 (decision 0039: HIGHs only). Check 2 of the review's own list, at the two
// tables this brief adds.
//
// Tenant isolation for sessions has a case (TestAnotherTenantsSessionRefRevokesNothing)
// and for bearer keys it has one (TestATenantsKeyIsNotACredentialAtAnotherTenant).
// Nothing in the tree asks the same question of totp_factors or recovery_codes, which
// are the two tables where "the other tenant's row" is a credential rather than a
// record: a factor seed and a recovery code that another tenant could spend would
// sign that tenant's people in as this tenant's.
//
// The refusal is checked as a fact about the database rather than as a sentence:
// after the attempt from globex, the code that was presented is still spendable at
// acme and the factor's last_step has not moved. A policy that had been dropped would
// have spent the code and opened a session, and no amount of reading the Go would
// show it — the guard is the CREATE POLICY in 000031_auth_factors.up.sql.

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestAnotherTenantsFactorAndRecoveryCodeAreNotSpendableHere(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	svc.EnableFactors([]byte("a factor key this deployment set"))
	seed(t, conn, acme)
	seed(t, conn, globex)
	ctx := httpx.WithConn(t.Context(), conn)
	const email = "ada@one-of-two-tenants.example.com"

	var secret, recovery string
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, email, email)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, authtest.Password); err != nil {
			return err
		}
		if _, err := users.SetRoles(ctx, tx, person.ID, usercontracts.Roles{contracts.RoleAdmin}); err != nil {
			return err
		}
		enrolment, err := svc.BeginTOTP(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if _, codes, err := svc.FinishTOTP(ctx, tx, person.ID, enrolment.Secret,
			codeFor(t, enrolment.Secret, db.Now())); err != nil {
			return err
		} else {
			secret, recovery = enrolment.Secret, codes[0]
		}
		return nil
	})
	if err != nil {
		t.Fatalf("enrol acme's factor: %v", err)
	}

	// The attempt, from the other tenant: acme's address, acme's recovery code, and
	// acme's current TOTP — either of which would open a session at acme.
	totp := codeFor(t, secret, db.Now())
	if err := db.Run(tenancy.WithTenant(ctx, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, answer := range []string{recovery, totp} {
			session, _, err := svc.VerifySecondFactor(ctx, tx, email, answer, nobody)
			if !errors.Is(err, contracts.ErrCredentials) {
				return fmt.Errorf("acme's %s presented at globex = %v, want %v", short(answer), err, contracts.ErrCredentials)
			}
			if session != nil {
				return fmt.Errorf("acme's %s opened a session at globex", short(answer))
			}
		}
		var live int64
		if err := tx.DB().Table("sessions").Count(&live).Error; err != nil {
			return err
		}
		if live != 0 {
			return fmt.Errorf("the refused attempts left %d sessions visible at globex", live)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}

	// And the state did not move at acme: no step was spent, no code was consumed,
	// and the credential that was presented abroad still does its job at home.
	if err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var step int64
		if err := tx.DB().Table("totp_factors").Select("last_step").Scan(&step).Error; err != nil {
			return err
		}
		if step != 0 {
			return fmt.Errorf("the attempt at globex spent acme's factor up to step %d, want 0", step)
		}
		var spent int64
		if err := tx.DB().Table("recovery_codes").Where("used_at IS NOT NULL").Count(&spent).Error; err != nil {
			return err
		}
		if spent != 0 {
			return fmt.Errorf("the attempt at globex spent %d of acme's recovery codes, want 0", spent)
		}
		if _, _, err := svc.VerifySecondFactor(ctx, tx, email, recovery, nobody); err != nil {
			return fmt.Errorf("the recovery code another tenant presented is no longer spendable at home: %w", err)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
}

// short names which of the two answers was refused without quoting it: a test that
// printed a live recovery code into its failure would be the one place a credential
// belongs in a log.
func short(answer string) string {
	if len(answer) == contracts.TOTPDigits {
		return "TOTP code"
	}
	return "recovery code"
}
