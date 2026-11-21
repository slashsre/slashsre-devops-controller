package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FluxTrigger matches Flux notification-controller events (generic webhook provider).
// Empty fields match anything.
type FluxTrigger struct {
	// Kind of the involved Flux object (Kustomization, HelmRelease, ...).
	// +optional
	Kind string `json:"kind,omitempty"`
	// Name of the involved Flux object.
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace of the involved Flux object.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Reasons the event must have (for example Progressing, ReconciliationSucceeded).
	// +optional
	Reasons []string `json:"reasons,omitempty"`
	// MessageContains requires the event message to contain this substring
	// (for example "Health check passed").
	// +optional
	MessageContains string `json:"messageContains,omitempty"`
}

// PostDeployTrigger defines what starts the tests.
type PostDeployTrigger struct {
	// Flux notification events.
	// +optional
	Flux *FluxTrigger `json:"flux,omitempty"`
}

// PipelineProviderType identifies a CI system.
// +kubebuilder:validation:Enum=github;bitbucket
type PipelineProviderType string

const (
	// PipelineProviderGitHub triggers a GitHub Actions workflow_dispatch.
	PipelineProviderGitHub PipelineProviderType = "github"
	// PipelineProviderBitbucket triggers a Bitbucket Cloud pipeline.
	PipelineProviderBitbucket PipelineProviderType = "bitbucket"
)

// PipelineAction triggers an external CI pipeline.
type PipelineAction struct {
	// Provider of the CI system.
	Provider PipelineProviderType `json:"provider"`

	// Repository as "<owner>/<repo>" (GitHub) or "<workspace>/<repo>" (Bitbucket).
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`
	Repository string `json:"repository"`

	// Ref is the branch or tag to run on.
	// +kubebuilder:default=main
	// +optional
	Ref string `json:"ref,omitempty"`

	// Pipeline is the GitHub workflow file name/ID (required for github) or the
	// Bitbucket custom pipeline name (optional; the default branch pipeline runs otherwise).
	// +optional
	Pipeline string `json:"pipeline,omitempty"`

	// Variables passed as workflow inputs (GitHub) or pipeline variables (Bitbucket).
	// +optional
	Variables map[string]string `json:"variables,omitempty"`

	// CredentialsSecretRef is a Secret in the same namespace. GitHub uses the key
	// "token"; Bitbucket uses "token" (bearer) or "username" + "password".
	CredentialsSecretRef LocalObjectReference `json:"credentialsSecretRef"`

	// BaseURL overrides the API endpoint (GitHub Enterprise, proxies).
	// +optional
	BaseURL string `json:"baseURL,omitempty"`
}

// PostDeployTestSpec defines tests that run after a deployment.
// +kubebuilder:validation:XValidation:rule="has(self.job) || has(self.pipeline)",message="at least one of job or pipeline must be set"
type PostDeployTestSpec struct {
	// Trigger that starts the tests. Without trigger the tests only run on demand
	// through the API.
	// +optional
	Trigger PostDeployTrigger `json:"trigger,omitempty"`

	// Job to run inside the cluster.
	// +optional
	Job *JobSource `json:"job,omitempty"`

	// Pipeline to trigger in an external CI system.
	// +optional
	Pipeline *PipelineAction `json:"pipeline,omitempty"`

	// Cooldown is the minimum time between two runs.
	// +kubebuilder:default="1m"
	// +optional
	Cooldown metav1.Duration `json:"cooldown,omitempty"`

	// Suspend disables automatic triggers.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// PostDeployTestStatus defines the observed state of PostDeployTest.
type PostDeployTestStatus struct {
	// LastRunTime is the last time the tests were started.
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`
	// LastTrigger describes what started the last run.
	// +optional
	LastTrigger string `json:"lastTrigger,omitempty"`
	// LastJobName is the Job created on the last run.
	// +optional
	LastJobName string `json:"lastJobName,omitempty"`
	// LastPipelineURL links to the pipeline started on the last run.
	// +optional
	LastPipelineURL string `json:"lastPipelineURL,omitempty"`
	// LastError of the last run, if it failed.
	// +optional
	LastError string `json:"lastError,omitempty"`
	// Runs counts started runs.
	// +optional
	Runs int64 `json:"runs,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pdt,categories=slashsre
// +kubebuilder:printcolumn:name="Runs",type=integer,JSONPath=`.status.runs`
// +kubebuilder:printcolumn:name="Last Run",type=date,JSONPath=`.status.lastRunTime`
// +kubebuilder:printcolumn:name="Error",type=string,JSONPath=`.status.lastError`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostDeployTest runs a Job and/or an external pipeline after a deployment.
type PostDeployTest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostDeployTestSpec   `json:"spec,omitempty"`
	Status PostDeployTestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostDeployTestList contains a list of PostDeployTest.
type PostDeployTestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostDeployTest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostDeployTest{}, &PostDeployTestList{})
}
