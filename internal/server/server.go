package server

type Server struct {
}

// Config contains the fields for the server configuration
type Config struct {
	Addr string
	Port int
}

func New(cfg *Config) *Server {
	return &Server{}
}

func (s *Server) Serve() error {
	return nil
}
