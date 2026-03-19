package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/internal/build"
)

type createBuildRequest struct {
	Repo      string           `json:"repo"`
	CommitSHA string           `json:"commit_sha"`
	Steps     []build.StepSpec `json:"steps"`
}

func main() {
	r := chi.NewRouter()

	buildSvc := build.NewService()

	go buildSvc.RunWorker()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	r.Post("/builds", func(w http.ResponseWriter, r *http.Request) {
		var req createBuildRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		if req.Repo == "" {
			http.Error(w, "repo is required", http.StatusBadRequest)
			return
		}

		if req.CommitSHA == "" {
			http.Error(w, "commit_sha is required", http.StatusBadRequest)
			return
		}

		if len(req.Steps) == 0 {
			http.Error(w, "at least one step is required", http.StatusBadRequest)
			return
		}

		for _, step := range req.Steps {
			if step.Name == "" || step.Command == "" {
				http.Error(w, "each step must have name and command", http.StatusBadRequest)
				return
			}
		}

		b := buildSvc.CreateBuild(req.Repo, req.CommitSHA, req.Steps)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(b)
	})

	r.Get("/builds", func(w http.ResponseWriter, r *http.Request) {
		builds := buildSvc.ListBuilds()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(builds)
	})

	r.Get("/builds/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		builds := buildSvc.ListBuilds()
		for _, b := range builds {
			if b.ID == id {
				json.NewEncoder(w).Encode(b)
				return
			}
		}
	})

	r.Get("/builds/{id}/steps", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		builds := buildSvc.ListBuilds()
		for _, b := range builds {
			if b.ID == id {
				json.NewEncoder(w).Encode(buildSvc.ListSteps(id))
				return
			}
		}
	})

	http.ListenAndServe(":8080", r)
}
