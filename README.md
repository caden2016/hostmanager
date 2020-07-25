# hostmanager
## auto manage rancher/remotedialer peers with the help of k8s crd.
## which will auto add and remove peer according to the change of crd when program is up or down.

hostmanager$ kubectl  create -f crd/hostcrd.yml

//kube/config file set to be in ./.kube/config
hostmanager$ ./hostmanager  -h
Usage of ./hostmanager:
  -debug
      debug remotedialer server (default true)
  -serverurl string
      remotedialer server url (default ":8123")

shell1 //8123 will auto detect  to connect with peer 8080
hostmanager$ ./hostmanager

shell2
hostmanager$ ./hostmanager -serverurl :8080

shell3 //client connect to 8123
$ ./client/client

shell4 // set request to 8080, which will pass to 8123 then to clinet the to outside
hostmanager$ curl http://10.0.2.15:8080/client/foo/http/baidu.com

//view crd create and delete with hostmanager start and shutdown
//crd namespace default to be default.
hostmanager$ kubectl  get hosts.hostmanager.crc.com
NAME             AGE
10.0.2.15-8080   3m52s
10.0.2.15-8123   7m7s

hostmanager$ kubectl  get hosts.hostmanager.crc.com 10.0.2.15-8123  -oyaml
apiVersion: hostmanager.crc.com/v1
kind: Host
metadata:
  creationTimestamp: "2020-07-25T07:30:45Z"
  generation: 1
  managedFields:
  - apiVersion: hostmanager.crc.com/v1
    fieldsType: FieldsV1
    fieldsV1:
      f:spec:
        .: {}
        f:hostAddress: {}
        f:hostInfo: {}
        f:hostStatus: {}
        f:hostToken: {}
    manager: hostmanager
    operation: Update
    time: "2020-07-25T07:30:45Z"
  name: 10.0.2.15-8123
  namespace: default
  resourceVersion: "9620"
  selfLink: /apis/hostmanager.crc.com/v1/namespaces/default/hosts/10.0.2.15-8123
  uid: 1593c4cb-dd52-4110-bf3d-3308ec0b8bf1
spec:
  hostAddress: 10.0.2.15:8123
  hostInfo: OS:[linux],Arch:[amd64],CPUS:[2]
  hostStatus: Available
  hostToken: b038f54222367fa2a53470f1b08b0d09


