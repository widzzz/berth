package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func secretName(appName string) string {
	return appName + "-secrets"
}

// EnsureAppKey guarantees that a Kubernetes Secret containing a stable APP_KEY
// exists for the given app before the Deployment is created or rolled.
//
// Behaviour:
//   - If the Secret does not exist → generate a new AES-256 key and create it.
//   - If the Secret already exists → do nothing (key is preserved across deploys).
//
// The returned string is the full Laravel-format key: "base64:<key>"
func EnsureAppKey(ctx context.Context, cs kubernetes.Interface, appName, ns string) (string, error) {
	name := secretName(appName)

	existing, err := cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		key, ok := existing.Data["app-key"]
		if !ok {
			return "", fmt.Errorf("secret %q exists but is missing 'app-key'", name)
		}
		return string(key), nil
	}

	if !k8serrors.IsNotFound(err) {
		return "", fmt.Errorf("check secret %q: %w", name, err)
	}

	appKey, err := generateAppKey()
	if err != nil {
		return "", fmt.Errorf("generate app key: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"app":        appName,
				"managed-by": "laravel-paas",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"app-key": appKey,
		},
	}

	if _, err := cs.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("create secret %q: %w", name, err)
	}

	return appKey, nil
}

// generateAppKey produces a Laravel-compatible APP_KEY.
// Laravel expects a base64-encoded 32-byte (AES-256) random key,
// prefixed with "base64:".
func generateAppKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return "base64:" + base64.StdEncoding.EncodeToString(raw), nil
}
