package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

var port = flag.Int("port", 8080, "Port for the example proxy")

func TestSimpleExample(t *testing.T) {
	os.Args = []string{"example", "-addr", fmt.Sprintf(":%v", *port)}
	go main()

	// Block so the main starts
	time.Sleep(100 * time.Millisecond)

	// Request the service
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%v/hello", *port))
	if err != nil {
		t.Fatal(err)
	}
	hello, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatalf("Read request body: %v", err)
	}

	if string(hello) != "Hello world (hello)" {
		t.Errorf("Unexpected response on example.hello: %v", string(hello))
	}

	// Request the endpoint forwarding to templated topic
	res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%v/hello/world", *port))
	if err != nil {
		t.Fatal(err)
	}
	hello, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatalf("Read request body: %v", err)
	}

	if string(hello) != "Hello world (hello.world)" {
		t.Errorf("Unexpected response on example.hello.world: %v", string(hello))
	}
}
