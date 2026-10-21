package server

import "connectrpc.com/connect/v2"

// codeOf returns err's connect code, or 0 for a nil error.
func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}
