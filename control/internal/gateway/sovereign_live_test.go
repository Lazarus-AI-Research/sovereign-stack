package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The fakes in provider_test.go prove the mapping matches what we believe the
// gateway's API to be. This proves the belief: it drives the real binary.
//
// Opt-in, because the binary lives in another repo and CI has no copy:
//
//	SOVEREIGN_GATEWAY_BIN=~/sovereign-gateway/target/debug/gateway \
//	  go test ./internal/gateway/ -run Live -v
//
// It writes a throwaway SQLite DB and uses the gateway's offline mock upstream,
// so it needs no network and no provider keys.

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startRealGateway boots the binary and returns its base URL plus an
// admin-scoped key, which is how a control plane authenticates.
func startRealGateway(t *testing.T) (string, string) {
	t.Helper()
	bin := os.Getenv("SOVEREIGN_GATEWAY_BIN")
	if bin == "" {
		t.Skip("SOVEREIGN_GATEWAY_BIN not set; skipping live gateway test")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("SOVEREIGN_GATEWAY_BIN=%s is not usable: %v", bin, err)
	}

	dir := t.TempDir()
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cfg := filepath.Join(dir, "gateway.toml")

	if err := os.WriteFile(cfg, []byte(fmt.Sprintf(`
[server]
bind = "127.0.0.1:%d"
deployment_mode = "selfhosted"

[database]
backend = "sqlite"
path = %q

[upstream]
mode = "mock"

[reqlog]
enabled = false

[routing]
strategy = "simple"
`, port, filepath.Join(dir, "gateway.db"))), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, cfg)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(base + "/health"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Sign in (the gateway seeds admin/admin on first run) and mint the
	// admin-scoped key Control would use as machine auth.
	jar := &cookieJar{}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	resp, err := client.Post(base+"/admin/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gateway did not come up: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login -> %d", resp.StatusCode)
	}

	body, _ = json.Marshal(map[string]any{"name": "control", "scopes": []string{"admin", "inference"}})
	resp, err = client.Post(base+"/admin/v1/keys", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var issued struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&issued)
	if issued.Token == "" {
		t.Fatal("no admin key issued")
	}
	return base, issued.Token
}

// cookieJar is the smallest thing that keeps the session cookie across the two
// setup calls.
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) { j.cookies = cookies }
func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie             { return j.cookies }

func TestLiveSovereignGatewayEndToEnd(t *testing.T) {
	base, adminKey := startRealGateway(t)
	c := NewSovereign(base, adminKey)
	ctx := context.Background()

	if !c.Healthy(ctx) {
		t.Fatal("real gateway reports unhealthy")
	}

	// Issue a key with a dollar budget: two calls against the real API, one of
	// which (PUT /budgets) rejects a partial body.
	issued, err := c.GenerateKey(ctx, KeyRequest{
		Alias:     "live-test",
		MaxBudget: ptrF(12.5),
		RPMLimit:  ptrI(60),
	})
	if err != nil {
		t.Fatalf("GenerateKey against the real gateway: %v", err)
	}
	if issued.Token == "" || issued.ID == "" {
		t.Fatalf("issued = %+v", issued)
	}

	keys, err := c.ListKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, k := range keys {
		if k.ID == issued.ID {
			found = true
			if k.Alias != "live-test" {
				t.Errorf("alias round-trip: %+v", k)
			}
		}
	}
	if !found {
		t.Errorf("issued key %s absent from ListKeys", issued.ID)
	}

	// The spend cap is changeable; rate limits are not.
	if err := c.UpdateBudget(ctx, issued.ID, BudgetUpdate{MaxBudget: ptrF(20)}); err != nil {
		t.Errorf("spend cap update: %v", err)
	}
	if err := c.UpdateBudget(ctx, issued.ID, BudgetUpdate{RPMLimit: ptrI(1)}); err == nil {
		t.Error("expected the real gateway to have no route for a rate-limit update")
	}

	if _, err := c.Usage(ctx); err != nil {
		t.Errorf("Usage: %v", err)
	}

	if err := c.DeleteKey(ctx, issued.ID); err != nil {
		t.Errorf("DeleteKey: %v", err)
	}
}
