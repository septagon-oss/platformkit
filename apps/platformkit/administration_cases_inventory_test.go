package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheInventoryKeepsTheAdministrationFloorCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Rows []struct {
			File    string `json:"file"`
			Name    string `json:"name"`
			Layer   string `json:"layer"`
			Tier    string `json:"tier"`
			NeedsDB bool   `json:"needs_db"`
			Verdict string `json:"verdict"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"TestTheTwoFloorsComposeIntoOneInvariant":                                            "apps/platformkit/administration_lock_test.go",
		"TestAnEmptyingWriteAndAStandingDownWriteRace":                                       "apps/platformkit/administration_lock_test.go",
		"TestTwoAdministratorsStandingDownAtOnce":                                            "apps/platformkit/administration_lock_test.go",
		"TestTwoAdministeringRolesEmptiedAtOnce":                                             "apps/platformkit/administration_lock_test.go",
		"TestTheComposedCheckJudgesOneTenant":                                                "apps/platformkit/administration_lock_test.go",
		"TestAdministrationRefusalsKeepTheirPublishedPayloads":                               "apps/platformkit/administration_refusal_schema_test.go",
		"TestDeactivatingSomebodyWhoNeverAdministeredDoesNotRefuseForFindingNoAdministrator": "apps/platformkit/deactivation_of_a_non_administrator_test.go",
		"TestADeactivationAndAnEmptyingWriteRace":                                            "apps/platformkit/deactivation_race_test.go",
		"TestAPasskeySignInGrantDoesNotHoldTheAdministratorsFloor":                           "apps/platformkit/passkey_grant_floor_test.go",
		"TestRemovingAPasskeyGrantDoesNotRefuseForFindingNoAdministrator":                    "apps/platformkit/passkey_grant_removal_test.go",
		"TestTheRaceGoalRunsThePackagesThatHoldTheFloorsRacingPairs":                         "apps/platformkit/race_goal_covers_the_floors_packages_test.go",
		"TestARefusedAdministrationWriteIsInTheTrail":                                        "apps/platformkit/refused_administration_audit_readback_test.go",
		"TestARefusedAdministrationWriteIsNotInTheNeighboursTrail":                           "apps/platformkit/refused_administration_audit_tenancy_test.go",
		"TestARefusedAdministrationWriteLeavesAnAuditRecord":                                 "apps/platformkit/refused_administration_trail_test.go",
		"TestRemovingAnOrdinaryMembershipDoesNotRefuseForFindingNoAdministrator":             "apps/platformkit/removal_of_an_ordinary_membership_test.go",
		"TestAnotherTenantsHoldersDoNotAnswerForTheRolesDoor":                                "apps/platformkit/roles_door_tenant_test.go",
		"TestAdministrationRefusalPreservesTheRowAndPublishesOnlyTheAttempt":                 "modules/user/internal/administration_refusal_state_test.go",
	}
	seen := make(map[string]int)
	for _, row := range inventory.Rows {
		if row.Name == "TestTheTwoFloorsStillDoNotComposeIntoOneInvariant" {
			t.Error("the inventory retains the replaced case that asserted the administration defect")
		}
		file, ok := want[row.Name]
		if !ok {
			continue
		}
		seen[row.Name]++
		layer := "composition"
		if strings.HasPrefix(file, "modules/") {
			layer = "behaviour"
		}
		if row.File != file || row.Layer != layer || row.Tier != "merge" || !row.NeedsDB || row.Verdict != "keep" {
			t.Errorf("%s: inventory row %+v must keep %s in the %s layer and database-backed merge tier", row.Name, row, file, layer)
		}
	}
	for name := range maps.Keys(want) {
		if seen[name] != 1 {
			t.Errorf("%s has %d inventory rows, want one", name, seen[name])
		}
	}
}
