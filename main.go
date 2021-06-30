package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/lambda"
	sns2 "github.com/pulumi/pulumi-aws/sdk/v4/go/aws/sns"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"os"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		//create a event rule triggers sns topic every 5 minutes
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
		//Link event rule to trigger sns
		_, err = cloudwatch.NewEventTarget(ctx, "pulumi-aws-demo-target-main-sns", &cloudwatch.EventTargetArgs{
			Rule: scheduleRule.Name,
			Arn:  mainSns.Arn,
		})
		if err != nil {
			return err
		}
		//create a lambda function recording event to log
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

		// Attach a policy to allow writing logs to CloudWatch
		logPolicy, err := iam.NewRolePolicy(ctx, "pulumi-aws-demo-lambda-log--policy", &iam.RolePolicyArgs{
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
                }]
            }`),
		})

		logGroup, err := cloudwatch.NewLogGroup(ctx, "pulumi-aws-demo-lambda-log-group", &cloudwatch.LogGroupArgs{
			RetentionInDays: pulumi.Int(14),
		})
		if err != nil {
			return err
		}

		// Set arguments for constructing the function resource.
		functionArgs := &lambda.FunctionArgs{
			Role:    lambdaRole.Arn,
			PackageType: pulumi.String("Image"),
			ImageUri: pulumi.String(os.Getenv("IMAGE_URI")),
		}

		// Create the lambda using the args.
		_, err = lambda.NewFunction(
			ctx,
			"pulumi-aws-demo-lambda-function",
			functionArgs,
			pulumi.DependsOn([]pulumi.Resource{
				logPolicy,
				logGroup,
			}),
		)
		if err != nil {
			return err
		}

		//create subscriptions for mainSns
		_, err = sns2.NewTopicSubscription(ctx,"pulumi-aws-demo-main-sns-email-sub", &sns2.TopicSubscriptionArgs{
			Topic: mainSns,
			Endpoint: pulumi.String(os.Getenv("MY_EMAIL_ADDRESS")),
			Protocol: pulumi.String("email"),
		})
		return nil
	})
}

