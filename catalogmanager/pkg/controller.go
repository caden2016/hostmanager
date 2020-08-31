package pkg

import (
	catalogmanagerv1 "catalogmanager/pkg/apis/catalogmanager/v1"
	catalogv1 "catalogmanager/pkg/apis/catalogmanager/v1"
	catalogclientset "catalogmanager/pkg/generated/clientset/versioned"
	catalogscheme "catalogmanager/pkg/generated/clientset/versioned/scheme"
	cataloginformers "catalogmanager/pkg/generated/informers/externalversions"
	cataloglisters "catalogmanager/pkg/generated/listers/catalogmanager/v1"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"time"
	apicorev1 "k8s.io/api/core/v1"
)

const (
	HOST_CONFIG_PATH      = "./.kube/config"
)

// Controller is the controller implementation for Host resources
type Controller struct {
	// catalogclientset is a clientset for our own API group
	catalogclientset catalogclientset.Interface
	catalogLister    cataloglisters.CatalogLister
	catalogSynced    cache.InformerSynced
	exitSignal       <-chan struct{}
	name             string
	localClient      *kubernetes.Clientset
}

// NewController returns a new host controller
func NewController(exitSignal <-chan struct{}) (*Controller, error) {

	// 处理入参
	cfg, err := clientcmd.BuildConfigFromFlags("", HOST_CONFIG_PATH)
	if err != nil {
		return nil, fmt.Errorf("Error building kubeconfig: %s", err.Error())
	}

	catalogClient, err := catalogclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("Error building catalog clientset: %s", err.Error())
	}

	localClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("Error building local clientset: %s", err.Error())
	}
	catalogInformerFactory := cataloginformers.NewSharedInformerFactory(catalogClient, time.Second)
	cataloginformer := catalogInformerFactory.Catalogmanager().V1().Catalogs()
	utilruntime.Must(catalogscheme.AddToScheme(scheme.Scheme))

	controller := &Controller{
		catalogclientset: catalogClient,
		catalogLister:    cataloginformer.Lister(),
		catalogSynced:    cataloginformer.Informer().HasSynced,
		exitSignal:       exitSignal,
		name:             "catalogmanager controller",
		localClient:localClient,
	}

	klog.Info("Setting up event handlers")
	// Set up an event handler for when Host resources change
	cataloginformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			catalog := obj.(*catalogmanagerv1.Catalog)
			if catalog.Status == catalogmanagerv1.Available {
				//TODO add repo
				klog.Infof("namespace:[%s], catalog[%s] added. spec:%+v.",catalog.Namespace, catalog.Name, catalog.Spec)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			oldCatalog := old.(*catalogmanagerv1.Catalog)
			newCatalog := new.(*catalogmanagerv1.Catalog)
			if oldCatalog.ResourceVersion == newCatalog.ResourceVersion {
				//版本一致，就表示没有实际更新的操作，立即返回
				return
			} else if oldCatalog.Namespace != newCatalog.Namespace  {
				return
			} else if oldCatalog.Status == catalogmanagerv1.UnAvailable && newCatalog.Status == catalogmanagerv1.Available {
				//TODO add repo
				klog.Infof("namespace:[%s], catalog[%s] status from %s to %s",newCatalog.Namespace, newCatalog.Name, catalogmanagerv1.UnAvailable, catalogmanagerv1.Available)
			} else if oldCatalog.Status == catalogmanagerv1.Available && newCatalog.Status == catalogmanagerv1.UnAvailable {
				//TODO del repo
				klog.Infof("namespace:[%s], catalog[%s] status from %s to %s",newCatalog.Namespace, newCatalog.Name, catalogmanagerv1.Available, catalogmanagerv1.UnAvailable)
			}
			//controller.enqueueHost(new)
		},
		DeleteFunc: func(obj interface{}) {
			//TODO del repo
			catalog := obj.(*catalogmanagerv1.Catalog)
			klog.Infof("namespace:[%s],  catalog[%s] deleted. spec:%+v ",catalog.Namespace, catalog.Name, catalog.Spec)
			//controller.enqueueHostForDelete(obj)
		},
	})
	//catalogInformerFactory.Start(exitSignal)
	go cataloginformer.Informer().Run(exitSignal)
	return controller, nil
}

//create ns named cluster-id to manager cluster catalog
func (c *Controller) CreateNamespace(name string) {
	_, err := c.localClient.CoreV1().Namespaces().Create(&apicorev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		}})
	if err != nil {
		klog.Errorf("cannot create ns: %s, err:%s", name, err)
	} else {
		klog.Infof("create ns: %s", name)
	}
}

func (c *Controller) DeleteCatalog(namespace, name string) {
	_, err := c.catalogclientset.CatalogmanagerV1().Catalogs(namespace).Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("namespace:%s, delete catalog:[%s] already deleted",namespace, name)
	} else if err == nil {
		//delete
		if err = c.catalogclientset.CatalogmanagerV1().Catalogs(namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
			klog.Errorf("namespace:%s, delete catalog:[%s] fail:%s",namespace, name, err.Error())
		} else {
			klog.Infof("namespace:%s, delete catalog:[%s] success",namespace, name)
		}
	} else {
		klog.Errorf("namespace:%s, get catalog:[%s] fail:%s",namespace, name, err.Error())
	}
}

func (c *Controller) GetNoEmptyValues(catalog *catalogv1.Catalog, type_value, value string) string {
	if value != "" {
		return value
	}
	switch type_value {
	case "Name":
		return catalog.Spec.Name
	case "Url":
		return catalog.Spec.Url
	case "Username":
		return catalog.Spec.Username
	case "Password":
		return catalog.Spec.Password
	case "Description":
		return catalog.Spec.Description
	case "Status":
		return catalog.Status
	}
	return value
}

func (c *Controller) UpdateCatalog(namespace, name, status, reponame, reppourl, user, pwd, desp string) {
	catalog, err := c.catalogclientset.CatalogmanagerV1().Catalogs(namespace).Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("namespace:%s, create catalog:[%s] to be %s",namespace, name, status)
		if _, err := c.catalogclientset.CatalogmanagerV1().Catalogs(namespace).Create(&catalogv1.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: catalogv1.CatalogSpec{
				Name:        reponame,
				Url:         reppourl,
				Username:    user,
				Password:    pwd,
				Description: desp,
			},
			Status: status,
		}); err != nil {
			klog.Errorf("namespace:%s, create catalog:[%s] to be %s fail:%s",namespace, name, status, err.Error())
		} else {
			klog.Infof("namespace:%s, create catalog:[%s] to be %s success",namespace, name, status)
		}
	} else if err == nil {
		//update
		if _, err = c.catalogclientset.CatalogmanagerV1().Catalogs(namespace).Update(&catalogv1.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:            catalog.Name,
				Namespace:       namespace,
				ResourceVersion: catalog.ResourceVersion,
			},
			Spec: catalogv1.CatalogSpec{
				Name:        c.GetNoEmptyValues(catalog, "Name", reponame),
				Url:         c.GetNoEmptyValues(catalog, "Url", reppourl),
				Username:    c.GetNoEmptyValues(catalog, "Username", user),
				Password:    c.GetNoEmptyValues(catalog, "Password", pwd),
				Description: c.GetNoEmptyValues(catalog, "Description", desp),
			},
			Status: c.GetNoEmptyValues(catalog, "Status", status),
		}); err != nil {
			klog.Errorf("namespace:%s, update catalog:[%s] to be %s fail:%s",namespace, name, status, err.Error())
		} else {
			klog.Infof("namespace:%s, update catalog:[%s] to be %s success",namespace, name, status)
		}
	} else {
		klog.Errorf("namespace:%s, get catalog:[%s] to be %s fail:%s",namespace, name, status, err.Error())
	}
}

//在此处开始controller的业务
func (c *Controller) Run() error {
	defer utilruntime.HandleCrash()

	if ok := cache.WaitForNamedCacheSync(c.name, c.exitSignal, c.catalogSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}
