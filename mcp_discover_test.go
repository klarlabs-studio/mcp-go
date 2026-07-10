package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func discover(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	handler := newRequestHandler(srv)
	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodServerDiscover, Params: json.RawMessage(`{}`)}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("server/discover: %v", err)
	}
	return resp.Result.(map[string]any)
}

func TestServerDiscover_Shape(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "disc", Version: "1.0.0"})
	srv.Tool("t").Description("").Handler(func(_ struct{}) (string, error) { return "ok", nil })

	res := discover(t, srv)

	if res["resultType"] != protocol.ResultTypeComplete {
		t.Errorf("resultType = %v, want complete", res["resultType"])
	}
	// supportedVersions must include the modern draft plus the legacy set.
	versions := toStrings(res["supportedVersions"])
	if !slices.Contains(versions, protocol.DraftVersion) {
		t.Errorf("supportedVersions missing %s: %v", protocol.DraftVersion, versions)
	}
	if !slices.Contains(versions, "2025-11-25") {
		t.Errorf("supportedVersions missing legacy 2025-11-25: %v", versions)
	}
	si := res["serverInfo"].(map[string]any)
	if si[fieldName] != "disc" {
		t.Errorf("serverInfo.name = %v", si[fieldName])
	}
	// Apps extension always advertised; tools capability present.
	caps := res["capabilities"].(map[string]any)
	ext := caps["extensions"].(map[string]any)
	if _, ok := ext[protocol.ExtensionUI]; !ok {
		t.Errorf("expected %s extension advertised, got %v", protocol.ExtensionUI, ext)
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("expected tools capability in discover, got %v", caps)
	}
}

func TestServerDiscover_TasksExtensionWhenOptedIn(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "disc", Version: "1"})
	srv.Tool("t").Description("").TaskSupport(TaskSupportOptional).
		Handler(func(_ struct{}) (string, error) { return "ok", nil })
	ext := discover(t, srv)["capabilities"].(map[string]any)["extensions"].(map[string]any)
	if _, ok := ext[protocol.ExtensionTasks]; !ok {
		t.Errorf("expected %s extension when a tool opts into tasks, got %v", protocol.ExtensionTasks, ext)
	}
}

func TestModernErrorConstructors(t *testing.T) {
	upv := protocol.NewUnsupportedProtocolVersion([]string{"2026-07-28"}, "1900-01-01")
	if upv.Code != protocol.CodeUnsupportedProtocolVersion {
		t.Errorf("code = %d, want %d", upv.Code, protocol.CodeUnsupportedProtocolVersion)
	}
	data := upv.Data.(map[string]any)
	if data["requested"] != "1900-01-01" {
		t.Errorf("data.requested = %v", data["requested"])
	}

	mrc := protocol.NewMissingRequiredClientCapability("sampling")
	if mrc.Code != protocol.CodeMissingRequiredClientCapability {
		t.Errorf("code = %d, want %d", mrc.Code, protocol.CodeMissingRequiredClientCapability)
	}
}

func toStrings(v any) []string { return v.([]string) }
