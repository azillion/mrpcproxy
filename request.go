package mrpcproxy

import (
	"net/http"
	"net/url"
)

// Request is the the format of a mrpcproxy request
type Request struct {
	RequestID string
	Timestamp int64
	Hops      int
	Topic     string
	Action    string
	IPAddress string
	Params    url.Values
	Msg       []byte
	Headers   http.Header
}
