package limits

import (
	"errors"
	"strconv"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/config"
)

// Reason says what kind of ceiling refused a write.
type Reason string

const (
	// ReasonLimit is a count limit.
	ReasonLimit Reason = "limit"
	// ReasonQuota is a metered quota; reserved.
	ReasonQuota Reason = "quota"
	// ReasonBalance is a spend balance; reserved.
	ReasonBalance Reason = "balance"
)

// ReachedError refuses a create that would take a subject past its limit; Name and Pending carry what the message shows.
type ReachedError struct {
	Key     Key
	Max     int64
	Reason  Reason
	Hidden  bool
	Name    string
	Pending int64

	mode    config.Mode
	contact string
}

func (e *ReachedError) Error() string {
	return "limits: " + string(e.Key) + " reached at " + strconv.FormatInt(e.Max, 10)
}

// ToAppError maps ReachedError to the LIM001 FailedPrecondition envelope.
func (e *ReachedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeLimitReached,
		Message(string(i18n.EnUS), e.mode, e, e.contact),
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeLimitReached,
			Meta: map[string]string{"key": string(e.Key), "max": strconv.FormatInt(e.Max, 10), "reason": string(e.Reason)},
		},
	)
}

// IsReachedError reports whether err's tree contains a *ReachedError.
func IsReachedError(err error) bool {
	_, ok := errors.AsType[*ReachedError](err)
	return ok
}
