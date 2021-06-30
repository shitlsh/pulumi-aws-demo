package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/cloudwatch"
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
		_, err = cloudwatch.NewEventTarget(ctx, "sns", &cloudwatch.EventTargetArgs{
			Rule: scheduleRule.Name,
			Arn:  mainSns.Arn,
		})
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

