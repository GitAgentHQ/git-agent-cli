package graph

import "github.com/google/uuid"

// UUIDSessionIDGenerator implements graph.SessionIDGenerator using google/uuid.
// It keeps the google/uuid dependency in the infrastructure layer so the
// application layer depends only on the domain port.
type UUIDSessionIDGenerator struct{}

// NewUUIDSessionIDGenerator creates a UUIDSessionIDGenerator.
func NewUUIDSessionIDGenerator() *UUIDSessionIDGenerator {
	return &UUIDSessionIDGenerator{}
}

// NewSessionID returns a new random UUID string.
func (UUIDSessionIDGenerator) NewSessionID() string {
	return uuid.New().String()
}
