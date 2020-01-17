package main

import (
	"encoding/json"
	"flag"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"path/filepath"
	"time"
)

var clientset *kubernetes.Clientset

// PodMetricsList : apis/metrics.k8s.io json struct
type PodMetricsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			SelfLink          string    `json:"selfLink"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Window     string    `json:"window"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// PodsInfo defines pod information struct
type PodsInfo struct {
	Name      string
	UID       types.UID
	Status    v1.PodPhase
	Timestamp time.Time
}

// InactivePodsThresholdCnt defines the maximum size of InactivePods, do cleanup if exceeded
const InactivePodsThresholdCnt = 1000

// InactivePodsThresholdTime defines the maximum recent time to keep in InactivePods when cleanup
const InactivePodsThresholdTime = 24 * time.Hour

// LogInterval defines the interval of main routine
const LogInterval = 15 * time.Second

// ActivePods stores active pods we are looking after
var ActivePods []PodsInfo

// InactivePods stores inactive pods which will never be looked after
var InactivePods []PodsInfo

// ActivePodsSet is the set of ActivePods.Name
var ActivePodsSet sets.String

// InactivePodsSet is the set of InactivePods.Name
var InactivePodsSet sets.String

// LastPodMetricsTime keeps the newest pod metrics by timestamp
var LastPodMetricsTime map[string]time.Time

func logPodInfo(name string) {
	//podRaw, err := clientset.RESTClient().Get().Namespace("default").Resource("pod").Name(name).DoRaw()
	podRaw, err := clientset.RESTClient().Get().AbsPath("api/v1/namespaces/default/pods/" + name).DoRaw()
	if err != nil {
		log.Fatal("cannot get pod " + name + err.Error())
		return
	}
	log.Print(string(podRaw))
}

func cleanup() {
	if len(InactivePods) > InactivePodsThresholdCnt {
		pos := -1
		for i, m := range InactivePods {
			if m.Timestamp.After(time.Now().Add(-InactivePodsThresholdTime)) {
				pos = i
				break
			}
		}
		if pos == -1 {
			log.Fatal("InactivePodsThresholdCnt and InactivePodsThresholdTime mismatch")
			return
		}
		InactivePods = InactivePods[pos+1:]
		InactivePodsSet = make(sets.String)
		for _, m := range InactivePods {
			InactivePodsSet.Insert(m.Name)
		}
	}
}

func worker() {

	cleanup()

	// clear map HasMetricsPodsSet
	HasMetricsPodsSet := make(sets.String)
	data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/namespaces/default/pods").DoRaw()
	if err != nil {
		log.Fatal(err.Error())
		return
	}
	var pods PodMetricsList
	err = json.Unmarshal(data, &pods)
	for _, m := range pods.Items {
		HasMetricsPodsSet.Insert(m.Metadata.Name)
		if v, ok := LastPodMetricsTime[m.Metadata.Name]; !ok || m.Timestamp.After(v) {
			LastPodMetricsTime[m.Metadata.Name] = m.Timestamp
			t, _ := json.Marshal(m)
			log.Println(string(t))
		}
	}

	podList, err := clientset.CoreV1().Pods("default").List(metav1.ListOptions{})
	if err != nil {
		log.Fatal(err.Error())
		return
	}
	for _, pod := range podList.Items {
		// ignore inactive pods
		if InactivePodsSet.Has(pod.Name) {
			continue
		}
		// first shown pods
		// add to active pods set
		// log pods info
		if !ActivePodsSet.Has(pod.Name) {
			ActivePodsSet.Insert(pod.Name)
			currPodInfo := PodsInfo{
				Name:      pod.Name,
				UID:       pod.UID,
				Status:    pod.Status.Phase,
				Timestamp: time.Now().Truncate(0),
			}
			ActivePods = append(ActivePods, currPodInfo)
			logPodInfo(currPodInfo.Name)
		} else {
			// update and log when status change
			// find pod in ActivePods
			flag := false
			for i, currPodInfo := range ActivePods {
				if currPodInfo.Name == pod.Name {
					flag = true
					// check status change
					if currPodInfo.Status != pod.Status.Phase {
						// update status and timestamp
						ActivePods[i].Status = pod.Status.Phase
						ActivePods[i].Timestamp = time.Now().Truncate(0)
						logPodInfo(currPodInfo.Name)
					}
					break
				}
			}
			// should never happen
			if !flag {
				log.Fatal("ActivePods mismatch with ActivePodsSet")
				return
			}

			// add pod into InactivePods if there's no metrics of it
			// only consider "completed" pods
			if !HasMetricsPodsSet.Has(pod.Name) {
				if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
					ActivePodsSet.Delete(pod.Name)
					InactivePodsSet.Insert(pod.Name)
					currPodInfo := PodsInfo{
						Name:      pod.Name,
						UID:       pod.UID,
						Status:    pod.Status.Phase,
						Timestamp: time.Now().Truncate(0),
					}
					InactivePods = append(InactivePods, currPodInfo)
					pos := -1
					for i, currPodInfo := range ActivePods {
						if currPodInfo.Name == pod.Name {
							pos = i
							break
						}
					}
					if pos != -1 {
						ActivePods = append(ActivePods[:pos], ActivePods[pos+1:]...)
					} else {
						log.Fatal("ActivePods mismatch with ActivePodsSet when deleting")
						return
					}
				}
			}
		}
	}
}

func buildConfig(master, kubeconfig string) (*rest.Config, error) {
	if master != "" || kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags(master, kubeconfig)
	}
	return rest.InClusterConfig()
}

func main() {
	var kubeconfig *string
	var logdir *string
	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	logdir = flag.String("logdir", "/log", "absolute path to log dir")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime)
	logf, err := rotatelogs.New(
		filepath.Join(*logdir, "PodLifecycle_log.%Y%m%d%H%M"),
		rotatelogs.WithLinkName(filepath.Join(*logdir, "PodLifecycle_log")),
		rotatelogs.WithRotationTime(24*time.Hour))
	if err != nil {
		log.Printf("failed to create rotatelogs: %s", err)
		panic("can't write log to " + *logdir)
	}

	config, err := buildConfig("", *kubeconfig)
	if err != nil {
		log.Fatal(err.Error())
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	ActivePodsSet = make(sets.String)
	InactivePodsSet = make(sets.String)
	LastPodMetricsTime = make(map[string]time.Time)

	log.Print("write log to " + *logdir)

	log.SetOutput(logf)

	wait.Forever(worker, LogInterval)

}
