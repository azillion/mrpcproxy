package sdk

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/miracl/mrpc"
	"github.com/miracl/mrpc/transport"
	"github.com/miracl/mrpcproxy"
)

// HandlerFunc is a custom handler.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, res *mrpcproxy.Response)

// addEpHandler creates handlers on the fly for given Endpoint.
type addEpHandler struct {
	mrpcService *mrpc.Service
	timeout     time.Duration

	getID func() string

	router *httprouter.Router

	// List of headers that will be added to every response
	headers map[string]string
	handler HandlerFunc

	debugger logger
	logger   logger
	requests logger
}

func (h *addEpHandler) Handle(ep *Endpoint) {
	h.router.Handle(ep.Method, ep.Path, h.getTopicHandler(ep))
}

func (h *addEpHandler) getTopicHandler(ep *Endpoint) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		res, err := h.mrpcRequest(r, p, ep)
		if err != nil {
			h.debugger.Println(err)
			h.requests.Printf("%v - %v:%v (%v)", http.StatusInternalServerError, r.Method, r.URL, ep.Topic)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Set the headers
		for header, values := range res.Headers {
			for _, v := range values {
				w.Header().Set(header, v)
			}
		}

		h.requests.Printf("%v - %v:%v (%v)", res.Code, r.Method, r.URL, ep.Topic)
		setHeaders(w, h.headers)

		// Run custom handler
		if h.handler != nil {
			h.handler(w, r, res)
		}

		w.WriteHeader(res.Code)
		if _, err := w.Write(res.Msg); err != nil {
			h.logger.Printf("writing to http.ResponseWriter failed: %v", err)
		}
	}
}

func (h *addEpHandler) mrpcRequest(r *http.Request, p httprouter.Params, ep *Endpoint) (*mrpcproxy.Response, error) {
	req, err := h.newRequestFromHTTP(r, p, ep)
	if err != nil {
		return nil, err
	}

	mrpcReq, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	setTimeout := h.timeout
	if ep.KeepAlive > 0 {
		setTimeout = time.Second * time.Duration(ep.KeepAlive)
	}

	h.logger.Printf("%s, remote Addr: %s, Id: %v", r.URL.Path, req.IPAddress, req.RequestID)

	res := &mrpcproxy.Response{}
	resBytes, err := h.mrpcService.Request(ep.Topic, mrpcReq, setTimeout)
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

func (h *addEpHandler) newRequestFromHTTP(r *http.Request, p httprouter.Params, ep *Endpoint) (*mrpcproxy.Request, error) {
	req := h.newRequest(ep.Topic, ep.Method)

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

func (h *addEpHandler) newRequest(topic, action string) *mrpcproxy.Request {
	return &mrpcproxy.Request{
		RequestID: h.getID(),
		Timestamp: time.Now().UnixNano(),
		Hops:      1,
		Topic:     topic,
		Action:    action,
	}
}

func mergeRequestParams(r *http.Request, p httprouter.Params) url.Values {
	params := r.URL.Query()
	for _, param := range p {
		params.Add(param.Key, param.Value)
	}

	return params
}

func setHeaders(w http.ResponseWriter, headers map[string]string) {
	for header, value := range headers {
		w.Header().Set(header, value)
	}
}
