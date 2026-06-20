// Package main demonstrates the typed MCP client API.
//
// It starts an in-process MCP server over HTTP, connects a client, and then
// invokes a tool three ways:
//
//   - client.Call: the recommended one-shot typed call.
//   - client.NewTypedTool: a reusable, pre-bound typed handle.
//   - client.NewDynamicTool: the raw JSON escape hatch (not recommended).
//
// Run it with:
//
//	go run ./examples/typed-client
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/client"
)

// GreetInput is the input for the greet tool.
type GreetInput struct {
	Name string `json:"name" jsonschema:"required,description=Who to greet"`
}

// GreetOutput is the structured output of the greet tool.
type GreetOutput struct {
	Message string `json:"message"`
	Length  int    `json:"length"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := startServer(ctx)

	tr, err := client.NewHTTPTransport("http://" + addr)
	if err != nil {
		log.Fatalf("transport: %v", err)
	}
	c := client.New(tr)
	defer func() { _ = c.Close() }()

	if _, err := c.Initialize(ctx); err != nil {
		log.Fatalf("initialize: %v", err)
	}

	// 1. Recommended: one-shot typed call.
	out, err := client.Call[GreetInput, GreetOutput](ctx, c, "greet", GreetInput{Name: "Ada"})
	if err != nil {
		log.Fatalf("call: %v", err)
	}
	fmt.Printf("Call:          %s (length %d)\n", out.Message, out.Length)

	// 2. Reusable, pre-bound typed handle.
	greet := client.NewTypedTool[GreetInput, GreetOutput](c, "greet")
	for _, name := range []string{"Grace", "Linus"} {
		out, err := greet.Call(ctx, GreetInput{Name: name})
		if err != nil {
			log.Fatalf("handle call: %v", err)
		}
		fmt.Printf("NewTypedTool: %s (length %d)\n", out.Message, out.Length)
	}

	// 3. Escape hatch: raw JSON in, raw JSON out. Prefer the typed APIs above.
	dyn := client.NewDynamicTool(c, "greet")
	raw, err := dyn.Call(ctx, json.RawMessage(`{"name":"Anonymous"}`))
	if err != nil {
		log.Fatalf("dynamic call: %v", err)
	}
	fmt.Printf("DynamicTool:   %s\n", raw)
}

// startServer launches an in-process MCP server over HTTP and returns its
// address once it is ready to serve requests.
func startServer(ctx context.Context) string {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "typed-client-example",
		Version: "1.0.0",
	})

	srv.Tool("greet").
		Description("Greet someone by name").
		Handler(func(in GreetInput) (GreetOutput, error) {
			msg := "Hello, " + in.Name + "!"
			return GreetOutput{Message: msg, Length: len(msg)}, nil
		})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	go func() {
		if err := mcp.ServeHTTP(ctx, srv, addr); err != nil && ctx.Err() == nil {
			log.Printf("server: %v", err)
		}
	}()

	waitForHealth(addr)
	return addr
}

// waitForHealth blocks until the server's /health endpoint answers.
func waitForHealth(addr string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "server did not become healthy")
	os.Exit(1)
}
