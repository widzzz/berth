package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"my-paas/internal/api"
	"my-paas/internal/db"
	"my-paas/internal/k8s"

	"k8s.io/client-go/util/homedir"
)

func getEnv(key string, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func main() {
	// ── Flags ────────────────────────────────────────────────────
	var kubeconfigPath string
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfigPath, "kubeconfig", filepath.Join(home, ".kube", "config"), "path to kubeconfig")
	} else {
		flag.StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig")
	}
	addr := flag.String("addr", ":8081", "HTTP listen address")

	mongoURI := flag.String(
		"mongo-uri",
		getEnv("MONGO_URI", "mongodb://localhost:27017"),
		"MongoDB connection URI",
	)

	mongoDBName := flag.String(
		"mongo-db",
		getEnv("MONGO_DB", "mypaas"),
		"MongoDB database name",
	)

	flag.Parse()

	// ── MongoDB client ───────────────────────────────────────────
	mongo, err := db.NewMongo(*mongoURI, *mongoDBName)
	if err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongo.Client.Disconnect(ctx); err != nil {
			log.Printf("error disconnecting MongoDB: %v", err)
		}
	}()
	log.Printf("Connected to MongoDB at %s / db=%s", *mongoURI, *mongoDBName)

	// ── Kubernetes clients ───────────────────────────────────────
	clients, err := k8s.NewClients(kubeconfigPath)
	if err != nil {
		log.Fatalf("failed to build k8s clients: %v", err)
	}

	// ── HTTP server setup ────────────────────────────────────────
	serverAPI := api.NewServer(clients, mongo)
	handler := serverAPI.Routes()

	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Start Server in a Goroutine ──────────────────────────────
	go func() {
		log.Printf("Laravel PaaS backend listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server startup error: %v", err)
		}
	}()

	// ── Graceful Shutdown ────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exiting")
}
