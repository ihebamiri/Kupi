// Kupi (Kubernetes Pilot) is a desktop app for managing Kubernetes clusters.
// This file only wires packages together; see CLAUDE.md for the layout.
package main

import (
	"embed"
	"log"
	"net/http"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iheb-amiri/kupi/internal/appdir"
	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/bridge"
	"github.com/iheb-amiri/kupi/internal/buildmode"
	"github.com/iheb-amiri/kupi/internal/guard"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/kubeconfig"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/trust"
)

//go:embed all:frontend/dist
var assets embed.FS

// contentSecurityPolicy allows only Kupi's own bundled code. Inline styles
// are allowed because the editor and canvas libraries inject <style> tags;
// scripts are never inline or remote.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func main() {
	deps, err := buildDeps()
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
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: securityHeaders,
		},
		Linux: application.LinuxOptions{ProgramName: "kupi"},
	})

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

func buildDeps() (bridge.Deps, error) {
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
	manager := kube.NewManager(kubeconfig.NewLoader(), trustStore, profiles)
	sessions := guard.SessionsFunc(func(name string) (guard.Session, bool) {
		s, ok := manager.Session(name)
		if !ok {
			return nil, false
		}
		return s, true
	})
	return bridge.Deps{
		Kube:      manager,
		Guard:     guard.New(sessions, auditLog),
		Audit:     auditLog,
		Trust:     trustStore,
		Profiles:  profiles,
		ConfigDir: dir,
	}, nil
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
