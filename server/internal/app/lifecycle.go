package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *App) Run(ctx context.Context) error {
	servers := []*http.Server{a.server}
	if a.metricsServer != nil {
		servers = append(servers, a.metricsServer)
	}
	if a.adminServer != nil {
		servers = append(servers, a.adminServer)
	}

	errCh := make(chan error, len(servers))

	for _, server := range servers {
		server := server
		go func() {
			a.logger.Info("listening", "addr", server.Addr, "config", a.configPath)
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}

	select {
	case err := <-errCh:
		_ = a.shutdownServers()
		return err
	case <-ctx.Done():
		return a.shutdownServers()
	}
}

func (a *App) shutdownServers() error {
	timeout := time.Duration(a.config.Server.ShutdownTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if a.voiceRuntime != nil {
		a.voiceRuntime.CloseAll(shutdownCtx)
	}

	var firstErr error
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		firstErr = err
	}
	if a.metricsServer != nil {
		if err := a.metricsServer.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.adminServer != nil {
		if err := a.adminServer.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.traceRecorder != nil {
		if err := a.traceRecorder.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.agentMemory != nil {
		if err := a.agentMemory.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
