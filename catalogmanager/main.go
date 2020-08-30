package main

import (
	controller "catalogmanager/pkg"
	"catalogmanager/pkg/signals"
	"k8s.io/klog"
	"time"
)

func main() {
	// 处理信号量
	stopCh := signals.SetupSignalHandler()

	//得到controller
	if controller, err := controller.NewController(stopCh); err != nil {
		klog.Error(err)
	} else {
		//controller开始处理消息
		if err := controller.Run(); err != nil {
			klog.Fatalf("Error running controller: %s", err.Error())
		}
		klog.Infof("delete syz")
		controller.DeleteCatalog("xyz")
		time.Sleep(time.Second * 5)
		klog.Infof("update syz")
		controller.UpdateCatalog("xyz", "UnAvailable", "aaa", "bbb", "ccc", "ddd", "11111111111")
		time.Sleep(time.Second * 5)
		klog.Infof("update syz")
		controller.UpdateCatalog("xyz", "Available", "111", "", "", "", "55555555555")
	}

	<-stopCh
	klog.Infof("main end")
}