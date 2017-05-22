package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport/mem"
	"github.com/miracl/mrpcproxy"
)

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
		time.Sleep(2 * time.Second)
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

	handler := func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response) {
		w.Header().Set("X-Test-Handler-Header", "OK")
	}

	cases := []struct {
		// Proxy
		topic   string
		timeout time.Duration

		// Proxy logging
		debugger []string
		logger   []string
		requests []string

		// HTTP Request/Response
		reqBody    io.Reader
		reqHeaders map[string][]string
		resStatus  int
		resBody    string
		resHeaders map[string][]string
	}{
		{
			topic:     "a",
			logger:    []string{"/a, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"200 - GET:/a (service.a)"},
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
			logger:   []string{"/a, remote Addr: 2.2.2.2, Id: uuid"},
			requests: []string{"200 - GET:/a (service.a)"},
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
			logger:    []string{"/b, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"408 - GET:/b (service.b)"},
			resStatus: http.StatusRequestTimeout,
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
			},
		},
		{
			topic:     "c",
			logger:    []string{"/c, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"408 - GET:/c (service.c)"},
			resStatus: http.StatusRequestTimeout,
			resHeaders: map[string][]string{
				"X-Test-Handler-Header": {"OK"},
			},
		},
		{
			topic:     "c",
			timeout:   2 * time.Second,
			logger:    []string{"/c, remote Addr: 1.1.1.1, Id: uuid"},
			requests:  []string{"200 - GET:/c (service.c)"},
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
			requests:   []string{"500 - GET:/a (service.a)"},
			reqBody:    &MockReader{err: errors.New("Request body read error")},
			resStatus:  http.StatusInternalServerError,
			resHeaders: map[string][]string{},
		},
		{
			topic:      "e",
			debugger:   []string{"Malformed mrpcproxy Response: invalid character 'M' looking for beginning of value\n"},
			logger:     []string{"/e, remote Addr: 1.1.1.1, Id: uuid"},
			requests:   []string{"500 - GET:/e (service.e)"},
			resStatus:  http.StatusInternalServerError,
			resHeaders: map[string][]string{},
		},
	}

	go service.Serve()
	time.Sleep(100 * time.Millisecond) // Block so service starts

	for i, tc := range cases {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			l := &MockLogger{}
			d := &MockLogger{}
			r := &MockLogger{}

			epHandler := &addEpHandler{
				mrpcService: service,
				timeout:     defaultTimeout,

				getID: func() string { return "uuid" },

				router: httprouter.New(),

				handler: handler,

				debugger: d,
				logger:   l,
				requests: r,
			}

			h := epHandler.getTopicHandler(&mrpcproxy.Endpoint{
				Topic:     fmt.Sprintf("service.%v", tc.topic),
				Method:    "GET",
				Path:      fmt.Sprintf("/%v", tc.topic),
				KeepAlive: tc.timeout,
			})

			req, err := http.NewRequest("GET", fmt.Sprintf("/%v", tc.topic), tc.reqBody)
			if err != nil {
				t.Fatal(err)
			}
			req.RemoteAddr = "1.1.1.1"
			if tc.reqHeaders != nil {
				req.Header = tc.reqHeaders
			}

			rr := httptest.NewRecorder()
			h(rr, req, httprouter.Params{})

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

type MockReader struct {
	p   []byte
	n   int
	err error
}

func (r *MockReader) Read(p []byte) (n int, err error) {
	p = r.p
	return r.n, r.err
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
