package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
)

func click(bot bool) Click {
	return Click{EventID: "evt-1", Code: "abc", Date: "2026-10-14", TS: ts, IsBot: bot,
		Referrer: "news.example", Country: "DE", Device: "desktop", Browser: "Firefox"}
}

// clicksFor returns n human clicks on code with distinct event IDs.
func clicksFor(code string, n int, vary func(i int, c *Click)) []Click {
	out := make([]Click, n)
	for i := range out {
		c := click(false)
		c.Code, c.EventID = code, fmt.Sprintf("%s-%d", code, i)
		if vary != nil {
			vary(i, &c)
		}
		out[i] = c
	}
	return out
}

// txLog records every TransactWriteItems call and answers from a script.
type txLog struct {
	calls  []*dynamodb.TransactWriteItemsInput
	answer func(call int, in *dynamodb.TransactWriteItemsInput) error
}

// errSameItem mirrors DynamoDB's ValidationException for two operations on
// one item in a single transaction.
var errSameItem = errors.New("ValidationException: Transaction request cannot include multiple operations on one item")

func (l *txLog) db() *fakeDB {
	return &fakeDB{transact: func(in *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
		l.calls = append(l.calls, in)
		seen := map[string]bool{}
		for _, it := range in.TransactItems {
			var key map[string]types.AttributeValue
			if it.Put != nil {
				key = it.Put.Item
			} else {
				key = it.Update.Key
			}
			k := s(key["pk"]) + "|" + s(key["sk"])
			if seen[k] {
				return nil, errSameItem
			}
			seen[k] = true
		}
		if len(in.TransactItems) > MaxTransactItems {
			return nil, errors.New("ValidationException: too many items")
		}
		if l.answer != nil {
			if err := l.answer(len(l.calls)-1, in); err != nil {
				return nil, err
			}
		}
		return &dynamodb.TransactWriteItemsOutput{}, nil
	}}
}

// deltas sums counter deltas per sort key across the given transactions.
func deltas(t *testing.T, calls []*dynamodb.TransactWriteItemsInput) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, in := range calls {
		for _, it := range in.TransactItems {
			if it.Update == nil {
				continue
			}
			for name, attr := range it.Update.ExpressionAttributeNames {
				if name == "#ttl" {
					continue
				}
				var n int64
				val := ":n" + strings.TrimPrefix(name, "#a")
				num, ok := it.Update.ExpressionAttributeValues[val].(*types.AttributeValueMemberN)
				if !ok {
					t.Fatalf("%s is not a number", val)
				}
				if _, err := fmt.Sscan(num.Value, &n); err != nil {
					t.Fatal(err)
				}
				out[s(it.Update.Key["sk"])+"/"+attr] += n
			}
		}
	}
	return out
}

func markers(in *dynamodb.TransactWriteItemsInput) []string {
	var ids []string
	for _, it := range in.TransactItems {
		if it.Put != nil {
			ids = append(ids, s(it.Put.Item["pk"]))
		}
	}
	return ids
}

func canceled(codes ...string) error {
	reasons := make([]types.CancellationReason, len(codes))
	for i, c := range codes {
		reasons[i] = types.CancellationReason{Code: aws.String(c)}
	}
	return &types.TransactionCanceledException{CancellationReasons: reasons}
}

func TestRecordClicksSingle(t *testing.T) {
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	res, err := a.RecordClicks(context.Background(), []Click{click(false)})
	if err != nil || res.Transactions != 1 || res.Results[0] != Recorded || len(l.calls) != 1 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, len(l.calls))
	}
	in := l.calls[0]
	if len(in.TransactItems) != 6 || in.ClientRequestToken != nil {
		t.Fatalf("items=%d token=%v", len(in.TransactItems), in.ClientRequestToken)
	}
	put := in.TransactItems[0].Put
	if s(put.Item["pk"]) != "EVENT#evt-1" || *put.ConditionExpression != "attribute_not_exists(pk)" || s(put.Item["ttl"]) != fmt.Sprint(ts.Add(DedupeTTL).Unix()) {
		t.Fatalf("dedupe put = %+v ttl=%s", put, s(put.Item["ttl"]))
	}
	wantSKs := []string{"DAY#2026-10-14", "DIM#ref#2026-10-14#news.example", "DIM#country#2026-10-14#DE", "DIM#device#2026-10-14#desktop", "DIM#browser#2026-10-14#Firefox"}
	for i, sk := range wantSKs {
		u := in.TransactItems[i+1].Update
		if s(u.Key["pk"]) != "STATS#abc" || s(u.Key["sk"]) != sk || u.ExpressionAttributeNames["#a0"] != "clicks" || *u.UpdateExpression != "ADD #a0 :n0 SET #ttl = :ttl" {
			t.Errorf("item %d = %s %s %v", i+1, s(u.Key["pk"]), s(u.Key["sk"]), u.ExpressionAttributeNames)
		}
	}

	l.calls = nil
	if _, err := a.RecordClicks(context.Background(), []Click{click(true)}); err != nil || len(l.calls[0].TransactItems) != 2 ||
		l.calls[0].TransactItems[1].Update.ExpressionAttributeNames["#a0"] != "bot_clicks" {
		t.Fatalf("bot: %v", err)
	}
}

// Every counter item carries a TTL 13 months after its day (ADR-0025).
func TestCountersExpireAfterThirteenMonths(t *testing.T) {
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	if _, err := a.RecordClicks(context.Background(), []Click{click(false)}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprint(time.Date(2027, 11, 14, 0, 0, 0, 0, time.UTC).Unix()) // 2026-10-14 + 13 months
	for _, it := range l.calls[0].TransactItems {
		if it.Update == nil {
			continue
		}
		if it.Update.ExpressionAttributeNames["#ttl"] != "ttl" || s(it.Update.ExpressionAttributeValues[":ttl"]) != want {
			t.Fatalf("%s ttl = %s, want %s", s(it.Update.Key["sk"]), s(it.Update.ExpressionAttributeValues[":ttl"]), want)
		}
	}
	if got := statsTTL("not-a-date", ts); got != ts.AddDate(0, 13, 0).Unix() {
		t.Fatalf("fallback ttl = %d", got)
	}
}

// Human and bot clicks on the same link and day share the DAY# item: they
// must become ONE update with two ADD clauses (DynamoDB rejects two
// operations on one item in a transaction).
func TestRecordClicksHumanAndBotShareDayItem(t *testing.T) {
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	in := clicksFor("abc", 2, nil)
	bot := click(true)
	bot.EventID = "abc-bot"
	in = append(in, bot)
	res, err := a.RecordClicks(context.Background(), in)
	if err != nil || res.Transactions != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var day *types.Update
	for _, it := range l.calls[0].TransactItems {
		if it.Update != nil && s(it.Update.Key["sk"]) == "DAY#2026-10-14" {
			if day != nil {
				t.Fatal("two updates on the DAY# item")
			}
			day = it.Update
		}
	}
	if day == nil || *day.UpdateExpression != "ADD #a0 :n0, #a1 :n1 SET #ttl = :ttl" || day.ExpressionAttributeNames["#a0"] != "bot_clicks" || day.ExpressionAttributeNames["#a1"] != "clicks" {
		t.Fatalf("day update = %+v", day)
	}
	if d := deltas(t, l.calls); d["DAY#2026-10-14/clicks"] != 2 || d["DAY#2026-10-14/bot_clicks"] != 1 {
		t.Fatalf("deltas = %v", d)
	}
}

// A hot link: 80 clicks share their counter items, so ONE transaction holds
// 80 markers and 5 counter updates carrying +80 each.
func TestRecordClicksAggregatesHotLink(t *testing.T) {
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	res, err := a.RecordClicks(context.Background(), clicksFor("hot", 80, nil))
	if err != nil || res.Transactions != 1 || len(l.calls) != 1 || len(l.calls[0].TransactItems) != 85 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, len(l.calls))
	}
	d := deltas(t, l.calls)
	if d["DAY#2026-10-14/clicks"] != 80 || d["DIM#country#2026-10-14#DE/clicks"] != 80 || len(d) != 5 {
		t.Fatalf("deltas = %v", d)
	}
	for _, r := range res.Results {
		if r != Recorded {
			t.Fatalf("results = %v", res.Results)
		}
	}
}

// Transactions never exceed 100 items; every click is in exactly one
// transaction; links are never mixed within one transaction.
func TestRecordClicksPacksWithinItemLimit(t *testing.T) {
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	in := clicksFor("wide", 150, func(i int, c *Click) { c.Referrer = fmt.Sprintf("ref-%d.example", i) }) // a new counter per click
	in = append(in, clicksFor("other", 3, nil)...)
	bot := click(true)
	bot.Code, bot.EventID = "other", "other-bot"
	in = append(in, bot)
	res, err := a.RecordClicks(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, call := range l.calls {
		if len(call.TransactItems) > MaxTransactItems {
			t.Fatalf("transaction with %d items", len(call.TransactItems))
		}
		pks := map[string]bool{}
		for _, it := range call.TransactItems {
			if it.Update != nil {
				pks[s(it.Update.Key["pk"])] = true
			}
		}
		if len(pks) != 1 {
			t.Fatalf("transaction mixes links: %v", pks)
		}
		for _, m := range markers(call) {
			seen[m]++
		}
	}
	if len(seen) != len(in) || res.Transactions != len(l.calls) || res.Transactions < 3 {
		t.Fatalf("markers=%d clicks=%d transactions=%d", len(seen), len(in), res.Transactions)
	}
	d := deltas(t, l.calls)
	if d["DAY#2026-10-14/clicks"] != 153 || d["DAY#2026-10-14/bot_clicks"] != 1 {
		t.Fatalf("day deltas = %v / %v", d["DAY#2026-10-14/clicks"], d["DAY#2026-10-14/bot_clicks"])
	}
}

func TestRecordClicksDuplicates(t *testing.T) {
	// The same event twice in one batch: counted once, second is a duplicate.
	l := &txLog{}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	c := click(false)
	res, err := a.RecordClicks(context.Background(), []Click{c, c})
	if err != nil || res.Results[0] != Recorded || res.Results[1] != Duplicate || len(markers(l.calls[0])) != 1 {
		t.Fatalf("in-batch duplicate: %+v %v", res, err)
	}

	// DynamoDB reports markers 1 and 3 as existing: the transaction is rebuilt
	// without them and the counters carry only the 3 new clicks.
	l = &txLog{answer: func(call int, in *dynamodb.TransactWriteItemsInput) error {
		if call == 0 {
			codes := make([]string, len(in.TransactItems))
			for i := range codes {
				codes[i] = "None"
			}
			codes[1], codes[3] = "ConditionalCheckFailed", "ConditionalCheckFailed"
			return canceled(codes...)
		}
		return nil
	}}
	a = NewAnalytics(l.db(), "analytics", time.Second)
	res, err = a.RecordClicks(context.Background(), clicksFor("abc", 5, nil))
	if err != nil || len(l.calls) != 2 || res.Transactions != 1 {
		t.Fatalf("retry after duplicates: %+v err=%v calls=%d", res, err, len(l.calls))
	}
	want := []ClickResult{Recorded, Duplicate, Recorded, Duplicate, Recorded}
	for i := range want {
		if res.Results[i] != want[i] {
			t.Fatalf("results = %v, want %v", res.Results, want)
		}
	}
	if d := deltas(t, l.calls[1:]); d["DAY#2026-10-14/clicks"] != 3 {
		t.Fatalf("retried deltas = %v", d)
	}
	if got := markers(l.calls[1]); len(got) != 3 || got[0] != "EVENT#abc-0" || got[1] != "EVENT#abc-2" {
		t.Fatalf("retried markers = %v", got)
	}

	// Every click already counted: nothing left to write, no error.
	l = &txLog{answer: func(call int, in *dynamodb.TransactWriteItemsInput) error {
		return canceled("ConditionalCheckFailed", "ConditionalCheckFailed", "None", "None", "None", "None", "None")
	}}
	a = NewAnalytics(l.db(), "analytics", time.Second)
	res, err = a.RecordClicks(context.Background(), clicksFor("abc", 2, nil))
	if err != nil || res.Transactions != 0 || res.Results[0] != Duplicate || res.Results[1] != Duplicate || len(l.calls) != 1 {
		t.Fatalf("all duplicates: %+v %v", res, err)
	}
}

func TestRecordClicksRetriesAndFailures(t *testing.T) {
	// A conflict on a shared counter is retried, then succeeds.
	l := &txLog{answer: func(call int, in *dynamodb.TransactWriteItemsInput) error {
		if call == 0 {
			return canceled("None", "TransactionConflict", "None", "None", "None", "None")
		}
		return nil
	}}
	a := NewAnalytics(l.db(), "analytics", time.Second)
	res, err := a.RecordClicks(context.Background(), []Click{click(false)})
	if err != nil || res.Results[0] != Recorded || len(l.calls) != 2 {
		t.Fatalf("conflict retry: %+v %v calls=%d", res, err, len(l.calls))
	}

	// Persistent throttling: bounded attempts, then Failed with an error.
	l = &txLog{answer: func(int, *dynamodb.TransactWriteItemsInput) error {
		return canceled("None", "ThrottlingError")
	}}
	a = NewAnalytics(l.db(), "analytics", time.Second)
	res, err = a.RecordClicks(context.Background(), []Click{click(true)})
	if err == nil || res.Results[0] != Failed || len(l.calls) != commitAttempts {
		t.Fatalf("throttled: %+v %v calls=%d", res, err, len(l.calls))
	}

	// Non-retryable errors fail at once and propagate; other links still commit.
	l = &txLog{answer: func(_ int, in *dynamodb.TransactWriteItemsInput) error {
		if s(in.TransactItems[len(in.TransactItems)-1].Update.Key["pk"]) == "STATS#bad" {
			return errBoom
		}
		return nil
	}}
	a = NewAnalytics(l.db(), "analytics", time.Second)
	in := append(clicksFor("bad", 2, nil), clicksFor("good", 2, nil)...)
	res, err = a.RecordClicks(context.Background(), in)
	if !errors.Is(err, errBoom) || res.Results[0] != Failed || res.Results[1] != Failed || res.Results[2] != Recorded || res.Results[3] != Recorded || res.Transactions != 1 {
		t.Fatalf("partial failure: %+v %v", res, err)
	}

	// A cancelled context stops the backoff.
	ctx, cancel := context.WithCancel(context.Background())
	l = &txLog{answer: func(int, *dynamodb.TransactWriteItemsInput) error {
		cancel()
		return canceled("None", "TransactionConflict")
	}}
	a = NewAnalytics(l.db(), "analytics", time.Second)
	if res, err := a.RecordClicks(ctx, []Click{click(true)}); err == nil || res.Results[0] != Failed || len(l.calls) != 1 {
		t.Fatalf("cancelled: %+v %v calls=%d", res, err, len(l.calls))
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
	if *upd.UpdateExpression != "SET uniques = :n, #ttl = :ttl" || s(upd.ExpressionAttributeValues[":ttl"]) != fmt.Sprint(time.Date(2027, 11, 13, 0, 0, 0, 0, time.UTC).Unix()) {
		t.Fatalf("uniques ttl: %s %s", *upd.UpdateExpression, s(upd.ExpressionAttributeValues[":ttl"]))
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
