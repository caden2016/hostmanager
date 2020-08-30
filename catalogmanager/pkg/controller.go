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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"time"
)

const controllerAgentName = "host-controller"

const (
	SuccessSynced = "Synced"

	MessageResourceSynced = "host synced successfully"

	CATALOG_CRD_NAMESPACE = "default"
	MAX_PEER_NUM          = 10
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

	catalogInformerFactory := cataloginformers.NewSharedInformerFactoryWithOptions(catalogClient, time.Second, cataloginformers.WithNamespace(CATALOG_CRD_NAMESPACE))
	cataloginformer := catalogInformerFactory.Catalogmanager().V1().Catalogs()
	utilruntime.Must(catalogscheme.AddToScheme(scheme.Scheme))

	controller := &Controller{
		catalogclientset: catalogClient,
		catalogLister:    cataloginformer.Lister(),
		catalogSynced:    cataloginformer.Informer().HasSynced,
		exitSignal:       exitSignal,
		name:             "catalogmanager controller",
	}

	klog.Info("Setting up event handlers")
	// Set up an event handler for when Host resources change
	cataloginformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			catalog := obj.(*catalogmanagerv1.Catalog)
			if catalog.Status == catalogmanagerv1.Available {
				//TODO add repo
				klog.Infof("catalog[%s] added. spec:%+v.", catalog.Name, catalog.Spec)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			oldCatalog := old.(*catalogmanagerv1.Catalog)
			newCatalog := new.(*catalogmanagerv1.Catalog)
			if oldCatalog.ResourceVersion == newCatalog.ResourceVersion {
				//版本一致，就表示没有实际更新的操作，立即返回
				return
			} else if oldCatalog.Status == catalogmanagerv1.UnAvailable && newCatalog.Status == catalogmanagerv1.Available {
				//TODO add repo
				klog.Infof("catalog[%s] status from %s to %s", newCatalog.Name, catalogmanagerv1.UnAvailable, catalogmanagerv1.Available)
			} else if oldCatalog.Status == catalogmanagerv1.Available && newCatalog.Status == catalogmanagerv1.UnAvailable {
				//TODO del repo
				klog.Infof("catalog[%s] status from %s to %s", newCatalog.Name, catalogmanagerv1.Available, catalogmanagerv1.UnAvailable)
			}
			//controller.enqueueHost(new)
		},
		DeleteFunc: func(obj interface{}) {
			//TODO del repo
			catalog := obj.(*catalogmanagerv1.Catalog)
			klog.Infof("catalog[%s] deleted. spec:%+v ", catalog.Name, catalog.Spec)
			//controller.enqueueHostForDelete(obj)
		},
	})
	//catalogInformerFactory.Start(exitSignal)
	go cataloginformer.Informer().Run(exitSignal)
	return controller, nil
}

func (c *Controller) DeleteCatalog(name string) {
	_, err := c.catalogclientset.CatalogmanagerV1().Catalogs(CATALOG_CRD_NAMESPACE).Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("delete catalog:[%s] already deleted", name)
	} else if err == nil {
		//delete
		if err = c.catalogclientset.CatalogmanagerV1().Catalogs(CATALOG_CRD_NAMESPACE).Delete(name, &metav1.DeleteOptions{}); err != nil {
			klog.Errorf("delete catalog:[%s] fail:%s", name, err.Error())
		} else {
			klog.Infof("delete catalog:[%s] success", name)
		}
	} else {
		klog.Errorf("get catalog:[%s] fail:%s", name, err.Error())
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

func (c *Controller) UpdateCatalog(name, status, reponame, reppourl, user, pwd, desp string) {
	catalog, err := c.catalogclientset.CatalogmanagerV1().Catalogs(CATALOG_CRD_NAMESPACE).Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Infof("create catalog:[%s] to be %s", name, status)
		if _, err := c.catalogclientset.CatalogmanagerV1().Catalogs(CATALOG_CRD_NAMESPACE).Create(&catalogv1.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: CATALOG_CRD_NAMESPACE,
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
			klog.Errorf("create catalog:[%s] to be %s fail:%s", name, status, err.Error())
		} else {
			klog.Infof("create catalog:[%s] to be %s success", name, status)
		}
	} else if err == nil {
		//update
		if _, err = c.catalogclientset.CatalogmanagerV1().Catalogs(CATALOG_CRD_NAMESPACE).Update(&catalogv1.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name:            catalog.Name,
				Namespace:       CATALOG_CRD_NAMESPACE,
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
			klog.Errorf("update catalog:[%s] to be %s fail:%s", name, status, err.Error())
		} else {
			klog.Infof("update catalog:[%s] to be %s success", name, status)
		}
	} else {
		klog.Errorf("get catalog:[%s] to be %s fail:%s", name, status, err.Error())
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
