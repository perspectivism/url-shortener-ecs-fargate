package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
)

func click(bot bool) Click {
	return Click{EventID: "evt-1", Code: "abc", Date: "2026-10-14", TS: ts, IsBot: bot,
		Referrer: "news.example", Country: "DE", Device: "desktop", Browser: "Firefox"}
}

func TestRecordClick(t *testing.T) {
	var last *dynamodb.TransactWriteItemsInput
	var result error
	db := &fakeDB{transact: func(in *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		last = in
		return &dynamodb.TransactWriteItemsOutput{}, result
	}}
	a := NewAnalytics(db, "analytics", time.Second)
	ctx := context.Background()

	dup, err := a.RecordClick(ctx, click(false))
	if err != nil || dup || len(last.TransactItems) != 6 || last.ClientRequestToken != nil {
		t.Fatalf("human: dup=%v err=%v items=%d", dup, err, len(last.TransactItems))
	}
	put := last.TransactItems[0].Put
	if s(put.Item["pk"]) != "EVENT#evt-1" || *put.ConditionExpression != "attribute_not_exists(pk)" || s(put.Item["ttl"]) != fmt.Sprint(ts.Add(DedupeTTL).Unix()) {
		t.Fatalf("dedupe put = %+v ttl=%s", put, s(put.Item["ttl"]))
	}
	wantSKs := []string{"DAY#2026-10-14", "DIM#ref#2026-10-14#news.example", "DIM#country#2026-10-14#DE", "DIM#device#2026-10-14#desktop", "DIM#browser#2026-10-14#Firefox"}
	for i, sk := range wantSKs {
		u := last.TransactItems[i+1].Update
		if s(u.Key["pk"]) != "STATS#abc" || s(u.Key["sk"]) != sk || u.ExpressionAttributeNames["#a"] != "clicks" {
			t.Errorf("item %d = %s %s %v", i+1, s(u.Key["pk"]), s(u.Key["sk"]), u.ExpressionAttributeNames)
		}
	}

	if _, err := a.RecordClick(ctx, click(true)); err != nil || len(last.TransactItems) != 2 || last.TransactItems[1].Update.ExpressionAttributeNames["#a"] != "bot_clicks" {
		t.Fatalf("bot: %v %d", err, len(last.TransactItems))
	}

	result = &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("ConditionalCheckFailed")}, {Code: aws.String("None")}}}
	if dup, err := a.RecordClick(ctx, click(false)); !dup || err != nil {
		t.Fatalf("duplicate: dup=%v err=%v", dup, err)
	}
	result = &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("None")}, {Code: aws.String("ThrottlingError")}}}
	if dup, err := a.RecordClick(ctx, click(false)); dup || err == nil {
		t.Fatal("throttling must be an error, not a duplicate")
	}
	result = errBoom
	if _, err := a.RecordClick(ctx, click(false)); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}
}

func TestUniquesAndQueries(t *testing.T) {
	var upd *dynamodb.UpdateItemInput
	var queries []*dynamodb.QueryInput
	pages := [][]map[string]types.AttributeValue{}
	db := &fakeDB{
		updateItem: func(in *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			upd = in
			return &dynamodb.UpdateItemOutput{}, nil
		},
		query: func(in *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			queries = append(queries, in)
			out := &dynamodb.QueryOutput{Items: pages[0]}
			pages = pages[1:]
			if len(pages) > 0 {
				out.LastEvaluatedKey = map[string]types.AttributeValue{"pk": sv("x"), "sk": sv("y")}
			}
			return out, nil
		},
	}
	a := NewAnalytics(db, "analytics", time.Second)
	ctx := context.Background()
	if err := a.SetUniques(ctx, "abc", "2026-10-13", 42); err != nil || s(upd.Key["sk"]) != "DAY#2026-10-13" || s(upd.ExpressionAttributeValues[":n"]) != "42" {
		t.Fatalf("set uniques: %v", err)
	}

	day1, _ := attributevalue.MarshalMap(map[string]any{"pk": "STATS#abc", "sk": "DAY#2026-10-13", "clicks": 5, "bot_clicks": 1, "uniques": 3})
	day2, _ := attributevalue.MarshalMap(map[string]any{"pk": "STATS#abc", "sk": "DAY#2026-10-14", "clicks": 2})
	pages = [][]map[string]types.AttributeValue{{day1}, {day2}} // two pages
	rows, err := a.Days(ctx, "abc", "2026-10-13", "2026-10-14")
	if err != nil || len(rows) != 2 || !rows[0].HasUniques || rows[0].Uniques != 3 || rows[1].HasUniques || rows[1].Clicks != 2 || len(queries) != 2 {
		t.Fatalf("days: %+v %v queries=%d", rows, err, len(queries))
	}
	if s(queries[0].ExpressionAttributeValues[":from"]) != "DAY#2026-10-13" || queries[1].ExclusiveStartKey == nil {
		t.Fatal("pagination / bounds")
	}

	queries = nil
	dim1, _ := attributevalue.MarshalMap(map[string]any{"sk": "DIM#ref#2026-10-13#t.co", "clicks": 4})
	bad, _ := attributevalue.MarshalMap(map[string]any{"sk": "DIM#ref#nohash", "clicks": 1})
	pages = [][]map[string]types.AttributeValue{{dim1, bad}}
	dims, err := a.Dimension(ctx, "abc", "ref", "2026-10-13", "2026-10-14")
	if err != nil || len(dims) != 1 || dims[0].Value != "t.co" || dims[0].Date != "2026-10-13" || dims[0].Clicks != 4 {
		t.Fatalf("dims: %+v %v", dims, err)
	}
	if s(queries[0].ExpressionAttributeValues[":to"]) != "DIM#ref#2026-10-14#\U0010FFFF" {
		t.Fatalf("upper bound = %q", s(queries[0].ExpressionAttributeValues[":to"]))
	}

	db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) { return nil, errBoom }
	if _, err := a.Days(ctx, "abc", "a", "b"); !errors.Is(err, errBoom) {
		t.Fatal("query errors propagate")
	}
	if _, err := a.Dimension(ctx, "abc", "ref", "a", "b"); !errors.Is(err, errBoom) {
		t.Fatal("query errors propagate")
	}
	badItem := map[string]types.AttributeValue{"clicks": sv("notanumber")}
	db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{badItem}}, nil
	}
	if _, err := a.Days(ctx, "abc", "a", "b"); err == nil {
		t.Fatal("decode error")
	}
	if _, err := a.Dimension(ctx, "abc", "ref", "a", "b"); err == nil {
		t.Fatal("decode error")
	}
	db.updateItem = func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) { return nil, errBoom }
	if err := a.SetUniques(ctx, "abc", "d", 1); !errors.Is(err, errBoom) {
		t.Fatal("update errors propagate")
	}
}

func TestScanRescan(t *testing.T) {
	item, _ := attributevalue.MarshalMap(toItem(sampleLink()))
	var scans []*dynamodb.ScanInput
	calls := 0
	db := &fakeDB{scan: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
		scans = append(scans, in)
		calls++
		if calls == 1 {
			return &dynamodb.ScanOutput{Items: []map[string]types.AttributeValue{item}, LastEvaluatedKey: map[string]types.AttributeValue{"pk": sv("x")}}, nil
		}
		return &dynamodb.ScanOutput{}, nil
	}}
	st := NewLinks(db, "links", time.Second)
	var got []links.Link
	cutoff := ts.Add(-7 * 24 * time.Hour)
	err := st.ScanRescan(context.Background(), 1, 4, cutoff, func(p []links.Link) error { got = append(got, p...); return nil })
	if err != nil || len(got) != 1 || got[0].Code != "abc123" || len(scans) != 2 {
		t.Fatalf("scan: %v %d %d", err, len(got), len(scans))
	}
	if *scans[0].Segment != 1 || *scans[0].TotalSegments != 4 || s(scans[0].ExpressionAttributeValues[":cutoff"]) != fmtTime(cutoff) || scans[1].ExclusiveStartKey == nil {
		t.Fatalf("scan input = %+v", scans[0])
	}
	calls = 0
	if err := st.ScanRescan(context.Background(), 0, 1, cutoff, func([]links.Link) error { return errBoom }); !errors.Is(err, errBoom) {
		t.Fatal("callback errors propagate")
	}
	db.scan = func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) { return nil, errBoom }
	if err := st.ScanRescan(context.Background(), 0, 1, cutoff, nil); !errors.Is(err, errBoom) {
		t.Fatal("scan errors propagate")
	}
	for _, bad := range []map[string]types.AttributeValue{
		{"custom": sv("x")},
		{"created_at": sv("bad")},
	} {
		db.scan = func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			return &dynamodb.ScanOutput{Items: []map[string]types.AttributeValue{bad}}, nil
		}
		if err := st.ScanRescan(context.Background(), 0, 1, cutoff, func([]links.Link) error { return nil }); err == nil {
			t.Errorf("expected decode error for %v", bad)
		}
	}
}
