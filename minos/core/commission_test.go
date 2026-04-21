package core_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/GoodOlClint/daedalus/minos/core"
	"github.com/GoodOlClint/daedalus/minos/storage/memstore"
	"github.com/GoodOlClint/daedalus/pkg/audit"
	"github.com/GoodOlClint/daedalus/pkg/envelope"
	"github.com/GoodOlClint/daedalus/pkg/jwt"
	"github.com/GoodOlClint/daedalus/pkg/provider"
)

// staticProvider is a test double that resolves a single credential by ref.
type staticProvider struct {
	refs map[string][]byte
}

func (p *staticProvider) Resolve(_ context.Context, ref string) (*provider.Value, error) {
	v, ok := p.refs[ref]
	if !ok {
		return nil, provider.ErrNotFound
	}
	return &provider.Value{Ref: ref, Data: v}, nil
}

func (p *staticProvider) Rotate(context.Context, string) error { return nil }
func (p *staticProvider) Revoke(context.Context, string) error { return nil }
func (p *staticProvider) AuditList(context.Context, string) ([]provider.AuditEntry, error) {
	return nil, nil
}

func newTestServer(t *testing.T) (*core.Server, *memstore.Store, []byte) {
	t.Helper()
	bearerSecret := []byte("bearer-secret-for-tests")
	prov := &staticProvider{refs: map[string][]byte{
		"minos-bearer-secret": bearerSecret,
		"minos-admin-token":   []byte("admin-token"),
	}}
	cfg := core.Config{
		ListenAddr:      ":0",
		BearerSecretRef: "minos-bearer-secret",
		AdminTokenRef:   "minos-admin-token",
		Admin: core.AdminIdentity{
			Surface:   "discord",
			SurfaceID: "admin-id",
		},
		Project: core.ProjectConfig{
			ID:                   "test-project",
			Backend:              "claude-code",
			PluginImage:          "ghcr.io/example/daedalus-claude-code:latest",
			DefaultWorkspaceSize: envelope.WorkspaceSmall,
			DefaultBaseBranch:    "main",
			DefaultBudget: envelope.Budget{
				MaxTokens:              100000,
				MaxWallClockSeconds:    600,
				WarningThresholdPct:    75,
				EscalationThresholdPct: 90,
			},
			Communication: envelope.Communication{
				ThreadSurface:    "discord",
				ThreadRef:        "",
				HermesURL:        "http://minos:8081/hermes",
				ArgusIngestURL:   "http://minos:8081/argus",
				AriadneIngestURL: "http://ariadne:8082/ingest",
			},
			Capabilities: core.CapabilitiesDefaults{
				InjectedCredentials: []envelope.InjectedCredential{
					{EnvVar: "GITHUB_TOKEN", CredentialsRef: "github-app-token"},
				},
				McpEndpoints: []envelope.McpEndpoint{
					{Name: "thread", URL: "http://localhost/thread", Scopes: []string{"post_status"}},
					{Name: "github", URL: "http://minos:8081/mcp/github", Scopes: []string{"pr.create", "pr.comment"}},
				},
			},
		},
	}
	store := memstore.New(nil)
	srv, err := core.New(cfg, prov, store, audit.NewWriterEmitter("minos-test", discardWriter{}),
		core.WithClock(func() time.Time { return time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC) }),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, store, bearerSecret
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestCommissionValidates(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  core.CommissionRequest
	}{
		{"missing task_type", core.CommissionRequest{
			Brief:     envelope.Brief{Summary: "x"},
			Execution: core.ExecutionRequest{RepoURL: "https://example.com", Branch: "f/x"},
		}},
		{"missing summary", core.CommissionRequest{
			TaskType:  envelope.TaskTypeCode,
			Execution: core.ExecutionRequest{RepoURL: "https://example.com", Branch: "f/x"},
		}},
		{"missing repo", core.CommissionRequest{
			TaskType:  envelope.TaskTypeCode,
			Brief:     envelope.Brief{Summary: "x"},
			Execution: core.ExecutionRequest{Branch: "f/x"},
		}},
		{"unsupported task_type", core.CommissionRequest{
			TaskType:  envelope.TaskTypeResearch,
			Brief:     envelope.Brief{Summary: "x"},
			Execution: core.ExecutionRequest{RepoURL: "https://example.com", Branch: "f/x"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := srv.Commission(ctx, tc.req); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestCommissionComposesEnvelope(t *testing.T) {
	srv, _, bearerSecret := newTestServer(t)
	ctx := context.Background()

	req := core.CommissionRequest{
		TaskType: envelope.TaskTypeCode,
		Brief:    envelope.Brief{Summary: "fix bug 123", Detail: "the widget leaks on teardown"},
		Execution: core.ExecutionRequest{
			RepoURL: "https://github.com/example/widget",
			Branch:  "fix/widget-teardown",
		},
		Origin: envelope.Origin{
			Surface:   "internal",
			RequestID: "cli-1",
			Requester: "admin",
		},
	}
	task, err := srv.Commission(ctx, req)
	if err != nil {
		t.Fatalf("commission: %v", err)
	}
	env := task.Envelope
	if env == nil {
		t.Fatal("envelope nil")
	}
	if env.SchemaVersion != envelope.SchemaVersion {
		t.Errorf("schema version: %s", env.SchemaVersion)
	}
	if env.Execution.BaseBranch != "main" {
		t.Errorf("default base branch not applied: %s", env.Execution.BaseBranch)
	}
	if env.Execution.WorkspaceSize != envelope.WorkspaceSmall {
		t.Errorf("default workspace size not applied: %s", env.Execution.WorkspaceSize)
	}
	if env.Budget.MaxTokens != 100000 {
		t.Errorf("default budget not applied: %+v", env.Budget)
	}
	if env.Capabilities.McpAuthToken == "" {
		t.Fatal("mcp_auth_token not minted")
	}

	// Verify the bearer round-trips.
	claims, err := jwt.VerifyBearer(bearerSecret, env.Capabilities.McpAuthToken)
	if err != nil {
		t.Fatalf("verify minted bearer: %v", err)
	}
	if claims.Subject != "task:"+task.ID.String() {
		t.Errorf("unexpected subject: %s", claims.Subject)
	}
	if !claims.HasScope("github", "pr.create") {
		t.Errorf("github:pr.create scope missing from minted claims: %+v", claims.McpScopes)
	}
	if !claims.HasScope("thread", "post_status") {
		t.Errorf("thread:post_status scope missing from minted claims: %+v", claims.McpScopes)
	}
}

func TestCommissionInputsDefault(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := core.CommissionRequest{
		TaskType:  envelope.TaskTypeCode,
		Brief:     envelope.Brief{Summary: "x"},
		Execution: core.ExecutionRequest{RepoURL: "https://example.com", Branch: "f/x"},
	}
	task, err := srv.Commission(context.Background(), req)
	if err != nil {
		t.Fatalf("commission: %v", err)
	}
	var inputs map[string]any
	if err := json.Unmarshal(task.Envelope.Inputs, &inputs); err != nil {
		t.Fatalf("inputs not valid JSON: %v", err)
	}
}
