package sdk

import (
	"encoding/json"
	"fmt"
	"log"

	"sync"

	"github.com/miracl/mrpc"
	"github.com/miracl/mrpcproxy"
)

type dynamicEndpoints struct {
	ch chan []mrpcproxy.Endpoint

	mu   sync.Mutex
	list map[string]struct{}
}

func (de *dynamicEndpoints) Serve(w mrpc.TopicWriter, requestTopic string, data []byte) {
	// fmt.Println("Start registering")
	req := mrpcproxy.RegisterRequest{}
	if err := json.Unmarshal(data, &req); err != nil {
		log.Println("fail unmarshaling endpoint", err)
		// TODO: respond invalid request
		return
	}

	newEps := []mrpcproxy.Endpoint{}
	for _, ep := range req.Endpoints {
		newEp := func() bool {
			de.mu.Lock()
			// fmt.Println("de.mu.Lock()")
			defer func() {
				de.mu.Unlock()
				// fmt.Println("de.mu.Unlock()")
			}()

			if _, ok := de.list[hashEp(ep)]; ok {
				return false
			}

			de.list[hashEp(ep)] = struct{}{}
			return true
		}()

		if newEp {
			newEps = append(newEps, ep)
		}
	}

	if len(newEps) > 0 {
		de.ch <- newEps
	}
	// fmt.Println("SENT")

	regRes := mrpcproxy.RegisterResponse{Status: mrpcproxy.RegistrationOK}
	res, err := json.Marshal(regRes)
	if err != nil {
		log.Println("fail marshaling response")
		return
	}

	// fmt.Println("WRITING")
	w.Write(res)
	// fmt.Println("DONE")
}

func hashEp(ep mrpcproxy.Endpoint) string {
	return fmt.Sprintf("%v:%v", ep.Method, ep.Path)
}

func startDynamicEndpoints(s *mrpc.Service) <-chan []mrpcproxy.Endpoint {
	de := &dynamicEndpoints{
		ch:   make(chan []mrpcproxy.Endpoint),
		list: map[string]struct{}{},
	}

	if err := s.Handle(mrpcproxy.RegistrationTopic, de); err != nil {
		log.Println("err")
	}
	return de.ch
}
