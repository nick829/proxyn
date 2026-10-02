package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen struct {
		Address string `yaml:"address"`
		Port    uint16 `yaml:"port"`
	} `yaml:"listen"`
	Destination struct {
		Host string `yaml:"host"`
		Port uint16 `yaml:"port"`
	} `yaml:"destination"`
	Timeouts struct {
		ConnectSeconds int `yaml:"connect_seconds"`
		SpawnSeconds   int `yaml:"spawn_seconds"`
	} `yaml:"timeouts"`
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if cfg.Listen.Address == "" {
		return Config{}, errors.New("listen.address is required")
	}
	if cfg.Listen.Port == 0 {
		return Config{}, errors.New("listen.port must be non-zero")
	}
	if cfg.Destination.Host == "" {
		return Config{}, errors.New("destination.host is required")
	}
	if cfg.Destination.Port == 0 {
		return Config{}, errors.New("destination.port must be non-zero")
	}
	if cfg.Timeouts.ConnectSeconds <= 0 {
		cfg.Timeouts.ConnectSeconds = 15
	}
	if cfg.Timeouts.SpawnSeconds <= 0 {
		cfg.Timeouts.SpawnSeconds = 30
	}
	return cfg, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig("config.yml")
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}

	destination := fmt.Sprintf("%s:%d", cfg.Destination.Host, cfg.Destination.Port)
	listenAddress := fmt.Sprintf("%s:%d", cfg.Listen.Address, cfg.Listen.Port)

	statusProvider, err := minecraft.NewForeignStatusProvider(destination)
	if err != nil {
		logger.Error("create destination status provider", "destination", destination, "error", err)
		os.Exit(1)
	}
	defer statusProvider.Close()

	listener, err := (minecraft.ListenConfig{
		StatusProvider: statusProvider,
	}).Listen("raknet", listenAddress)
	if err != nil {
		logger.Error("listen failed", "address", listenAddress, "error", err)
		os.Exit(1)
	}
	defer listener.Close()

	logger.Info("Bedrock proxy listening", "listen", listenAddress, "destination", destination, "gophertunnel", "v1.62.0")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("accept failed", "error", err)
				continue
			}
			mcConn, ok := conn.(*minecraft.Conn)
			if !ok {
				logger.Error("unexpected connection type")
				_ = conn.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleConnection(ctx, mcConn, listener, destination, cfg, logger)
			}()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown requested")
	_ = listener.Close()
	<-acceptDone

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("proxy stopped cleanly")
	case <-time.After(10 * time.Second):
		logger.Warn("shutdown timeout; active connections may still be closing")
	}
}

func handleConnection(
	parent context.Context,
	client *minecraft.Conn,
	listener *minecraft.Listener,
	destination string,
	cfg Config,
	logger *slog.Logger,
) {
	identity := client.IdentityData()
	logger.Info("client connected", "username", identity.DisplayName, "xuid", identity.XUID, "remote", client.RemoteAddr())

	defer func() {
		_ = listener.Disconnect(client, "connection closed")
		_ = client.Close()
		logger.Info("client disconnected", "username", identity.DisplayName)
	}()

	ctx, cancel := context.WithTimeout(parent, time.Duration(cfg.Timeouts.ConnectSeconds)*time.Second)
	defer cancel()

	serverConn, err := (minecraft.Dialer{
		ClientData:   client.ClientData(),
		IdentityData: identity,
	}).DialContext(ctx, "raknet", destination)
	if err != nil {
		logger.Error("destination connection failed", "username", identity.DisplayName, "destination", destination, "error", err)
		_ = listener.Disconnect(client, "Unable to connect to destination server")
		return
	}
	defer serverConn.Close()

	spawnCtx, cancelSpawn := context.WithTimeout(parent, time.Duration(cfg.Timeouts.SpawnSeconds)*time.Second)
	defer cancelSpawn()

	startErr := make(chan error, 1)
	go func() {
		startErr <- client.StartGame(serverConn.GameData())
	}()
	go func() {
		startErr <- serverConn.DoSpawnContext(spawnCtx)
	}()

	for i := 0; i < 2; i++ {
		if err := <-startErr; err != nil {
			logger.Error("login/spawn failed", "username", identity.DisplayName, "error", err)
			_ = listener.Disconnect(client, "Destination login/spawn failed")
			return
		}
	}

	var transferWG sync.WaitGroup
	transferWG.Add(2)

	go func() {
		defer transferWG.Done()
		for {
			pk, err := client.ReadPacket()
			if err != nil {
				return
			}
			if err := serverConn.WritePacket(pk); err != nil {
				logger.Debug("client to destination stopped", "username", identity.DisplayName, "error", err)
				return
			}
		}
	}()

	go func() {
		defer transferWG.Done()
		for {
			pk, err := serverConn.ReadPacket()
			if err != nil {
				return
			}
			if err := client.WritePacket(pk); err != nil {
				logger.Debug("destination to client stopped", "username", identity.DisplayName, "error", err)
				return
			}
		}
	}()

	transferWG.Wait()
}
