package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/miracl/mrpc"
	"github.com/miracl/mrpcproxy"
)

const (
	defaultTimeout = 1 * time.Second
)

var (
	defaultDebugger = log.New(os.Stdout, "[DEBUG]", log.LstdFlags|log.LUTC)
	defaultLogger   = log.New(os.Stdout, "[PROXY]", log.LstdFlags|log.LUTC)
	defaultRequests = log.New(os.Stdout, "[R]", log.LstdFlags|log.LUTC)

	// ErrNoService is returned when proxy doesn't have service.
	ErrNoService = errors.New("service should not be nil")
)

// Proxy is a service proxying messages from HTTP to MRPC.
type Proxy struct {
	http        *http.Server
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

// FuncOptsError is returned when functional option configuration returns error.
type FuncOptsError struct {
	err error
}

func (e FuncOptsError) Error() string {
	return fmt.Sprintf("error executing functional option: %v", e.err)
}

// ResponseError is returned when the prooxy can't return the response.
type ResponseError struct {
	err error
}

func (e ResponseError) Error() string {
	return fmt.Sprintf("Malformed mrpcproxy Response: %v", e.err)
}

// New creates new Proxy.
func New(addr string, s *mrpc.Service, opts ...func(*Proxy) error) (*Proxy, error) {
	if s == nil {
		return nil, ErrNoService
	}
	r := httprouter.New()
	pxy := &Proxy{
		http:        &http.Server{Addr: addr, Handler: r},
		MRPCService: s,
		Timeout:     defaultTimeout,

		GetID: func() string { return "" },

		router: r,

		Debugger: defaultDebugger,
		Logger:   defaultLogger,
		Requests: defaultRequests,
	}

	for _, opt := range opts {
		if err := opt(pxy); err != nil {
			return nil, FuncOptsError{err}
		}
	}

	return pxy, nil
}

// Handle adds endpoints to the proxy.
func (pxy *Proxy) Handle(eps ...Endpoint) error {
	pxy.Eps = append(pxy.Eps, eps...)
	for _, ep := range eps {
		h, err := pxy.getTopicHandler(ep)
		if err != nil {
			return err
		}
		pxy.router.Handle(ep.Method, ep.Path, h)
	}

	return nil
}

// Serve starts the HTTP server.
func (pxy *Proxy) Serve() error {
	pxy.router.NotFound = &notFoundHandler{pxy.Requests}

	for _, ep := range pxy.Eps {
		if ep.Method == "OPTIONS" {
			continue
		}

		h, _, _ := pxy.router.Lookup("OPTIONS", ep.Path)
		if h == nil {
			pxy.router.Handle("OPTIONS", ep.Path, pxy.defaultOptionsHandler)
		}
	}

	return pxy.http.ListenAndServe()
}

// Stop shutdowns the HTTP server
func (pxy *Proxy) Stop(ctx context.Context) error {
	return pxy.http.Shutdown(ctx)
}

func (pxy *Proxy) getTopicHandler(ep Endpoint) (httprouter.Handle, error) {
	topicTmpl, err := template.New("topic").Parse(ep.Topic)
	if err != nil {
		return nil, err
	}

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		var err error
		ep.Topic, err = getTopic(topicTmpl, p)
		if err != nil {
			pxy.Debugger.Println(err)
			pxy.Requests.Printf("%v - %v:%v (%v)", http.StatusInternalServerError, r.Method, r.URL, ep.Topic)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		res, err := pxy.mrpcRequest(r, p, ep)
		if err != nil {
			pxy.Debugger.Println(err)
			pxy.Requests.Printf("%v - %v:%v (%v)", http.StatusInternalServerError, r.Method, r.URL, ep.Topic)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Set default headers
		pxy.setHeaders(w)

		// Set custom response headers
		for header, values := range res.Headers {
			for _, v := range values {
				w.Header().Set(header, v)
			}
		}

		pxy.Requests.Printf("%v - %v:%v (%v)", res.Code, r.Method, r.URL, ep.Topic)

		// Run custom handler
		if pxy.Handler != nil {
			pxy.Handler(w, r, res)
		}

		w.WriteHeader(res.Code)
		if _, err := w.Write(res.Msg); err != nil {
			pxy.Logger.Printf("writing to http.ResponseWriter failed: %v", err)
		}
	}, nil
}

func getTopic(t *template.Template, p httprouter.Params) (string, error) {
	params := map[string]string{}
	for _, p := range p {
		params[p.Key] = p.Value
	}
	var topicBuf bytes.Buffer
	if err := t.Execute(&topicBuf, params); err != nil {
		return "", err
	}
	topic, err := ioutil.ReadAll(&topicBuf)
	if err != nil {
		return "", err
	}
	if err := t.Execute(&topicBuf, params); err != nil {
		return "", err
	}
	return string(topic), nil
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
		setTimeout = time.Duration(ep.KeepAlive) * time.Millisecond
	}

	pxy.Logger.Printf("%s, remote Addr: %s, Id: %v", r.URL.Path, req.IPAddress, req.RequestID)

	res := &mrpcproxy.Response{}
	ctx, cancel := context.WithTimeout(r.Context(), setTimeout)
	defer cancel()
	resBytes, err := pxy.MRPCService.Request(ctx, ep.Topic, mrpcReq)
	if err != nil {
		if err == context.DeadlineExceeded {
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

func (pxy *Proxy) defaultOptionsHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	pxy.setHeaders(w)

	// Run custom handler
	if pxy.Handler != nil {
		pxy.Handler(w, r, nil)
	}

	pxy.Requests.Printf("%v - %v:%v", 200, r.Method, r.URL)
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

// mergeRequestParams request with path parameters.
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
