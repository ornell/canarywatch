package main

import (
	"context"
	"flag"
	"fmt"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	kubeconfig    string
	namespace     = "canarywatch"
	checkInterval time.Duration
	limiter       = rate.NewLimiter(rate.Every(10*time.Minute), 1)
)

func main() {
	// Support for running both inside and outside of the cluster.
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig file")
	flag.Parse()
	namespace = os.Getenv("NAMESPACE")
	var config *rest.Config
	var err error
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("Failed to build Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes clientset: %v", err)
	}

	// Load configurations
	if err := loadConfig(clientset, namespace); err != nil {
		log.Fatalf("Failed to load configurations: %v", err)
	}

	if err := setCheckIntervalBasedOnNodeCount(clientset); err != nil {
		log.Printf("Failed to adjust check interval based on node count: %v", err)
	}
	fmt.Println(err)
	if err := reportStartupEvent(clientset, namespace); err != nil {
		log.Printf("Failed to report startup event: %v", err)
		fmt.Println(err)
	}
	fmt.Println(err)
	// Start the HTTP server.
	go startHTTPServer()

	// Main loop for pod communication.
	for {
		// Fetch CanaryWatch pods in the cluster.
		pods, err := getCanaryWatchPods(clientset, namespace)
		if err != nil {
			log.Printf("Failed to retrieve CanaryWatch pods: %v", err)
		} else {
			for _, pod := range pods.Items {
				go communicateWithPod(pod, clientset) // It's good to handle this concurrently.
			}
		}
		time.Sleep(checkInterval)
	}
}

func getCanaryWatchPods(clientset *kubernetes.Clientset, namespace string) (*v1.PodList, error) {
	return clientset.CoreV1().Pods(os.Getenv(namespace)).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=canarywatch",
	})
}

func setCheckIntervalBasedOnNodeCount(clientset *kubernetes.Clientset) error {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	nodeCount := len(nodes.Items)

	if nodeCount <= 15 {
		checkInterval = 1 * time.Second
	} else {
		// You can adjust the interval for clusters with more than 15 nodes here.
		// For now, I'm setting it to every 5 seconds as an example.
		checkInterval = 5 * time.Second
	}
	return nil
}

func communicateWithPod(pod v1.Pod, clientset *kubernetes.Clientset) {
	podIP := pod.Status.PodIP
	podURL := fmt.Sprintf("http://%s:8080/ping", podIP)

	backoff := wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   2,
		Jitter:   0.1,
		Steps:    3, // Total of 3 tries
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		resp, err := http.Get(podURL)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			return false, nil
		}
		resp.Body.Close()
		return true, nil
	})

	if err == wait.ErrWaitTimeout {
		log.Printf("Failed to communicate with pod %s (%s) after retries.", pod.Name, podIP)
		log.Printf("Failed to communicate with pod %s (%s) twice in a row.", pod.Name, podIP)
		createEvent(clientset, pod.Namespace, pod.Name, fmt.Sprintf("CanaryWatch: Failed to communicate with pod %s (%s) twice in a row.", pod.Name, podIP))
	}
}

func createEvent(clientset *kubernetes.Clientset, namespace, involvedObjectName, message string) {
	if !limiter.Allow() {
		return
	}
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: involvedObjectName + "-",
			Namespace:    namespace,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:      "Pod",
			Name:      involvedObjectName,
			Namespace: namespace,
		},
		Reason:  "CommunicationFailure",
		Message: message,
		Type:    "Warning",
		Source: v1.EventSource{
			Component: "canarywatch",
		},
	}

	_, err := clientset.CoreV1().Events(namespace).Create(context.TODO(), event, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Failed to create event: %v", err)
	}
}

func startHTTPServer() {
	http.HandleFunc("/ping", pingHandler)
	http.HandleFunc("/healthz", healthzHandler)

	go http.ListenAndServe(":8080", nil)
}

func pingHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Pong from %s!", r.Host)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Healthy!")
}

func loadConfig(clientset *kubernetes.Clientset, namespace string) error {
	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "canarywatch-config", metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Load and apply checkInterval
	if intervalStr, ok := configMap.Data["checkInterval"]; ok {
		interval, err := strconv.Atoi(intervalStr)
		if err != nil {
			log.Printf("Invalid checkInterval in ConfigMap: %s", intervalStr)
		} else {
			checkInterval = time.Duration(interval) * time.Second
		}
	}

	// Adjust the rate limiter for event creation
	if rateStr, ok := configMap.Data["maxEventRate"]; ok {
		rateDuration, err := strconv.Atoi(rateStr)
		if err != nil {
			log.Printf("Invalid maxEventRate in ConfigMap: %s", rateStr)
		} else {
			limiter.SetLimit(rate.Every(time.Duration(rateDuration) * time.Minute))
		}
	}

	return nil
}

func reportStartupEvent(clientset *kubernetes.Clientset, namespace string) error {
	// Create an event
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "canarywatch-startup-",
			Namespace:    namespace,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:      "Pod",
			Name:      os.Getenv("POD_NAME"),
			Namespace: namespace,
			UID:       types.UID(os.Getenv("POD_UID")),
		},
		Type:    "Normal",
		Reason:  "Startup",
		Message: "CanaryWatch has started",
	}

	_, err := clientset.CoreV1().Events(namespace).Create(context.TODO(), event, metav1.CreateOptions{})
	return err
}
