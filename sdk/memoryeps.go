package sdk

type memoryEndpoints struct {
	l []Endpoint
}

func (e *memoryEndpoints) Add(eps ...Endpoint) {
	e.l = append(e.l, eps...)
}
