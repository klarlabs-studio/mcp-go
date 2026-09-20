// Package main demonstrates the experimental Skills extension (SEP-2640 Draft):
// Agent Skills served as skill:// resources with an automatic skill://index.json
// catalog. The upstream standard is still in definition — expect API churn.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go.klarlabs.de/mcp"
)

func main() {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "skills-example",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Resources: true,
		},
	}, mcp.WithInstructions(
		"Load skill://git-workflow/SKILL.md for Git conventions.",
	))

	if err := srv.SkillsFromDir(filepath.Join("skills")); err != nil {
		fmt.Fprintf(os.Stderr, "register skills: %v\n", err)
		os.Exit(1)
	}

	// Optional: archive and template index entries (SEP-2640).
	// srv.Skill("pdf-pack").FromArchive("pdf-pack.tar.gz")
	// srv.SkillTemplate("skill://docs/{product}/SKILL.md", "Product docs").
	//     Handler(...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcp.ServeStdio(ctx, srv); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
