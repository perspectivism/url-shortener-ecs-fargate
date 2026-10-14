package store

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"

	"github.com/shortener/url-shortener-ecs-fargate/internal/auth"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
)

// fakeDB records requests and returns canned responses. Real DynamoDB
// semantics are covered by the integration tests against DynamoDB Local.
type fakeDB struct {
	getItem    func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	putItem    func(*dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	updateItem func(*dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
	query      func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	scan       func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error)
	transact   func(*dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error)
	deadlines  []bool
}

func (f *fakeDB) note(ctx context.Context) {
	_, ok := ctx.Deadline()
	f.deadlines = append(f.deadlines, ok)
}

func (f *fakeDB) GetItem(ctx context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.note(ctx)
	return f.getItem(in)
}

func (f *fakeDB) PutItem(ctx context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.note(ctx)
	return f.putItem(in)
}

func (f *fakeDB) UpdateItem(ctx context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.note(ctx)
	return f.updateItem(in)
}

func (f *fakeDB) Query(ctx context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.note(ctx)
	return f.query(in)
}

func (f *fakeDB) Scan(ctx context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	f.note(ctx)
	return f.scan(in)
}

func (f *fakeDB) TransactWriteItems(ctx context.Context, in *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	f.note(ctx)
	return f.transact(in)
}

var (
	ts      = time.Date(2026, 10, 14, 12, 0, 0, 123_000_000, time.UTC)
	errBoom = errors.New("boom")
	ccf     = &types.ConditionalCheckFailedException{Message: aws.String("condition")}
)

func sampleLink() links.Link {
	exp := ts.Add(48 * time.Hour).Truncate(time.Second)
	return links.Link{
		Code: "abc123", LongURL: "https://example.com", OwnerID: "alice", Status: links.StatusActive,
		CreatedAt: ts, UpdatedAt: ts, ExpiresAt: &exp, ScanStatus: links.ScanClean, LastScannedAt: &ts,
	}
}

func s(av types.AttributeValue) string {
	if v, ok := av.(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	if v, ok := av.(*types.AttributeValueMemberN); ok {
		return v.Value
	}
	return ""
}

func TestItemRoundTrip(t *testing.T) {
	l := sampleLink()
	it := toItem(l)
	if it.PK != "LINK#abc123" || it.SK != "META" || it.GSI1PK != "OWNER#alice" ||
		it.GSI1SK != "CREATED#2026-10-14T12:00:00.123Z#abc123" || it.TTL != l.ExpiresAt.Add(TTLGrace).Unix() {
		t.Fatalf("item = %+v", it)
	}
	back, err := fromItem(it)
	if err != nil || back.Code != l.Code || !back.CreatedAt.Equal(l.CreatedAt) || !back.ExpiresAt.Equal(*l.ExpiresAt) || !back.LastScannedAt.Equal(ts) {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	noExp := l
	noExp.ExpiresAt, noExp.LastScannedAt = nil, nil
	if it := toItem(noExp); it.TTL != 0 || it.ExpiresAt != "" {
		t.Fatal("no expiry must mean no ttl")
	}
	for _, bad := range []linkItem{
		{CreatedAt: "x"},
		{CreatedAt: it.CreatedAt, UpdatedAt: "x"},
		{CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt, ExpiresAt: "x"},
		{CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt, LastScannedAt: "x"},
	} {
		if _, err := fromItem(bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}

func TestGetLink(t *testing.T) {
	item, _ := attributevalue.MarshalMap(toItem(sampleLink()))
	db := &fakeDB{getItem: func(in *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		if s(in.Key["pk"]) != "LINK#abc123" || s(in.Key["sk"]) != "META" || *in.TableName != "links" || in.ConsistentRead != nil {
			t.Fatalf("bad request %+v", in)
		}
		return &dynamodb.GetItemOutput{Item: item}, nil
	}}
	st := NewLinks(db, "links", 200*time.Millisecond)
	l, err := st.GetLink(context.Background(), "abc123")
	if err != nil || l.LongURL != "https://example.com" || !db.deadlines[0] {
		t.Fatalf("get: %+v %v (deadline %v)", l, err, db.deadlines)
	}
	db.getItem = func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) { return &dynamodb.GetItemOutput{}, nil }
	if _, err := st.GetLink(context.Background(), "x"); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("missing item must be ErrNotFound")
	}
	db.getItem = func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) { return nil, errBoom }
	if _, err := st.GetLink(context.Background(), "x"); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}
	db.getItem = func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		return &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{"created_at": &types.AttributeValueMemberS{Value: "bad"}}}, nil
	}
	if _, err := st.GetLink(context.Background(), "x"); err == nil {
		t.Fatal("decode errors propagate")
	}
	db.getItem = func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
		return &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{"custom": &types.AttributeValueMemberS{Value: "notbool"}}}, nil
	}
	if _, err := st.GetLink(context.Background(), "x"); err == nil {
		t.Fatal("unmarshal errors propagate")
	}
}

func TestCreateAndUpdateLink(t *testing.T) {
	var last *dynamodb.PutItemInput
	result := error(nil)
	db := &fakeDB{putItem: func(in *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
		last = in
		return &dynamodb.PutItemOutput{}, result
	}}
	st := NewLinks(db, "links", time.Second)
	ctx := context.Background()
	if err := st.CreateLink(ctx, sampleLink()); err != nil || *last.ConditionExpression != "attribute_not_exists(pk)" || s(last.Item["gsi1pk"]) != "OWNER#alice" {
		t.Fatalf("create: %v %+v", err, last)
	}
	result = ccf
	if err := st.CreateLink(ctx, sampleLink()); !errors.Is(err, links.ErrCodeTaken) {
		t.Fatalf("condition failure = %v", err)
	}
	result = errBoom
	if err := st.CreateLink(ctx, sampleLink()); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}

	result = nil
	prev := ts.Add(-time.Minute)
	if err := st.UpdateLink(ctx, sampleLink(), prev); err != nil ||
		*last.ConditionExpression != "attribute_exists(pk) AND updated_at = :prev" ||
		s(last.ExpressionAttributeValues[":prev"]) != fmtTime(prev) {
		t.Fatalf("update: %v %+v", err, last)
	}
	result = ccf
	if err := st.UpdateLink(ctx, sampleLink(), prev); !errors.Is(err, links.ErrConflict) {
		t.Fatalf("condition failure = %v", err)
	}
	result = errBoom
	if err := st.UpdateLink(ctx, sampleLink(), prev); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}
}

func TestListLinksAndCursor(t *testing.T) {
	l := sampleLink()
	item, _ := attributevalue.MarshalMap(toItem(l))
	lek := map[string]types.AttributeValue{"pk": item["pk"], "sk": item["sk"], "gsi1pk": item["gsi1pk"], "gsi1sk": item["gsi1sk"]}
	var last *dynamodb.QueryInput
	db := &fakeDB{query: func(in *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		last = in
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}, LastEvaluatedKey: lek}, nil
	}}
	st := NewLinks(db, "links", time.Second)
	page, err := st.ListLinks(context.Background(), "alice", 500, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Code != "abc123" || page.NextCursor == "" {
		t.Fatalf("page = %+v %v", page, err)
	}
	if *last.IndexName != "gsi1" || *last.ScanIndexForward || *last.Limit != 100 || s(last.ExpressionAttributeValues[":pk"]) != "OWNER#alice" || last.ExclusiveStartKey != nil {
		t.Fatalf("query = %+v", last)
	}
	// Following the cursor sends it back as ExclusiveStartKey.
	if _, err := st.ListLinks(context.Background(), "alice", 0, page.NextCursor); err != nil || s(last.ExclusiveStartKey["gsi1sk"]) != s(lek["gsi1sk"]) || *last.Limit != 1 {
		t.Fatalf("cursor follow: %v %+v", err, last)
	}
	// Another owner's cursor is rejected (IDOR via forged cursor).
	if _, err := st.ListLinks(context.Background(), "mallory", 10, page.NextCursor); !errors.Is(err, links.ErrInvalidCursor) {
		t.Fatalf("foreign cursor = %v", err)
	}
	for _, bad := range []string{"%%%", base64.RawURLEncoding.EncodeToString([]byte(`{"pk":"LINK#a","x":1}`)), base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 2000))),
		base64.RawURLEncoding.EncodeToString([]byte(`{"pk":"APIKEY#a","sk":"META","gsi1pk":"OWNER#alice","gsi1sk":"CREATED#x"}`))} {
		if _, err := st.ListLinks(context.Background(), "alice", 10, bad); !errors.Is(err, links.ErrInvalidCursor) {
			t.Errorf("cursor %q = %v", bad, err)
		}
	}
	// Last page has no cursor; errors propagate.
	db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) { return &dynamodb.QueryOutput{}, nil }
	if p, err := st.ListLinks(context.Background(), "alice", 10, ""); err != nil || p.NextCursor != "" || len(p.Items) != 0 {
		t.Fatal("empty page")
	}
	db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) { return nil, errBoom }
	if _, err := st.ListLinks(context.Background(), "alice", 10, ""); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}
	for _, bad := range []map[string]types.AttributeValue{
		{"pk": &types.AttributeValueMemberS{Value: "LINK#x"}, "created_at": &types.AttributeValueMemberS{Value: "bad"}},
		{"pk": &types.AttributeValueMemberS{Value: "LINK#x"}, "created_at": item["created_at"], "expires_at": &types.AttributeValueMemberS{Value: "bad"}},
		{"pk": &types.AttributeValueMemberN{Value: "1"}},
	} {
		db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{bad}}, nil
		}
		if _, err := st.ListLinks(context.Background(), "alice", 10, ""); err == nil {
			t.Errorf("expected decode error for %v", bad)
		}
	}
	db.query = func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		return &dynamodb.QueryOutput{LastEvaluatedKey: map[string]types.AttributeValue{"pk": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{}}}}, nil
	}
	if _, err := st.ListLinks(context.Background(), "alice", 10, ""); err == nil {
		t.Fatal("bad LastEvaluatedKey must error")
	}
}

func TestClaimIDBlock(t *testing.T) {
	var last *dynamodb.UpdateItemInput
	resp := &dynamodb.UpdateItemOutput{Attributes: map[string]types.AttributeValue{"value": &types.AttributeValueMemberN{Value: "3000"}}}
	var respErr error
	db := &fakeDB{updateItem: func(in *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		last = in
		return resp, respErr
	}}
	st := NewLinks(db, "links", time.Second)
	v, err := st.ClaimIDBlock(context.Background(), 1000)
	if err != nil || v != 3000 || s(last.Key["pk"]) != "COUNTER#link_id" || *last.UpdateExpression != "ADD #v :n" ||
		s(last.ExpressionAttributeValues[":n"]) != "1000" || last.ReturnValues != types.ReturnValueUpdatedNew {
		t.Fatalf("claim: %d %v %+v", v, err, last)
	}
	for _, r := range []*dynamodb.UpdateItemOutput{
		{Attributes: map[string]types.AttributeValue{}},
		{Attributes: map[string]types.AttributeValue{"value": &types.AttributeValueMemberN{Value: "x"}}},
	} {
		resp = r
		if _, err := st.ClaimIDBlock(context.Background(), 1); err == nil {
			t.Errorf("expected error for %+v", r)
		}
	}
	respErr = errBoom
	if _, err := st.ClaimIDBlock(context.Background(), 1); !errors.Is(err, errBoom) {
		t.Fatal("errors propagate")
	}
}

func TestAPIKeys(t *testing.T) {
	ctx := context.Background()
	stored := map[string]types.AttributeValue{}
	var update *dynamodb.UpdateItemInput
	var updateErr, putErr error
	db := &fakeDB{
		putItem: func(in *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			stored = in.Item
			return &dynamodb.PutItemOutput{}, putErr
		},
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			if len(stored) == 0 {
				return &dynamodb.GetItemOutput{}, nil
			}
			return &dynamodb.GetItemOutput{Item: stored}, nil
		},
		updateItem: func(in *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			update = in
			return &dynamodb.UpdateItemOutput{}, updateErr
		},
	}
	st := NewLinks(db, "links", time.Second)
	if _, err := st.GetAPIKey(ctx, "h"); !errors.Is(err, auth.ErrKeyNotFound) {
		t.Fatal("missing key")
	}
	rec := auth.KeyRecord{Hash: "h1", OwnerID: "alice", Name: "ci", Scopes: []string{"links:read", "links:write"}, Status: auth.StatusActive, CreatedAt: ts}
	if err := st.PutAPIKey(ctx, rec); err != nil || s(stored["pk"]) != "APIKEY#h1" {
		t.Fatalf("put: %v", err)
	}
	if _, ok := stored["scopes"].(*types.AttributeValueMemberSS); !ok {
		t.Fatal("scopes must be a string set")
	}
	got, err := st.GetAPIKey(ctx, "h1")
	if err != nil || got.OwnerID != "alice" || len(got.Scopes) != 2 || !got.CreatedAt.Equal(ts) {
		t.Fatalf("get: %+v %v", got, err)
	}

	if err := st.RevokeAPIKey(ctx, "h1"); err != nil || s(update.ExpressionAttributeValues[":revoked"]) != "revoked" {
		t.Fatalf("revoke: %v", err)
	}
	updateErr = ccf
	if err := st.RevokeAPIKey(ctx, "nope"); !errors.Is(err, auth.ErrKeyNotFound) {
		t.Fatal("revoke missing")
	}
	if err := st.TouchAPIKey(ctx, "h1", "2026-10-14"); err != nil {
		t.Fatal("touch with unchanged day must be a no-op success")
	}
	updateErr = errBoom
	if err := st.TouchAPIKey(ctx, "h1", "2026-10-14"); !errors.Is(err, errBoom) {
		t.Fatal("touch errors propagate")
	}
	if err := st.RevokeAPIKey(ctx, "h1"); !errors.Is(err, errBoom) {
		t.Fatal("revoke errors propagate")
	}
	putErr = errBoom
	if err := st.PutAPIKey(ctx, rec); !errors.Is(err, errBoom) {
		t.Fatal("put errors propagate")
	}
	stored = map[string]types.AttributeValue{"created_at": &types.AttributeValueMemberS{Value: "bad"}}
	if _, err := st.GetAPIKey(ctx, "h1"); err == nil {
		t.Fatal("decode error")
	}
	stored = map[string]types.AttributeValue{"scopes": &types.AttributeValueMemberN{Value: "1"}}
	if _, err := st.GetAPIKey(ctx, "h1"); err == nil {
		t.Fatal("unmarshal error")
	}
	db.getItem = func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) { return nil, errBoom }
	if _, err := st.GetAPIKey(ctx, "h1"); !errors.Is(err, errBoom) {
		t.Fatal("get errors propagate")
	}
}

type fakeSchema struct {
	createErr error
	statuses  []types.TableStatus
	descErr   error
	ttlErr    error
	created   []string
}

func (f *fakeSchema) CreateTable(_ context.Context, in *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.created = append(f.created, *in.TableName)
	return &dynamodb.CreateTableOutput{}, f.createErr
}

func (f *fakeSchema) DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if f.descErr != nil {
		return nil, f.descErr
	}
	st := f.statuses[0]
	if len(f.statuses) > 1 {
		f.statuses = f.statuses[1:]
	}
	return &dynamodb.DescribeTableOutput{Table: &types.TableDescription{TableStatus: st}}, nil
}

func (f *fakeSchema) UpdateTimeToLive(context.Context, *dynamodb.UpdateTimeToLiveInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateTimeToLiveOutput, error) {
	return &dynamodb.UpdateTimeToLiveOutput{}, f.ttlErr
}

func TestEnsureTable(t *testing.T) {
	ctx := context.Background()
	li := LinksTableInput("l")
	if len(li.GlobalSecondaryIndexes) != 1 || li.GlobalSecondaryIndexes[0].Projection.ProjectionType != types.ProjectionTypeInclude || li.BillingMode != types.BillingModePayPerRequest {
		t.Fatal("links schema")
	}
	f := &fakeSchema{statuses: []types.TableStatus{types.TableStatusCreating, types.TableStatusActive}}
	if err := EnsureTable(ctx, f, li, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// Existing table and already-enabled TTL are fine.
	f = &fakeSchema{createErr: &types.ResourceInUseException{}, statuses: []types.TableStatus{types.TableStatusActive},
		ttlErr: &smithy.GenericAPIError{Code: "ValidationException", Message: "TimeToLive is already enabled"}}
	if err := EnsureTable(ctx, f, AnalyticsTableInput("a"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]*fakeSchema{
		"create": {createErr: errBoom},
		"desc":   {descErr: errBoom},
		"ttl":    {statuses: []types.TableStatus{types.TableStatusActive}, ttlErr: errBoom},
	} {
		if err := EnsureTable(ctx, f, li, time.Millisecond); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := EnsureTable(cctx, &fakeSchema{statuses: []types.TableStatus{types.TableStatusCreating}}, li, time.Hour); err == nil {
		t.Fatal("expected context error")
	}
}
