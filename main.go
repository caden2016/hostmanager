package main

import (
	"flag"
	controller "hostmanager/pkg"
	"io"
	"k8s.io/klog"
	"strconv"
	"sync"
	"time"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
	"hostmanager/pkg/signals"
	"net/http"
)

var (
	serverURL string
	debug     bool
)

func main() {

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
		remotedialer.PrintTunnelData = true
	}

	flag.Parse()
	wg := &sync.WaitGroup{}
	wg.Add(1) //wait for controller to be actually done.
	// 处理信号量
	stopCh := signals.SetupSignalHandler()

	handler := remotedialer.New(authorizer, remotedialer.DefaultErrorWriter)

	//得到controller
	controller := controller.NewController(stopCh, wg, handler, serverURL)

	//controller开始处理消息
	if err := controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}

	//handler.PeerToken = peerToken
	//handler.PeerID = peerID
	//
	//for _, peer := range strings.Split(peers, ",") {
	//	parts := strings.SplitN(strings.TrimSpace(peer), ":", 3)
	//	if len(parts) != 3 {
	//		continue
	//	}
	//	handler.AddPeer(parts[2], parts[0], parts[1])
	//}

	router := mux.NewRouter()
	router.Handle("/connect", handler)
	router.HandleFunc("/client/{id}/{scheme}/{host}{path:.*}", func(rw http.ResponseWriter, req *http.Request) {
		Client(handler, rw, req)
	})

	fmt.Println("Listening on ", serverURL)
	http.ListenAndServe(serverURL, router)
	wg.Wait()
	klog.Infof("main end")
}

func authorizer(req *http.Request) (string, bool, error) {
	id := req.Header.Get("x-tunnel-id")
	return id, id != "", nil
}

func Client(server *remotedialer.Server, rw http.ResponseWriter, req *http.Request) {
	timeout := req.URL.Query().Get("timeout")
	if timeout == "" {
		timeout = "15"
	}

	vars := mux.Vars(req)
	clientKey := vars["id"]
	url := fmt.Sprintf("%s://%s%s", vars["scheme"], vars["host"], vars["path"])
	client := getClient(server, clientKey, timeout)

	klog.Infof("REQ t=%s %s", timeout, url)

	resp, err := client.Get(url)
	if err != nil {
		klog.Errorf("REQ ERR t=%s %s: %v", timeout, url, err)
		remotedialer.DefaultErrorWriter(rw, req, 500, err)
		return
	}
	defer resp.Body.Close()

	klog.Infof(" REQ OK t=%s %s", timeout, url)
	rw.WriteHeader(resp.StatusCode)
	io.Copy(rw, resp.Body)
	klog.Infof("REQ DONE t=%s %s", timeout, url)
}

func getClient(server *remotedialer.Server, clientKey, timeout string) *http.Client {

	dialer := server.Dialer(clientKey, 15*time.Second)
	client := &http.Client{
		Transport: &http.Transport{
			Dial: dialer,
		},
	}
	if timeout != "" {
		t, err := strconv.Atoi(timeout)
		if err == nil {
			client.Timeout = time.Duration(t) * time.Second
		}
	}
	return client
}

func init() {
	flag.StringVar(&serverURL, "serverurl", ":8123", "remotedialer server url")
	flag.BoolVar(&debug, "debug", true, "debug remotedialer server")
}
