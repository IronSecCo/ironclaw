// Command mcp-sample is a tiny, dependency-free MCP server exposing two pure tools
// (echo, add). It is both a test fixture for the host MCP broker and a runnable
// "local" server an operator can point IronClaw at to try MCP without installing a
// Node/Python server:
//
//	mcp-sample              # serve over stdio (the local/stdio transport)
//	mcp-sample --http :9000 # serve over streamable HTTP (the remote transport)
//
// It holds no credentials and makes no network calls, so it is safe to wire as a
// first MCP server end to end.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/version"
)

func main() {
	httpAddr := flag.String("http", "", "serve over streamable HTTP on this address (e.g. :9000) instead of stdio")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("ironclaw-mcp-sample " + version.String())
		return
	}

	srv := mcp.SampleServer()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/", srv.Handler())
		httpServer := &http.Server{Addr: *httpAddr, Handler: mux}
		go func() {
			<-ctx.Done()
			httpServer.Close()
		}()
		log.Printf("mcp-sample: serving MCP over HTTP at %s", *httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("mcp-sample: %v", err)
		}
		return
	}

	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		log.Fatalf("mcp-sample: %v", err)
	}
}
