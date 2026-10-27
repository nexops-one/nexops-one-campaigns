// SPDX-License-Identifier: Apache-2.0

// Package access defines the workspace roles and the permission matrix that
// every API route and engine action is checked against. Roles are additive:
// a principal may hold several, and is allowed what any of them allows.
package access

import (
	"fmt"
	"strings"
)

// Role is a workspace role.
type Role string

const (
	RoleAuditor  Role = "auditor"  // reads everything, including the audit log; never writes
	RoleOwner    Role = "owner"    // maintains data and evidence, submits controls
	RoleReviewer Role = "reviewer" // reviews submitted controls, can send them back
	RoleApprover Role = "approver" // approves controls
	RoleAdmin    Role = "admin"    // manages members, tokens and settings; not implicitly an approver
)

var allRoles = []Role{RoleAuditor, RoleOwner, RoleReviewer, RoleApprover, RoleAdmin}

var descriptions = map[Role]string{
	RoleAuditor:  "Reads everything in the workspace, including the audit log, workflow history and evidence metadata. Never writes.",
	RoleOwner:    "Maintains register data (ingestion, import, rollback), runs evaluations, attaches evidence, submits controls for review, generates reports; can be assigned as control owner.",
	RoleReviewer: "Reviews submitted controls: records review notes, recommends, sends back.",
	RoleApprover: "Everything a reviewer may do, plus approval (never of a control they own, unless self-approval is allowed).",
	RoleAdmin:    "Manages members, roles, tokens, retention and workspace settings. Not implicitly an approver.",
}

// Describe returns a role's purpose.
func Describe(r Role) string { return descriptions[r] }

// AllRoles lists every role in canonical order.
func AllRoles() []Role { return append([]Role(nil), allRoles...) }

// Permission is an action a route or engine operation requires.
type Permission string

const (
	PermDataRead        Permission = "data.read"
	PermDataWrite       Permission = "data.write"
	PermEvaluationRun   Permission = "evaluation.run"
	PermEvidenceWrite   Permission = "evidence.write"
	PermWorkflowAssign  Permission = "workflow.assign"
	PermWorkflowSubmit  Permission = "workflow.submit"
	PermWorkflowReview  Permission = "workflow.review"
	PermWorkflowApprove Permission = "workflow.approve"
	PermAuditRead       Permission = "audit.read"
	PermMembersManage   Permission = "members.manage"
	PermTokensOwn       Permission = "tokens.own"
	PermTokensManage    Permission = "tokens.manage"
	PermSettingsManage  Permission = "settings.manage"
	PermReportGenerate  Permission = "report.generate"
)

var permissions = []Permission{
	PermDataRead, PermDataWrite, PermEvaluationRun, PermEvidenceWrite, PermWorkflowAssign, PermWorkflowSubmit, PermWorkflowReview,
	PermWorkflowApprove, PermReportGenerate, PermAuditRead, PermMembersManage, PermTokensOwn, PermTokensManage, PermSettingsManage,
}

// matrix is the single source of the role and permission table; docs/access.md
// reproduces it and a test keeps the two in sync.
var matrix = map[Permission][]Role{
	PermDataRead:        allRoles,
	PermDataWrite:       {RoleOwner},
	PermEvaluationRun:   {RoleOwner, RoleReviewer, RoleApprover},
	PermEvidenceWrite:   {RoleOwner},
	PermWorkflowAssign:  {RoleOwner, RoleAdmin},
	PermWorkflowSubmit:  {RoleOwner},
	PermWorkflowReview:  {RoleReviewer, RoleApprover},
	PermWorkflowApprove: {RoleApprover},
	PermReportGenerate:  {RoleOwner, RoleReviewer, RoleApprover},
	PermAuditRead:       {RoleAuditor, RoleAdmin},
	PermMembersManage:   {RoleAdmin},
	PermTokensOwn:       allRoles,
	PermTokensManage:    {RoleAdmin},
	PermSettingsManage:  {RoleAdmin},
}

// Permissions lists every permission in canonical order.
func Permissions() []Permission { return append([]Permission(nil), permissions...) }

// Granted lists the roles holding p.
func Granted(p Permission) []Role { return append([]Role(nil), matrix[p]...) }

// Allows reports whether any of rs holds p.
func Allows(rs []Role, p Permission) bool {
	for _, r := range matrix[p] {
		if Contains(rs, r) {
			return true
		}
	}
	return false
}

// Contains reports whether r is in rs.
func Contains(rs []Role, r Role) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// Valid reports whether r is a known role.
func Valid(r Role) bool { return Contains(allRoles, r) }

// Validate fails on an unknown role.
func Validate(rs []Role) error {
	for _, r := range rs {
		if !Valid(r) {
			return fmt.Errorf("unknown role %q (known: %s)", r, joinRoles(allRoles, ", "))
		}
	}
	return nil
}

// Normalize deduplicates rs and orders it canonically. Unknown roles are dropped;
// call Validate first where they must be refused.
func Normalize(rs []Role) []Role {
	out := []Role{}
	for _, r := range allRoles {
		if Contains(rs, r) {
			out = append(out, r)
		}
	}
	return out
}

// Subset reports whether every role of sub is in of.
func Subset(sub, of []Role) bool {
	for _, r := range sub {
		if !Contains(of, r) {
			return false
		}
	}
	return true
}

// Intersect returns the roles in both a and b, in canonical order.
func Intersect(a, b []Role) []Role {
	out := []Role{}
	for _, r := range Normalize(a) {
		if Contains(b, r) {
			out = append(out, r)
		}
	}
	return out
}

// ParseRoles parses roles separated by sep, for example "owner+admin".
func ParseRoles(s, sep string) ([]Role, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("no roles given (known: %s)", joinRoles(allRoles, ", "))
	}
	var rs []Role
	for _, part := range strings.Split(s, sep) {
		r := Role(strings.TrimSpace(part))
		if r == "" {
			return nil, fmt.Errorf("empty role in %q", s)
		}
		rs = append(rs, r)
	}
	if err := Validate(rs); err != nil {
		return nil, err
	}
	return Normalize(rs), nil
}

// Join formats roles with sep, for example for configuration entries.
func Join(rs []Role, sep string) string { return joinRoles(rs, sep) }

func joinRoles(rs []Role, sep string) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = string(r)
	}
	return strings.Join(s, sep)
}
