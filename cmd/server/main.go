// Command server runs the asset-vault-api HTTP service.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"asset-vault-api/internal/api"
	"asset-vault-api/internal/auth"
	"asset-vault-api/internal/config"
	"asset-vault-api/internal/db"
	"asset-vault-api/internal/storage"
	"asset-vault-api/internal/worker"
)

func main() {
	cfg := config.Load()

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer conn.Close()

	assets := db.NewAssetRepo(conn)
	jobs := db.NewJobRepo(conn)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, localStore, err := buildStorage(ctx, cfg)
	if err != nil {
		log.Fatalf("configuring storage backend: %v", err)
	}

	processor := &worker.ThumbnailProcessor{Storage: store, Assets: assets, Jobs: jobs}
	pool := worker.NewPool(processor)
	pool.Start(ctx, cfg.WorkerCount)
	defer pool.Stop()

	if cfg.JWKSURL == "" {
		log.Fatal("JWKS_URL must be set")
	}
	keySet := auth.NewKeySet(cfg.JWKSURL, cfg.JWKSCacheTTL)

	router := api.NewRouter(api.Deps{
		Assets:        assets,
		Jobs:          jobs,
		Storage:       store,
		Pool:          pool,
		KeySet:        keySet,
		JWTAudience:   cfg.JWTAudience,
		JWTIssuer:     cfg.JWTIssuer,
		TempDir:       os.TempDir(),
		LocalStorage:  localStore,
		PresignExpiry: 15 * time.Minute,
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("asset-vault-api listening on :%s (storage=%s)", cfg.Port, cfg.StorageBackend)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// buildStorage selects the configured storage backend. It returns the
// LocalStorage instance as well when that backend is active, so the API
// router can wire up the signed /download endpoint.
func buildStorage(ctx context.Context, cfg config.Config) (storage.Storage, *storage.LocalStorage, error) {
	switch cfg.StorageBackend {
	case "s3":
		s3store, err := storage.NewS3Storage(ctx, cfg.S3Bucket, cfg.S3Region, cfg.S3Endpoint)
		if err != nil {
			return nil, nil, err
		}
		return s3store, nil, nil
	case "local", "":
		baseURL := "http://localhost:" + cfg.Port
		local, err := storage.NewLocalStorage(cfg.LocalStorageDir, baseURL, cfg.LocalURLSecret)
		if err != nil {
			return nil, nil, err
		}
		return local, local, nil
	default:
		return nil, nil, errors.New("unknown STORAGE_BACKEND: " + cfg.StorageBackend)
	}
}
