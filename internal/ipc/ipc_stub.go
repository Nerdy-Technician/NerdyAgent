//go:build !linux

package ipc

import "fmt"

type Server struct {
	token string
}

func (s *Server) Path() string         { return "" }
func (s *Server) Token() string        { return "" }
func (s *Server) SetHandler(h Handler) {}
func (s *Server) Close()               {}

func Listen(token string) (*Server, error) {
	return &Server{token: token}, nil
}

func Call(socket, token string, req Request) (Response, error) {
	return Response{OK: false, Error: "ipc not supported"}, fmt.Errorf("ipc not supported on this platform")
}
