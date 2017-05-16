package sdk

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/miracl/mrpc"
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

// Proxy is a server that proxies http to mrpc requests.
type Proxy struct {
	addr         string
	addEpHandler addEpHandler

	router *httprouter.Router
	eps    endpoints
	epsCh  <-chan Endpoint

	debugger logger
	logger   logger
	requests logger
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
		addr: addr,
		addEpHandler: addEpHandler{
			mrpcService: s,
			timeout:     defaultTimeout,

			getID: func() string { return "" },

			router: r,

			debugger: defaultDebugger,
			logger:   defaultLogger,
			requests: defaultRequests,
		},

		router: r,
		eps:    &memoryEndpoints{},

		debugger: defaultDebugger,
		logger:   defaultLogger,
		requests: defaultRequests,
	}

	for _, opt := range opts {
		if err := opt(pxy); err != nil {
			return nil, FuncOptsError{err}
		}
	}

	return pxy, nil
}

type endpoints interface {
	Add(eps ...Endpoint)
}

// Handle adds endpoints to the proxy.
func (pxy *Proxy) Handle(eps ...Endpoint) {
	pxy.eps.Add(eps...)
	for _, ep := range eps {
		pxy.addEpHandler.Handle(&ep)
	}
}

// Serve starts the HTTP server.
func (pxy *Proxy) Serve() error {
	pxy.router.NotFound = &notFoundHandler{pxy.requests}
	pxy.router.Handle("OPTIONS", "/*all", func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		setHeaders(w, pxy.addEpHandler.headers)

		// Run custom handler
		if pxy.addEpHandler.handler != nil {
			pxy.addEpHandler.handler(w, r, nil)
		}

		pxy.requests.Printf("%v - %v:%v", 200, r.Method, r.URL)
	})

	return http.ListenAndServe(pxy.addr, pxy.router)
}

// WithHeaders is a functional option to set default headers.
func WithHeaders(headers map[string]string) func(p *Proxy) error {
	return func(p *Proxy) error {
		p.addEpHandler.headers = headers
		return nil
	}
}

// WithHandler is a functional option to set custom handler.
func WithHandler(f HandlerFunc) func(p *Proxy) error {
	return func(p *Proxy) error {
		p.addEpHandler.handler = f
		return nil
	}
}

// WithLoggers is a functional option to set loggers.
func WithLoggers(d, l, r logger) func(p *Proxy) error {
	return func(p *Proxy) error {
		if d != nil {
			p.debugger = d
			p.addEpHandler.debugger = d
		}

		if l != nil {
			p.logger = l
			p.addEpHandler.logger = l
		}

		if r != nil {
			p.requests = r
			p.addEpHandler.requests = r
		}

		return nil
	}
}

// WithIDGetter is a functional option to set loggers.
func WithIDGetter(f func() string) func(p *Proxy) error {
	return func(p *Proxy) error {
		p.addEpHandler.getID = f
		return nil
	}
}

type notFoundHandler struct {
	Requests logger
}

func (h *notFoundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Requests.Printf("%v - %v:%v", http.StatusNotFound, r.Method, r.URL)
	w.WriteHeader(http.StatusNotFound)
}
