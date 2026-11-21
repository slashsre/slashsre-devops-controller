// Package nodeevents parses cloud provider node lifecycle notifications.
package nodeevents

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

// ASGTestNotification is sent by AWS Auto Scaling when a lifecycle hook is created or updated.
const ASGTestNotification = "autoscaling:TEST_NOTIFICATION"

// ErrMissingFields is returned when a payload parses but lacks the instance or event type.
var ErrMissingFields = errors.New("event is missing critical fields")

// Event is a normalized node lifecycle event.
type Event struct {
	// InstanceID is the provider instance identifier, matched against node.spec.providerID.
	InstanceID string
	// Type is the provider event type (e.g. "EC2 Spot Instance Interruption Warning").
	Type string
	// Test marks provider test notifications that must be acknowledged and ignored.
	Test bool
}

// Parse decodes body according to format.
func Parse(format v1alpha1.NodeEventFormat, body []byte) (Event, error) {
	switch format {
	case v1alpha1.NodeEventFormatAWSSpotInterruption:
		return parseSpot(body)
	case v1alpha1.NodeEventFormatAWSASGLifecycleHook:
		return parseASGHook(body)
	default:
		return Event{}, fmt.Errorf("unsupported event format %q", format)
	}
}

type spotEvent struct {
	DetailType string `json:"detail-type"`
	Detail     struct {
		InstanceID     string `json:"instance-id"`
		InstanceAction string `json:"instance-action"`
	} `json:"detail"`
}

// parseSpot handles raw EventBridge events and SNS / Lambda ("original_event") wrappers.
func parseSpot(data []byte) (Event, error) {
	raw := data

	var lambda struct {
		OriginalEvent json.RawMessage `json:"original_event"`
	}
	if err := json.Unmarshal(data, &lambda); err == nil && len(lambda.OriginalEvent) > 0 {
		data = lambda.OriginalEvent
	}
	var sns struct {
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(data, &sns); err == nil && sns.Message != "" && json.Valid([]byte(sns.Message)) {
		data = []byte(sns.Message)
	}

	var ev spotEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return Event{}, fmt.Errorf("decoding spot event: %w (raw: %.256s)", err, raw)
	}
	if ev.DetailType == "" || ev.Detail.InstanceID == "" {
		return Event{}, fmt.Errorf("%w: detail-type=%q instance-id=%q", ErrMissingFields, ev.DetailType, ev.Detail.InstanceID)
	}
	return Event{InstanceID: ev.Detail.InstanceID, Type: ev.DetailType}, nil
}

type asgHookEvent struct {
	LifecycleTransition string `json:"LifecycleTransition"`
	EC2InstanceID       string `json:"EC2InstanceId"`
	Event               string `json:"Event"`
}

func parseASGHook(data []byte) (Event, error) {
	var ev asgHookEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return Event{}, fmt.Errorf("decoding lifecycle hook event: %w", err)
	}
	if ev.Event == ASGTestNotification {
		return Event{Test: true, Type: ev.Event}, nil
	}
	if ev.LifecycleTransition == "" || ev.EC2InstanceID == "" {
		return Event{}, fmt.Errorf("%w: transition=%q instance=%q", ErrMissingFields, ev.LifecycleTransition, ev.EC2InstanceID)
	}
	return Event{InstanceID: ev.EC2InstanceID, Type: ev.LifecycleTransition}, nil
}
