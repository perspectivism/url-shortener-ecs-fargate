//go:build integration

// Package integration runs the real binaries against the local emulators
// (docker compose: DynamoDB Local, Valkey, ElasticMQ, S3Mock). Each run uses
// uniquely named tables, queues and buckets, so runs never interfere.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/redis/go-redis/v9"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/contract"
)

// runAlphabet returns a freshly shuffled Sqids alphabet. Each run creates new
// tables (the ID counter restarts at 0), so a fixed alphabet would regenerate
// the previous run's codes and collide with its keys in the shared local
// Valkey (l:{code}, hll:{code}:date).
func runAlphabet() string {
	a := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	for i := len(a) - 1; i > 0; i-- {
		var b [1]byte
		_, _ = rand.Read(b[:])
		j := int(b[0]) % (i + 1)
		a[i], a[j] = a[j], a[i]
	}
	return string(a)
}

var env *testEnv

type testEnv struct {
	runID    string
	binDir   string
	logDir   string
	base     map[string]string // shared environment for all services
	contract *contract.Validator
	ddb      *dynamodb.Client
	sqs      *sqs.Client
	s3       *s3.Client
	valkey   *redis.Client
	queueURL string
	dlqURL   string

	redirect  *service
	api       *service // general-purpose API instance
	apiLimit  *service // low write rate limit (429 test)
	apiB      *service // two instances sharing the ID counter (KGS test)
	apiC      *service
	apiNoScan *service // SCANNER=noop: links stay scan_status=pending (nightly rescan test)
	worker    *service

	keys map[string]string // name -> plaintext API key

	mu    sync.Mutex
	procs []*service
}

type service struct {
	name      string
	url       string
	cmd       *exec.Cmd // process mode
	container string    // container mode
	logPath   string
	done      chan error
}

// containerMode runs every service as a container through `docker compose run`
// (read-only root FS, all capabilities dropped, UID 65532 — the compose "app"
// profile settings) instead of as host processes.
func containerMode() bool { return os.Getenv("IT_MODE") == "container" }

// alive reports whether the service has not exited.
func (s *service) alive() bool {
	select {
	case err := <-s.done:
		s.done <- err
		return false
	default:
		return true
	}
}

// syncLogs refreshes the log file from the container (container mode).
func (s *service) syncLogs() {
	if s.container == "" {
		return
	}
	out, err := exec.Command("docker", "logs", s.container).CombinedOutput()
	if err == nil {
		_ = os.WriteFile(s.logPath, out, 0o600)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		fmt.Fprintln(os.Stderr, "integration setup failed:", err)
		if env != nil {
			env.dumpLogs(os.Stderr)
			env.stopAll()
		}
		os.Exit(1)
	}
	code := m.Run()
	if code != 0 {
		env.dumpLogs(os.Stderr)
	}
	env.stopAll()
	os.Exit(code)
}

func setup() error {
	ctx := context.Background()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	tmp, err := os.MkdirTemp("", "shortener-it-")
	if err != nil {
		return err
	}
	env = &testEnv{runID: hex.EncodeToString(b), binDir: filepath.Join(tmp, "bin"), logDir: filepath.Join(tmp, "logs"), keys: map[string]string{}}
	_ = os.MkdirAll(env.logDir, 0o750)

	prefix := "it-" + env.runID
	env.base = map[string]string{
		"APP_ENV":                   "test",
		"LOG_LEVEL":                 "info",
		"AWS_ACCESS_KEY_ID":         "local",
		"AWS_SECRET_ACCESS_KEY":     "local",
		"AWS_REGION":                "us-east-1",
		"AWS_ENDPOINT_URL_DYNAMODB": getenv("AWS_ENDPOINT_URL_DYNAMODB", "http://localhost:8000"),
		"AWS_ENDPOINT_URL_SQS":      getenv("AWS_ENDPOINT_URL_SQS", "http://localhost:9324"),
		"AWS_ENDPOINT_URL_S3":       getenv("AWS_ENDPOINT_URL_S3", "http://localhost:9090"),
		"LINKS_TABLE":               prefix + "-links",
		"ANALYTICS_TABLE":           prefix + "-analytics",
		"CLICK_QUEUE_NAME":          prefix + "-clicks",
		"CLICK_DLQ_NAME":            prefix + "-clicks-dlq",
		"EVENTS_BUCKET":             prefix + "-events",
		"CACHE_ADDR":                getenv("CACHE_ADDR", "localhost:6379"),
		"CACHE_MODE":                "standalone",
		"CACHE_AUTH":                "none",
		"CACHE_TLS":                 "false",
		"CLIENT_IP_SOURCE":          "remote_addr",
		"VISITOR_HASH_SALT":         "integration-test-salt-0123456789abcdef",
		"SQIDS_ALPHABET":            runAlphabet(),
		"SCANNER":                   "fake",
		"METRICS_FLUSH_INTERVAL":    "1s",
		"SHUTDOWN_TIMEOUT":          "10s",
	}

	if env.contract, err = loadContract(); err != nil {
		return err
	}
	if err := env.clients(ctx); err != nil {
		return err
	}
	if err := env.build(); err != nil {
		return err
	}
	out, err := env.ctl("local-init")
	if err != nil {
		return fmt.Errorf("local-init: %w\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "queue" {
			env.queueURL = f[1]
		} else if len(f) == 2 && f[0] == "dlq" {
			env.dlqURL = f[1]
		}
	}
	env.base["CLICK_QUEUE_URL"] = env.queueURL

	for name, scopes := range map[string]string{
		"alice":     "links:read,links:write",
		"alice-ro":  "links:read",
		"bob":       "links:read,links:write",
		"admin":     "admin",
		"throttled": "links:read,links:write",
		"bulk":      "links:read,links:write",
	} {
		// Owners are run-scoped so rate-limit and idempotency keys never overlap between runs.
		owner := env.runID + "-" + strings.TrimSuffix(name, "-ro")
		out, err := env.ctl("apikey", "create", "--owner", owner, "--scopes", scopes, "--name", name)
		if err != nil {
			return fmt.Errorf("apikey create: %w\n%s", err, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "api_key: "); ok {
				env.keys[name] = v
			}
		}
	}

	redirectPort, err := freePort()
	if err != nil {
		return err
	}
	baseURL := "http://127.0.0.1:" + redirectPort
	env.base["BASE_URL"] = baseURL
	env.base["BLOCKED_HOSTS"] = "sho.rt"

	if env.redirect, err = env.start("redirect", "redirect", redirectPort, nil); err != nil {
		return err
	}
	high := map[string]string{"RATE_LIMIT_WRITE_PER_MIN": "100000", "RATE_LIMIT_READ_PER_MIN": "100000"}
	if env.api, err = env.start("api", "api", "", high); err != nil {
		return err
	}
	if env.apiLimit, err = env.start("api-limited", "api", "", map[string]string{"RATE_LIMIT_WRITE_PER_MIN": "3"}); err != nil {
		return err
	}
	kgs := map[string]string{"RATE_LIMIT_WRITE_PER_MIN": "100000", "RATE_LIMIT_READ_PER_MIN": "100000", "KEY_BLOCK_SIZE": "100"}
	if env.apiB, err = env.start("api-b", "api", "", kgs); err != nil {
		return err
	}
	if env.apiC, err = env.start("api-c", "api", "", kgs); err != nil {
		return err
	}
	if env.apiNoScan, err = env.start("api-noscan", "api", "", map[string]string{"SCANNER": "noop", "RATE_LIMIT_WRITE_PER_MIN": "100000"}); err != nil {
		return err
	}
	if env.worker, err = env.start("worker", "worker", "", map[string]string{"WORKER_FLUSH_INTERVAL": "2s", "WORKER_WAIT_SECONDS": "1"}); err != nil {
		return err
	}
	return nil
}

func loadContract() (*contract.Validator, error) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		return nil, err
	}
	return contract.Parse(b)
}

func (e *testEnv) clients(ctx context.Context) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "local", SecretAccessKey: "local"}, nil
		})))
	if err != nil {
		return err
	}
	e.ddb = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(e.base["AWS_ENDPOINT_URL_DYNAMODB"]) })
	e.sqs = sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(e.base["AWS_ENDPOINT_URL_SQS"]) })
	e.s3 = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(e.base["AWS_ENDPOINT_URL_S3"])
		o.UsePathStyle = true
	})
	e.valkey = redis.NewClient(&redis.Options{Addr: e.base["CACHE_ADDR"]})
	if err := e.valkey.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("valkey not reachable (run `make up`): %w", err)
	}
	return nil
}

func (e *testEnv) build() error {
	bins := []string{"redirect", "api", "worker", "nightly", "shortenerctl"}
	if containerMode() {
		bins = []string{"shortenerctl"} // services run from images (make images-local)
	}
	for _, c := range bins {
		if _, err := os.Stat(filepath.Join("../../cmd", c)); err != nil {
			continue // binaries added in later milestones
		}
		cmd := exec.Command("go", "build", "-race", "-o", filepath.Join(e.binDir, c), "./cmd/"+c)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s: %w\n%s", c, err, out)
		}
	}
	return nil
}

func (e *testEnv) environ(extra map[string]string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range e.base {
		if _, over := extra[k]; !over {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func (e *testEnv) ctl(args ...string) (string, error) {
	cmd := exec.Command(filepath.Join(e.binDir, "shortenerctl"), args...)
	cmd.Env = e.environ(nil)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err != nil {
		return out.String() + errOut.String(), err
	}
	return out.String(), nil
}

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port, nil
}

// start launches a binary and waits for /healthz (HTTP services) or a short
// settle period (background workers, port == "-").
func (e *testEnv) start(name, bin, port string, extra map[string]string) (*service, error) {
	if extra == nil {
		extra = map[string]string{}
	}
	if port == "" {
		p, err := freePort()
		if err != nil {
			return nil, err
		}
		port = p
	}
	logPath := filepath.Join(e.logDir, name+".log")
	if containerMode() {
		s, err := e.startContainer(name, bin, port, extra, logPath)
		if err != nil {
			return nil, err
		}
		return s, e.awaitHealthy(s, port)
	}
	if port != "-" {
		extra["HTTP_ADDR"] = "127.0.0.1:" + port
	}
	f, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(filepath.Join(e.binDir, bin))
	cmd.Env = e.environ(extra)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &service{name: name, url: "http://127.0.0.1:" + port, cmd: cmd, logPath: logPath, done: make(chan error, 1)}
	go func() { s.done <- cmd.Wait(); _ = f.Close() }()
	e.mu.Lock()
	e.procs = append(e.procs, s)
	e.mu.Unlock()
	return s, e.awaitHealthy(s, port)
}

func (e *testEnv) awaitHealthy(s *service, port string) error {
	if port == "-" {
		return nil
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-s.done:
			s.syncLogs()
			return fmt.Errorf("%s exited during startup: %w", s.name, err)
		default:
		}
		resp, err := http.Get(s.url + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.syncLogs()
	return fmt.Errorf("%s did not become healthy", s.name)
}

// containerEnv rewrites host-side emulator addresses to compose service names.
func containerEnv(env []string) []string {
	repl := map[string]string{
		"localhost:8000": "dynamodb:8000", "localhost:9324": "sqs:9324",
		"localhost:9090": "s3:9090", "localhost:6379": "valkey:6379",
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		if k != "CLICK_QUEUE_URL" && k != "BASE_URL" {
			for from, to := range repl {
				v = strings.ReplaceAll(v, from, to)
			}
		}
		out = append(out, k+"="+v)
	}
	return out
}

func composeArgs(name, svc string, env []string) []string {
	args := []string{"compose", "-f", "../../docker-compose.yml", "run", "--no-deps", "--name", name}
	for _, kv := range containerEnv(env) {
		args = append(args, "-e", kv)
	}
	return args
}

func (e *testEnv) startContainer(name, svc, port string, extra map[string]string, logPath string) (*service, error) {
	cname := "it-" + e.runID + "-" + name
	extra["HTTP_ADDR"] = ":8080"
	args := composeArgs(cname, svc, e.environ(extra))
	args = append(args, "-d")
	if port != "-" {
		args = append(args, "-p", "127.0.0.1:"+port+":8080")
	}
	args = append(args, svc)
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("compose run %s: %w\n%s", name, err, out)
	}
	s := &service{name: name, url: "http://127.0.0.1:" + port, container: cname, logPath: logPath, done: make(chan error, 1)}
	go func() {
		out, err := exec.Command("docker", "wait", cname).Output()
		code := strings.TrimSpace(string(out))
		switch {
		case err != nil:
			s.done <- err
		case code != "0":
			s.done <- fmt.Errorf("container exited with status %s", code)
		default:
			s.done <- nil
		}
	}()
	e.mu.Lock()
	e.procs = append(e.procs, s)
	e.mu.Unlock()
	return s, nil
}

// stop sends SIGTERM and waits for a clean exit.
func (s *service) stop(timeout time.Duration) error {
	if s.container != "" {
		// docker stop sends SIGTERM, then SIGKILL after the timeout.
		_ = exec.Command("docker", "stop", "-t", fmt.Sprint(int(timeout.Seconds())), s.container).Run()
		err := <-s.done
		s.done <- err
		s.syncLogs()
		_ = exec.Command("docker", "rm", "-f", s.container).Run()
		return err
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-s.done:
		s.done <- err
		return err
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		return fmt.Errorf("%s did not stop within %s", s.name, timeout)
	}
}

func (e *testEnv) stopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.procs {
		if s.alive() {
			_ = s.stop(15 * time.Second)
		}
	}
}

func (e *testEnv) dumpLogs(w io.Writer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.procs {
		s.syncLogs()
		f, err := os.Open(s.logPath)
		if err != nil {
			continue
		}
		var lines []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		_ = f.Close()
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		fmt.Fprintf(w, "--- %s (last %d lines of %s)\n%s\n", s.name, len(lines), s.logPath, strings.Join(lines, "\n"))
	}
}

// --- HTTP helpers ----------------------------------------------------------

var noRedirect = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{MaxIdleConnsPerHost: 64},
}

type reply struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

// call performs a request and validates the response against the contract.
func call(t testing.TB, method, url, key, body string, hdr map[string]string) reply {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if err := env.contract.Response(req, resp); err != nil {
		t.Errorf("contract: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	r := reply{Status: resp.StatusCode, Header: resp.Header, Raw: raw}
	_ = json.Unmarshal(raw, &r.Body)
	return r
}

func expectStatus(t testing.TB, r reply, status int, code string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d: %s", r.Status, status, r.Raw)
	}
	if code != "" && r.Body["code"] != code {
		t.Fatalf("problem code = %v, want %s: %s", r.Body["code"], code, r.Raw)
	}
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// createLink creates a link on the general API instance as owner.
func createLink(t testing.TB, owner, body string) map[string]any {
	t.Helper()
	r := call(t, http.MethodPost, env.api.url+"/api/v1/links", env.keys[owner], body, nil)
	expectStatus(t, r, http.StatusCreated, "")
	return r.Body
}
