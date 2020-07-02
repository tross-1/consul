package agent

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"golang.org/x/sync/errgroup"
)

type apiServers struct {
	logger  hclog.Logger
	group   *errgroup.Group
	servers []apiServer
	// failed channel is closed when the first server goroutines exit with a
	// non-nil error.
	failed <-chan struct{}
}

type apiServer struct {
	// Protocol supported by this server. One of: dns, http, https
	Protocol string
	// Addr the server is listening on
	Addr net.Addr
	// Run will be called in a goroutine to run the server. When any Run exits
	// with a non-nil error, the failed channel will be closed.
	Run func() error
	// Shutdown function used to stop the server
	Shutdown func(context.Context) error
}

func NewAPIServers(logger hclog.Logger) *apiServers {
	group, ctx := errgroup.WithContext(context.TODO())
	return &apiServers{
		logger: logger,
		group:  group,
		failed: ctx.Done(),
	}
}

func (s *apiServers) Start(srv apiServer) {
	srv.logger(s.logger).Info("Starting server")
	s.servers = append(s.servers, srv)
	s.group.Go(srv.Run)
}

func (s apiServer) logger(base hclog.Logger) hclog.Logger {
	return base.With(
		"protocol", s.Protocol,
		"address", s.Addr.String(),
		"network", s.Addr.Network())
}

// Shutdown all the servers and log any errors as warning. Each server is given
// 1 second to shutdown gracefully.
func (s *apiServers) Shutdown(ctx context.Context) {
	shutdownGroup := new(sync.WaitGroup)

	for i := range s.servers {
		server := s.servers[i]
		shutdownGroup.Add(1)

		go func() {
			defer shutdownGroup.Done()
			logger := server.logger(s.logger)
			logger.Info("Stopping server")

			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				logger.Warn("Failed to stop server")
			}
		}()
	}
	s.servers = nil
	shutdownGroup.Wait()
}

// Wait until all server goroutines have exited.
func (s *apiServers) WaitForShutdown() error {
	return s.group.Wait()
}
