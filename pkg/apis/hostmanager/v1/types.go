package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Host struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HostSpec `json:"spec"`
}

type HostSpec struct {
	HostAddress string `json:"hostAddress"`
	HostStatus  string `json:"hostStatus"`
	HostInfo    string `json:"hostInfo"`
	HostToken   string `json:"hostToken"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StudentList is a list of Student resources
type HostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Host `json:"items"`
}

const (
	Available   = "Available"
	UnAvailable = "UnAvailable"
)
