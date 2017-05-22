package mrpcproxy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/miracl/mrpc"
)

const (
	// RegistrationTopic is the topic for new endpoint registration
	RegistrationTopic = "endpoint.registration"
)

// Endpoint is the the representation of a single route.
type Endpoint struct {
	Topic  string `json:"topic"`
	Method string `json:"method"`
	Path   string `json:"path"`
	// KeepAlive overwrites the proxy default timeout.
	KeepAlive time.Duration `json:"keepAlive"`
}

// RegisterRequest is the request for new endpoint registration from the proxy.
type RegisterRequest struct {
	Endpoints []Endpoint
}

type regStatus int

// Dynamic endpoint registration response statuses.
const (
	RegistrationOK regStatus = iota
	RegistrationErr
)

// RegisterResponse is the response for new endpoint registration from the proxy.
type RegisterResponse struct {
	Status regStatus
	Reason string
}

type mrpcRequester interface {
	Request(topic string, data []byte, t time.Duration) ([]byte, error)
}

// RegisterEndpoints dynamically registers endpoints.
func RegisterEndpoints(s mrpcRequester, proxyServiceName string, t time.Duration, eps ...Endpoint) error {
	data, err := json.Marshal(RegisterRequest{Endpoints: eps})
	if err != nil {
		return err
	}

	resData, err := s.Request(mrpc.GetFQTopic(proxyServiceName, RegistrationTopic), data, t)
	if err != nil {
		return fmt.Errorf("service request failed: %v", err)
	}

	res := RegisterResponse{}
	if err := json.Unmarshal(resData, &res); err != nil {
		return fmt.Errorf("can't unmarshal registration response %v", err)
	}

	if res.Status == RegistrationErr {
		return fmt.Errorf("endpoint registration failed: %v", res.Reason)
	}

	return nil
}
