# AWS setup for NodeLifecycleRule

The controller only needs an SQS queue per event source and an IAM role on its ServiceAccount
(IRSA or EKS Pod Identity) allowing `sqs:ReceiveMessage`, `sqs:DeleteMessage` and
`sqs:GetQueueAttributes` on those queues. Configure a redrive policy (dead-letter queue) so that
malformed messages do not circulate forever.

## Spot interruption warnings (`eventFormat: aws-spot-interruption`)

Create an EventBridge rule that forwards Spot interruption warnings to the queue:

```json
{
  "source": ["aws.ec2"],
  "detail-type": ["EC2 Spot Instance Interruption Warning"]
}
```

The queue policy must allow `events.amazonaws.com` to `sqs:SendMessage`. Events wrapped by SNS
(`Message`) or by a Lambda (`original_event`) are also accepted.

## Auto Scaling lifecycle hooks (`eventFormat: aws-asg-lifecycle-hook`)

The SQS queue and an IAM role that Auto Scaling can assume to publish to it must exist.
See [Prepare to add a lifecycle hook](https://docs.aws.amazon.com/autoscaling/ec2/userguide/prepare-for-lifecycle-notifications.html#sqs-notifications).

```bash
aws autoscaling put-lifecycle-hook \
  --lifecycle-hook-name termination-hook \
  --auto-scaling-group-name <ASG_NAME> \
  --lifecycle-transition autoscaling:EC2_INSTANCE_TERMINATING \
  --heartbeat-timeout 120 \
  --default-result CONTINUE \
  --notification-target-arn arn:aws:sqs:<REGION>:<ACCOUNT_ID>:<QUEUE_NAME> \
  --role-arn arn:aws:iam::<ACCOUNT_ID>:role/<HOOK_ROLE_NAME>
```

With CloudFormation use
[`AWS::AutoScaling::LifecycleHook`](https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-autoscaling-lifecyclehook.html);
for EKS managed node groups the hook is attached to the node group's Auto Scaling group.

The controller drains the node and lets the hook time out with `CONTINUE` (it does not call
`CompleteLifecycleAction` yet). Keep `--heartbeat-timeout` longer than the drain timeout.

When a hook is created or updated, Auto Scaling sends an `autoscaling:TEST_NOTIFICATION`
message; the controller deletes it without acting.

## Example

```yaml
apiVersion: slashsre.controller.io/v1alpha1
kind: NodeLifecycleRule
metadata:
  name: asg-termination
spec:
  eventFormat: aws-asg-lifecycle-hook
  source:
    url: https://sqs.<REGION>.amazonaws.com/<ACCOUNT_ID>/<QUEUE_NAME>
  drain:
    timeout: 90s
```
