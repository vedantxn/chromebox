package orchestrator

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2 starts and stops the instances behind nodes. A node named node-b is the
// instance tagged Name=chromebox-node-b and Project=chromebox. Credentials
// come from the gateway's instance role (scripts/aws/storage.sh), which may
// start and stop only instances carrying the Project=chromebox tag.
type EC2 struct{ c *ec2.Client }

func NewEC2(ctx context.Context, region string) (*EC2, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &EC2{c: ec2.NewFromConfig(cfg)}, nil
}

// state is the instance's id and EC2 state for a node.
func (e *EC2) state(ctx context.Context, node string) (id, state string, err error) {
	out, err := e.c.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: []types.Filter{
		{Name: aws.String("tag:Name"), Values: []string{"chromebox-" + node}},
		{Name: aws.String("tag:Project"), Values: []string{"chromebox"}},
		{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
	}})
	if err != nil {
		return "", "", err
	}
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			return aws.ToString(i.InstanceId), string(i.State.Name), nil
		}
	}
	return "", "", fmt.Errorf("no instance for node %q", node)
}

func (e *EC2) id(ctx context.Context, node string) (string, error) {
	id, _, err := e.state(ctx, node)
	return id, err
}

// Start starts the node's instance. Asking again for one already starting or
// running is not an error; one that is still stopping cannot be started yet,
// and is reported so that the caller retries.
func (e *EC2) Start(ctx context.Context, node string) error {
	id, st, err := e.state(ctx, node)
	if err != nil {
		return err
	}
	switch st {
	case "pending", "running":
		return nil
	case "stopping":
		return fmt.Errorf("instance %s is still stopping; will retry", id)
	}
	_, err = e.c.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
	return err
}

func (e *EC2) Stop(ctx context.Context, node string) error {
	id, err := e.id(ctx, node)
	if err != nil {
		return err
	}
	_, err = e.c.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	return err
}
