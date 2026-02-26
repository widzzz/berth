package api

import (
	_ "my-paas/docs"
	"my-paas/internal/db"
	"my-paas/internal/k8s"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

type Server struct {
	clients *k8s.Clients
	mongo   *db.Mongo
}

func NewServer(clients *k8s.Clients, mongo *db.Mongo) *Server {
	return &Server{
		clients: clients,
		mongo:   mongo,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	mux.HandleFunc("POST /apps", s.createApp)
	mux.HandleFunc("GET /apps", s.listApps)
	mux.HandleFunc("GET /apps/{name}", s.getApp)
	mux.HandleFunc("DELETE /apps/{name}", s.deleteApp)
	mux.HandleFunc("POST /apps/{name}/build", s.triggerBuild)

	return mux
}
