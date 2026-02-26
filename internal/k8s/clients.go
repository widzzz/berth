package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Clients struct {
	Clientset *kubernetes.Clientset
}

func NewClients(kubeconfigPath string) (*Clients, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Clients{
		Clientset: clientset,
	}, nil
}
