package sdk

type memoryEndpoints struct {
	l []Endpoint
	h *addEpHandler
}

func (e *memoryEndpoints) Add(eps ...Endpoint) {
	e.l = append(e.l, eps...)
	for _, ep := range eps {
		e.h.Handle(&ep)
	}
}
