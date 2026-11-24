package controlplane

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// ProjectUnresolvedError signals a request omitted project_id while its principal carries no active project.
type ProjectUnresolvedError struct{}

func (*ProjectUnresolvedError) Error() string {
	return "controlplane: project_id omitted and the principal carries no active project"
}

// ToAppError maps ProjectUnresolvedError to a FailedPrecondition envelope naming the way out.
func (*ProjectUnresolvedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeProjectUnresolved,
		"No project was given and this credential has no active project. Call project_list to get a projectId, then pass it as projectId.",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeProjectUnresolved,
			Meta: map[string]string{"field": "project_id"},
		},
	)
}

// IsProjectUnresolvedError reports whether err's tree contains a *ProjectUnresolvedError.
func IsProjectUnresolvedError(err error) bool {
	_, ok := errors.AsType[*ProjectUnresolvedError](err)
	return ok
}

// OrgChoiceRequiredError signals a caller in several orgs that named none; Orgs lists their slugs.
type OrgChoiceRequiredError struct {
	Orgs []string
}

func (e *OrgChoiceRequiredError) Error() string {
	return "controlplane: more than one org is available: " + strings.Join(e.Orgs, ",")
}

// ToAppError maps OrgChoiceRequiredError to a FailedPrecondition envelope naming the orgs to choose from.
func (e *OrgChoiceRequiredError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeOrgChoiceRequired,
		"You belong to more than one organization. Pass org as one of: "+strings.Join(e.Orgs, ", ")+".",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeOrgChoiceRequired,
			Meta: map[string]string{"field": "org", "orgs": strings.Join(e.Orgs, ",")},
		},
	)
}

// IsOrgChoiceRequiredError reports whether err's tree contains an *OrgChoiceRequiredError.
func IsOrgChoiceRequiredError(err error) bool {
	_, ok := errors.AsType[*OrgChoiceRequiredError](err)
	return ok
}
