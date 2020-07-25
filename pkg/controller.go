package pkg

import (
	"crypto/rand"
	"fmt"
	"github.com/rancher/remotedialer"
	hostmanagerv1 "hostmanager/pkg/apis/hostmanager/v1"
	hostv1 "hostmanager/pkg/apis/hostmanager/v1"
	hostclientset "hostmanager/pkg/generated/clientset/versioned"
	hostscheme "hostmanager/pkg/generated/clientset/versioned/scheme"
	hostinformers "hostmanager/pkg/generated/informers/externalversions"
	hostlisters "hostmanager/pkg/generated/listers/hostmanager/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const controllerAgentName = "host-controller"

const (
	SuccessSynced = "Synced"

	MessageResourceSynced = "host synced successfully"

	HOST_CRD_NAMESPACE = "default"
	MAX_PEER_NUM       = 10
	HOST_CONFIG_PATH   = "./.kube/config"
)

// Controller is the controller implementation for Host resources
type Controller struct {
	// hostclientset is a clientset for our own API group
	hostclientset    hostclientset.Interface
	hostLister       hostlisters.HostLister
	hostSynced       cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	ExitPeerSignal   chan string
	exitSignal       chan struct{}
	LocalHostname    string
	LocalIp          string
	HostToken        string
	rserver          *remotedialer.Server
	rserverServerUrl string
}

// NewController returns a new host controller
func NewController(exitSignal <-chan struct{}, wg *sync.WaitGroup, rserver *remotedialer.Server, serverPort string) *Controller {

	// 处理入参
	cfg, err := clientcmd.BuildConfigFromFlags("", HOST_CONFIG_PATH)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	hostClient, err := hostclientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}

	hostInformerFactory := hostinformers.NewSharedInformerFactoryWithOptions(hostClient, time.Second, hostinformers.WithNamespace(HOST_CRD_NAMESPACE))
	hostinformer := hostInformerFactory.Hostmanager().V1().Hosts()
	utilruntime.Must(hostscheme.AddToScheme(scheme.Scheme))

	controller := &Controller{
		hostclientset:  hostClient,
		hostLister:     hostinformer.Lister(),
		hostSynced:     hostinformer.Informer().HasSynced,
		workqueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Hosts"),
		ExitPeerSignal: make(chan string, MAX_PEER_NUM),
		HostToken:      RandToken(16),
		rserver:        rserver,
	}

	controller.LocalIp = getFirtIP()
	if controller.LocalIp == "" {
		klog.Fatalf("Error cannot get host IP")
	}
	if osname, err := os.Hostname(); err != nil {
		controller.LocalHostname = osname
	} else {
		klog.Error("cannot get hostname")
	}
	controller.rserverServerUrl = fmt.Sprintf("%s%s", controller.LocalIp, serverPort)

	//set remotedialer server peerid and token
	controller.rserver.PeerID = controller.rserverServerUrl
	controller.rserver.PeerToken = controller.HostToken

	klog.Info("Setting up event handlers")
	// Set up an event handler for when Host resources change
	hostinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			host := obj.(*hostmanagerv1.Host)
			if host.Spec.HostAddress != controller.rserverServerUrl {
				if !controller.rserver.HasSession(host.Spec.HostAddress) {
					klog.Infof("host[%s] added. spec:%+v. add peer", host.Name, host.Spec)
					controller.rserver.AddPeer(fmt.Sprintf("ws://%s/connect", host.Spec.HostAddress), host.Spec.HostAddress, host.Spec.HostToken)
				} else {
					klog.Errorf("host[%s] added. spec:%+v session already exist", host.Name, host.Spec)
				}
			}
			//controller.enqueueHost(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			oldHost := old.(*hostmanagerv1.Host)
			newHost := new.(*hostmanagerv1.Host)
			if oldHost.ResourceVersion == newHost.ResourceVersion {
				//版本一致，就表示没有实际更新的操作，立即返回
				return
			} else if oldHost.Spec.HostStatus == hostmanagerv1.UnAvailable && newHost.Spec.HostStatus == hostmanagerv1.Available {
				klog.Infof("host[%s] status from %s to %s", newHost.Name, hostmanagerv1.UnAvailable, hostmanagerv1.Available)
			} else if oldHost.Spec.HostStatus == hostmanagerv1.Available && newHost.Spec.HostStatus == hostmanagerv1.UnAvailable {
				klog.Infof("host[%s] status from %s to %s", newHost.Name, hostmanagerv1.Available, hostmanagerv1.UnAvailable)
			}
			//controller.enqueueHost(new)
		},
		DeleteFunc: func(obj interface{}) {
			host := obj.(*hostmanagerv1.Host)
			klog.Infof("host[%s] deleted. spec:%+v del peer", host.Name, host.Spec)
			controller.rserver.RemovePeer(host.Spec.HostAddress)
			//controller.enqueueHostForDelete(obj)
		},
	})
	hostInformerFactory.Start(exitSignal)
	controller.updateHostStatus(controller.rserverServerUrl, hostv1.Available)

	//wait for peer exit singnal ExitSignal
	go func(exitSignal <-chan struct{}, wg *sync.WaitGroup) {
	EXITSIGNAL:
		for {
			select {
			case <-exitSignal:
				break EXITSIGNAL
			case peerid := <-controller.ExitPeerSignal:
				klog.Infof("ExitSignal set peer:[%s] %s", peerid, hostv1.UnAvailable)
				controller.updateHostStatus(peerid, hostv1.UnAvailable)
				//default:
				//	klog.Infof("2 second pass")
				//	time.Sleep(time.Second * 2)
			}
		}
		klog.Infof("hostmanager signal process ended.")
		controller.deleteHost(strings.Replace(controller.rserverServerUrl, ":", "-", 1))
		wg.Done()
	}(exitSignal, wg)
	return controller
}

//get first ip, not lookback
func getFirtIP() string {
	address := ""
	if addrs, err := net.InterfaceAddrs(); err != nil {
		klog.Errorf("cannot get localip")
	} else {
		for _, add := range addrs {
			if ipnet, ok := add.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				address = ipnet.IP.String()
				break
			}
		}
	}
	return address
}

//generate random token for host
func RandToken(num int) string {
	b := make([]byte, num)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (c *Controller) deleteHost(peerid string) {
	_, err := c.hostclientset.HostmanagerV1().Hosts(HOST_CRD_NAMESPACE).Get(peerid, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("delete peer:[%s] already deleted", peerid)
	} else if err == nil {
		//delete
		if err = c.hostclientset.HostmanagerV1().Hosts(HOST_CRD_NAMESPACE).Delete(peerid, &metav1.DeleteOptions{}); err != nil {
			klog.Errorf("delete peer:[%s] fail:%s", peerid, err.Error())
		} else {
			klog.Infof("delete peer:[%s] success", peerid)
		}
	} else {
		klog.Errorf("get peer:[%s] fail:%s", peerid, err.Error())
	}
}

func (c *Controller) updateHostStatus(peerid, status string) {
	hostcrdname := strings.Replace(peerid, ":", "-", 1)
	host, err := c.hostclientset.HostmanagerV1().Hosts(HOST_CRD_NAMESPACE).Get(strings.Replace(hostcrdname, ":", "-", 1), metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("create peer:[%s] to be %s", hostcrdname, status)
		if _, err := c.hostclientset.HostmanagerV1().Hosts(HOST_CRD_NAMESPACE).Create(&hostv1.Host{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hostcrdname,
				Namespace: HOST_CRD_NAMESPACE,
			},
			Spec: hostv1.HostSpec{
				HostAddress: peerid,
				HostStatus:  status,
				HostToken:   c.HostToken,
				HostInfo:    fmt.Sprintf("OS:[%s],Arch:[%s],CPUS:[%d]", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0)),
			},
		}); err != nil {
			klog.Errorf("create peer:[%s] to be %s fail:%s", hostcrdname, status, err.Error())
		} else {
			klog.Infof("create peer:[%s] to be %s success", hostcrdname, status)
		}
	} else if err == nil {
		//update
		if _, err = c.hostclientset.HostmanagerV1().Hosts(HOST_CRD_NAMESPACE).Update(&hostv1.Host{
			ObjectMeta: metav1.ObjectMeta{
				Name:            hostcrdname,
				Namespace:       HOST_CRD_NAMESPACE,
				ResourceVersion: host.ResourceVersion,
			},
			Spec: hostv1.HostSpec{
				HostAddress: peerid,
				HostStatus:  status,
				HostToken:   c.HostToken,
				HostInfo:    fmt.Sprintf("OS:[%s],Arch:[%s],CPUS:[%d]", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0)),
			},
		}); err != nil {
			klog.Errorf("update peer:[%s] to be %s fail:%s", hostcrdname, status, err.Error())
		} else {
			klog.Infof("update peer:[%s] to be %s success", hostcrdname, status)
		}
	} else {
		klog.Errorf("get peer:[%s] to be %s fail:%s", hostcrdname, status, err.Error())
	}
}

//在此处开始controller的业务
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("开始controller业务，开始一次缓存数据同步")
	if ok := cache.WaitForCacheSync(stopCh, c.hostSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("worker启动")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("worker已经启动")
	//<-stopCh
	//klog.Info("worker已经结束")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// 取数据处理
func (c *Controller) processNextWorkItem() bool {

	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {

			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// 在syncHandler中处理业务
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}

		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// 处理
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// 从缓存中取对象
	host, err := c.hostLister.Hosts(namespace).Get(name)
	if err != nil {
		// 如果Host对象被删除了，就会走到这里，所以应该在这里加入执行
		if errors.IsNotFound(err) {
			klog.Infof("Host对象被删除，请在这里执行实际的删除业务: %s/%s ...", namespace, name)

			return nil
		}

		utilruntime.HandleError(fmt.Errorf("failed to list host by: %s/%s", namespace, name))

		return err
	}

	klog.Infof("这里是host对象的期望状态: %#v ...", host)
	klog.Infof("实际状态是从业务层面得到的，此处应该去的实际状态，与期望状态做对比，并根据差异做出响应(新增或者删除)")

	return nil
}

// 数据先放入缓存，再入队列
func (c *Controller) enqueueHost(obj interface{}) {
	var key string
	var err error
	// 将对象放入缓存
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	// 将key放入队列
	c.workqueue.AddRateLimited(key)
}

// 删除操作
func (c *Controller) enqueueHostForDelete(obj interface{}) {
	var key string
	var err error
	// 从缓存中删除指定对象
	key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	//再将key放入队列
	c.workqueue.AddRateLimited(key)
}
