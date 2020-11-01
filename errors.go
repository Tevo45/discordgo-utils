package dgutils

import (
	"fmt"
)

/*
 * Errors that are supposed to be introspectable at runtime should
 * be defined here. i.e. things like failure of the command parser
 * (maybe because the user fed it bad data).
 */

type ArgCountMismatch struct {
	Expected, Got int
}

func (e ArgCountMismatch) Error() string {
	return fmt.Sprintf("expected %d arguments but got %d", e.Expected, e.Got)
}

type AccessDenied struct{}

func (e AccessDenied) Error() string {
	return "access denied"
}

//
// Argument parser failure
// Why (probably) has more information about what actually happened
//
type UnmarshalError struct {
	Why error /* underlying error */
}

func (e UnmarshalError) Error() string {
	return fmt.Sprintf("cannot unmarshal arguments: %s", e.Why)
}
