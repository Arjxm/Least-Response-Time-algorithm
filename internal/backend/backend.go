package backend

import (
	"net/url"
)

type Backend struct {
	URL *url.URL
}

func NewBackend(url *url.URL) *Backend {
	return &Backend{URL: url}
}