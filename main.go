// Kupi (Kubernetes Pilot) is a desktop app for managing Kubernetes clusters.
// This file only wires packages together; see CLAUDE.md for the layout.
package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/ihebamiri/kupi/internal/appdir"
	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/bridge"
	"github.com/ihebamiri/kupi/internal/buildmode"
	"github.com/ihebamiri/kupi/internal/charts"
	"github.com/ihebamiri/kupi/internal/clipboard"
	"github.com/ihebamiri/kupi/internal/forward"
	"github.com/ihebamiri/kupi/internal/gitpr"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/history"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/live"
	"github.com/ihebamiri/kupi/internal/metrics"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/settings"
	"github.com/ihebamiri/kupi/internal/terminal"
	"github.com/ihebamiri/kupi/internal/trust"
	"github.com/ihebamiri/kupi/internal/vault"
)

//go:embed all:frontend/dist
var assets embed.FS

// contentSecurityPolicy allows only Kupi's own bundled code. Inline styles
// are allowed because the editor and canvas libraries inject <style> tags;
// scripts are never inline or remote.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Typed events, so the generated TypeScript knows their payloads.
func init() {
	application.RegisterEvent[live.Changed](live.EventChanged)
	application.RegisterEvent[live.LogBatch](live.EventLogs)
	application.RegisterEvent[live.ShellChunk](live.EventShell)
	application.RegisterEvent[forward.Changed](forward.Event)
	application.RegisterEvent[bridge.PullEvent](bridge.EventPull)
	application.RegisterEvent[terminal.Chunk](terminal.EventName)
}

func main() {
	emitter := &live.LateEmitter{}
	deps, err := buildDeps(emitter)
	if err != nil {
		log.Fatal(err)
	}

	app := application.New(application.Options{
		Name:        "Kupi",
		Description: "Kubernetes Pilot: a desktop app for managing Kubernetes clusters",
		Services: []application.Service{
			application.NewService(bridge.NewAppService(deps)),
			application.NewService(bridge.NewClusterService(deps)),
			application.NewService(bridge.NewGuardService(deps)),
			application.NewService(bridge.NewAuditService(deps)),
			application.NewService(bridge.NewResourceService(deps)),
			application.NewService(bridge.NewLogService(deps)),
			application.NewService(bridge.NewShellService(deps)),
			application.NewService(bridge.NewFormService(deps)),
			application.NewService(bridge.NewMetricsService(deps)),
			application.NewService(bridge.NewForwardService(deps)),
			application.NewService(bridge.NewCanvasService(deps)),
			application.NewService(bridge.NewImageService(deps)),
			application.NewService(bridge.NewLocalClusterService(deps)),
			application.NewService(bridge.NewSecretService(deps)),
			application.NewService(bridge.NewVaultService(deps)),
			application.NewService(bridge.NewHistoryService(deps)),
			application.NewService(bridge.NewIncidentService(deps)),
			application.NewService(bridge.NewGitOpsService(deps)),
			application.NewService(bridge.NewHelmService(deps)),
			application.NewService(bridge.NewSettingsService(deps)),
			application.NewService(bridge.NewTerminalService(deps)),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: securityHeaders,
		},
		Linux: application.LinuxOptions{ProgramName: "kupi"},
		OnShutdown: func() {
			deps.Terminals.CloseAll()
			deps.Live.StopAll()
			deps.Forwards.StopAll()
			deps.Metrics.StopAll()
			deps.Clipboard.Close()
			deps.Vault.Close()
			deps.History.StopAll()
			deps.HistoryStore.Close()
			deps.Laptop.Close()
		},
	})
	emitter.Set(func(name string, data any) { app.Event.Emit(name, data) })
	deps.Clipboard.Attach(clipboard.Native(app.Clipboard))
	deps.Native.Set(
		func(title, message, accept string) bool { return nativeConfirm(app, title, message, accept) },
		func(url string) error { return app.Browser.OpenURL(url) },
	)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                      "Kupi",
		Width:                      1280,
		Height:                     800,
		MinWidth:                   960,
		MinHeight:                  600,
		BackgroundColour:           application.NewRGB(15, 17, 23),
		URL:                        "/",
		DefaultContextMenuDisabled: buildmode.Production,
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func buildDeps(emitter live.Emitter) (bridge.Deps, error) {
	dir, err := appdir.Dir()
	if err != nil {
		return bridge.Deps{}, err
	}
	trustStore, err := trust.Open(filepath.Join(dir, "trust.json"))
	if err != nil {
		return bridge.Deps{}, err
	}
	profiles, err := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	if err != nil {
		return bridge.Deps{}, err
	}
	auditLog, err := audit.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		return bridge.Deps{}, err
	}
	// A config dir set through KUPI_CONFIG_DIR (tests, portable setups) gets its
	// own password-store entry, so it never touches the default vault key.
	keys := vault.OSKeyStore{}
	if os.Getenv(appdir.EnvOverride) != "" {
		keys.Key = vault.KeyNameFor(dir)
	}
	historyStore := history.Open(filepath.Join(dir, "history"))
	settingsStore, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		return bridge.Deps{}, err
	}
	bridge.CleanTerminalFiles(dir)
	manager := kube.NewManager(kubeconfig.NewLoader(), trustStore, profiles)
	sessions := guard.SessionsFunc(func(name string) (guard.Session, bool) {
		s, ok := manager.Session(name)
		if !ok {
			return nil, false
		}
		return s, true
	})
	deps := bridge.Deps{
		Kube:         manager,
		Guard:        guard.New(sessions, auditLog),
		Audit:        auditLog,
		Trust:        trustStore,
		Profiles:     profiles,
		Live:         live.New(emitter),
		Metrics:      metrics.New(),
		Forwards:     forward.New(emitter),
		Emit:         emitter,
		Clipboard:    &clipboard.Secure{},
		Vault:        vault.New(dir, keys),
		History:      history.NewRecorder(historyStore),
		HistoryStore: historyStore,
		ConfigDir:    dir,
		Settings:     settingsStore,
		Terminals:    terminal.NewManager(func(c terminal.Chunk) { emitter.Emit(terminal.EventName, c) }),
		Native:       &bridge.Native{},
		Charts:       charts.New(),
		Git:          gitpr.New(),
	}
	deps.ApplySettings(settingsStore.Get())
	if deps.Laptop, err = bridge.NewLaptop(filepath.Join(dir, "laptop.json"), deps); err != nil {
		return bridge.Deps{}, err
	}
	deps.Laptop.Run()
	return deps, nil
}

// securityHeaders adds the content security policy to production builds.
// Development builds skip it because Vite's hot reload needs inline scripts.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if buildmode.Production {
			h.Set("Content-Security-Policy", contentSecurityPolicy)
		}
		next.ServeHTTP(w, r)
	})
}
