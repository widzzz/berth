package build

import (
	"flag"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

//
// Registry configuration
//

var registryBase = flag.String(
	"registry-base",
	"registry.kube-system.svc.cluster.local:5000",
	"In-cluster Docker registry",
)

func ImageTag(appName string) string {
	return fmt.Sprintf("%s/%s:latest", *registryBase, appName)
}

//
// Main Job
//

func NewBuildJob(appName, repoURL, commitHash string) *batchv1.Job {
	imageTag := ImageTag(appName)
	jobName := fmt.Sprintf(
		"build-%s-%.8s",
		appName,
		commitHash,
	)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName,
			Labels: map[string]string{
				"app":  appName,
				"role": "build",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(0)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeUnconfined,
						},
					},
					InitContainers: []corev1.Container{
						gitCloneContainer(
							repoURL,
							commitHash,
						),
						injectDockerfileContainer(),
					},
					Containers: []corev1.Container{
						buildkitContainer(
							imageTag,
						),
					},
					Volumes: []corev1.Volume{
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

//
// Git Clone
//

func gitCloneContainer(
	repoURL string,
	commitHash string,
) corev1.Container {
	script := fmt.Sprintf(`
	set -e
	git clone %s /workspace
	cd /workspace
	git checkout %s
	`,
		repoURL,
		commitHash,
	)
	return corev1.Container{
		Name:  "git-clone",
		Image: "alpine/git",
		Command: []string{
			"/bin/sh",
			"-c",
			script,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: "/workspace",
			},
		},
	}
}

//
// Dockerfile Injector
//

func injectDockerfileContainer() corev1.Container {

	script := strings.Join([]string{

		"set -e",

		"if [ ! -f /workspace/Dockerfile ]; then",

		"cat > /workspace/Dockerfile <<'EOF'",

		"FROM serversideup/php:8.4-fpm-nginx",

		"USER root",

		"# 2. Install the MongoDB extension using the built-in helper",
		"RUN install-php-extensions mongodb",

		"# 3. IMPORTANT: Switch back to the unprivileged user for security",
		"USER www-data",

		"WORKDIR /var/www/html",

		"",
		"# Install dependencies (layer caching)",
		"COPY composer.json composer.lock ./",
		"",
		"RUN composer install \\",
		" --no-dev \\",
		" --optimize-autoloader \\",
		" --no-interaction",
		"",
		"# Copy application",
		"COPY --chown=www-data:www-data . .",
		"",
		"# Ensure Laravel directories exist",
		"RUN mkdir -p storage bootstrap/cache",
		"",
		"# Fix permissions",
		"RUN chown -R www-data:www-data storage bootstrap/cache",
		"",
		"RUN chmod -R 775 storage bootstrap/cache",
		"",
		"# Ensure .env exists (APP_KEY injected at runtime)",
		"RUN if [ ! -f .env ]; then cp .env.example .env || true; fi",
		"",
		"# Clear caches (safe without APP_KEY)",
		"RUN php artisan config:clear || true",
		"RUN php artisan route:clear || true",
		"RUN php artisan view:clear || true",
		"",
		"EOF",

		"fi",
	}, "\n")

	return corev1.Container{
		Name:  "inject-dockerfile",
		Image: "alpine",
		Command: []string{
			"/bin/sh",
			"-c",
			script,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: "/workspace",
			},
		},
	}
}

//
// BuildKit container
//

func buildkitContainer(
	imageTag string,
) corev1.Container {
	return corev1.Container{
		Name:  "buildkit",
		Image: "moby/buildkit:rootless",
		Env: []corev1.EnvVar{
			{
				Name:  "BUILDKITD_FLAGS",
				Value: "--oci-worker-no-process-sandbox",
			},
		},
		Command: []string{
			"buildctl-daemonless.sh",
			"build",
			"--frontend",
			"dockerfile.v0",
			"--local",
			"context=/workspace",
			"--local",
			"dockerfile=/workspace",
			"--output",
			"type=image,name=" +
				imageTag +
				",push=true,registry.insecure=true",
		},
		SecurityContext: &corev1.SecurityContext{
			AppArmorProfile: &corev1.AppArmorProfile{
				Type: corev1.AppArmorProfileTypeUnconfined,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: "/workspace",
			},
		},
	}
}
