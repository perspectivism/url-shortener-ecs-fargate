//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/shortener/url-shortener-ecs-fargate/internal/events"
)

// --- helpers -------------------------------------------------------------

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		ok, state := cond()
		if ok {
			return
		}
		last = state
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s (last: %s)", timeout, what, last)
}

func stats(t *testing.T, owner, code string) map[string]any {
	t.Helper()
	r := call(t, http.MethodGet, env.api.url+"/api/v1/links/"+code+"/stats", env.keys[owner], "", nil)
	expectStatus(t, r, http.StatusOK, "")
	return r.Body
}

func num(m map[string]any, k string) int64 {
	f, _ := m[k].(float64)
	return int64(f)
}

func waitStats(t *testing.T, owner, code string, clicks, bots int64) map[string]any {
	t.Helper()
	var s map[string]any
	waitFor(t, 60*time.Second, "stats of "+code, func() (bool, string) {
		s = stats(t, owner, code)
		return num(s, "total_clicks") == clicks && num(s, "total_bot_clicks") == bots,
			fmt.Sprintf("clicks=%d bots=%d want %d/%d", num(s, "total_clicks"), num(s, "total_bot_clicks"), clicks, bots)
	})
	return s
}

// s3Events downloads every raw event object and returns events for code.
func s3Events(t *testing.T, code string) []events.ClickEvent {
	t.Helper()
	ctx := context.Background()
	var out []events.ClickEvent
	p := s3.NewListObjectsV2Paginator(env.s3, &s3.ListObjectsV2Input{Bucket: aws.String(env.base["EVENTS_BUCKET"]), Prefix: aws.String("events/")})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasPrefix(key, "events/dt=") || !strings.Contains(key, "/hour=") || !strings.HasSuffix(key, ".json.gz") {
				t.Fatalf("unexpected object key %s", key)
			}
			res, err := env.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(env.base["EVENTS_BUCKET"]), Key: obj.Key})
			if err != nil {
				t.Fatal(err)
			}
			zr, err := gzip.NewReader(res.Body)
			if err != nil {
				t.Fatalf("%s is not gzip: %v", key, err)
			}
			sc := bufio.NewScanner(zr)
			for sc.Scan() {
				var e events.ClickEvent
				if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
					t.Fatalf("bad NDJSON line in %s: %v", key, err)
				}
				if e.Code == code {
					out = append(out, e)
				}
			}
			res.Body.Close()
		}
	}
	return out
}

// waitS3Events waits until the deduplicated (by event_id) S3 events for code
// reach want, and returns them.
func waitS3Events(t *testing.T, code string, want int) []events.ClickEvent {
	t.Helper()
	var uniq []events.ClickEvent
	waitFor(t, 60*time.Second, "S3 raw events for "+code, func() (bool, string) {
		seen := map[string]bool{}
		uniq = uniq[:0]
		for _, e := range s3Events(t, code) {
			if !seen[e.EventID] {
				seen[e.EventID] = true
				uniq = append(uniq, e)
			}
		}
		return len(uniq) == want, fmt.Sprintf("%d unique events", len(uniq))
	})
	return uniq
}

func queueDrained(t *testing.T, url string) bool {
	t.Helper()
	out, err := env.sqs.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameApproximateNumberOfMessages, sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible},
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.Attributes["ApproximateNumberOfMessages"] == "0" && out.Attributes["ApproximateNumberOfMessagesNotVisible"] == "0"
}

// emfSum sums an EMF metric across all documents in a service log.
func emfSum(t *testing.T, s *service, metric string) float64 {
	t.Helper()
	s.syncLogs()
	f, err := os.Open(s.logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var total float64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), []byte(`"_aws"`)) {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(sc.Bytes(), &doc) == nil {
			if v, ok := doc[metric].(float64); ok {
				total += v
			}
		}
	}
	return total
}

type visit struct{ ua, ref, country string }

func get(t *testing.T, base, code string, v visit) {
	t.Helper()
	hdr := map[string]string{"User-Agent": v.ua}
	if v.ref != "" {
		hdr["Referer"] = v.ref
	}
	if v.country != "" {
		hdr["CloudFront-Viewer-Country"] = v.country
	}
	r := call(t, http.MethodGet, base+"/"+code, "", "", hdr)
	expectStatus(t, r, http.StatusFound, "")
}

const firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0 visitor-%d"

// --- tests ---------------------------------------------------------------

// TestPipelineExactCounts: 500 clicks across 3 links (humans with distinct
// visitors, plus bots) → stats match exactly; uniques exclude bots; S3 holds
// every event; redelivering every message changes nothing.
func TestPipelineExactCounts(t *testing.T) {
	type plan struct {
		code                   string
		humans, bots, visitors int
	}
	plans := []*plan{{humans: 180, bots: 20, visitors: 12}, {humans: 140, bots: 10, visitors: 7}, {humans: 140, bots: 10, visitors: 1}}
	for i, p := range plans {
		p.code = str(createLink(t, "alice", fmt.Sprintf(`{"url":"https://example.com/pipeline/%d"}`, i)), "code")
	}
	countries := []string{"US", "DE", "JP"}
	refs := []string{"https://news.ycombinator.com/item?id=1", "https://t.co/abc", ""}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, p := range plans {
		for i := range p.humans + p.bots {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				v := visit{ua: fmt.Sprintf(firefox, i%p.visitors), ref: refs[i%3], country: countries[i%3]}
				if i >= p.humans {
					v = visit{ua: "curl/8.5.0", country: "US"}
				}
				get(t, env.redirect.url, p.code, v)
			}()
		}
	}
	wg.Wait()

	total := 0
	for _, p := range plans {
		s := waitStats(t, "alice", p.code, int64(p.humans), int64(p.bots))
		daily, _ := s["daily"].([]any)
		today, _ := daily[len(daily)-1].(map[string]any)
		if num(today, "uniques") != int64(p.visitors) {
			t.Errorf("%s uniques = %d, want %d (bots excluded)", p.code, num(today, "uniques"), p.visitors)
		}
		top, _ := s["top"].(map[string]any)
		for _, dim := range []string{"referrers", "countries", "devices", "browsers"} {
			entries, _ := top[dim].([]any)
			var sum int64
			for _, e := range entries {
				m, _ := e.(map[string]any)
				sum += num(m, "clicks")
			}
			if sum != int64(p.humans) {
				t.Errorf("%s %s dimension sums to %d, want %d", p.code, dim, sum, p.humans)
			}
		}
		total += p.humans + p.bots
	}
	if total != 500 {
		t.Fatalf("sent %d clicks, want 500", total)
	}

	// Raw events in S3: deduplicating by event_id yields the exact count.
	var all []events.ClickEvent
	for _, p := range plans {
		all = append(all, waitS3Events(t, p.code, p.humans+p.bots)...)
	}

	// Redeliver every event as a new SQS message: counts must not change.
	ctx := context.Background()
	for start := 0; start < len(all); start += 10 {
		var entries []sqstypes.SendMessageBatchRequestEntry
		for i, e := range all[start:min(start+10, len(all))] {
			b, _ := json.Marshal(e)
			entries = append(entries, sqstypes.SendMessageBatchRequestEntry{Id: aws.String(fmt.Sprint(i)), MessageBody: aws.String(string(b))})
		}
		if _, err := env.sqs.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(env.queueURL), Entries: entries}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 60*time.Second, "redelivered messages to be consumed", func() (bool, string) {
		return queueDrained(t, env.queueURL) && emfSum(t, env.worker, "WorkerDuplicateEvents") >= 500, "queue not drained"
	})
	for _, p := range plans {
		s := stats(t, "alice", p.code)
		if num(s, "total_clicks") != int64(p.humans) || num(s, "total_bot_clicks") != int64(p.bots) {
			t.Fatalf("%s changed after redelivery: %v/%v", p.code, s["total_clicks"], s["total_bot_clicks"])
		}
	}
}

// TestSIGTERMFlushesBufferedClicks: events buffered in the producer (60 s
// flush interval) are sent when the redirect service receives SIGTERM.
func TestSIGTERMFlushesBufferedClicks(t *testing.T) {
	code := str(createLink(t, "alice", `{"url":"https://example.com/sigterm"}`), "code")
	// One sender, 60 s flush: full batches of 10 go out immediately, the last
	// 7 events stay buffered until shutdown.
	r, err := env.start("redirect-sigterm", "redirect", "", map[string]string{"CLICK_FLUSH_INTERVAL": "60s", "CLICK_SENDERS": "1"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 37 {
		get(t, r.url, code, visit{ua: fmt.Sprintf(firefox, i)})
	}
	waitStats(t, "alice", code, 30, 0)
	time.Sleep(3 * time.Second) // worker flushes every 2 s; nothing more may arrive
	if s := stats(t, "alice", code); num(s, "total_clicks") != 30 {
		t.Fatalf("7 events should still be buffered, got %v clicks", s["total_clicks"])
	}
	if err := r.stop(25 * time.Second); err != nil {
		t.Fatalf("redirect did not exit cleanly on SIGTERM: %v", err)
	}
	waitStats(t, "alice", code, 37, 0)
}

// TestQueueDownStillRedirects: SQS unreachable → 302s keep flowing and
// ClickEventsDropped is counted.
func TestQueueDownStillRedirects(t *testing.T) {
	code := str(createLink(t, "alice", `{"url":"https://example.com/queue-down"}`), "code")
	r, err := env.start("redirect-nosqs", "redirect", "", map[string]string{"AWS_ENDPOINT_URL_SQS": "http://127.0.0.1:1", "CLICK_FLUSH_INTERVAL": "100ms"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.stop(15 * time.Second) }()
	for i := range 10 {
		get(t, r.url, code, visit{ua: fmt.Sprintf(firefox, i)})
	}
	waitFor(t, 20*time.Second, "ClickEventsDropped", func() (bool, string) {
		n := emfSum(t, r, "ClickEventsDropped")
		return n >= 10, fmt.Sprintf("dropped=%v", n)
	})
}

// TestValkeyDownStillRedirects: cache unreachable → redirects resolve via
// DynamoDB and CacheErrors is counted.
func TestValkeyDownStillRedirects(t *testing.T) {
	code := str(createLink(t, "alice", `{"url":"https://example.com/valkey-down"}`), "code")
	r, err := env.start("redirect-novalkey", "redirect", "", map[string]string{"CACHE_ADDR": "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.stop(15 * time.Second) }()
	for range 3 {
		res := call(t, http.MethodGet, r.url+"/"+code, "", "", nil)
		expectStatus(t, res, http.StatusFound, "")
		if res.Header.Get("Location") != "https://example.com/valkey-down" {
			t.Fatalf("location = %s", res.Header.Get("Location"))
		}
	}
	expectStatus(t, call(t, http.MethodGet, r.url+"/unknown"+env.runID, "", "", nil), http.StatusNotFound, "not_found")
	waitFor(t, 15*time.Second, "CacheErrors metric", func() (bool, string) {
		n := emfSum(t, r, "CacheErrors")
		return n >= 3, fmt.Sprintf("cache errors=%v", n)
	})
}

// TestPoisonMessageGoesToDLQ: an unparseable message reaches the DLQ after
// maxReceiveCount and the worker keeps processing other events.
func TestPoisonMessageGoesToDLQ(t *testing.T) {
	ctx := context.Background()
	body := "poison-" + env.runID + " {not json"
	if _, err := env.sqs.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(env.queueURL), MessageBody: aws.String(body)}); err != nil {
		t.Fatal(err)
	}
	found := false
	waitFor(t, 90*time.Second, "poison message in DLQ", func() (bool, string) {
		out, err := env.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: aws.String(env.dlqURL), MaxNumberOfMessages: 10, WaitTimeSeconds: 1, VisibilityTimeout: 1})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range out.Messages {
			if aws.ToString(m.Body) == body {
				found = true
			}
		}
		return found, "not yet in DLQ"
	})
	if !env.worker.alive() {
		t.Fatal("worker exited")
	}
	code := str(createLink(t, "alice", `{"url":"https://example.com/after-poison"}`), "code")
	get(t, env.redirect.url, code, visit{ua: fmt.Sprintf(firefox, 1)})
	waitStats(t, "alice", code, 1, 0)
	if n := emfSum(t, env.worker, "WorkerPoisonMessages"); n < 5 {
		t.Fatalf("WorkerPoisonMessages = %v, want >= 5 receives", n)
	}
}

func runNightly(t *testing.T, date string) string {
	t.Helper()
	extra := map[string]string{"NIGHTLY_DATE": date, "SCANNER": "fake", "SCAN_SEGMENTS": "4"}
	cmd := exec.Command(filepath.Join(env.binDir, "nightly"))
	cmd.Env = env.environ(extra)
	if containerMode() {
		args := composeArgs("it-"+env.runID+"-nightly-"+fmt.Sprint(time.Now().UnixNano()), "nightly", env.environ(extra))
		cmd = exec.Command("docker", append(args, "--rm", "nightly")...)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("nightly failed: %v\n%s", err, out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, `"msg":"nightly summary"`) {
			return line
		}
	}
	t.Fatalf("no summary line in nightly output:\n%s", out.String())
	return ""
}

func summaryField(t *testing.T, line, field string) float64 {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	v, _ := m[field].(float64)
	return v
}

// TestNightly: uniques are persisted; a destination the (fake) scanner flags
// is disabled and its cache entry removed; re-running is a no-op.
func TestNightly(t *testing.T) {
	ctx := context.Background()
	today := time.Now().UTC().Format(time.DateOnly)

	code := str(createLink(t, "alice", `{"url":"https://example.com/nightly-uniques"}`), "code")
	for i := range 8 {
		get(t, env.redirect.url, code, visit{ua: fmt.Sprintf(firefox, i%4)})
	}
	get(t, env.redirect.url, code, visit{ua: "Googlebot/2.1"})
	waitStats(t, "alice", code, 8, 1)

	// Created with scanning disabled → pending; the fake scanner flags it tonight.
	r := call(t, http.MethodPost, env.apiNoScan.url+"/api/v1/links", env.keys["alice"], `{"url":"https://malware.testing.google.test/testing/malware/`+env.runID+`"}`, nil)
	expectStatus(t, r, http.StatusCreated, "")
	if r.Body["scan_status"] != "pending" {
		t.Fatalf("scan_status = %v", r.Body["scan_status"])
	}
	bad := str(r.Body, "code")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+bad, "", "", nil), http.StatusFound, "")
	if n, _ := env.valkey.Exists(ctx, "l:"+bad).Result(); n != 1 {
		t.Fatal("expected a cached entry before the nightly run")
	}

	first := runNightly(t, today)
	if summaryField(t, first, "disabled") < 1 || summaryField(t, first, "uniques_written") < 1 {
		t.Fatalf("summary = %s", first)
	}
	item, err := env.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(env.base["ANALYTICS_TABLE"]),
		Key:       map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: "STATS#" + code}, "sk": &ddbtypes.AttributeValueMemberS{Value: "DAY#" + today}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := item.Item["uniques"].(*ddbtypes.AttributeValueMemberN); !ok || u.Value != "4" {
		t.Fatalf("persisted uniques = %v, want 4 (bot excluded)", item.Item["uniques"])
	}
	if n, _ := env.valkey.Exists(ctx, "l:"+bad).Result(); n != 0 {
		t.Fatal("cache entry of the disabled link must be removed")
	}
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+bad, "", "", nil), http.StatusGone, "gone")
	link := call(t, http.MethodGet, env.api.url+"/api/v1/links/"+bad, env.keys["alice"], "", nil)
	if link.Body["status"] != "disabled" || link.Body["disabled_reason"] != "unsafe_url" || link.Body["scan_status"] != "flagged" {
		t.Fatalf("link after nightly = %s", link.Raw)
	}

	second := runNightly(t, today)
	if summaryField(t, second, "disabled") != 0 || summaryField(t, second, "flagged") != 0 || summaryField(t, second, "scanned") != 0 {
		t.Fatalf("re-run must be a no-op: %s", second)
	}
	item, _ = env.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(env.base["ANALYTICS_TABLE"]),
		Key:       map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: "STATS#" + code}, "sk": &ddbtypes.AttributeValueMemberS{Value: "DAY#" + today}},
	})
	if u, _ := item.Item["uniques"].(*ddbtypes.AttributeValueMemberN); u == nil || u.Value != "4" {
		t.Fatal("uniques changed on re-run")
	}
}
