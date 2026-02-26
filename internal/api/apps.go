package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"my-paas/internal/build"
	"my-paas/internal/deploy"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type createAppRequest struct {
	AppName    string `json:"app_name"`            // e.g. "my-laravel-app"
	RepoURL    string `json:"repo_url"`            // e.g. "https://github.com/laravel/laravel.git"
	CommitHash string `json:"commit_hash"`         // e.g. "latest"
	Replicas   int32  `json:"replicas"`            // defaults to 1
	Namespace  string `json:"namespace,omitempty"` // defaults to "default"
}

// @Summary Create a new Laravel app
// @Description Create a new Laravel deployment and trigger initial build.
// @Tags apps
// @Accept json
// @Produce json
// @Param body body createAppRequest true "App details"
// @Success 201 {object} response
// @Failure 400 {object} response
// @Failure 500 {object} response
// @Router /apps [post]
func (s *Server) createApp(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.AppName == "" || req.RepoURL == "" || req.CommitHash == "" {
		fail(w, http.StatusBadRequest, "app_name, repo_url, and commit_hash are required")
		return
	}
	if req.Replicas <= 0 {
		req.Replicas = 1
	}

	ns := namespace(req.Namespace)

	// 1. Determine the image tag this app will use.
	imageTag := build.ImageTag(req.AppName)

	// 2. Persist the app record in MongoDB (status: provisioning).
	col := s.mongo.DB.Collection("apps")
	appDoc := bson.M{
		"_id":        req.AppName, // use app name as natural key
		"repo_url":   req.RepoURL,
		"commit":     req.CommitHash,
		"namespace":  ns,
		"replicas":   req.Replicas,
		"image":      imageTag,
		"status":     "provisioning",
		"created_at": time.Now().UTC(),
		"updated_at": time.Now().UTC(),
	}
	if _, err := col.InsertOne(r.Context(), appDoc); err != nil {
		// If the app already exists (duplicate _id), surface a clear error.
		if mongo.IsDuplicateKeyError(err) {
			fail(w, http.StatusConflict, fmt.Sprintf("app '%s' already exists", req.AppName))
			return
		}
		fail(w, http.StatusInternalServerError, fmt.Sprintf("failed to save app record: %v", err))
		return
	}

	// 3. Deploy the app (ensures APP_KEY secret exists, then creates or rolls the Deployment).
	//    This is the correct way - it ensures the secret is created BEFORE the deployment.
	if err := deploy.DeployApp(r.Context(), s.clients.Clientset, req.AppName, imageTag, ns, req.Replicas); err != nil {
		// Roll back the MongoDB record so the app name is not left as a ghost.
		_, _ = col.DeleteOne(r.Context(), bson.M{"_id": req.AppName})
		fail(w, http.StatusInternalServerError, fmt.Sprintf("failed to deploy app: %v", err))
		return
	}

	// 4. Create the initial Build Job.
	job := build.NewBuildJob(req.AppName, req.RepoURL, req.CommitHash)
	createdJob, err := s.clients.Clientset.BatchV1().Jobs(ns).Create(
		r.Context(), job, metav1.CreateOptions{},
	)
	if err != nil {
		// Non-fatal: deployment exists, but build failed to start — update status.
		_, _ = col.UpdateOne(r.Context(),
			bson.M{"_id": req.AppName},
			bson.M{"$set": bson.M{
				"status":     "build_failed",
				"updated_at": time.Now().UTC(),
			}},
		)
		ok(w, http.StatusAccepted,
			fmt.Sprintf("app '%s' created, but initial build job failed: %v", req.AppName, err),
			map[string]any{"app": req.AppName, "image": imageTag},
		)
		return
	}

	// 5. Update MongoDB with the build job name and running status.
	_, _ = col.UpdateOne(r.Context(),
		bson.M{"_id": req.AppName},
		bson.M{"$set": bson.M{
			"status":     "building",
			"build_job":  createdJob.Name,
			"updated_at": time.Now().UTC(),
		}},
	)

	ok(w, http.StatusCreated,
		fmt.Sprintf("app '%s' created; initial build job '%s' started", req.AppName, createdJob.Name),
		map[string]any{
			"app":   req.AppName,
			"job":   createdJob.Name,
			"image": imageTag,
		},
	)
}

// @Summary List all apps
// @Description List all Laravel deployments in a namespace.
// @Tags apps
// @Produce json
// @Param namespace query string false "Namespace (defaults to 'default')"
// @Success 200 {object} response
// @Failure 500 {object} response
// @Router /apps [get]
func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	ns := namespace(r.URL.Query().Get("namespace"))

	list, err := s.clients.Clientset.AppsV1().Deployments(ns).List(
		r.Context(), metav1.ListOptions{},
	)
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("failed to list deployments: %v", err))
		return
	}

	summaries := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		summaries = append(summaries, deploymentSummary(&list.Items[i]))
	}

	ok(w, http.StatusOK,
		fmt.Sprintf("found %d app(s) in namespace '%s'", len(summaries), ns),
		summaries,
	)
}

// @Summary Get app status
// @Description Get the status of a single app.
// @Tags apps
// @Produce json
// @Param name path string true "App name"
// @Param namespace query string false "Namespace (defaults to 'default')"
// @Success 200 {object} response
// @Failure 404 {object} response
// @Router /apps/{name} [get]
func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ns := namespace(r.URL.Query().Get("namespace"))

	d, err := s.clients.Clientset.AppsV1().Deployments(ns).Get(
		r.Context(), name, metav1.GetOptions{},
	)
	if err != nil {
		fail(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in '%s': %v", name, ns, err))
		return
	}

	ok(w, http.StatusOK, "app found", map[string]any{
		"name":               d.Name,
		"namespace":          d.Namespace,
		"desired_replicas":   d.Spec.Replicas,
		"ready_replicas":     d.Status.ReadyReplicas,
		"available_replicas": d.Status.AvailableReplicas,
		"updated_replicas":   d.Status.UpdatedReplicas,
		"image":              firstImage(d),
		"conditions":         d.Status.Conditions,
	})
}

// @Summary Delete an app
// @Description Delete an app's deployment.
// @Tags apps
// @Param name path string true "App name"
// @Param namespace query string false "Namespace (defaults to 'default')"
// @Success 200 {object} response
// @Failure 500 {object} response
// @Router /apps/{name} [delete]
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ns := namespace(r.URL.Query().Get("namespace"))

	err := s.clients.Clientset.AppsV1().Deployments(ns).Delete(
		r.Context(), name, metav1.DeleteOptions{},
	)
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete app '%s': %v", name, err))
		return
	}

	ok(w, http.StatusOK, fmt.Sprintf("app '%s' deleted from '%s'", name, ns), nil)
}
