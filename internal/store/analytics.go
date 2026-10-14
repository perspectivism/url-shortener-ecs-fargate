package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DedupeTTL is how long event dedupe markers live (SPEC §5.2).
const DedupeTTL = 3 * 24 * time.Hour

// Click is one click as recorded by the worker (already normalized).
type Click struct {
	EventID string
	Code    string
	Date    string // yyyy-mm-dd (UTC) of the click
	TS      time.Time
	IsBot   bool
	// Dimension values (human clicks only): referrer host, country, device, browser.
	Referrer, Country, Device, Browser string
}

// DayRow is a daily totals item.
type DayRow struct {
	Date       string
	Clicks     int64
	BotClicks  int64
	Uniques    int64
	HasUniques bool
}

// DimRow is one dimension counter item.
type DimRow struct {
	Date   string
	Value  string
	Clicks int64
}

// Analytics is the analytics-table repository.
type Analytics struct {
	db      DynamoDB
	table   string
	timeout time.Duration
}

// NewAnalytics returns a repository for table.
func NewAnalytics(db DynamoDB, table string, timeout time.Duration) *Analytics {
	return &Analytics{db: db, table: table, timeout: timeout}
}

func statsPK(code string) string { return "STATS#" + code }

func sv(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }

func nv(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}

func (a *Analytics) addOne(pk, sk, attr string) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 &a.table,
		Key:                       map[string]types.AttributeValue{"pk": sv(pk), "sk": sv(sk)},
		UpdateExpression:          aws.String("ADD #a :one"),
		ExpressionAttributeNames:  map[string]string{"#a": attr},
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": nv(1)},
	}}
}

// RecordClick implements access pattern 6: one transaction that inserts the
// dedupe marker EVENT#{id} (only if absent) and increments the day and
// dimension counters. A duplicate event cancels the whole transaction, so
// counts are exactly-once even though SQS is at-least-once. It returns
// duplicate=true when the marker already existed.
func (a *Analytics) RecordClick(ctx context.Context, c Click) (duplicate bool, err error) {
	pk := statsPK(c.Code)
	items := []types.TransactWriteItem{{Put: &types.Put{
		TableName: &a.table,
		Item: map[string]types.AttributeValue{
			"pk":  sv("EVENT#" + c.EventID),
			"sk":  sv(skMeta),
			"ttl": nv(c.TS.Add(DedupeTTL).Unix()),
		},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}}
	if c.IsBot {
		items = append(items, a.addOne(pk, "DAY#"+c.Date, "bot_clicks"))
	} else {
		items = append(items,
			a.addOne(pk, "DAY#"+c.Date, "clicks"),
			a.addOne(pk, dimSK("ref", c.Date, c.Referrer), "clicks"),
			a.addOne(pk, dimSK("country", c.Date, c.Country), "clicks"),
			a.addOne(pk, dimSK("device", c.Date, c.Device), "clicks"),
			a.addOne(pk, dimSK("browser", c.Date, c.Browser), "clicks"),
		)
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	// No ClientRequestToken: within its 10-minute window DynamoDB would report a
	// replayed transaction as success, hiding duplicates from the
	// WorkerDuplicateEvents metric. The EVENT# condition alone guarantees
	// exactly-once counting (a lost-response SDK retry surfaces as a duplicate).
	_, err = a.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	if err == nil {
		return false, nil
	}
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) > 0 && aws.ToString(tce.CancellationReasons[0].Code) == "ConditionalCheckFailed" {
		return true, nil
	}
	return false, fmt.Errorf("record click: %w", err)
}

func dimSK(dim, date, value string) string { return "DIM#" + dim + "#" + date + "#" + value }

// SetUniques writes the persisted unique-visitor count for a day (idempotent).
func (a *Analytics) SetUniques(ctx context.Context, code, date string, n int64) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	_, err := a.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &a.table,
		Key:                       map[string]types.AttributeValue{"pk": sv(statsPK(code)), "sk": sv("DAY#" + date)},
		UpdateExpression:          aws.String("SET uniques = :n"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":n": nv(n)},
	})
	if err != nil {
		return fmt.Errorf("set uniques: %w", err)
	}
	return nil
}

// queryAll pages through a key-condition query on the analytics table.
func (a *Analytics) queryAll(ctx context.Context, code, from, to string) ([]map[string]types.AttributeValue, error) {
	var out []map[string]types.AttributeValue
	var start map[string]types.AttributeValue
	for {
		qctx, cancel := context.WithTimeout(ctx, a.timeout)
		res, err := a.db.Query(qctx, &dynamodb.QueryInput{
			TableName:              &a.table,
			KeyConditionExpression: aws.String("pk = :pk AND sk BETWEEN :from AND :to"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": sv(statsPK(code)), ":from": sv(from), ":to": sv(to),
			},
			ExclusiveStartKey: start,
		})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("query stats: %w", err)
		}
		out = append(out, res.Items...)
		if len(res.LastEvaluatedKey) == 0 {
			return out, nil
		}
		start = res.LastEvaluatedKey
	}
}

type counterItem struct {
	SK        string `dynamodbav:"sk"`
	Clicks    int64  `dynamodbav:"clicks"`
	BotClicks int64  `dynamodbav:"bot_clicks"`
	Uniques   *int64 `dynamodbav:"uniques"`
}

// Days returns daily totals for [from, to] (inclusive, yyyy-mm-dd).
func (a *Analytics) Days(ctx context.Context, code, from, to string) ([]DayRow, error) {
	raw, err := a.queryAll(ctx, code, "DAY#"+from, "DAY#"+to)
	if err != nil {
		return nil, err
	}
	var items []counterItem
	if err := attributevalue.UnmarshalListOfMaps(raw, &items); err != nil {
		return nil, fmt.Errorf("decode days: %w", err)
	}
	rows := make([]DayRow, 0, len(items))
	for _, it := range items {
		r := DayRow{Date: strings.TrimPrefix(it.SK, "DAY#"), Clicks: it.Clicks, BotClicks: it.BotClicks}
		if it.Uniques != nil {
			r.Uniques, r.HasUniques = *it.Uniques, true
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// Dimension returns dimension counters of one type for [from, to].
func (a *Analytics) Dimension(ctx context.Context, code, dim, from, to string) ([]DimRow, error) {
	prefix := "DIM#" + dim + "#"
	raw, err := a.queryAll(ctx, code, prefix+from+"#", prefix+to+"#\U0010FFFF") // upper bound sorts after any value
	if err != nil {
		return nil, err
	}
	var items []counterItem
	if err := attributevalue.UnmarshalListOfMaps(raw, &items); err != nil {
		return nil, fmt.Errorf("decode dimension: %w", err)
	}
	rows := make([]DimRow, 0, len(items))
	for _, it := range items {
		rest := strings.TrimPrefix(it.SK, prefix)
		date, value, ok := strings.Cut(rest, "#")
		if !ok {
			continue
		}
		rows = append(rows, DimRow{Date: date, Value: value, Clicks: it.Clicks})
	}
	return rows, nil
}
