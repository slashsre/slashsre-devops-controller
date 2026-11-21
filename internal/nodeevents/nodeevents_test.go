package nodeevents

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

const spotRaw = `{"version":"0","id":"1","detail-type":"EC2 Spot Instance Interruption Warning","source":"aws.ec2","account":"123456789012","region":"eu-central-1","resources":["arn:aws:ec2:eu-central-1a:instance/i-0abc"],"detail":{"instance-id":"i-0abc","instance-action":"terminate"}}`

func TestParseSpot(t *testing.T) {
	snsMsg, _ := json.Marshal(map[string]string{"Type": "Notification", "Message": spotRaw})
	lambda := `{"original_event":` + spotRaw + `}`

	for name, body := range map[string]string{"raw": spotRaw, "sns": string(snsMsg), "lambda": lambda} {
		ev, err := Parse(v1alpha1.NodeEventFormatAWSSpotInterruption, []byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ev.InstanceID != "i-0abc" || ev.Type != "EC2 Spot Instance Interruption Warning" || ev.Test {
			t.Errorf("%s: unexpected event %+v", name, ev)
		}
	}
}

func TestParseSpotMissingFields(t *testing.T) {
	_, err := Parse(v1alpha1.NodeEventFormatAWSSpotInterruption, []byte(`{"detail-type":"x"}`))
	if !errors.Is(err, ErrMissingFields) {
		t.Fatalf("expected ErrMissingFields, got %v", err)
	}
}

func TestParseASGHook(t *testing.T) {
	body := `{"Origin":"AutoScalingGroup","LifecycleHookName":"drain","Destination":"EC2","AccountId":"123456789012","RequestId":"r-1","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"example-asg","Service":"AWS Auto Scaling","Time":"2026-01-01T00:00:00.000Z","EC2InstanceId":"i-0def","LifecycleActionToken":"tok"}`
	ev, err := Parse(v1alpha1.NodeEventFormatAWSASGLifecycleHook, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if ev.InstanceID != "i-0def" || ev.Type != "autoscaling:EC2_INSTANCE_TERMINATING" || ev.Test {
		t.Errorf("unexpected event %+v", ev)
	}
}

func TestParseASGHookTestNotification(t *testing.T) {
	body := `{"AccountId":"123456789012","RequestId":"r-2","AutoScalingGroupARN":"arn:aws:autoscaling:eu-central-1:123456789012:autoScalingGroup:x:autoScalingGroupName/example-asg","AutoScalingGroupName":"example-asg","Service":"AWS Auto Scaling","Event":"autoscaling:TEST_NOTIFICATION","Time":"2026-01-01T00:00:00.000Z"}`
	ev, err := Parse(v1alpha1.NodeEventFormatAWSASGLifecycleHook, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Test {
		t.Errorf("expected test notification, got %+v", ev)
	}
}

func TestParseUnknownFormat(t *testing.T) {
	if _, err := Parse("nope", []byte(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}
