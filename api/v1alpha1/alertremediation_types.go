package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemediationAction is the action executed on the pod referenced by a matching alert.
// +kubebuilder:validation:Enum=DeletePod;RolloutRestart
type RemediationAction string

const (
	// ActionDeletePod deletes the pod so its controller recreates it.
	ActionDeletePod RemediationAction = "DeletePod"
	// ActionRolloutRestart restarts the owning Deployment, StatefulSet or DaemonSet.
	ActionRolloutRestart RemediationAction = "RolloutRestart"
)

// AlertRemediationSpec matches Alertmanager alerts and remediates the affected pod.
// Only pods in the same namespace as the AlertRemediation are acted upon.
type AlertRemediationSpec struct {
	// AlertName must equal the alert's "alertname" label.
	// +kubebuilder:validation:MinLength=1
	AlertName string `json:"alertName"`

	// MatchLabels must all be present with equal values in the alert labels
	// (for example container: my-service).
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// Threshold, when set, is compared against the alert annotation named by
	// valueAnnotation. Both values may carry a trailing "%". The action runs when
	// value >= threshold.
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?%?$`
	// +optional
	Threshold string `json:"threshold,omitempty"`

	// ValueAnnotation is the alert annotation holding the measured value.
	// +kubebuilder:default=value
	// +optional
	ValueAnnotation string `json:"valueAnnotation,omitempty"`

	// PodLabel is the alert label holding the pod name.
	// +kubebuilder:default=pod
	// +optional
	PodLabel string `json:"podLabel,omitempty"`

	// Action to execute.
	// +kubebuilder:default=DeletePod
	// +optional
	Action RemediationAction `json:"action,omitempty"`

	// Cooldown is the minimum time between two remediations of the same target.
	// +kubebuilder:default="5m"
	// +optional
	Cooldown metav1.Duration `json:"cooldown,omitempty"`

	// Suspend disables the rule.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// AlertRemediationStatus defines the observed state of AlertRemediation.
type AlertRemediationStatus struct {
	// LastRemediationTime is the last time an action was executed.
	// +optional
	LastRemediationTime *metav1.Time `json:"lastRemediationTime,omitempty"`
	// LastTarget is the object acted upon last (kind/name).
	// +optional
	LastTarget string `json:"lastTarget,omitempty"`
	// LastError is the error of the last failed remediation, if any.
	// +optional
	LastError string `json:"lastError,omitempty"`
	// Remediations counts successful actions.
	// +optional
	Remediations int64 `json:"remediations,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=arem,categories=slashsre
// +kubebuilder:printcolumn:name="Alert",type=string,JSONPath=`.spec.alertName`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Remediations",type=integer,JSONPath=`.status.remediations`
// +kubebuilder:printcolumn:name="Last",type=date,JSONPath=`.status.lastRemediationTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AlertRemediation restarts workloads in response to Alertmanager alerts.
type AlertRemediation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertRemediationSpec   `json:"spec,omitempty"`
	Status AlertRemediationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AlertRemediationList contains a list of AlertRemediation.
type AlertRemediationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertRemediation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertRemediation{}, &AlertRemediationList{})
}
