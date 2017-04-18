package main

import (
	"encoding/json"
	"flag"
	"log"

	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport/mem"
	"github.com/miracl/mrpcproxy"
	"github.com/miracl/mrpcproxy/sdk"
)

var (
	address = flag.String("addr", ":8080", "Address for the http server")
)

const (
	group   = "example"
	name    = "example"
	version = "1.0"

	endpoints = `
		{
		  "example.hello": {
		    "endpoints": [
		      {
		        "method": "GET",
		        "path": "/hello"
		      }
		    ]
		  }
		}`
)

func main() {
	flag.Parse()

	// Create the service
	service, _ := mrpc.NewService(
		mem.New(),
		func(s *mrpc.Service) error {
			s.Group = group
			s.Name = name
			s.Version = version
			return nil
		},
	)

	eps, _ := sdk.ParseMapping([]byte(endpoints))
	pxy, _ := sdk.New(
		*address, service,
		func(pxy *sdk.Proxy) error {
			pxy.Headers = map[string]string{"Content-Type": "text/plain; charset=utf-8"}
			return nil
		},
	)
	pxy.Handle(eps...)

	// Simulate application handling mrpc request
	service.HandleFunc("hello", func(w mrpc.TopicWriter, data []byte) {
		log.Println("[Upstream] Request: hello")

		msg, _ := json.Marshal(&mrpcproxy.Response{
			// RequestID string
			Code: 200,
			Msg:  []byte("Hello world"),
		})

		w.Write(msg)
	})

	log.Println("Starting example proxy")
	log.Fatalf("Service stopped: %v", pxy.Serve())
}
