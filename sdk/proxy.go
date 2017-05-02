package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport"
	"github.com/miracl/mrpcproxy"
)

const (
	defaultTimeout = 1 * time.Second
)

var (
	defaultDebugger = log.New(os.Stdout, "[DEBUG]", log.LstdFlags|log.LUTC)
	defaultLogger   = log.New(os.Stdout, "[PROXY]", log.LstdFlags|log.LUTC)
	defaultReqeusts = log.New(os.Stdout, "[R]", log.LstdFlags|log.LUTC)

	// ErrNoService is returned when proxy doesn't have service
	ErrNoService = errors.New("service should not be nil")
)

// Proxy is the
type Proxy struct {
	Addr        string
	MRPCService *mrpc.Service
	Timeout     time.Duration

	// Request ID generator
	GetID func() string

	// List of headers that will be added to every response
	Headers map[string]string
	Handler func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response)

	Eps    []Endpoint
	router *httprouter.Router

	Debugger logger
	Logger   logger
	Requests logger
}

type logger interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
}

// FuncOptsError is returned when functional option configuration returns error
type FuncOptsError struct {
	err error
}

func (e FuncOptsError) Error() string {
	return fmt.Sprintf("error executing functional option: %v", e.err)
}

// ResponseError is returned when
type ResponseError struct {
	err error
}

func (e ResponseError) Error() string {
	return fmt.Sprintf("Malformed mrpcproxy Response: %v", e.err)
}

// New creates new Proxy
func New(addr string, s *mrpc.Service, opts ...func(*Proxy) error) (*Proxy, error) {
	if s == nil {
		return nil, ErrNoService
	}
	pxy := &Proxy{
		Addr:        addr,
		MRPCService: s,
		Timeout:     defaultTimeout,

		GetID: func() string { return "" },

		router: httprouter.New(),

		Debugger: defaultDebugger,
		Logger:   defaultLogger,
		Requests: defaultReqeusts,
	}

	for _, opt := range opts {
		if err := opt(pxy); err != nil {
			return nil, FuncOptsError{err}
		}
	}

	return pxy, nil
}

// Handle adds endpoints to the proxy
func (pxy *Proxy) Handle(eps ...Endpoint) {
	pxy.Eps = append(pxy.Eps, eps...)
	for _, ep := range eps {
		pxy.router.Handle(ep.Method, ep.Path, pxy.getTopicHandler(ep))
	}
}

// Serve starts the HTTP server
func (pxy *Proxy) Serve() error {
	pxy.router.NotFound = &notFoundHandler{pxy.Requests}
	pxy.router.Handle("OPTIONS", "/*all", func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		pxy.setHeaders(w)

		// Run custom handler
		if pxy.Handler != nil {
			pxy.Handler(w, r, nil)
		}

		pxy.Requests.Printf("%v - %v:%v", 200, r.Method, r.URL)
	})

	return http.ListenAndServe(pxy.Addr, pxy.router)
}

func (pxy *Proxy) getTopicHandler(ep Endpoint) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		res, err := pxy.mrpcRequest(r, p, ep)
		if err != nil {
			pxy.Debugger.Println(err)
			pxy.Requests.Printf("%v - %v:%v (%v)", http.StatusInternalServerError, r.Method, r.URL, ep.Topic)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Set the headers
		for header, values := range res.Headers {
			for _, v := range values {
				w.Header().Set(header, v)
			}
		}

		pxy.Requests.Printf("%v - %v:%v (%v)", res.Code, r.Method, r.URL, ep.Topic)
		pxy.setHeaders(w)

		// Run custom handler
		if pxy.Handler != nil {
			pxy.Handler(w, r, res)
		}

		w.WriteHeader(res.Code)
		if _, err := w.Write(res.Msg); err != nil {
			pxy.Logger.Printf("writing to http.ResponseWriter failed: %v", err)
		}
	}
}

func (pxy *Proxy) mrpcRequest(r *http.Request, p httprouter.Params, ep Endpoint) (*mrpcproxy.Response, error) {
	req, err := pxy.newRequestFromHTTP(r, p, ep)
	if err != nil {
		return nil, err
	}

	mrpcReq, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	setTimeout := pxy.Timeout
	if ep.KeepAlive > 0 {
		setTimeout = time.Second * time.Duration(ep.KeepAlive)
	}

	pxy.Logger.Printf("%s, remote Addr: %s, Id: %v", r.URL.Path, req.IPAddress, req.RequestID)

	res := &mrpcproxy.Response{}
	resBytes, err := pxy.MRPCService.Request(ep.Topic, mrpcReq, setTimeout)
	if err != nil {
		if err == transport.ErrTimeout {
			res.Code = http.StatusRequestTimeout
			return res, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(resBytes, res); err != nil {
		return nil, ResponseError{err}
	}

	return res, nil
}

func (pxy *Proxy) setHeaders(w http.ResponseWriter) {
	for header, value := range pxy.Headers {
		w.Header().Set(header, value)
	}
}

func (pxy *Proxy) newRequestFromHTTP(r *http.Request, p httprouter.Params, ep Endpoint) (*mrpcproxy.Request, error) {
	req := pxy.newRequest(ep.Topic, ep.Method)

	if r.Body != nil {
		var err error
		req.Msg, err = ioutil.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
	}

	req.Params = mergeRequestParams(r, p)
	req.Headers = r.Header

	req.IPAddress = r.Header.Get("X-Forwarded-For")
	if req.IPAddress == "" {
		req.IPAddress = strings.Split(strings.Split(r.RemoteAddr, ":")[0], "/")[0]
	}

	return req, nil
}

func (pxy *Proxy) newRequest(topic, action string) *mrpcproxy.Request {
	return &mrpcproxy.Request{
		RequestID: pxy.GetID(),
		Timestamp: time.Now().UnixNano(),
		Hops:      1,
		Topic:     topic,
		Action:    action,
	}
}

// mergeRequestParams request with path parameters
func mergeRequestParams(r *http.Request, p httprouter.Params) url.Values {
	params := r.URL.Query()
	for _, param := range p {
		params.Add(param.Key, param.Value)
	}

	return params
}

type notFoundHandler struct {
	Requests logger
}

func (h *notFoundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Requests.Printf("%v - %v:%v", http.StatusNotFound, r.Method, r.URL)
	w.WriteHeader(http.StatusNotFound)
}
