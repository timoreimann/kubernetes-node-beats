package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	informers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

type controller struct {
	nodes       corelisters.NodeLister
	nodesSynced cache.InformerSynced
}

func newController(nodes coreinformers.NodeInformer) *controller {
	c := &controller{
		nodes:       nodes.Lister(),
		nodesSynced: nodes.Informer().HasSynced,
	}

	nodes.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*corev1.Node)
			klog.Infof("[node added] resource version: %s", node.ResourceVersion)
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			oldNode := old.(*corev1.Node)
			newNode := new.(*corev1.Node)
			klog.Infof("[node updated] old resource version: %s\tnew resource version: %s", oldNode.ResourceVersion, newNode.ResourceVersion)
		},
		DeleteFunc: func(obj interface{}) {
			node := obj.(*corev1.Node)
			klog.Infof("[node deleted] resource version: %s", node.ResourceVersion)
		},
	})

	return c
}

func (c *controller) Run(stopCh chan struct{}) error {
	defer runtime.HandleCrash()

	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.nodesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Controller running")
	<-stopCh
	return nil
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	usr, err := user.Current()
	if err != nil {
		klog.Fatalf("Error loading user: %s", err)
	}

	kubeconfig := path.Join(usr.HomeDir, ".kube", "config")

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building clientset: %s", err)
	}

	kubeInformerFactory := informers.NewSharedInformerFactory(clientset, time.Duration(0))

	controller := newController(kubeInformerFactory.Core().V1().Nodes())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	stopCh := make(chan struct{})

	go func() {
		<-c
		close(stopCh)
	}()

	klog.Info("Starting informer")
	kubeInformerFactory.Start(stopCh)

	if err = controller.Run(stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err)
	}
}
