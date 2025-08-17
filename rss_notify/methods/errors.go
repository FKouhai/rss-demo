package methods

import "errors"

var (
	incorrect_addr error = errors.New("incorrect method")
	incorrect_type error = errors.New("incorrect content-type")
)
