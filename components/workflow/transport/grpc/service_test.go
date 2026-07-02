package grpc

import (
	"testing"

	googlegrpc "google.golang.org/grpc"
)

func TestRegisterWorkflowService(t *testing.T) {
	server := googlegrpc.NewServer()
	RegisterWorkflowService(server, nil)
	server.Stop()
}
