package api

import (
	"encoding/json"
	"net/http"
)

type response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ok(w http.ResponseWriter, status int, message string, data any) {
	writeJSON(w, status, response{Success: true, Message: message, Data: data})
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, response{Success: false, Message: message})
}

func namespace(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}
