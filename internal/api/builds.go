package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"my-paas/internal/build"
	"my-paas/internal/deploy"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildRequest is the JSON body for POST /apps/{name}/build.
type buildRequest struct {
	// RepoURL is the git repository to clone and build.
	RepoURL string `json:"repo_url"`
	// CommitHash pins the build to a specific commit (also used to name the Job).
	CommitHash string `json:"commit_hash"`
	// Namespace to run the build Job in (defaults to "default").
	Namespace string `json:"namespace,omitempty"`
}

// @Summary Trigger a new build
// @Description Clone, build Docker image, push, then roll the deployment.
// @Tags builds
// @Accept json
// @Produce json
// @Param name path string true "App name"
// @Param body body buildRequest true "Build details"
// @Success 202 {object} response
// @Failure 400 {object} response
// @Failure 500 {object} response
// @Router /apps/{name}/build [post]
func (s *Server) triggerBuild(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("name")

	var req buildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.RepoURL == "" || req.CommitHash == "" {
		fail(w, http.StatusBadRequest, "repo_url and commit_hash are required")
		return
	}

	ns := namespace(req.Namespace)

	// Create the Kubernetes Job that clones the repo and builds the image.
	job := build.NewBuildJob(appName, req.RepoURL, req.CommitHash)
	createdJob, err := s.clients.Clientset.BatchV1().Jobs(ns).Create(
		r.Context(), job, metav1.CreateOptions{},
	)
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("failed to create build job: %v", err))
		return
	}

	imageTag := build.ImageTag(appName)

	if err := deploy.DeployApp(r.Context(), s.clients.Clientset, appName, imageTag, ns, 1); err != nil {
		// Non-fatal: the build job is already running. Warn instead of hard-fail.
		ok(w, http.StatusAccepted,
			fmt.Sprintf("build job '%s' created, but rolling deployment failed: %v", createdJob.Name, err),
			map[string]any{"job": createdJob.Name, "image": imageTag},
		)
		return
	}

	ok(w, http.StatusAccepted,
		fmt.Sprintf("build job '%s' created; deployment will roll once the image is pushed", createdJob.Name),
		map[string]any{"job": createdJob.Name, "image": imageTag},
	)
}
