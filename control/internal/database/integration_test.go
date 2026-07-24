// Integration tests for database, auth, and jobs against a real Postgres.
// Skipped unless TEST_DATABASE_URL is set, e.g.:
//
//	docker run --rm -d --name control-test-pg -p 54329:5432 \
//	  -e POSTGRES_PASSWORD=test pgvector/pgvector:pg16
//	TEST_DATABASE_URL=postgres://postgres:test@127.0.0.1:54329/postgres go test ./internal/database/
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/auth"
	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
)

func testPool(t *testing.T) (context.Context, *TestDeps) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	// clean slate per run
	pool.Exec(ctx, "TRUNCATE workspace_memberships, invitations, setup_claims, sessions, admin_users, jobs CASCADE")
	return ctx, &TestDeps{Auth: auth.New(pool), Jobs: jobs.New(pool)}
}

func TestFirstAdminInvitationAndRoleSafety(t *testing.T) {
	ctx, deps := testPool(t)
	claim, expires, err := deps.Auth.EnsureSetupClaim(ctx)
	if err != nil || claim == "" || !expires.After(time.Now()) {
		t.Fatalf("setup claim: token=%q expires=%v err=%v", claim, expires, err)
	}
	admin, err := deps.Auth.Claim(ctx, claim, "Owner.Admin", "Owner", "correct-horse-battery")
	if err != nil || admin.Role != "admin" || admin.Username != "owner.admin" {
		t.Fatalf("claim: %+v %v", admin, err)
	}
	if token, _, err := deps.Auth.EnsureSetupClaim(ctx); err != nil || token != "" {
		t.Fatalf("claim remained available after first user: %q %v", token, err)
	}

	invite, rawToken, err := deps.Auth.CreateInvitation(ctx, admin.ID, "member", []string{"workspace-a"})
	if err != nil || rawToken == "" || invite.Role != "member" {
		t.Fatalf("invitation: %+v %v", invite, err)
	}
	member, err := deps.Auth.AcceptInvitation(ctx, rawToken, "Team.Member", "Teammate", "another-correct-password")
	if err != nil || len(member.WorkspaceIDs) != 1 || member.WorkspaceIDs[0] != "workspace-a" {
		t.Fatalf("accept invitation: %+v %v", member, err)
	}
	users, err := deps.Auth.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("list users: %+v %v", users, err)
	}
	if _, err := deps.Auth.UpdateUser(ctx, admin.ID, "member", false, nil); err == nil {
		t.Fatal("final active administrator was demoted")
	}
	if _, err := deps.Auth.UpdateUser(ctx, member.ID, "admin", false, nil); err != nil {
		t.Fatalf("promote second admin: %v", err)
	}
	if _, err := deps.Auth.UpdateUser(ctx, admin.ID, "admin", true, nil); err != nil {
		t.Fatalf("disable admin with a replacement: %v", err)
	}
	if _, err := deps.Auth.Login(ctx, admin.Username, "correct-horse-battery"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled user login: %v", err)
	}
}

type TestDeps struct {
	Auth *auth.Service
	Jobs *jobs.Runner
}

func TestAuthRoundTrip(t *testing.T) {
	ctx, deps := testPool(t)

	if err := deps.Auth.Bootstrap(ctx, "admin", "hunter2-hunter2"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// second bootstrap is a no-op
	if err := deps.Auth.Bootstrap(ctx, "admin", "different"); err != nil {
		t.Fatalf("re-bootstrap: %v", err)
	}

	if _, err := deps.Auth.Login(ctx, "admin", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v", err)
	}
	if _, err := deps.Auth.Login(ctx, "ghost", "hunter2-hunter2"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("unknown user: got %v", err)
	}

	token, err := deps.Auth.Login(ctx, "admin", "hunter2-hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	username, err := deps.Auth.Validate(ctx, token)
	if err != nil || username != "admin" {
		t.Fatalf("validate: %q %v", username, err)
	}
	if err := deps.Auth.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := deps.Auth.Validate(ctx, token); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("validate after logout: got %v", err)
	}
}

func TestJobLifecycle(t *testing.T) {
	ctx, deps := testPool(t)

	deps.Jobs.Register("echo", func(ctx context.Context, payload json.RawMessage) (any, error) {
		var body map[string]string
		json.Unmarshal(payload, &body)
		if body["fail"] == "yes" {
			return nil, fmt.Errorf("intentional failure")
		}
		total := int64(10)
		if err := jobs.Report(ctx, jobs.Progress{Stage: "echoing", Message: "Echoing value", Current: 5, Total: &total, Unit: "bytes"}); err != nil {
			return nil, err
		}
		return map[string]string{"echoed": body["message"]}, nil
	})

	goodID, err := deps.Jobs.Enqueue(ctx, "echo", map[string]string{"message": "hello"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	badID, _ := deps.Jobs.Enqueue(ctx, "echo", map[string]string{"fail": "yes"})

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go deps.Jobs.Run(runCtx)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		good, err1 := deps.Jobs.Get(ctx, goodID)
		bad, err2 := deps.Jobs.Get(ctx, badID)
		if err1 == nil && err2 == nil && good.Status == "succeeded" && bad.Status == "failed" {
			if string(good.Result) != `{"echoed": "hello"}` && string(good.Result) != `{"echoed":"hello"}` {
				t.Errorf("result: %s", good.Result)
			}
			if bad.Error == nil || *bad.Error != "intentional failure" {
				t.Errorf("error: %v", bad.Error)
			}
			if good.ProgressTotal == nil || *good.ProgressTotal != 10 || good.ProgressCurrent != 10 || good.HeartbeatAt.IsZero() {
				t.Errorf("observable progress: %+v", good)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("jobs did not finish in time")
}
