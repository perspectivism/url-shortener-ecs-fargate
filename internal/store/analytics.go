package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DedupeTTL is how long an event dedupe marker lives after it is written. It
// must cover the click queue's 4-day retention: any redelivery of a counted
// event arrives while its marker still exists (SPEC §5.2 says 3 days; a backlog
// could outlive that, see GATES.md).
const DedupeTTL = 4 * 24 * time.Hour

// StatsRetentionMonths is how long daily and dimension counters are kept
// (ADR-0025): 13 months allows year-over-year comparison. Counters outlive a
// deleted link and expire on the same schedule.
const StatsRetentionMonths = 13

// statsTTL is the DynamoDB TTL (epoch seconds) for counters of day date
// (yyyy-mm-dd): the start of that day plus 13 months, or 13 months after
// fallback if the date does not parse.
func statsTTL(date string, fallback time.Time) int64 {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		d = fallback
	}
	return d.AddDate(0, StatsRetentionMonths, 0).Unix()
}

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
	now     func() time.Time
}

// NewAnalytics returns a repository for table.
func NewAnalytics(db DynamoDB, table string, timeout time.Duration) *Analytics {
	return &Analytics{db: db, table: table, timeout: timeout, now: time.Now}
}

// WithClock replaces the clock (tests).
func (a *Analytics) WithClock(now func() time.Time) *Analytics {
	a.now = now
	return a
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func statsPK(code string) string { return "STATS#" + code }

func sv(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }

func nv(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}

// MaxTransactItems is DynamoDB's limit on items per TransactWriteItems call.
const MaxTransactItems = 100

// ClickResult is the outcome of recording one click.
type ClickResult int

// Click outcomes.
const (
	Recorded  ClickResult = iota // counted now
	Duplicate                    // already counted earlier (dedupe marker existed)
	Failed                       // not counted; the caller must let SQS redeliver it
)

// BatchResult reports RecordClicks outcomes in input order.
type BatchResult struct {
	Results      []ClickResult
	Transactions int // committed transactions (counter writes are amortized over these)
}

type counterKey struct{ sk, attr string }

// chunk is one transaction's worth of clicks for a single link: their dedupe
// markers plus the summed counter deltas. Deltas are grouped per item (sort
// key) because a transaction may contain only ONE operation per item: a
// day's human and bot clicks share the DAY# item and become one update with
// two ADD clauses.
type chunk struct {
	code   string
	idx    []int    // indices into the input
	items  []string // counter sort keys, in first-use order
	deltas map[string]map[string]int64
	ttl    map[string]int64 // counter sort key -> TTL (13 months after its day)
}

func counterKeys(c Click) []counterKey {
	if c.IsBot {
		return []counterKey{{"DAY#" + c.Date, "bot_clicks"}}
	}
	return []counterKey{
		{"DAY#" + c.Date, "clicks"},
		{dimSK("ref", c.Date, c.Referrer), "clicks"},
		{dimSK("country", c.Date, c.Country), "clicks"},
		{dimSK("device", c.Date, c.Device), "clicks"},
		{dimSK("browser", c.Date, c.Browser), "clicks"},
	}
}

// fits reports whether click c can join ch without exceeding the item limit.
func (ch *chunk) fits(c Click) bool {
	n := len(ch.idx) + len(ch.items) + 1
	for _, k := range counterKeys(c) {
		if _, ok := ch.deltas[k.sk]; !ok {
			n++
		}
	}
	return n <= MaxTransactItems
}

func (ch *chunk) add(i int, c Click) {
	ch.idx = append(ch.idx, i)
	for _, k := range counterKeys(c) {
		attrs, ok := ch.deltas[k.sk]
		if !ok {
			attrs = map[string]int64{}
			ch.deltas[k.sk] = attrs
			ch.items = append(ch.items, k.sk)
			ch.ttl[k.sk] = statsTTL(c.Date, c.TS)
		}
		attrs[k.attr]++
	}
}

func newChunk(code string) *chunk {
	return &chunk{code: code, deltas: map[string]map[string]int64{}, ttl: map[string]int64{}}
}

// RecordClicks implements access pattern 6 for a batch of clicks (ADR-0024).
// Clicks are grouped by link and packed into transactions of at most 100
// items. Each transaction holds the dedupe marker EVENT#{id} of every click it
// counts (put only if absent) plus ONE update per counter item carrying the
// summed delta. A transaction therefore commits exactly the clicks whose
// markers it creates: counting stays exactly-once although SQS is
// at-least-once, while a hot link's counters are written once per transaction
// instead of once per click.
//
// If DynamoDB cancels a transaction because some markers already exist, those
// clicks are duplicates; the transaction is rebuilt without them and retried.
// Conflicts and throttling are retried with backoff; clicks still not committed
// are reported as Failed (and err is set) so the caller leaves them for
// redelivery.
func (a *Analytics) RecordClicks(ctx context.Context, clicks []Click) (BatchResult, error) {
	res := BatchResult{Results: make([]ClickResult, len(clicks))}
	first := make(map[string]int, len(clicks))
	repeats := map[int]int{} // index -> index of the same event's first copy
	byCode := map[string][]int{}
	var codes []string
	for i, c := range clicks {
		if f, ok := first[c.EventID]; ok { // the same event twice in one batch
			repeats[i] = f
			continue
		}
		first[c.EventID] = i
		if _, ok := byCode[c.Code]; !ok {
			codes = append(codes, c.Code)
		}
		byCode[c.Code] = append(byCode[c.Code], i)
	}
	var firstErr error
	for _, code := range codes {
		ch := newChunk(code)
		for _, i := range byCode[code] {
			if !ch.fits(clicks[i]) {
				firstErr = errors.Join(firstErr, a.commit(ctx, clicks, ch, &res))
				ch = newChunk(code)
			}
			ch.add(i, clicks[i])
		}
		firstErr = errors.Join(firstErr, a.commit(ctx, clicks, ch, &res))
	}
	// A repeat shares its first copy's fate: a duplicate once that copy is
	// counted (or already was), but Failed while it is not, so neither copy is
	// acknowledged before the event is counted.
	for i, f := range repeats {
		if res.Results[f] == Failed {
			res.Results[i] = Failed
		} else {
			res.Results[i] = Duplicate
		}
	}
	if firstErr != nil {
		return res, fmt.Errorf("record clicks: %w", firstErr)
	}
	return res, nil
}

// commitAttempts bounds retries of conflicts and throttling per transaction.
const commitAttempts = 4

func (a *Analytics) commit(ctx context.Context, clicks []Click, ch *chunk, res *BatchResult) error {
	var lastErr error
	for attempt := 0; attempt < commitAttempts && len(ch.idx) > 0; {
		err := a.transact(ctx, clicks, ch)
		if err == nil {
			for _, i := range ch.idx {
				res.Results[i] = Recorded
			}
			res.Transactions++
			return nil
		}
		lastErr = err
		dups, retryable := cancellation(err, len(ch.idx))
		if len(dups) > 0 {
			// Rebuild without the duplicates (does not use up an attempt:
			// every round removes at least one click).
			next := newChunk(ch.code)
			for n, i := range ch.idx {
				if dups[n] {
					res.Results[i] = Duplicate
				} else {
					next.add(i, clicks[i])
				}
			}
			ch = next
			continue
		}
		if !retryable {
			break
		}
		attempt++
		if attempt < commitAttempts {
			select {
			case <-ctx.Done():
				attempt = commitAttempts
			case <-time.After(time.Duration(attempt*attempt) * 25 * time.Millisecond):
			}
		}
	}
	if len(ch.idx) == 0 {
		return nil
	}
	for _, i := range ch.idx {
		res.Results[i] = Failed
	}
	return lastErr
}

func (a *Analytics) transact(ctx context.Context, clicks []Click, ch *chunk) error {
	pk := statsPK(ch.code)
	items := make([]types.TransactWriteItem, 0, len(ch.idx)+len(ch.items))
	for _, i := range ch.idx {
		c := clicks[i]
		items = append(items, types.TransactWriteItem{Put: &types.Put{
			TableName: &a.table,
			Item: map[string]types.AttributeValue{
				"pk":  sv("EVENT#" + c.EventID),
				"sk":  sv(skMeta),
				"ttl": nv(later(a.now(), c.TS).Add(DedupeTTL).Unix()),
			},
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		}})
	}
	for _, sk := range ch.items {
		attrs := slices.Sorted(maps.Keys(ch.deltas[sk]))
		names := map[string]string{"#ttl": "ttl"}
		values := map[string]types.AttributeValue{":ttl": nv(ch.ttl[sk])}
		clauses := make([]string, len(attrs))
		for n, attr := range attrs {
			names[fmt.Sprintf("#a%d", n)] = attr
			values[fmt.Sprintf(":n%d", n)] = nv(ch.deltas[sk][attr])
			clauses[n] = fmt.Sprintf("#a%d :n%d", n, n)
		}
		items = append(items, types.TransactWriteItem{Update: &types.Update{
			TableName:                 &a.table,
			Key:                       map[string]types.AttributeValue{"pk": sv(pk), "sk": sv(sk)},
			UpdateExpression:          aws.String("ADD " + strings.Join(clauses, ", ") + " SET #ttl = :ttl"),
			ExpressionAttributeNames:  names,
			ExpressionAttributeValues: values,
		}})
	}
	tctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	// No ClientRequestToken: within its 10-minute window DynamoDB would report a
	// replayed transaction as success, hiding duplicates from the
	// WorkerDuplicateEvents metric. The EVENT# conditions alone guarantee
	// exactly-once counting (a lost-response SDK retry surfaces as duplicates).
	_, err := a.db.TransactWriteItems(tctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	return err
}

// cancellation inspects a TransactionCanceledException: which of the first
// nMarkers items (the dedupe markers) failed their condition, and whether the
// rest of the cancellation is worth retrying (conflict, throttling).
func cancellation(err error, nMarkers int) (dups map[int]bool, retryable bool) {
	var tce *types.TransactionCanceledException
	if !errors.As(err, &tce) {
		return nil, false
	}
	for n, r := range tce.CancellationReasons {
		switch code := aws.ToString(r.Code); {
		case code == "ConditionalCheckFailed" && n < nMarkers:
			if dups == nil {
				dups = map[int]bool{}
			}
			dups[n] = true
		case code == "TransactionConflict" || code == "ThrottlingError" || code == "ProvisionedThroughputExceeded":
			retryable = true
		}
	}
	return dups, retryable
}

func dimSK(dim, date, value string) string { return "DIM#" + dim + "#" + date + "#" + value }

// SetUniques writes the persisted unique-visitor count for a day (idempotent),
// with the same 13-month TTL as the day's counters.
func (a *Analytics) SetUniques(ctx context.Context, code, date string, n int64) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	_, err := a.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &a.table,
		Key:                       map[string]types.AttributeValue{"pk": sv(statsPK(code)), "sk": sv("DAY#" + date)},
		UpdateExpression:          aws.String("SET uniques = :n, #ttl = :ttl"),
		ExpressionAttributeNames:  map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":n": nv(n), ":ttl": nv(statsTTL(date, time.Now()))},
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
