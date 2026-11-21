package awssqs

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
)

type fakeAPI struct {
	depth   string
	msgs    []types.Message
	deleted []string
}

func (f *fakeAPI) GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	return &sqs.GetQueueAttributesOutput{Attributes: map[string]string{"ApproximateNumberOfMessages": f.depth}}, nil
}

func (f *fakeAPI) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{Messages: f.msgs}, nil
}

func (f *fakeAPI) DeleteMessage(_ context.Context, in *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

func TestRegionFromURL(t *testing.T) {
	cases := map[string]string{
		"https://sqs.eu-central-1.amazonaws.com/123456789012/my-queue": "eu-central-1",
		"https://sqs.us-east-2.amazonaws.com/123456789012/q":           "us-east-2",
		"http://localhost:4566/000000000000/q":                         "",
		"::not a url":                                                  "",
	}
	for in, want := range cases {
		if got := RegionFromURL(in); got != want {
			t.Errorf("RegionFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQueueOperations(t *testing.T) {
	api := &fakeAPI{
		depth: "3",
		msgs:  []types.Message{{MessageId: aws.String("m1"), Body: aws.String("{}"), ReceiptHandle: aws.String("h1")}},
	}
	q := NewWithAPI(api, "https://sqs.eu-central-1.amazonaws.com/123456789012/q")
	ctx := context.Background()

	depth, err := q.ApproximateDepth(ctx)
	if err != nil || depth != 3 {
		t.Fatalf("depth = %d, %v; want 3", depth, err)
	}
	msgs, err := q.Receive(ctx, queue.ReceiveOptions{MaxMessages: 1})
	if err != nil || len(msgs) != 1 || msgs[0].Handle != "h1" {
		t.Fatalf("receive = %+v, %v", msgs, err)
	}
	if err := q.Delete(ctx, msgs[0]); err != nil {
		t.Fatal(err)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "h1" {
		t.Fatalf("deleted = %v", api.deleted)
	}
}
