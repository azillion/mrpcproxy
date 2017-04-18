package mrpcproxy

import "net/http"

// Response is the the format of a mrpcproxy response
type Response struct {
	RequestID string
	Code      int
	Msg       []byte
	Headers   http.Header
}
