package clients

import "fmt"

// ServiceUnavailableError is returned when a downstream gRPC service is not reachable.
type ServiceUnavailableError struct {
	Service string
}

func (e *ServiceUnavailableError) Error() string {
	return fmt.Sprintf("%s service unavailable", e.Service)
}

func errUnavailable(service string) error {
	return &ServiceUnavailableError{Service: service}
}
