package api

import (
	appsv1 "k8s.io/api/apps/v1"
)

// firstImage returns the image of the first container in a deployment,
// or an empty string when there are no containers.
func firstImage(d *appsv1.Deployment) string {
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return d.Spec.Template.Spec.Containers[0].Image
}

func deploymentSummary(d *appsv1.Deployment) map[string]any {
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	return map[string]any{
		"name":               d.Name,
		"namespace":          d.Namespace,
		"replicas":           replicas,
		"available_replicas": d.Status.AvailableReplicas,
		"image":              firstImage(d),
	}
}
