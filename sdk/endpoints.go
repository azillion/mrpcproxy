package sdk

import (
	"encoding/json"
	"errors"
	"fmt"

	"time"

	"github.com/miracl/mrpcproxy"
)

var (
	// ErrNoEndpoints is returned on parsing when endpoints.json is empty
	ErrNoEndpoints = errors.New("no paths parsed")
)

// ParseError is returned when endpoints.json can't be parsed.
type ParseError struct {
	err error
}

func (e ParseError) Error() string {
	return fmt.Sprintf("error parsing endpoints: %v", e.err)
}

type endpointsJSON map[string]struct {
	Endpoints []struct {
		Method    string `json:"method"`
		Path      string `json:"path"`
		KeepAlive int    `json:"keepAlive"` // in seconds
	} `json:"endpoints"`
}

// ParseMapping validates and parses endpoints.
//
// ParseMapping won't check for duplicated method:path pairs, router.Handle will panic in
// that case.
func ParseMapping(eps []byte) ([]mrpcproxy.Endpoint, error) {
	topicMap := endpointsJSON{}
	if err := json.Unmarshal(eps, &topicMap); err != nil {
		return nil, ParseError{err}
	}

	if len(topicMap) == 0 {
		// Map is empty
		return nil, ErrNoEndpoints
	}

	mapping := []mrpcproxy.Endpoint{}
	for topic, eps := range topicMap {
		for _, ep := range eps.Endpoints {
			mapping = append(mapping, mrpcproxy.Endpoint{
				Topic:     topic,
				Method:    ep.Method,
				Path:      ep.Path,
				KeepAlive: time.Duration(ep.KeepAlive) * time.Second,
			})
		}
	}

	return mapping, nil
}
