package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ParseError is returned when endpoints.json can't be parsed.
type ParseError struct {
	err error
}

func (e ParseError) Error() string {
	return fmt.Sprintf("error parsing endpoints: %v", e.err)
}

var (
	// ErrNoEndpoints is returned on parsing when endpoints.json is empty
	ErrNoEndpoints = errors.New("no paths parsed")
)

// Endpoint is the the representation of a single route.
type Endpoint struct {
	Topic     string `json:"topic"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	KeepAlive int    `json:"keepAlive"` // In seconds. Overrides the default NATS timeout.
}

type endpointsJSON map[string]struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// ParseMapping validates and parses endpoints.
//
// ParseMapping won't check for duplicated method:path pairs, router.Handle will panic in
// that case.
func ParseMapping(eps []byte) ([]Endpoint, error) {
	topicMap := endpointsJSON{}
	if err := json.Unmarshal(eps, &topicMap); err != nil {
		return nil, ParseError{err}
	}

	if len(topicMap) == 0 {
		// Map is empty
		return nil, ErrNoEndpoints
	}

	mapping := []Endpoint{}
	for topic, eps := range topicMap {
		for _, ep := range eps.Endpoints {
			ep.Topic = topic
			mapping = append(mapping, ep)
		}
	}

	return mapping, nil
}
