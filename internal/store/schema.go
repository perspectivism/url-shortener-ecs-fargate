package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// SchemaAPI is the subset of DynamoDB used to create tables locally.
// In AWS the tables are created by Terraform (infra/modules/dynamodb), which
// must match these definitions.
type SchemaAPI interface {
	CreateTable(ctx context.Context, in *dynamodb.CreateTableInput, opts ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	DescribeTable(ctx context.Context, in *dynamodb.DescribeTableInput, opts ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	UpdateTimeToLive(ctx context.Context, in *dynamodb.UpdateTimeToLiveInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateTimeToLiveOutput, error)
}

func attr(name string) types.AttributeDefinition {
	return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: types.ScalarAttributeTypeS}
}

func keySchema(hash, rng string) []types.KeySchemaElement {
	return []types.KeySchemaElement{
		{AttributeName: aws.String(hash), KeyType: types.KeyTypeHash},
		{AttributeName: aws.String(rng), KeyType: types.KeyTypeRange},
	}
}

// LinksTableInput is the links table definition (SPEC §5.1).
func LinksTableInput(name string) *dynamodb.CreateTableInput {
	return &dynamodb.CreateTableInput{
		TableName:            aws.String(name),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{attr("pk"), attr("sk"), attr("gsi1pk"), attr("gsi1sk")},
		KeySchema:            keySchema("pk", "sk"),
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{{
			IndexName: aws.String(gsi1),
			KeySchema: keySchema("gsi1pk", "gsi1sk"),
			Projection: &types.Projection{
				ProjectionType:   types.ProjectionTypeInclude,
				NonKeyAttributes: []string{"long_url", "status", "created_at", "expires_at"},
			},
		}},
	}
}

// AnalyticsTableInput is the analytics table definition (SPEC §5.2).
func AnalyticsTableInput(name string) *dynamodb.CreateTableInput {
	return &dynamodb.CreateTableInput{
		TableName:            aws.String(name),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{attr("pk"), attr("sk")},
		KeySchema:            keySchema("pk", "sk"),
	}
}

// EnsureTable creates the table if missing, waits until ACTIVE, and enables TTL on "ttl".
func EnsureTable(ctx context.Context, db SchemaAPI, in *dynamodb.CreateTableInput, poll time.Duration) error {
	name := aws.ToString(in.TableName)
	_, err := db.CreateTable(ctx, in)
	var inUse *types.ResourceInUseException
	if err != nil && !errors.As(err, &inUse) {
		return fmt.Errorf("create table %s: %w", name, err)
	}
	for {
		out, err := db.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: in.TableName})
		if err != nil {
			return fmt.Errorf("describe table %s: %w", name, err)
		}
		if out.Table != nil && out.Table.TableStatus == types.TableStatusActive {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for table %s: %w", name, ctx.Err())
		case <-time.After(poll):
		}
	}
	_, err = db.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName:               in.TableName,
		TimeToLiveSpecification: &types.TimeToLiveSpecification{AttributeName: aws.String("ttl"), Enabled: aws.Bool(true)},
	})
	// Re-enabling TTL on a table that already has it is a ValidationException; that is fine.
	var ve interface{ ErrorCode() string }
	if err != nil && (!errors.As(err, &ve) || ve.ErrorCode() != "ValidationException") {
		return fmt.Errorf("enable ttl %s: %w", name, err)
	}
	return nil
}
