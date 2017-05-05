package sdk

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport/mem"
	"github.com/miracl/mrpcproxy"
)

var portFlag = flag.Int("port", 8001, "Port for the example proxy")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestNew(t *testing.T) {
	addr := ":80"
	funcOptsExecuted := false

	// Create the service
	service, _ := mrpc.NewService(mem.New())
	pxy, _ := New(
		addr, service,
		func(pxy *Proxy) error {
			funcOptsExecuted = true
			return nil
		},
	)

	if !funcOptsExecuted {
		t.Errorf("Functional option not executed")
	}

	if pxy.addr != addr || pxy.eps.addHandler.mrpcService != service {
		t.Errorf("Unexpected procxy ")
	}
}

func TestSimpleFuncOptions(t *testing.T) {
	l := &MockLogger{}
	cases := []struct {
		fopt func(p *Proxy) error

		assert func(t *testing.T, p *Proxy, i int)
	}{
		{
			WithIDGetter(func() string { return "uuid" }),
			func(t *testing.T, p *Proxy, i int) {
				if p.eps.addHandler.getID() != "uuid" {
					t.Errorf("Case %v: SetIDGetter is not working correctly", i)
				}
			},
		},
		{
			WithLoggers(l, l, l),
			func(t *testing.T, p *Proxy, i int) {
				p.debugger.Println("p.debugger")
				p.logger.Println("p.logger")
				p.requests.Println("p.requests")

				p.eps.addHandler.debugger.Println("p.eps.addHandler.debugger")
				p.eps.addHandler.logger.Println("p.eps.addHandler.logger")
				p.eps.addHandler.requests.Println("p.eps.addHandler.requests")

				expected := []string{
					"p.debugger\n",
					"p.logger\n",
					"p.requests\n",
					"p.eps.addHandler.debugger\n",
					"p.eps.addHandler.logger\n",
					"p.eps.addHandler.requests\n",
				}
				if strings.Join(l.storage, ",") != strings.Join(expected, ",") {
					t.Errorf("Case %v: SetLoggers is not working correctly", i)
				}
			},
		},
	}

	for i, tc := range cases {
		service, _ := mrpc.NewService(mem.New())
		pxy, _ := New(":80", service, tc.fopt)
		tc.assert(t, pxy, i)
	}
}

func TestNewError(t *testing.T) {
	// Create the service
	service, _ := mrpc.NewService(mem.New())
	_, err := New(
		":80", service,
		func(pxy *Proxy) error {
			return errors.New("some error")
		},
	)

	switch err.(type) {
	case FuncOptsError:
		if err.Error() != "error executing functional option: some error" {
			t.Errorf("Unexpected error: %v", err)
		}
	default:
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewNoServiceErr(t *testing.T) {
	// Create the service
	_, err := New(":80", nil)
	if err != ErrNoService {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewServe(t *testing.T) {
	port := *portFlag

	l := &MockLogger{}

	headerKey, headerVal := "Content-Type", "text/plain; charset=utf-8"
	service, _ := mrpc.NewService(mem.New())
	pxy, _ := New(
		fmt.Sprintf(":%v", port), service,
		WithHeaders(map[string]string{headerKey: headerVal}),
		WithHandler(func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response) {
			w.Header().Set("X-Test-Handler-Header", "OK")
		}),
		WithLoggers(nil, nil, l),
	)
	pxy.Handle(Endpoint{"service.a", "GET", "/a", 0})

	// Simulate application handling mrpc request
	service.HandleFunc("a", func(w mrpc.TopicWriter, data []byte) {
		msg, _ := json.Marshal(&mrpcproxy.Response{Code: 200, Msg: []byte("a response")})
		w.Write(msg)
	})

	// Start the proxy
	go pxy.Serve()

	// Block so the main starts
	time.Sleep(100 * time.Millisecond)

	client := http.DefaultClient

	// Hit A endpoint
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%v/a", port))
	if err != nil {
		t.Fatal(err)
	}
	aRes, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatalf("Read request body: %v", err)
	}

	if string(aRes) != "a response" {
		t.Errorf("Unexpected response: %v", string(aRes))
	}

	if h, ok := res.Header["X-Test-Handler-Header"]; !ok || h[0] != "OK" {
		t.Errorf("Expected 'X-Test-Handler-Header: OK'")
	}

	// Hit options endpoint
	req, err := http.NewRequest("OPTIONS", fmt.Sprintf("http://127.0.0.1:%v/a", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if h, ok := res.Header[headerKey]; !ok || h[0] != headerVal {
		t.Errorf("Missing header in OPTIONS request")
	}

	if h, ok := res.Header["X-Test-Handler-Header"]; !ok || h[0] != "OK" {
		t.Errorf("Expected 'X-Test-Handler-Header: OK'")
	}

	// Hit non existing endpoint
	req, err = http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%v/404", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusNotFound {
		t.Errorf("404 handler returns wrong status code: %v", res.StatusCode)
	}

	fmt.Printf("%#v\n", l.storage)
	if l.storage[len(l.storage)-1] != "404 - GET:/404" {
		t.Errorf("404 not logged")
	}
}

type MockLogger struct {
	storage []string
}

func (l *MockLogger) Println(v ...interface{}) {
	l.storage = append(l.storage, fmt.Sprintln(v...))
}

func (l *MockLogger) Printf(format string, v ...interface{}) {
	l.storage = append(l.storage, fmt.Sprintf(format, v...))
}
