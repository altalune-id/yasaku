package limits

import "errors"

// NoUnitOfWorkError refuses a Lock made outside a transaction, where the lock would end before the insert it guards.
type NoUnitOfWorkError struct{}

func (*NoUnitOfWorkError) Error() string { return "limits: Lock needs a unit of work" }

// IsNoUnitOfWorkError reports whether err's tree contains a *NoUnitOfWorkError.
func IsNoUnitOfWorkError(err error) bool {
	_, ok := errors.AsType[*NoUnitOfWorkError](err)
	return ok
}

// UnknownKeyError refuses a key the catalogue does not name.
type UnknownKeyError struct{ Key Key }

func (e *UnknownKeyError) Error() string { return "limits: unknown key " + string(e.Key) }

// IsUnknownKeyError reports whether err's tree contains an *UnknownKeyError.
func IsUnknownKeyError(err error) bool {
	_, ok := errors.AsType[*UnknownKeyError](err)
	return ok
}

// SubjectMismatchError refuses an override whose subject kind is not the one its key counts against.
type SubjectMismatchError struct {
	Key  Key
	Want SubjectKind
	Got  SubjectKind
}

func (e *SubjectMismatchError) Error() string {
	return "limits: " + string(e.Key) + " counts against a " + string(e.Want) + ", not a " + string(e.Got)
}

// IsSubjectMismatchError reports whether err's tree contains a *SubjectMismatchError.
func IsSubjectMismatchError(err error) bool {
	_, ok := errors.AsType[*SubjectMismatchError](err)
	return ok
}

// SubjectMissingError refuses a Lock whose subject row does not exist or is out of scope, so no create proceeds unlocked.
type SubjectMissingError struct{ Subject Subject }

func (e *SubjectMissingError) Error() string {
	return "limits: lock found no " + string(e.Subject.Kind) + " " + e.Subject.ID.String()
}

// IsSubjectMissingError reports whether err's tree contains a *SubjectMissingError.
func IsSubjectMissingError(err error) bool {
	_, ok := errors.AsType[*SubjectMissingError](err)
	return ok
}
