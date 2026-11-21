package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QueueTriggeredJobSpec defines a Job launched when a queue accumulates messages.
type QueueTriggeredJobSpec struct {
	// Queue to poll.
	Queue QueueSource `json:"queue"`

	// MinMessages is the approximate queue depth that triggers a Job.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	MinMessages int32 `json:"minMessages,omitempty"`

	// PollInterval between queue depth checks.
	// +kubebuilder:default="30s"
	// +optional
	PollInterval metav1.Duration `json:"pollInterval,omitempty"`

	// MaxActiveJobs caps the number of concurrently running Jobs. Jobs started by the
	// referenced CronJob itself also count towards this limit.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxActiveJobs int32 `json:"maxActiveJobs,omitempty"`

	// Job to create.
	Job JobSource `json:"job"`

	// Suspend stops polling without deleting the resource.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// QueueTriggeredJobStatus defines the observed state of QueueTriggeredJob.
type QueueTriggeredJobStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// LastPollTime is the last time the queue was checked.
	// +optional
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`
	// LastQueueDepth is the approximate number of messages seen on the last poll.
	// +optional
	LastQueueDepth int32 `json:"lastQueueDepth,omitempty"`
	// ActiveJobs is the number of running Jobs on the last poll.
	// +optional
	ActiveJobs int32 `json:"activeJobs,omitempty"`
	// LastTriggerTime is the last time a Job was created.
	// +optional
	LastTriggerTime *metav1.Time `json:"lastTriggerTime,omitempty"`
	// LastJobName is the name of the last Job created.
	// +optional
	LastJobName string `json:"lastJobName,omitempty"`
	// TriggeredJobs counts Jobs created since the resource was created.
	// +optional
	TriggeredJobs int64 `json:"triggeredJobs,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=qtj,categories=slashsre
// +kubebuilder:printcolumn:name="Queue Depth",type=integer,JSONPath=`.status.lastQueueDepth`
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=`.status.activeJobs`
// +kubebuilder:printcolumn:name="Triggered",type=integer,JSONPath=`.status.triggeredJobs`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// QueueTriggeredJob launches Kubernetes Jobs when a message queue has pending work.
type QueueTriggeredJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   QueueTriggeredJobSpec   `json:"spec,omitempty"`
	Status QueueTriggeredJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// QueueTriggeredJobList contains a list of QueueTriggeredJob.
type QueueTriggeredJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []QueueTriggeredJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&QueueTriggeredJob{}, &QueueTriggeredJobList{})
}
