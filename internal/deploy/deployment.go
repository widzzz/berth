package deploy

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// BuildDeployment returns a Deployment manifest for a Laravel app.
// It is intentionally simple: one container, port 80, and a rolling update strategy.
func BuildDeployment(appName, image, ns string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
			Labels: map[string]string{
				"app":        appName,
				"managed-by": "laravel-paas",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": appName},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						laravelContainer(appName, image),
					},
				},
			},
		},
	}
}

// laravelContainer builds the main application container, including the
// APP_KEY env var sourced from the app's Kubernetes Secret.
func laravelContainer(appName, image string) corev1.Container {
	return corev1.Container{
		Name:            appName,
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 80, Protocol: corev1.ProtocolTCP},
		},
		Env: []corev1.EnvVar{
			{
				Name: "APP_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: appName + "-secrets",
						},
						Key: "app-key",
					},
				},
			},
		},
	}
}

// RollDeployment updates a deployment's image and adds a restart annotation so
// Kubernetes replaces running pods even when the image tag (":latest") has not changed.
//
// This is called right after a build job is submitted. Because the build may still
// be in progress, the pods will crash-loop until the new image lands in the registry –
// that is expected and acceptable for this simple PaaS design.
func RollDeployment(ctx context.Context, cs kubernetes.Interface, appName, imageTag, ns string) error {
	// Ensure secret exists before rolling (defensive: protects against direct calls to RollDeployment)
	if _, err := EnsureAppKey(ctx, cs, appName, ns); err != nil {
		return fmt.Errorf("ensure app key: %w", err)
	}

	d, err := cs.AppsV1().Deployments(ns).Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("deployment '%s' has no containers", appName)
	}

	d.Spec.Template.Spec.Containers[0].Image = imageTag
	d.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways

	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = make(map[string]string)
	}
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	if _, err := cs.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}
	return nil
}

// DeployApp is the single entry point to call on every push.
// It ensures the APP_KEY secret exists before creating or rolling the Deployment.
func DeployApp(ctx context.Context, cs kubernetes.Interface, appName, imageTag, ns string, replicas int32) error {
	//  Guarantee APP_KEY secret exists before the pod tries to start.
	if _, err := EnsureAppKey(ctx, cs, appName, ns); err != nil {
		return fmt.Errorf("ensure app key: %w", err)
	}

	_, err := cs.AppsV1().Deployments(ns).Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get deployment: %w", err)
		}
		d := BuildDeployment(appName, imageTag, ns, replicas)
		if _, err := cs.AppsV1().Deployments(ns).Create(ctx, d, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create deployment: %w", err)
		}
		return nil
	}

	return RollDeployment(ctx, cs, appName, imageTag, ns)
}
