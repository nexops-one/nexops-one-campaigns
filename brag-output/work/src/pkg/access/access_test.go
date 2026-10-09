// SPDX-License-Identifier: Apache-2.0

package access_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
)

// matrix mirrors the table in docs/access.md: one string per permission, the
// letters of the roles holding it in AllRoles order (a=auditor o=owner
// r=reviewer p=approver d=admin).
var matrix = map[access.Permission]string{
	access.PermDataRead:        "aorpd",
	access.PermDataWrite:       "o",
	access.PermEvaluationRun:   "orp",
	access.PermEvidenceWrite:   "o",
	access.PermWorkflowAssign:  "od",
	access.PermWorkflowSubmit:  "o",
	access.PermWorkflowReview:  "rp",
	access.PermWorkflowApprove: "p",
	access.PermReportGenerate:  "orp",
	access.PermAuditRead:       "ad",
	access.PermMembersManage:   "d",
	access.PermTokensOwn:       "aorpd",
	access.PermTokensManage:    "d",
	access.PermSettingsManage:  "d",
}

var letters = map[access.Role]string{
	access.RoleAuditor: "a", access.RoleOwner: "o", access.RoleReviewer: "r", access.RoleApprover: "p", access.RoleAdmin: "d",
}

func TestMatrix(t *testing.T) {
	if len(access.Permissions()) != len(matrix) {
		t.Fatalf("permissions = %v", access.Permissions())
	}
	for _, p := range access.Permissions() {
		want, ok := matrix[p]
		if !ok {
			t.Fatalf("permission %s missing from the test matrix", p)
		}
		for _, r := range access.AllRoles() {
			if got := access.Allows([]access.Role{r}, p); got != strings.Contains(want, letters[r]) {
				t.Errorf("Allows(%s, %s) = %v", r, p, got)
			}
		}
	}
}

func TestEveryPermissionIsGranted(t *testing.T) {
	for _, p := range access.Permissions() {
		if len(access.Granted(p)) == 0 {
			t.Errorf("%s is granted to no role", p)
		}
	}
	if access.Allows(nil, access.PermDataRead) {
		t.Fatal("no roles must allow nothing")
	}
	if access.Allows(access.AllRoles(), "unknown.permission") {
		t.Fatal("an unknown permission must never be allowed")
	}
}

func TestParseRoles(t *testing.T) {
	got, err := access.ParseRoles("owner+admin", "+")
	if err != nil || !reflect.DeepEqual(got, []access.Role{access.RoleOwner, access.RoleAdmin}) {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = access.ParseRoles("admin+owner+owner", "+")
	if err != nil || !reflect.DeepEqual(got, []access.Role{access.RoleOwner, access.RoleAdmin}) {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = access.ParseRoles(" auditor , approver ", ",")
	if err != nil || !reflect.DeepEqual(got, []access.Role{access.RoleAuditor, access.RoleApprover}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"", "root", "owner+", "owner++admin"} {
		if _, err := access.ParseRoles(bad, "+"); err == nil {
			t.Errorf("ParseRoles(%q) must fail", bad)
		}
	}
}

func TestSubsetIntersect(t *testing.T) {
	o, a, p := access.RoleOwner, access.RoleAdmin, access.RoleApprover
	if !access.Subset([]access.Role{o}, []access.Role{a, o}) || access.Subset([]access.Role{p}, []access.Role{a, o}) {
		t.Fatal("Subset")
	}
	if !access.Subset(nil, nil) {
		t.Fatal("the empty set is a subset of anything")
	}
	if got := access.Intersect([]access.Role{a, p, o}, []access.Role{o, a}); !reflect.DeepEqual(got, []access.Role{o, a}) {
		t.Fatalf("Intersect = %v", got)
	}
	if !access.Contains([]access.Role{o}, o) || access.Contains(nil, o) {
		t.Fatal("Contains")
	}
	if err := access.Validate([]access.Role{"root"}); err == nil {
		t.Fatal("Validate must refuse unknown roles")
	}
}
