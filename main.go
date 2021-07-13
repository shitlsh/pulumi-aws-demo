package main

import (
	"encoding/json"
	"fmt"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	kms2 "github.com/pulumi/pulumi-aws/sdk/v4/go/aws/kms"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/lambda"
	sns2 "github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sns"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"os"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// GetCurrentAccount
		callerIdentity, err := aws.GetCallerIdentity(ctx)
		if err != nil {
			return err
		}

		// Create KMS
		cmkRole, err := iam.NewRole(ctx,"pulumi-aws-demo-cmk-role",&iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Sid": "",
					"Effect": "Allow",
					"Principal": {
						"Service": "events.amazonaws.com"
					},
					"Action": "sts:AssumeRole"
				}]
			}`),
			Description:  pulumi.String("role to use cmk"),
			Name: pulumi.String("pulumi-aws-demo-cmk-role"),
		})
		if err != nil {
			return err
		}

		kms, err := kms2.NewKey(ctx,"pulumi-aws-demo-kms-key",&kms2.KeyArgs{
			Description: pulumi.String("cmk created by pulumi to protect sns & sqs"),
			Policy: pulumi.String(fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Id": "default-sns-1",
				"Statement": [
					{
						"Sid": "Allow access through SNS for all principals in the account that are authorized to use SNS",
						"Effect": "Allow",
						"Principal": {
							"AWS": "*"
						},
						"Action": [
							"kms:Decrypt",
							"kms:GenerateDataKey*",
							"kms:CreateGrant",
							"kms:ListGrants",
							"kms:DescribeKey"
						],
						"Resource": "*",
						"Condition": {
							"ArnEquals": {
								"aws:SourceArn": [
									"arn:aws:sqs:ap-southeast-2:%s:pulumi-aws-demo-sqs",
									"arn:aws:sns:ap-southeast-2:%s:pulumi-aws-demo-main-sns"
								]
							}
						}
					},
					{
						"Sid": "Allow direct access to key metadata to the account",
						"Effect": "Allow",
						"Principal": {
							"AWS": "arn:aws:iam::%s:root"
						},
						"Action": "kms:*",
						"Resource": "*"
					}
				]
			}`,callerIdentity.AccountId,callerIdentity.AccountId,callerIdentity.AccountId)),
		})
		if err != nil {
			return err
		}

		cmkPolicy := kms.Arn.ApplyT(func (arn string) (string, error) {
			policyJSON, err := json.Marshal(map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []interface{}{
					map[string]interface{}{
						"Effect": "Allow",
						"Action": []interface{}{
							"kms:Decrypt",
							"kms:Encrypt",
							"kms:ReEncrypt",
							"kms:GenerateDataKey*",
							"kms:DescribeKey",
						},
						"Resource": arn,
					},
				},
			})
			if err != nil {
				return "", err
			}
			return string(policyJSON), nil
		}).(pulumi.StringOutput)

		_, err = iam.NewRolePolicy(ctx,"pulumi-aws-demo-cmk-role-policy",&iam.RolePolicyArgs{
			Role: cmkRole.Name,
			Policy: cmkPolicy,
		})

		// Create a event rule triggers sns topic every 5 minutes
		scheduleRule, err := cloudwatch.NewEventRule(ctx, "pulumi-aws-demo-schedule-rule", &cloudwatch.EventRuleArgs{
			Description:  pulumi.String("Trigger pulumi-aws-demo-main-sns every 5 minutes"),
			ScheduleExpression:  pulumi.String("rate(5 minutes)"),
			RoleArn: cmkRole.Arn,
		})
		if err != nil {
			return err
		}

		// Create an AWS resource (SNS:Topic)
		mainSns, err := sns2.NewTopic(ctx,"pulumi-aws-demo-main-sns",&sns2.TopicArgs{
			Name: pulumi.String("pulumi-aws-demo-main-sns"),
			KmsMasterKeyId: kms.KeyId,
			Tags: pulumi.StringMap{"Owner": pulumi.String("awstraining")},
		})
		if err != nil {
			return err
		}

		// Link event rule to trigger sns
		_, err = cloudwatch.NewEventTarget(ctx, "pulumi-aws-demo-target-main-sns", &cloudwatch.EventTargetArgs{
			Rule: scheduleRule.Name,
			Arn:  mainSns.Arn,
		})
		if err != nil {
			return err
		}

		// Allow scheduleRule to publish to sns
		topicPolicy := mainSns.Arn.ApplyT(func (arn string) (string, error) {
			policyJSON, err := json.Marshal(map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []interface{}{
					map[string]interface{}{
						"Effect": "Allow",
						"Principal": map[string]interface{}{
								"AWS": "arn:aws:iam::"+callerIdentity.AccountId+":role/pulumi-aws-demo-cmk-role",
						},
						"Action": "sns:Publish",
						"Resource": arn,
					},
				},
			})
			if err != nil {
				return "", err
			}
			return string(policyJSON), nil
		}).(pulumi.StringOutput)

		_, err = sns2.NewTopicPolicy(ctx, "_default", &sns2.TopicPolicyArgs{
			Arn: mainSns.Arn,
			Policy: topicPolicy,
		})
		if err != nil {
			return err
		}

		// Create a SQS to consume SNS & trigger lambda function
		deadQueue, err := sqs.NewQueue(ctx, "pulumi-aws-demo-sqs-dead-letter", &sqs.QueueArgs{
			Name: pulumi.String("pulumi-aws-demo-sqs-dead-letter"),
			MessageRetentionSeconds:   pulumi.Int(7*24*60*60), //retain 7 days
			VisibilityTimeoutSeconds:  pulumi.Int(3000), //timeout 5 minutes
		})
		if err != nil {
			return err
		}

		retrievePolicy := deadQueue.Arn.ApplyT(func (arn string) (string, error) {
			policyJSON, err := json.Marshal(map[string]interface{}{
				"deadLetterTargetArn": arn,
				"maxReceiveCount": 10,
			})
			if err != nil {
				return "", err
			}
			return string(policyJSON), nil
		}).(pulumi.StringOutput)

		queue, err := sqs.NewQueue(ctx, "pulumi-aws-demo-sqs", &sqs.QueueArgs{
			Name: pulumi.String("pulumi-aws-demo-sqs"),
			MessageRetentionSeconds:  pulumi.Int(7*24*60*60), //retain 7 days
			VisibilityTimeoutSeconds: pulumi.Int(3000), //timeout 5 minutes
			RedrivePolicy:            retrievePolicy,
		})
		if err != nil {
			return err
		}

		// Allow sns to SendMessage to sqs
		queuePolicy := queue.Arn.ApplyT(func (arn string) (string, error) {
			policyJSON, err := json.Marshal(map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []interface{}{
					map[string]interface{}{
						"Effect": "Allow",
						"Principal": map[string]interface{}{
							"Service": "sns.amazonaws.com",
						},
						"Action": "sqs:SendMessage",
						"Resource": arn,
					},
				},
			})
			if err != nil {
				return "", err
			}
			return string(policyJSON), nil
		}).(pulumi.StringOutput)

		_, err = sqs.NewQueuePolicy(ctx,"_default", &sqs.QueuePolicyArgs{
			QueueUrl: queue.Url,
			Policy: queuePolicy,
		})


		// Create a lambda function recording event to log
		lambdaRole, err := iam.NewRole(ctx,"pulumi-aws-demo-lambda-exec-role",&iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Sid": "",
					"Effect": "Allow",
					"Principal": {
						"Service": "lambda.amazonaws.com"
					},
					"Action": "sts:AssumeRole"
				}]
			}`),
			Description:  pulumi.String("lambda exec role"),
			Name: pulumi.String("pulumi-aws-demo-lambda-exec-role"),
		})
		if err != nil {
			return err
		}

		// Attach a policy to allow writing logs to CloudWatch
		lambdaRolePolicy, err := iam.NewRolePolicy(ctx, "pulumi-aws-demo-lambda-log-policy", &iam.RolePolicyArgs{
			Role: lambdaRole.Name,
			Policy: pulumi.String(fmt.Sprintf(`{
                "Version": "2012-10-17",
                "Statement": [{
                    "Effect": "Allow",
                    "Action": [
                        "logs:CreateLogGroup",
                        "logs:CreateLogStream",
                        "logs:PutLogEvents"
                    ],
                    "Resource": "arn:aws:logs:*:*:*"
                },
				{
					"Sid": "",
					"Effect": "Allow",
					"Action": [
						"sqs:ReceiveMessage",
						"sqs:DeleteMessage",
						"sqs:GetQueueAttributes",
						"sqs:GetQueueUrl",
						"sqs:SendMessage"
					],
					"Resource": [
						"arn:aws:sqs:ap-southeast-2:%s:pulumi-aws-demo-sqs",
						"arn:aws:sqs:ap-southeast-2:%s:pulumi-aws-demo-sqs-lambda-dead-letter"
					]
				}]
            }`, callerIdentity.AccountId, callerIdentity.AccountId)),
		})

		// Create dead letter queue for lambda
		deadLambda, err := sqs.NewQueue(ctx, "pulumi-aws-demo-sqs-lambda-dead-letter", &sqs.QueueArgs{
			Name: pulumi.String("pulumi-aws-demo-sqs-lambda-dead-letter"),
			MessageRetentionSeconds:   pulumi.Int(7*24*60*60), //retain 7 days
			VisibilityTimeoutSeconds:  pulumi.Int(3000), //timeout 5 minutes
		})
		if err != nil {
			return err
		}
		// Set arguments for constructing the function resource.
		functionArgs := &lambda.FunctionArgs{
			Name: pulumi.String("pulumi-aws-demo-lambda-function"),
			Role:    lambdaRole.Arn,
			PackageType: pulumi.String("Image"),
			ImageUri: pulumi.String(os.Getenv("IMAGE_URI")),
			DeadLetterConfig: &lambda.FunctionDeadLetterConfigArgs{TargetArn: deadLambda.Arn},
		}

		// Create the lambda using the args.
		lambdaFunction, err := lambda.NewFunction(
			ctx,
			"pulumi-aws-demo-lambda-function",
			functionArgs,
			pulumi.DependsOn([]pulumi.Resource{
				lambdaRolePolicy,
			}),
		)
		if err != nil {
			return err
		}

		_, err = lambda.NewEventSourceMapping(ctx, "pulumi-aws-demo-lambda-sqs-event", &lambda.EventSourceMappingArgs{
			EventSourceArn: queue.Arn,
			FunctionName:   lambdaFunction.Name,
		})
		if err != nil {
			return err
		}

		_, err = lambda.NewPermission(ctx, "pulumi-aws-demo-lambda-sns-permission", &lambda.PermissionArgs{
			Action:            pulumi.String("lambda:InvokeFunction"),
			Function:          lambdaFunction,
			Principal:         pulumi.String("sns.amazonaws.com"),
			SourceArn:         mainSns.Arn,
		})

		// Create subscriptions for mainSns
		// Send email
		_, err = sns2.NewTopicSubscription(ctx,"pulumi-aws-demo-main-sns-email-sub", &sns2.TopicSubscriptionArgs{
			Topic: mainSns,
			Endpoint: pulumi.String(os.Getenv("MY_EMAIL_ADDRESS")),
			Protocol: pulumi.String("email"),
		})
		if err != nil {
			return err
		}

		// Trigger lambda
		_, err = sns2.NewTopicSubscription(ctx,"pulumi-aws-demo-main-sns-lambda-sub", &sns2.TopicSubscriptionArgs{
			Topic: mainSns,
			Endpoint: lambdaFunction.Arn,
			Protocol: pulumi.String("lambda"),
		})
		if err != nil {
			return err
		}

		// Sqs consume
		_, err = sns2.NewTopicSubscription(ctx,"pulumi-aws-demo-main-sns-sqs-sub", &sns2.TopicSubscriptionArgs{
			Topic:    mainSns,
			Endpoint: queue.Arn,
			Protocol: pulumi.String("sqs"),
		})
		if err != nil {
			return err
		}

		return nil
	})
}

