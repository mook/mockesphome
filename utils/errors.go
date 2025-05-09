package utils

import "errors"

// Reports whether any of the targets satisifies any error in the tree of the
// given error; that is, if [errors.Is] returns true.
func AnyError(err error, targets ...error) bool {
	for _, target := range targets {
		if target == nil && err == nil {
			return true
		} else if errors.Is(err, target) {
			return true
		}
	}
	return false
}
