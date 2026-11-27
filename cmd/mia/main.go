package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/bot"
	"github.com/demonidze1337/mia-lead-radar/internal/config"
	"github.com/demonidze1337/mia-lead-radar/internal/events"
	"github.com/demonidze1337/mia-lead-radar/internal/filter"
	"github.com/demonidze1337/mia-lead-radar/internal/insights"
	"github.com/demonidze1337/mia-lead-radar/internal/llm"
	"github.com/demonidze1337/mia-lead-radar/internal/server"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
	"github.com/demonidze1337/mia-lead-radar/internal/telegram"
)

func main() {
	if err := run(); err != nil {
		slog.Error("mia: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := sqlite.New(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		return err
	}

	bus := events.NewBus(200)
	defer bus.Close()

	engine := filter.NewEngine()
	settings, err := store.GetSettings(ctx)
	if err != nil {
		return err
	}
	insightService := insights.NewService(store, bus)

	provider, err := llm.NewClient(settings.LLM)
	if err != nil {
		logger.Warn("LLM provider is not configured yet; Setup Wizard can configure it", "error", err)
		provider = nil
	}

	governor := telegram.NewRateGovernor(nil, nil)
	gate := telegram.NewFloodGate(store, 2*time.Second, nil)
	if err := gate.SyncFromStore(ctx); err != nil {
		logger.Warn("FloodGate sync failed", "error", err)
	}

	transports := make([]telegram.AccountTransport, 0)
	var firstMTProto *telegram.MTProtoClient
	var mtprotoClients []*telegram.MTProtoClient
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if cfg.TelegramAppID > 0 && cfg.TelegramAppHash != "" {
		for _, account := range accounts {
			if account.SessionPath == "" {
				continue
			}
			client, err := telegram.NewMTProtoClient(telegram.MTProtoConfig{
				AccountID:   account.ID,
				AppID:       cfg.TelegramAppID,
				AppHash:     cfg.TelegramAppHash,
				SessionsDir: cfg.SessionsDir,
			})
			if err != nil {
				logger.Warn("cannot create MTProto client", "account_id", account.ID, "error", err)
				continue
			}
			startResult := make(chan error, 1)
			go func() { startResult <- client.Start(ctx) }()
			select {
			case err := <-startResult:
				if err != nil {
					logger.Warn("MTProto start failed", "account_id", account.ID, "error", err)
					_ = client.Close()
					continue
				}
			case <-time.After(15 * time.Second):
				logger.Warn("MTProto start timed out", "account_id", account.ID)
				_ = client.Close()
				continue
			}
			mtprotoClients = append(mtprotoClients, client)
			transports = append(transports, client.Transport())
			if firstMTProto == nil {
				firstMTProto = client
			}
		}
	}
	if firstMTProto == nil && cfg.TelegramAppID > 0 && cfg.TelegramAppHash != "" {
		setupAccountID := int64(1)
		if len(accounts) > 0 {
			setupAccountID = accounts[0].ID
		}
		client, err := telegram.NewMTProtoClient(telegram.MTProtoConfig{
			AccountID:   setupAccountID,
			AppID:       cfg.TelegramAppID,
			AppHash:     cfg.TelegramAppHash,
			SessionsDir: cfg.SessionsDir,
		})
		if err == nil {
			startResult := make(chan error, 1)
			go func() { startResult <- client.Start(ctx) }()
			select {
			case err := <-startResult:
				if err != nil {
					logger.Warn("setup MTProto client failed", "error", err)
					_ = client.Close()
				} else {
					firstMTProto = client
					mtprotoClients = append(mtprotoClients, client)
				}
			case <-time.After(15 * time.Second):
				logger.Warn("setup MTProto client start timed out")
				_ = client.Close()
			}
		}
	}
	defer func() {
		for _, client := range mtprotoClients {
			_ = client.Close()
		}
	}()

	pipeline := telegram.NewPipeline(telegram.PipelineDeps{
		Store:      store,
		Bus:        bus,
		Filter:     engine,
		LLM:        provider,
		Insights:   insightService,
		Governor:   governor,
		FloodGate:  gate,
		Transports: transports,
	})
	pipelineDone := make(chan error, 1)
	go func() { pipelineDone <- pipeline.Run(ctx) }()

	var scout *telegram.Scout
	if len(transports) > 0 {
		scout = telegram.NewScout(store, governor, transports[0])
	}
	httpServer, err := server.New(server.Deps{
		Store:     store,
		Bus:       bus,
		Filter:    engine,
		Insights:  insightService,
		LLM:       provider,
		Pipeline:  pipeline,
		Telegram:  firstMTProto,
		Scout:     scout,
		Hardware:  config.DetectHardware(),
		Debug:     cfg.Debug,
		Logger:    logger,
		WebAppURL: cfg.WebAppBaseURL,
	})
	if err != nil {
		return err
	}
	defer httpServer.Close()

	var botDone chan error
	if settings.OwnerBotToken != "" && settings.OwnerTelegramID > 0 {
		ownerBot := bot.New(bot.Config{
			Token:           settings.OwnerBotToken,
			OwnerTelegramID: settings.OwnerTelegramID,
			WebAppBaseURL:   cfg.WebAppBaseURL,
		}, pipeline)
		botDone = make(chan error, 1)
		go func() { botDone <- ownerBot.Start(ctx) }()
	}

	httpServerInstance := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("MIA Lead Radar listening", "addr", cfg.HTTPAddr)
		serveErr <- httpServerInstance.ListenAndServe()
	}()

	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	case err := <-botDone:
		if err != nil {
			logger.Warn("owner bot stopped", "error", err)
		}
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServerInstance.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP shutdown failed", "error", err)
	}
	httpServer.Close()
	select {
	case err := <-pipelineDone:
		if err != nil {
			logger.Warn("pipeline stopped", "error", err)
		}
	case <-shutdownCtx.Done():
		logger.Warn("pipeline shutdown timed out")
	}
	_ = store.Close()
	return result
}
