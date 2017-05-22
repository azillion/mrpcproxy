package main

import (
	"encoding/json"
	"flag"
	"log"
	"time"

	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport/mem"
	"github.com/miracl/mrpcproxy"
	"github.com/miracl/mrpcproxy/sdk"
)

func main() {
	addr := flag.String("addr", ":8080", "Address for the http server")
	flag.Parse()

	transport := mem.New()
	go server(transport)
	proxy(*addr, transport)
}

func proxy(addr string, transport mrpc.Transport) {
	service, _ := mrpc.NewService(transport, mrpc.WithName("proxy"))

	pxy, _ := sdk.New(
		addr, service,
		sdk.WithDynamicEndpoints(),
		sdk.WithHeaders(map[string]string{"Content-Type": "text/plain; charset=utf-8"}),
	)

	eps, _ := sdk.ParseMapping([]byte(`{
    "service.hello": {
        "endpoints": [{
            "method": "GET",
            "path": "/hello",
						"keepAlive": 1
        }]
    }
	}`))
	pxy.Handle(eps...)

	log.Println("Starting example proxy")
	log.Fatalf("Proxy stopped: %v", pxy.Serve())
}

// Simulate service handling mrpc request
func server(transport mrpc.Transport) {
	service, _ := mrpc.NewService(transport)

	// Handle statically registered endpoint
	service.HandleFunc("hello", func(w mrpc.TopicWriter, data []byte) {
		log.Println("[Upstream] Request: hello")

		msg, _ := json.Marshal(&mrpcproxy.Response{
			Code: 200,
			Msg:  []byte("Hello world"),
		})

		w.Write(msg)
	})

	// Register and handle dynamic endpoint
	ep := mrpcproxy.Endpoint{
		Topic:  "service.dynamic",
		Method: "GET",
		Path:   "/dynamic",
	}
	if err := mrpcproxy.RegisterEndpoints(service, "proxy", time.Second, ep); err != nil {
		log.Println("Dynamic endpoint registration failed")
	}

	// TODO: REMOVE
	if err := mrpcproxy.RegisterEndpoints(service, "proxy", time.Second, ep); err != nil {
		log.Println("Dynamic endpoint registration failed")
	}

	service.HandleFunc("dynamic", func(w mrpc.TopicWriter, data []byte) {
		log.Println("[Upstream] Request: dynamic")

		msg, _ := json.Marshal(&mrpcproxy.Response{
			Code: 200,
			Msg:  []byte("Hello world"),
		})

		w.Write(msg)
	})

	log.Println("Starting example service")
	log.Fatalf("Service stopped: %v", service.Serve())
}
