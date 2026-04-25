// Command github-broker is the Phase 2 Slice F GitHub broker — a
// JWT-gated mint surface for GitHub App installation access tokens.
// Worker pods present their Minos-minted JWT (audience=github,
// scope=clone) and exchange it for a per-call installation token
// scoped to the App's installed repo.
//
// Phase 2 Slice F shape: token-mint only. Phase 2 L2 (Momus) adds
// the upstream `github/github-mcp-server` subprocess wrapper for
// MCP-style PR/issue calls; the verifier and supervision layer
// here is the same one that work will plug into.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zakros-hq/zakros/minos/secrets/file"
	"github.com/zakros-hq/zakros/pkg/audit"
	"github.com/zakros-hq/zakros/pkg/brokerauth"
	"github.com/zakros-hq/zakros/pkg/githubapp"
	"github.com/zakros-hq/zakros/pkg/jwt"
)

// Config is the github-broker daemon configuration. Mirrors the Minos
// config shape: secret-provider refs for the GitHub App + signing
// pubkey, plus listen address. Loaded from a JSON file at startup.
type Config struct {
	ListenAddr string `json:"listen_addr"`

	// SecretsFile points at the same secrets.json the Minos VM uses,
	// or a separate file if the operator wants to compartmentalize. The
	// file-backed provider is the only one ready for Slice F; Hecate
	// lands in Phase 2 H1 and replaces this.
	SecretsFile string `json:"secrets_file"`

	// SigningKeyPubRef resolves to the PEM-encoded Ed25519 public key
	// matching Minos's signing private key. Brokers verify pod JWTs
	// with it.
	SigningKeyPubRef string `json:"signing_key_pub_ref"`

	// GitHubAppID and GitHubInstallationID identify the App + the
	// installation on the operator's account. Phase 1 single-project
	// has one App + one installation. Multi-project lands with the
	// project registry in Phase 3.
	GitHubAppID            int64  `json:"github_app_id"`
	GitHubInstallationID   int64  `json:"github_installation_id"`
	GitHubAppPrivateKeyRef string `json:"github_app_private_key_ref"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	configPath := flag.String("config", "/etc/zakros/github-broker.json", "path to config")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	prov, err := file.Open(cfg.SecretsFile)
	if err != nil {
		logger.Error("open secret provider", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pubVal, err := prov.Resolve(ctx, cfg.SigningKeyPubRef)
	if err != nil {
		logger.Error("resolve signing pub key", "error", err)
		os.Exit(1)
	}
	pub, err := jwt.ParsePublicKey(pubVal.Data)
	if err != nil {
		logger.Error("parse signing pub key", "error", err)
		os.Exit(1)
	}

	keyVal, err := prov.Resolve(ctx, cfg.GitHubAppPrivateKeyRef)
	if err != nil {
		logger.Error("resolve github app private key", "error", err)
		os.Exit(1)
	}
	app, err := githubapp.NewClient(cfg.GitHubAppID, keyVal.Data)
	if err != nil {
		logger.Error("github app client", "error", err)
		os.Exit(1)
	}

	em := audit.NewWriterEmitter("github-broker", os.Stdout)
	verifier := &brokerauth.Verifier{
		Broker:    "github",
		PublicKey: pub,
		Replay:    brokerauth.NopReplayStore{},
		Audit:     em,
	}

	srv := &server{
		logger:         logger,
		audit:          em,
		verifier:       verifier,
		github:         app,
		installationID: cfg.GitHubInstallationID,
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("github-broker ready", "listen", cfg.ListenAddr,
		"github_app_id", cfg.GitHubAppID,
		"installation_id", cfg.GitHubInstallationID)

	listenErr := make(chan error, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
		close(listenErr)
	}()

	select {
	case err, ok := <-listenErr:
		if ok {
			logger.Error("listen error", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("shutdown error", "error", err)
	}
	logger.Info("github-broker stopped")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := decodeJSON(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.ListenAddr == "" {
		return nil, errors.New("listen_addr required")
	}
	if c.SecretsFile == "" {
		return nil, errors.New("secrets_file required")
	}
	if c.SigningKeyPubRef == "" {
		return nil, errors.New("signing_key_pub_ref required")
	}
	if c.GitHubAppID == 0 {
		return nil, errors.New("github_app_id required")
	}
	if c.GitHubInstallationID == 0 {
		return nil, errors.New("github_installation_id required")
	}
	if c.GitHubAppPrivateKeyRef == "" {
		return nil, errors.New("github_app_private_key_ref required")
	}
	return &c, nil
}
