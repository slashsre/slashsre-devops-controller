package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
)

// Common condition types and reasons shared by all kinds.
const (
	// ConditionReady indicates the resource is configured correctly and being processed.
	ConditionReady = "Ready"

	ReasonReconciled    = "Reconciled"
	ReasonSuspended     = "Suspended"
	ReasonProviderError = "ProviderError"
	ReasonInvalidSpec   = "InvalidSpec"
	ReasonJobError      = "JobError"

	// LabelOwnerKind and LabelOwnerName are set on every object the controller creates.
	LabelOwnerKind = "slashsre.controller.io/owner-kind"
	LabelOwnerName = "slashsre.controller.io/owner-name"
	// LabelManagedBy marks objects created by this controller.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// ManagedByValue is the value used for LabelManagedBy.
	ManagedByValue = "slashsre-devops-controller"
)

// QueueProviderType identifies a message queue implementation.
// +kubebuilder:validation:Enum=aws-sqs
type QueueProviderType string

const (
	// QueueProviderAWSSQS is Amazon Simple Queue Service.
	QueueProviderAWSSQS QueueProviderType = "aws-sqs"
)

// QueueSource describes a message queue polled by the controller.
type QueueSource struct {
	// Provider is the queue implementation.
	// +kubebuilder:default=aws-sqs
	// +optional
	Provider QueueProviderType `json:"provider,omitempty"`

	// URL of the queue. For aws-sqs this is the queue URL
	// (https://sqs.<region>.amazonaws.com/<account>/<name>).
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// Region overrides the provider region. For aws-sqs it is derived from the
	// queue URL, falling back to the ambient AWS configuration.
	// +optional
	Region string `json:"region,omitempty"`
}

// LocalObjectReference references an object in the same namespace.
type LocalObjectReference struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// JobSource defines how a Kubernetes Job is built.
// Exactly one of cronJobRef or template must be set.
// +kubebuilder:validation:XValidation:rule="has(self.cronJobRef) != has(self.template)",message="exactly one of cronJobRef or template must be set"
type JobSource struct {
	// CronJobRef copies the job template of an existing CronJob in the same namespace.
	// +optional
	CronJobRef *LocalObjectReference `json:"cronJobRef,omitempty"`

	// Template is an inline Job template.
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Template *batchv1.JobTemplateSpec `json:"template,omitempty"`

	// TTLSecondsAfterFinished is applied to created Jobs so they are garbage collected.
	// +kubebuilder:default=3600
	// +kubebuilder:validation:Minimum=0
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}
