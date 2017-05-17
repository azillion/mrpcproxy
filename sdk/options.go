package sdk

// WithHeaders sets default headers.
func WithHeaders(h map[string]string) func(pxy *Proxy) error {
	return func(pxy *Proxy) error {
		pxy.SetHeaders(h)
		return nil
	}
}

// WithHandler sets custom handler.
func WithHandler(f HandlerFunc) func(pxy *Proxy) error {
	return func(pxy *Proxy) error {
		pxy.SetHandler(f)
		return nil
	}
}

// WithLoggers sets loggers.
func WithLoggers(d, l, r logger) func(pxy *Proxy) error {
	return func(pxy *Proxy) error {
		pxy.SetLoggers(d, l, r)
		return nil
	}
}

// WithIDGetter sets ID getter function.
func WithIDGetter(f func() string) func(pxy *Proxy) error {
	return func(pxy *Proxy) error {
		pxy.SetGetID(f)
		return nil
	}
}

// WithDynamicEndpoints enables the dynamic endpoint registration.
func WithDynamicEndpoints() func(pxy *Proxy) error {
	return func(pxy *Proxy) error {
		pxy.EnableDynamicEndpoints()
		return nil
	}
}
