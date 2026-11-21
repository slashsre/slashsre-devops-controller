// Package awssqs implements the queue.Provider interface on Amazon SQS.
package awssqs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
)

// API is the subset of the SQS client used, to allow fakes in tests.
type API interface {
	GetQueueAttributes(ctx context.Context, in *sqs.GetQueueAttributesInput, opts ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, opts ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// Queue is an SQS-backed queue.Provider.
type Queue struct {
	api API
	url string
}

var _ queue.Provider = (*Queue)(nil)

// New builds a provider using the default AWS credential chain (IRSA, Pod Identity, env, profile).
func New(ctx context.Context, src v1alpha1.QueueSource) (queue.Provider, error) {
	region := src.Region
	if region == "" {
		region = RegionFromURL(src.URL)
	}
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return NewWithAPI(sqs.NewFromConfig(cfg), src.URL), nil
}

// NewWithAPI builds a provider around an existing client.
func NewWithAPI(api API, queueURL string) *Queue {
	return &Queue{api: api, url: queueURL}
}

// RegionFromURL extracts the region from https://sqs.<region>.amazonaws.com/... URLs.
func RegionFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(u.Hostname(), ".")
	if len(parts) >= 4 && parts[0] == "sqs" {
		return parts[1]
	}
	return ""
}

// ApproximateDepth implements queue.Provider.
func (q *Queue) ApproximateDepth(ctx context.Context) (int, error) {
	out, err := q.api.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(q.url),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	if err != nil {
		return 0, fmt.Errorf("getting queue attributes: %w", err)
	}
	v, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parsing queue depth %q: %w", v, err)
	}
	return n, nil
}

// Receive implements queue.Provider.
func (q *Queue) Receive(ctx context.Context, opts queue.ReceiveOptions) ([]queue.Message, error) {
	maxMsgs := opts.MaxMessages
	if maxMsgs <= 0 || maxMsgs > 10 {
		maxMsgs = 10
	}
	in := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.url),
		MaxNumberOfMessages: int32(maxMsgs),
		WaitTimeSeconds:     int32(opts.WaitTime.Seconds()),
	}
	if opts.VisibilityTimeout > 0 {
		in.VisibilityTimeout = int32(opts.VisibilityTimeout.Seconds())
	}
	out, err := q.api.ReceiveMessage(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("receiving messages: %w", err)
	}
	msgs := make([]queue.Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msgs = append(msgs, queue.Message{
			ID:     aws.ToString(m.MessageId),
			Body:   aws.ToString(m.Body),
			Handle: aws.ToString(m.ReceiptHandle),
		})
	}
	return msgs, nil
}

// Delete implements queue.Provider.
func (q *Queue) Delete(ctx context.Context, msg queue.Message) error {
	_, err := q.api.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.url),
		ReceiptHandle: aws.String(msg.Handle),
	})
	if err != nil {
		return fmt.Errorf("deleting message %s: %w", msg.ID, err)
	}
	return nil
}
