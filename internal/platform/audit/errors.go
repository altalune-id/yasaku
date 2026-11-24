package audit

import "errors"

// NoUnitOfWorkError refuses a Record made outside a transaction, where the entry could commit without its change or the change without it.
type NoUnitOfWorkError struct{ Action string }

func (e *NoUnitOfWorkError) Error() string {
	return "audit: " + e.Action + " recorded outside a unit of work"
}

// IsNoUnitOfWorkError reports whether err's tree contains a *NoUnitOfWorkError.
func IsNoUnitOfWorkError(err error) bool {
	_, ok := errors.AsType[*NoUnitOfWorkError](err)
	return ok
}
