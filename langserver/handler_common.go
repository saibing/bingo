package langserver

import (
	"errors"
	"log"
	"sync"
)

// HandlerCommon contains functionality that both the build and lang
// handlers need. They do NOT share the memory of this HandlerCommon
// struct; it is just common functionality. (Unlike HandlerCommon,
// HandlerShared is shared in-memory.)
type HandlerCommon struct {
	mu       sync.Mutex // guards all fields
	shutdown bool
}

// ShutDown marks this server as being shut down and causes all future calls to checkReady to return an error.
func (h *HandlerCommon) ShutDown() {
	h.mu.Lock()
	if h.shutdown {
		log.Printf("Warning: server received a shutdown request after it was already shut down.")
	}
	h.shutdown = true
	h.mu.Unlock()
}

// CheckReady returns an error if the handler has been shut
// down.
func (h *HandlerCommon) CheckReady() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shutdown {
		return errors.New("server is shutting down")
	}
	return nil
}
