package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeEventFormat identifies the payload format of node lifecycle events.
// +kubebuilder:validation:Enum=aws-spot-interruption;aws-asg-lifecycle-hook
type NodeEventFormat string

const (
	// NodeEventFormatAWSSpotInterruption is the EventBridge "EC2 Spot Instance Interruption Warning"
	// event, optionally wrapped by SNS or a Lambda ("original_event").
	NodeEventFormatAWSSpotInterruption NodeEventFormat = "aws-spot-interruption"
	// NodeEventFormatAWSASGLifecycleHook is the Auto Scaling lifecycle hook notification.
	NodeEventFormatAWSASGLifecycleHook NodeEventFormat = "aws-asg-lifecycle-hook"
)

// DrainOptions tunes how nodes are drained.
type DrainOptions struct {
	// Timeout for the whole drain operation.
	// +kubebuilder:default="120s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// GracePeriodSeconds for evicted pods. -1 uses each pod's own value.
	// +kubebuilder:default=-1
	// +optional
	GracePeriodSeconds *int32 `json:"gracePeriodSeconds,omitempty"`

	// Force deletes pods not managed by a controller. Defaults to true.
	// +optional
	Force *bool `json:"force,omitempty"`

	// IgnoreDaemonSets skips DaemonSet-managed pods. Defaults to true.
	// +optional
	IgnoreDaemonSets *bool `json:"ignoreDaemonSets,omitempty"`

	// DeleteEmptyDirData allows deleting pods using emptyDir volumes. Defaults to true.
	// +optional
	DeleteEmptyDirData *bool `json:"deleteEmptyDirData,omitempty"`

	// DisableEviction deletes pods directly, bypassing PodDisruptionBudgets.
	// +optional
	DisableEviction bool `json:"disableEviction,omitempty"`
}

// NodeLifecycleRuleSpec defines which events drain which nodes.
type NodeLifecycleRuleSpec struct {
	// Source queue that receives the node lifecycle events.
	Source QueueSource `json:"source"`

	// EventFormat of the messages in the queue.
	EventFormat NodeEventFormat `json:"eventFormat"`

	// PollInterval between queue reads.
	// +kubebuilder:default="5s"
	// +optional
	PollInterval metav1.Duration `json:"pollInterval,omitempty"`

	// NodeSelector restricts the nodes this rule may drain.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// Drain options.
	// +optional
	Drain DrainOptions `json:"drain,omitempty"`

	// Suspend stops processing events without deleting the resource.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// NodeLifecycleRuleStatus defines the observed state of NodeLifecycleRule.
type NodeLifecycleRuleStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// LastPollTime is the last time the queue was read.
	// +optional
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`
	// LastEventTime is the last time an actionable event was received.
	// +optional
	LastEventTime *metav1.Time `json:"lastEventTime,omitempty"`
	// LastDrainedNode is the name of the last node drained.
	// +optional
	LastDrainedNode string `json:"lastDrainedNode,omitempty"`
	// DrainedNodes counts successful drains.
	// +optional
	DrainedNodes int64 `json:"drainedNodes,omitempty"`
	// FailedDrains counts failed drains.
	// +optional
	FailedDrains int64 `json:"failedDrains,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nlr,categories=slashsre
// +kubebuilder:printcolumn:name="Format",type=string,JSONPath=`.spec.eventFormat`
// +kubebuilder:printcolumn:name="Drained",type=integer,JSONPath=`.status.drainedNodes`
// +kubebuilder:printcolumn:name="Last Node",type=string,JSONPath=`.status.lastDrainedNode`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeLifecycleRule cordons and drains nodes when the cloud provider announces they will go away.
type NodeLifecycleRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeLifecycleRuleSpec   `json:"spec,omitempty"`
	Status NodeLifecycleRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeLifecycleRuleList contains a list of NodeLifecycleRule.
type NodeLifecycleRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeLifecycleRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeLifecycleRule{}, &NodeLifecycleRuleList{})
}
