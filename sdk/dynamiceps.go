package sdk

type dynamicEndpoints struct {
}

func newDynamicEndpoints() (*dynamicEndpoints, <-chan Endpoint) {
	return nil, nil
}

func (e *dynamicEndpoints) Add(eps ...Endpoint) {

}
