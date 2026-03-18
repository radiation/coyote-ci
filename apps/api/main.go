package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/internal/build"
)

func main() {
	r := chi.NewRouter()

	buildSvc := build.NewService()

	go buildSvc.RunWorker()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	r.Post("/builds", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		sha := r.URL.Query().Get("sha")
		command := r.URL.Query().Get("command")

		b := buildSvc.CreateBuild(repo, sha, command)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(b)
	})

	r.Get("/builds", func(w http.ResponseWriter, r *http.Request) {
		builds := buildSvc.ListBuilds()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(builds)
	})

	http.ListenAndServe(":8080", r)
}
