// Package store implements the DynamoDB repositories (SPEC §5).
package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/shortener/url-shortener-ecs-fargate/internal/auth"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
)

// DynamoDB is the subset of the DynamoDB client used by the repositories.
type DynamoDB interface {
	GetItem(ctx context.Context, in *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	UpdateItem(ctx context.Context, in *dynamodb.UpdateItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	Scan(ctx context.Context, in *dynamodb.ScanInput, opts ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	TransactWriteItems(ctx context.Context, in *dynamodb.TransactWriteItemsInput, opts ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

// Key prefixes and constants.
const (
	linkPrefix    = "LINK#"
	ownerPrefix   = "OWNER#"
	createdPrefix = "CREATED#"
	apiKeyPrefix  = "APIKEY#"
	skMeta        = "META"
	counterPK     = "COUNTER#link_id"
	gsi1          = "gsi1"
	// TTLGrace: expired links are kept 30 days before DynamoDB TTL removes them (SPEC §5.1).
	TTLGrace = 30 * 24 * time.Hour
)

// timeFormat is fixed-width so gsi1sk sorts chronologically.
const timeFormat = "2006-01-02T15:04:05.000Z"

func fmtTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeFormat, s) }

func parseOptTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := parseTime(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func fmtOptTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmtTime(*t)
}

type linkItem struct {
	PK             string `dynamodbav:"pk"`
	SK             string `dynamodbav:"sk"`
	Code           string `dynamodbav:"code"`
	LongURL        string `dynamodbav:"long_url"`
	OwnerID        string `dynamodbav:"owner_id"`
	Custom         bool   `dynamodbav:"custom"`
	Status         string `dynamodbav:"status"`
	DisabledReason string `dynamodbav:"disabled_reason,omitempty"`
	CreatedAt      string `dynamodbav:"created_at"`
	UpdatedAt      string `dynamodbav:"updated_at"`
	ExpiresAt      string `dynamodbav:"expires_at,omitempty"`
	ScanStatus     string `dynamodbav:"scan_status"`
	LastScannedAt  string `dynamodbav:"last_scanned_at,omitempty"`
	TTL            int64  `dynamodbav:"ttl,omitempty"`
	GSI1PK         string `dynamodbav:"gsi1pk"`
	GSI1SK         string `dynamodbav:"gsi1sk"`
}

func toItem(l links.Link) linkItem {
	it := linkItem{
		PK: linkPrefix + l.Code, SK: skMeta, Code: l.Code, LongURL: l.LongURL, OwnerID: l.OwnerID,
		Custom: l.Custom, Status: string(l.Status), DisabledReason: l.DisabledReason,
		CreatedAt: fmtTime(l.CreatedAt), UpdatedAt: fmtTime(l.UpdatedAt), ExpiresAt: fmtOptTime(l.ExpiresAt),
		ScanStatus: string(l.ScanStatus), LastScannedAt: fmtOptTime(l.LastScannedAt),
		GSI1PK: ownerPrefix + l.OwnerID, GSI1SK: createdPrefix + fmtTime(l.CreatedAt) + "#" + l.Code,
	}
	if l.ExpiresAt != nil {
		it.TTL = l.ExpiresAt.Add(TTLGrace).Unix()
	}
	return it
}

func fromItem(it linkItem) (links.Link, error) {
	created, err := parseTime(it.CreatedAt)
	if err != nil {
		return links.Link{}, fmt.Errorf("created_at: %w", err)
	}
	updated, err := parseTime(it.UpdatedAt)
	if err != nil {
		return links.Link{}, fmt.Errorf("updated_at: %w", err)
	}
	exp, err := parseOptTime(it.ExpiresAt)
	if err != nil {
		return links.Link{}, fmt.Errorf("expires_at: %w", err)
	}
	scanned, err := parseOptTime(it.LastScannedAt)
	if err != nil {
		return links.Link{}, fmt.Errorf("last_scanned_at: %w", err)
	}
	return links.Link{
		Code: it.Code, LongURL: it.LongURL, OwnerID: it.OwnerID, Custom: it.Custom,
		Status: links.Status(it.Status), DisabledReason: it.DisabledReason,
		CreatedAt: created, UpdatedAt: updated, ExpiresAt: exp,
		ScanStatus: links.ScanStatus(it.ScanStatus), LastScannedAt: scanned,
	}, nil
}

// Links is the links-table repository (links, ID counter, API keys).
type Links struct {
	db      DynamoDB
	table   string
	timeout time.Duration
}

// NewLinks returns a repository for table. timeout bounds each call (200 ms on
// the request path, AGENTS §8).
func NewLinks(db DynamoDB, table string, timeout time.Duration) *Links {
	return &Links{db: db, table: table, timeout: timeout}
}

func (s *Links) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.timeout)
}

func key(pk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: pk},
		"sk": &types.AttributeValueMemberS{Value: skMeta},
	}
}

func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

// GetLink implements access pattern 1 (eventually consistent GetItem).
func (s *Links) GetLink(ctx context.Context, code string) (links.Link, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.table, Key: key(linkPrefix + code)})
	if err != nil {
		return links.Link{}, fmt.Errorf("get link: %w", err)
	}
	if out.Item == nil {
		return links.Link{}, links.ErrNotFound
	}
	var it linkItem
	if err := attributevalue.UnmarshalMap(out.Item, &it); err != nil {
		return links.Link{}, fmt.Errorf("decode link: %w", err)
	}
	l, err := fromItem(it)
	if err != nil {
		return links.Link{}, fmt.Errorf("decode link: %w", err)
	}
	return l, nil
}

// CreateLink implements access pattern 2 (PutItem if the code is unused).
func (s *Links) CreateLink(ctx context.Context, l links.Link) error {
	item, err := attributevalue.MarshalMap(toItem(l))
	if err != nil {
		return fmt.Errorf("encode link: %w", err)
	}
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &s.table,
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if isConditionFailed(err) {
		return links.ErrCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create link: %w", err)
	}
	return nil
}

// UpdateLink replaces a link with optimistic concurrency on updated_at.
func (s *Links) UpdateLink(ctx context.Context, l links.Link, prevUpdatedAt time.Time) error {
	item, err := attributevalue.MarshalMap(toItem(l))
	if err != nil {
		return fmt.Errorf("encode link: %w", err)
	}
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 &s.table,
		Item:                      item,
		ConditionExpression:       aws.String("attribute_exists(pk) AND updated_at = :prev"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":prev": &types.AttributeValueMemberS{Value: fmtTime(prevUpdatedAt)}},
	})
	if isConditionFailed(err) {
		return links.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	return nil
}

type cursorKey struct {
	PK     string `json:"pk"     dynamodbav:"pk"`
	SK     string `json:"sk"     dynamodbav:"sk"`
	GSI1PK string `json:"gsi1pk" dynamodbav:"gsi1pk"`
	GSI1SK string `json:"gsi1sk" dynamodbav:"gsi1sk"`
}

func encodeCursor(lek map[string]types.AttributeValue) (string, error) {
	if len(lek) == 0 {
		return "", nil
	}
	var k cursorKey
	if err := attributevalue.UnmarshalMap(lek, &k); err != nil {
		return "", err
	}
	b, err := json.Marshal(k)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// decodeCursor validates the cursor belongs to owner, so a forged cursor can
// never page through someone else's partition.
func decodeCursor(cursor, owner string) (map[string]types.AttributeValue, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(b) > 1024 {
		return nil, links.ErrInvalidCursor
	}
	var k cursorKey
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&k); err != nil {
		return nil, links.ErrInvalidCursor
	}
	if k.GSI1PK != ownerPrefix+owner || !strings.HasPrefix(k.PK, linkPrefix) || k.SK != skMeta || !strings.HasPrefix(k.GSI1SK, createdPrefix) {
		return nil, links.ErrInvalidCursor
	}
	m, err := attributevalue.MarshalMap(k)
	if err != nil {
		return nil, links.ErrInvalidCursor
	}
	return m, nil
}

type summaryItem struct {
	PK        string `dynamodbav:"pk"`
	LongURL   string `dynamodbav:"long_url"`
	Status    string `dynamodbav:"status"`
	CreatedAt string `dynamodbav:"created_at"`
	ExpiresAt string `dynamodbav:"expires_at"`
}

// ListLinks implements access pattern 5 (Query gsi1, newest first). Deleted
// links are filtered out; a filtered page may hold fewer than limit items.
func (s *Links) ListLinks(ctx context.Context, owner string, limit int, cursor string) (links.Page, error) {
	in := &dynamodb.QueryInput{
		TableName:              &s.table,
		IndexName:              aws.String(gsi1),
		KeyConditionExpression: aws.String("gsi1pk = :pk"),
		FilterExpression:       aws.String("#st <> :deleted"),
		ExpressionAttributeNames: map[string]string{
			"#st": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":      &types.AttributeValueMemberS{Value: ownerPrefix + owner},
			":deleted": &types.AttributeValueMemberS{Value: string(links.StatusDeleted)},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(min(max(limit, 1), 100))),
	}
	if cursor != "" {
		start, err := decodeCursor(cursor, owner)
		if err != nil {
			return links.Page{}, err
		}
		in.ExclusiveStartKey = start
	}
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.db.Query(ctx, in)
	if err != nil {
		return links.Page{}, fmt.Errorf("list links: %w", err)
	}
	var items []summaryItem
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return links.Page{}, fmt.Errorf("decode links: %w", err)
	}
	page := links.Page{Items: make([]links.Summary, 0, len(items))}
	for _, it := range items {
		created, err := parseTime(it.CreatedAt)
		if err != nil {
			return links.Page{}, fmt.Errorf("decode links: %w", err)
		}
		exp, err := parseOptTime(it.ExpiresAt)
		if err != nil {
			return links.Page{}, fmt.Errorf("decode links: %w", err)
		}
		page.Items = append(page.Items, links.Summary{
			Code: strings.TrimPrefix(it.PK, linkPrefix), LongURL: it.LongURL,
			Status: links.Status(it.Status), CreatedAt: created, ExpiresAt: exp,
		})
	}
	if page.NextCursor, err = encodeCursor(out.LastEvaluatedKey); err != nil {
		return links.Page{}, fmt.Errorf("encode cursor: %w", err)
	}
	return page, nil
}

// ClaimIDBlock implements access pattern 3 (atomic ADD on the counter item).
func (s *Links) ClaimIDBlock(ctx context.Context, size int64) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.table,
		Key:                       key(counterPK),
		UpdateExpression:          aws.String("ADD #v :n"),
		ExpressionAttributeNames:  map[string]string{"#v": "value"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":n": &types.AttributeValueMemberN{Value: strconv.FormatInt(size, 10)}},
		ReturnValues:              types.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, fmt.Errorf("claim id block: %w", err)
	}
	n, ok := out.Attributes["value"].(*types.AttributeValueMemberN)
	if !ok {
		return 0, errors.New("claim id block: missing counter value")
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("claim id block: %w", err)
	}
	return v, nil
}

type apiKeyItem struct {
	PK          string   `dynamodbav:"pk"`
	SK          string   `dynamodbav:"sk"`
	OwnerID     string   `dynamodbav:"owner_id"`
	Name        string   `dynamodbav:"name"`
	Scopes      []string `dynamodbav:"scopes,stringset"`
	Status      string   `dynamodbav:"status"`
	CreatedAt   string   `dynamodbav:"created_at"`
	LastUsedDay string   `dynamodbav:"last_used_day,omitempty"`
}

// GetAPIKey implements access pattern 4.
func (s *Links) GetAPIKey(ctx context.Context, hash string) (auth.KeyRecord, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.table, Key: key(apiKeyPrefix + hash)})
	if err != nil {
		return auth.KeyRecord{}, fmt.Errorf("get api key: %w", err)
	}
	if out.Item == nil {
		return auth.KeyRecord{}, auth.ErrKeyNotFound
	}
	var it apiKeyItem
	if err := attributevalue.UnmarshalMap(out.Item, &it); err != nil {
		return auth.KeyRecord{}, fmt.Errorf("decode api key: %w", err)
	}
	created, err := parseTime(it.CreatedAt)
	if err != nil {
		return auth.KeyRecord{}, fmt.Errorf("decode api key: %w", err)
	}
	return auth.KeyRecord{
		Hash: hash, OwnerID: it.OwnerID, Name: it.Name, Scopes: it.Scopes,
		Status: it.Status, CreatedAt: created, LastUsedDay: it.LastUsedDay,
	}, nil
}

// PutAPIKey stores a new key record (hash only).
func (s *Links) PutAPIKey(ctx context.Context, r auth.KeyRecord) error {
	item, err := attributevalue.MarshalMap(apiKeyItem{
		PK: apiKeyPrefix + r.Hash, SK: skMeta, OwnerID: r.OwnerID, Name: r.Name, Scopes: r.Scopes,
		Status: r.Status, CreatedAt: fmtTime(r.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("encode api key: %w", err)
	}
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &s.table, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)")})
	if err != nil {
		return fmt.Errorf("put api key: %w", err)
	}
	return nil
}

// RevokeAPIKey marks a key revoked.
func (s *Links) RevokeAPIKey(ctx context.Context, hash string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.table,
		Key:                       key(apiKeyPrefix + hash),
		UpdateExpression:          aws.String("SET #st = :revoked"),
		ConditionExpression:       aws.String("attribute_exists(pk)"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":revoked": &types.AttributeValueMemberS{Value: auth.StatusRevoked}},
	})
	if isConditionFailed(err) {
		return auth.ErrKeyNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// TouchAPIKey records last_used_day (no write if already set to day).
func (s *Links) TouchAPIKey(ctx context.Context, hash, day string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.table,
		Key:                       key(apiKeyPrefix + hash),
		UpdateExpression:          aws.String("SET last_used_day = :d"),
		ConditionExpression:       aws.String("attribute_exists(pk) AND (attribute_not_exists(last_used_day) OR last_used_day <> :d)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":d": &types.AttributeValueMemberS{Value: day}},
	})
	if err != nil && !isConditionFailed(err) {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
