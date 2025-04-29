package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lc/void/internal/config"
	"github.com/lc/void/internal/dnsresolver"
	"github.com/lc/void/internal/engine"
	"github.com/lc/void/internal/log"
	"github.com/lc/void/internal/pf"
	"github.com/lc/void/pkg/api"
)

func main() {
	// load config
	cfg, err := config.New().Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// check if user is root:
	if os.Geteuid() != 0 {
		log.Fatal("voidd must run as root")
	}

	// build deps
	res := dnsresolver.New(cfg.Rules.DNSTimeout)
	pfMgr := pf.New()

	ctx, cancel := context.WithCancel(context.Background())
	eng := engine.New(pfMgr, res, cfg.Rules.RefreshInterval)
	eng.Run(ctx)

	// start the api over unix socket
	apiSrv := api.New(eng)
	sockPath := cfg.Socket.Path

	go func() {
		if err := apiSrv.ListenAndServe(sockPath); err != nil {
			log.Fatalf("api listen: %v", err)
		}
	}()

	// graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	<-sig
	log.Info("shutting down…")

	shutdownCtx, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()

	if err := apiSrv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("api shutdown error: %v", err)
	}
	cancel()
	eng.Close()
}
