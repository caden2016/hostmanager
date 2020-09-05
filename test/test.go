package main

import (
	"flag"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"k8s.io/klog"
)

var nspath = flag.String("netns", "/var/run/netns/test1", "netns路径")

func main() {
	klog.Infof("test netlink")
	rl, err := netlink.RouteList(nil, nl.FAMILY_V4)
	if err != nil {
		klog.Errorf("err:%v", err)
		return
	}
	for _, v := range rl {
		klog.Infof("Route:%+v", v)
	}
	netns, err := ns.GetNS(*nspath)
	if err != nil {
		klog.Infof("failed to open netns %q: %v", *nspath, err)
		return
	}
	defer netns.Close()

	err = netns.Do(func(hostNS ns.NetNS) error {
		klog.Infof("In netns: %s", *nspath)
		rl, err := netlink.RouteList(nil, nl.FAMILY_V4)
		if err != nil {
			klog.Errorf("err:%v", err)
			return nil
		}
		for _, v := range rl {
			klog.Infof("Route:%+v", v)
		}
		return nil
	})
	if err != nil {
		klog.Errorf("err:%v", err)
		return
	}
}
