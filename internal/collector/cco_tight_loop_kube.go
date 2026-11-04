package collector

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	cloudCredentialGVR = schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "cloudcredentials",
	}
	authenticationGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "authentications",
	}
)

const (
	ccoPodLabelSelector = "app=cloud-credential-operator"
	ccoContainerName    = "cloud-credential-operator"
)

type ccoKubeLister struct {
	dynamicClient dynamic.Interface
	clientset     kubernetes.Interface
}

func (k *ccoKubeLister) GetCredentialsMode(ctx context.Context) (string, error) {
	obj, err := k.dynamicClient.Resource(cloudCredentialGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting CloudCredential/cluster: %w", err)
	}
	return nestedStringFromMap(obj.Object, "spec", "credentialsMode"), nil
}

func (k *ccoKubeLister) GetServiceAccountIssuer(ctx context.Context) (string, error) {
	obj, err := k.dynamicClient.Resource(authenticationGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting Authentication/cluster: %w", err)
	}
	return nestedStringFromMap(obj.Object, "spec", "serviceAccountIssuer"), nil
}

// GetCCOLogs fetches logs from the running Cloud Credential Operator pod,
// scoped to the given window (equivalent to `oc logs --since=<window>`).
func (k *ccoKubeLister) GetCCOLogs(ctx context.Context, window time.Duration) (string, error) {
	pods, err := k.clientset.CoreV1().Pods(ccoNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ccoPodLabelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("listing CCO pods in %s: %w", ccoNamespace, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no CCO pods found in %s matching %q", ccoNamespace, ccoPodLabelSelector)
	}

	podName := pods.Items[0].Name
	for _, p := range pods.Items {
		if p.Status.Phase == "Running" {
			podName = p.Name
			break
		}
	}

	sinceSeconds := int64(window.Seconds())
	req := k.clientset.CoreV1().Pods(ccoNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:    ccoContainerName,
		SinceSeconds: &sinceSeconds,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("streaming logs from pod %s/%s: %w", ccoNamespace, podName, err)
	}
	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("reading logs from pod %s/%s: %w", ccoNamespace, podName, err)
	}
	return string(data), nil
}

func newCCOKubeClientBuilder() ccoClientBuilderFunc {
	return func(kubeconfigPath string) (ccoLister, error) {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("cco-tight-loop: building rest config: %w", err)
		}
		dynClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("cco-tight-loop: creating dynamic client: %w", err)
		}
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("cco-tight-loop: creating typed clientset: %w", err)
		}
		return &ccoKubeLister{dynamicClient: dynClient, clientset: clientset}, nil
	}
}
