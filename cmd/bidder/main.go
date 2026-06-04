package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/bidder"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/registration"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/server"
)

func main() {
	var register bool
	var configPath string
	flag.BoolVar(&register, "register", false, "register this bidder endpoint with ClearLedger and exit")
	flag.StringVar(&configPath, "config", getenv("BIDDER_CONFIG", "config/campaigns.sample.json"), "campaign config path")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if register {
		if err := registration.Register(context.Background(), cfg); err != nil {
			log.Fatalf("register: %v", err)
		}
		fmt.Println("registered")
		return
	}

	engine := bidder.NewEngine(cfg)
	handler := server.New(cfg, engine)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout(),
		ReadTimeout:       cfg.ReadTimeout(),
		WriteTimeout:      cfg.WriteTimeout(),
		IdleTimeout:       cfg.IdleTimeout(),
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	go func() {
		log.Printf("clearledger bidder listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
