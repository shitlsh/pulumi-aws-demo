package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/lambda"
	sns2 "github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sns"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"os"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create a event rule triggers sns topic every 5 minutes
		scheduleRule, err := cloudwatch.NewEventRule(ctx, "pulumi-aws-demo-schedule-rule", &cloudwatch.EventRuleArgs{
			Description:  pulumi.String("Trigger pulumi-aws-demo-main-sns every 5 minutes"),
			ScheduleExpression:  pulumi.String("rate(5 minutes)"),
		})
		if err != nil {
			return err
		}

		// Create an AWS resource (SNS:Topic)
		mainSns, err := sns2.NewTopic(ctx,"pulumi-aws-demo-main-sns",nil)
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


		_, err = sns2.NewTopicPolicy(ctx, "_default", &sns2.TopicPolicyArgs{
			Arn: mainSns.Arn,
			Policy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Principal": {
						"Service": "events.amazonaws.com"
					},
					"Action": "sns:Publish",
					"Resource": "*"
				}]
			}`),
		})
		if err != nil {
			return err
		}

		// Create a SQS to consume SNS & trigger lambda function
		queue, err := sqs.NewQueue(ctx, "pulumi-aws-demo-sqs", &sqs.QueueArgs{
			MessageRetentionSeconds:   pulumi.Int(7*24*60*60), //retain 7 days
			VisibilityTimeoutSeconds:  pulumi.Int(3000), //timeout 5 minutes
		})
		if err != nil {
			return err
		}

		_, err = sqs.NewQueuePolicy(ctx,"_default", &sqs.QueuePolicyArgs{
			QueueUrl: queue.Url,
			Policy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Principal": {
						"Service": "sns.amazonaws.com"
					},
					"Action": "sqs:SendMessage",
					"Resource": "*"
				}]
			}`),
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
		})
		if err != nil {
			return err
		}

		//tmpJSON, err := json.Marshal(map[string]interface{}{
		//	"Version": "2012-10-17",
		//	"Statement": []map[string]interface{}{
		//		{
		//			"Effect":    "Allow",
		//			"Action":    []string{"s3:GetObject"},
		//			"Resource":  []string{pulumi.},
		//		},
		//	},
		//})

		// Attach a policy to allow writing logs to CloudWatch
		logPolicy, err := iam.NewRolePolicy(ctx, "pulumi-aws-demo-lambda-log-policy", &iam.RolePolicyArgs{
			Role: lambdaRole.Name,
			Policy: pulumi.String(`{
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
						"sqs:GetQueueAttributes"
					],
					"Resource": "*"
				}]
            }`),
		})

		// Set arguments for constructing the function resource.
		functionArgs := &lambda.FunctionArgs{
			Role:    lambdaRole.Arn,
			PackageType: pulumi.String("Image"),
			ImageUri: pulumi.String(os.Getenv("IMAGE_URI")),
		}

		// Create the lambda using the args.
		lambdaFunction, err := lambda.NewFunction(
			ctx,
			"pulumi-aws-demo-lambda-function",
			functionArgs,
			pulumi.DependsOn([]pulumi.Resource{
				logPolicy,
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
		//_, err = sns2.NewTopicSubscription(ctx,"pulumi-aws-demo-main-sns-email-sub", &sns2.TopicSubscriptionArgs{
		//	Topic: mainSns,
		//	Endpoint: pulumi.String(os.Getenv("MY_EMAIL_ADDRESS")),
		//	Protocol: pulumi.String("email"),
		//})
		//if err != nil {
		//	return err
		//}

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

