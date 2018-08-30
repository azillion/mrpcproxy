package sdk

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"context"

	"github.com/julienschmidt/httprouter"
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

	if pxy.http.Addr != addr || pxy.MRPCService != service {
		t.Errorf("Unexpected proxy")
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

	headerKey, headerVal := "X-Test-Generic-Header", "OK"
	// Create the service
	service, _ := mrpc.NewService(mem.New())
	pxy, _ := New(
		fmt.Sprintf(":%v", port), service,
		func(pxy *Proxy) error {
			pxy.Headers = map[string]string{headerKey: headerVal}
			pxy.Handler = func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response) {
				w.Header().Set("X-Test-Handler-Header", "OK")
			}
			pxy.Requests = l
			return nil
		},
	)
	pxy.Handle(Endpoint{
		Topic:     "service.a",
		Method:    "GET",
		Path:      "/a",
		KeepAlive: 0,
	})

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

	if l.storage[len(l.storage)-1] != "404 - GET:/404" {
		t.Errorf("404 not logged")
	}

	pxy.Stop(context.Background())
	req, _ = http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%v/404", port), nil)
	res, _ = client.Do(req)
	if res != nil {
		t.Error("Proxy still working")
	}
}

func TestGetTopicHandler(t *testing.T) {
	service, _ := mrpc.NewService(mem.New())
	service.HandleFunc("a", func(w mrpc.TopicWriter, data []byte) {
		req := &mrpcproxy.Request{}
		json.Unmarshal(data, req)

		msg, _ := json.Marshal(&mrpcproxy.Response{
			Code: 200,
			Msg:  []byte("OK"),
			Headers: http.Header{
				"X-Test-Header": []string{"OK"},
				"X-Test-Ip":     []string{req.IPAddress},
			},
		})

		w.Write(msg)
	})
	service.HandleFunc("b", func(w mrpc.TopicWriter, data []byte) {})
	service.HandleFunc("c", func(w mrpc.TopicWriter, data []byte) {
		time.Sleep(10 * time.Millisecond)
		msg, _ := json.Marshal(&mrpcproxy.Response{
			Code:    200,
			Msg:     []byte("OK"),
			Headers: http.Header{"X-Test-Header": []string{"OK"}},
		})

		w.Write(msg)
	})
	service.HandleFunc("e", func(w mrpc.TopicWriter, data []byte) {
		w.Write([]byte("MRPC response that is not mrpcproxy.Response formatted"))
	})
	service.HandleFunc("w.1", func(w mrpc.TopicWriter, data []byte) {
		msg, _ := json.Marshal(&mrpcproxy.Response{Code: 200, Msg: []byte("w.1")})
		w.Write(msg)
	})
	service.HandleFunc("w.2", func(w mrpc.TopicWriter, data []byte) {
		msg, _ := json.Marshal(&mrpcproxy.Response{Code: 200, Msg: []byte("w.2")})
		w.Write(msg)
	})

	handler := func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response) {
		w.Header().Set("X-Test-Handler-Header", "OK")
	}

	cases := []struct {
		// Proxy
		topic   string
		pattern string // defaults to topic
		timeout int

		// Proxy logging
		debugger []string
		logger   []string
		requests []string

		// HTTP Request/Response
		reqURL     string // defaults to topic
		reqBody    io.Reader
		reqParams  httprouter.Params
		reqHeaders map[string][]string
		resStatus  int
		resBody    string
		resHeaders map[string][]string
	}{
		{
			topic:     "a",
			logger:    []string{"GET:/a, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"GET:/a, status: 200, topic: service.a, Id: uuid"},
			resStatus: http.StatusOK,
			resBody:   "OK",
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
				"X-Test-Header":         {"OK"},
				"X-Test-Ip":             {"1.1.1.1"},
			},
		},
		{
			topic:    "a",
			logger:   []string{"GET:/a, remote Addr: 2.2.2.2, Id: uuid"},
			requests: []string{"GET:/a, status: 200, topic: service.a, Id: uuid"},
			reqHeaders: map[string][]string{
				"X-Forwarded-For": {"2.2.2.2"},
			},
			resStatus: http.StatusOK,
			resBody:   "OK",
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
				"X-Test-Header":         {"OK"},
				"X-Test-Ip":             {"2.2.2.2"},
			},
		},
		{
			topic:     "b",
			timeout:   1,
			logger:    []string{"GET:/b, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"GET:/b, status: 408, topic: service.b, Id: uuid"},
			resStatus: http.StatusRequestTimeout,
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
			},
		},
		{
			topic:     "c",
			timeout:   1,
			logger:    []string{"GET:/c, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"GET:/c, status: 408, topic: service.c, Id: uuid"},
			resStatus: http.StatusRequestTimeout,
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
			},
		},
		{
			topic:     "c",
			timeout:   20,
			logger:    []string{"GET:/c, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"GET:/c, status: 200, topic: service.c, Id: uuid"},
			resStatus: http.StatusOK,
			resBody:   "OK",
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
				"X-Test-Header":         {"OK"},
			},
		},
		{
			topic:      "a",
			debugger:   []string{"Request body read error\n"},
			requests:   []string{"GET:/a, status: 500, topic: service.a"},
			reqBody:    &MockReader{err: errors.New("Request body read error")},
			resStatus:  http.StatusInternalServerError,
			resHeaders: map[string][]string{},
		},
		{
			topic:      "e",
			debugger:   []string{"Malformed mrpcproxy Response: invalid character 'M' looking for beginning of value\n"},
			logger:     []string{"GET:/e, remote Addr: 1.1.1.1, Id: uuid"},
			requests:   []string{"GET:/e, status: 500, topic: service.e"},
			resStatus:  http.StatusInternalServerError,
			resHeaders: map[string][]string{},
		},
		{
			topic:      "w.{{.id}}",
			pattern:    "/w/:id",
			logger:     []string{"GET:/w/1, remote Addr: 1.1.1.1, Id: uuid"},
			requests:   []string{"GET:/w/1, status: 200, topic: service.w.1, Id: uuid"},
			reqURL:     "/w/1",
			resStatus:  http.StatusOK,
			resBody:    "w.1",
			reqParams:  httprouter.Params{{Key: "id", Value: "1"}},
			resHeaders: map[string][]string{"X-Test-Handler-Header": {"OK"}},
		},
		{
			topic:      "w.{{.id}}",
			pattern:    "/w/:id",
			logger:     []string{"GET:/w/2, remote Addr: 1.1.1.1, Id: uuid"},
			requests:   []string{"GET:/w/2, status: 200, topic: service.w.2, Id: uuid"},
			reqURL:     "/w/2",
			resStatus:  http.StatusOK,
			resBody:    "w.2",
			reqParams:  httprouter.Params{{Key: "id", Value: "2"}},
			resHeaders: map[string][]string{"X-Test-Handler-Header": {"OK"}},
		},
	}

	go service.Serve()
	defer service.Stop(nil)

	time.Sleep(1 * time.Millisecond) // Block so service starts

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			pxy, _ := New(":80", service)

			pxy.Handler = handler

			pxy.GetID = func() string { return "uuid" }
			l := &MockLogger{}
			pxy.Logger = l
			d := &MockLogger{}
			pxy.Debugger = d
			r := &MockLogger{}
			pxy.Requests = r

			var pattern string
			if tc.pattern != "" {
				pattern = tc.pattern
			} else {
				pattern = fmt.Sprintf("/%v", tc.topic)
			}

			h, err := pxy.getTopicHandler(Endpoint{
				Topic:     fmt.Sprintf("service.%v", tc.topic),
				Method:    "GET",
				Path:      pattern,
				KeepAlive: tc.timeout,
			})
			if err != nil {
				t.Fatal(err)
			}

			var reqURL string
			if tc.reqURL != "" {
				reqURL = tc.reqURL
			} else {
				reqURL = fmt.Sprintf("/%v", tc.topic)
			}
			req, err := http.NewRequest("GET", reqURL, tc.reqBody)
			if err != nil {
				t.Fatal(err)
			}
			req.RemoteAddr = "1.1.1.1"
			if tc.reqHeaders != nil {
				req.Header = tc.reqHeaders
			}

			rr := httptest.NewRecorder()
			h(rr, req, tc.reqParams)

			// Check the status code is what we expect.
			if status := rr.Code; status != tc.resStatus {
				t.Errorf("Case %v: handler returned wrong status code: got %v want %v", i, status, tc.resStatus)
			}

			// Check the response body is what we expect.
			if rr.Body.String() != tc.resBody {
				t.Errorf("Case %v: handler returned unexpected body: got %v want %v", i, rr.Body.String(), tc.resBody)
			}

			if len(rr.HeaderMap) != len(tc.resHeaders) {
				t.Errorf("Case %v: unexpected response headers length: got %v want %v", i, rr.HeaderMap, tc.resHeaders)
			}

			for h, vs := range tc.resHeaders {
				if !reflect.DeepEqual(rr.HeaderMap[h], vs) {
					t.Errorf("Case %v: unexpected response headers: got %v want %v", i, rr.HeaderMap, tc.resHeaders)
				}
			}

			// Check logging
			loggers := map[string]struct {
				l *MockLogger
				e []string
			}{
				"logger":   {l, tc.logger},
				"debugger": {d, tc.debugger},
				"requests": {r, tc.requests},
			}
			for typ, logger := range loggers {

				if len(logger.l.storage) != len(logger.e) {
					t.Errorf("Case %v: %v unexpected logs count: got %v:%v want %v:%v", i, typ, len(logger.l.storage), logger.l.storage, len(logger.e), logger.e)
					continue
				}

				for logNum := 0; logNum < len(logger.l.storage); logNum++ {
					if logNum >= len(logger.e) || logger.l.storage[logNum] != logger.e[logNum] {
						t.Errorf("Case %v: %v unexpected logs: got %v want %v", i, typ, logger.l.storage, logger.e)
					}
				}
			}
		})
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

func TestMergeRequestParams(t *testing.T) {
	cases := []struct {
		method string
		url    string
		p      httprouter.Params

		v url.Values
	}{
		{
			// GET request with path and get parameters
			"GET",
			"http://ip:port/test/1/?b=2&c=3",
			httprouter.Params{
				httprouter.Param{Key: "a", Value: "1"},
			},

			url.Values{
				"a": []string{"1"},
				"b": []string{"2"},
				"c": []string{"3"},
			},
		},
		{
			// GET request with path and get parameters that overlaps
			"GET",
			"http://ip:port/test/1/?b=2&a=3",
			httprouter.Params{
				httprouter.Param{Key: "a", Value: "1"},
			},

			url.Values{
				"a": []string{"3", "1"},
				"b": []string{"2"},
			},
		},
		{
			// GET request with path and get parameters that overlaps
			"POST",
			"http://ip:port/test/1/?b=2&a=3",
			httprouter.Params{
				httprouter.Param{Key: "a", Value: "1"},
			},

			url.Values{
				"a": []string{"3", "1"},
				"b": []string{"2"},
			},
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			r, _ := http.NewRequest(tc.method, tc.url, nil)
			v := mergeRequestParams(r, tc.p)

			if !reflect.DeepEqual(v, tc.v) {
				t.Errorf("Params don't match\nExpected\n%#v\nReceived\n%#v", tc.v, v)
			}
		})
	}
}

func TestCustomOptionsHandler(t *testing.T) {
	port := *portFlag

	// Create the service
	service, _ := mrpc.NewService(mem.New())
	pxy, _ := New(fmt.Sprintf(":%v", port), service)

	pxy.Handle(
		Endpoint{
			Path:      "/a",
			Method:    "GET",
			Topic:     "service.a",
			KeepAlive: 0,
		},
		Endpoint{
			Path:      "/a",
			Method:    "OPTIONS",
			Topic:     "service.a",
			KeepAlive: 0,
		},
		Endpoint{
			Path:      "/b",
			Method:    "GET",
			Topic:     "service.b",
			KeepAlive: 0,
		},
	)

	service.HandleFunc("a", func(w mrpc.TopicWriter, data []byte) {
		req := &mrpcproxy.Request{}
		json.Unmarshal(data, req)

		var msg []byte
		switch req.Action {
		case "GET":
			msg, _ = json.Marshal(&mrpcproxy.Response{
				Code: 200,
				Msg:  []byte("a response"),
			})
		case "OPTIONS":
			msg, _ = json.Marshal(&mrpcproxy.Response{
				Code: 200,
				Headers: http.Header{
					"X-Test-Handler-Header": []string{"OPTIONS"},
				},
			})
		}

		w.Write(msg)
	})

	// Start the proxy
	go pxy.Serve()
	defer pxy.Stop(context.Background())

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

	// Request OPTIONS for the endpoint with custom handler
	req, err := http.NewRequest("OPTIONS", fmt.Sprintf("http://127.0.0.1:%v/a", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if h, ok := res.Header["X-Test-Handler-Header"]; !ok || h[0] != "OPTIONS" {
		t.Errorf("Expected 'X-Test-Handler-Header: OPTIONS'")
	}

	// Request OPTIONS for the endpoint with default handler
	req, err = http.NewRequest("OPTIONS", fmt.Sprintf("http://127.0.0.1:%v/b", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
}

type MockReader struct {
	p   []byte
	n   int
	err error
}

func (r *MockReader) Read(p []byte) (n int, err error) {
	p = r.p
	return r.n, r.err
}
