package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Catalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CatalogSpec `json:"spec"`
	Status            string      `json:"status"`
}

type CatalogSpec struct {
	Name        string `json:"name"`
	Url         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Description string `json:"description"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StudentList is a list of Student resources
type CatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Catalog `json:"items"`
}

const (
	Available   = "Available"
	UnAvailable = "UnAvailable"
)
