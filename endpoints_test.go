package mrpcproxy

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type regServiceMock struct {
	response RegisterResponse

	called bool
	topic  string
	data   []byte
}

func (s *regServiceMock) withResponse(r RegisterResponse) {
	s.response = r
}

func (s *regServiceMock) Request(topic string, data []byte, t time.Duration) ([]byte, error) {
	s.called = true
	s.topic = topic
	s.data = data
	return json.Marshal(s.response)
}

func TestRegisterEndpoints(t *testing.T) {
	cases := []struct {
		given  string
		regRes RegisterResponse
		err    bool
	}{
		{
			given:  "everything is OK",
			regRes: RegisterResponse{Status: RegistrationOK},
			err:    false,
		},
		{
			given:  "proxy responds with error",
			regRes: RegisterResponse{Status: RegistrationErr, Reason: ""},
			err:    true,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v when %v", i, tc.given), func(t *testing.T) {
			s := &regServiceMock{}
			s.withResponse(tc.regRes)

			ep := Endpoint{Topic: "topic", Method: "GET", Path: "/topic"}

			err := RegisterEndpoints(s, "proxy", time.Second, ep)
			if err != nil && !tc.err {
				t.Fatal(err)
			}
			if err == nil && tc.err {
				t.Fatal("expected error")
			}

			if !s.called {
				t.Fatal("no request to the proxy is performed")
			}

			if s.topic != fmt.Sprintf("proxy.%v", RegistrationTopic) {
				t.Fatalf("mrpc request on the wrong topic: %v", s.topic)
			}
		})
	}

}
