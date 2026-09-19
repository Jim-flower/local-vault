package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	webMode := flag.Bool("web", false, "serve DevHub in the default browser")
	host := flag.String("host", "127.0.0.1", "web server listen address")
	port := flag.Int("port", 8787, "local web server port")
	remoteVaultPort := flag.Int("remote-vault-port", 0, "optional Vault-only port for other devices")
	noOpen := flag.Bool("no-open", false, "do not open the browser automatically")
	allowRemote := flag.Bool("allow-remote", false, "allow browser requests forwarded by a gateway or reverse proxy")
	flag.Parse()
	if *webMode || strings.Contains(strings.ToLower(filepath.Base(os.Args[0])), "web-server") {
		app := NewApp()
		if err := runWebMode(app, *host, *port, *remoteVaultPort, !*noOpen, *allowRemote); err != nil {
			log.Fatal(err)
		}
		return
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "Vault",
		Width:     1100,
		Height:    700,
		MinWidth:  800,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 247, B: 250, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func webStartupContext() context.Context { return context.Background() }
