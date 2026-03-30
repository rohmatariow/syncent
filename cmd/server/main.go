package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"vps-command/internal/config"
	"vps-command/internal/database"
	"vps-command/internal/handlers"
	"vps-command/internal/services"
	"vps-command/internal/websocket"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	cfg := config.Load()
	cfg.RunSecurityChecklist()

	if err := database.Connect(cfg.DatabaseURL); err != nil {
		log.Fatal("Database: ", err)
	}
	defer database.Close()

	if err := database.Migrate(); err != nil {
		log.Fatal("Migration: ", err)
	}

	crypto := services.NewCryptoService(cfg.MasterKey)

	sshMgr := services.NewSSHManager(
		crypto,
		cfg.SSHMaxConcurrentGlobal,
		cfg.SSHMaxConcurrentPerSvr,
		cfg.SSHConnectTimeout,
		cfg.SSHCommandTimeout,
	)

	healthChecker := services.NewHealthChecker(sshMgr, crypto, database.Pool, cfg.HealthCheckFailures)
	healthChecker.Start()
	defer healthChecker.Stop()

	app := &handlers.App{
		Config: cfg,
		Pool:   database.Pool,
		Crypto: crypto,
		SSH:    sshMgr,
		Health: healthChecker,
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(handlers.SecurityHeaders(cfg.IsDev()))

	if cfg.IsDev() {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:3000"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type"},
			AllowCredentials: true,
		}))
	}

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		handlers.RespondPublicJSON(w, 200, map[string]interface{}{
			"status": "ok", "version": "1.2.0", "app": "SynCent",
		})
	})

	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/check-setup", app.CheckSetup)
		r.Post("/setup", app.Setup)
		r.Post("/setup/verify", app.VerifyTOTPSetup)
		r.Post("/login", app.Login)
		r.Post("/refresh", app.Refresh)
		r.Get("/captcha", app.GenerateCaptcha)
		r.Post("/validate-credentials", app.ValidateCredentials)
		r.Get("/registration-allowed", app.IsRegistrationAllowed)
		r.Post("/register", app.Register)

		r.Group(func(r chi.Router) {
			r.Use(app.AuthMiddleware)
			r.Post("/logout", app.Logout)
			r.Get("/me", app.Me)
			r.Get("/2fa-status", app.Get2FAStatus)
		})
	})

	r.Route("/api/servers", func(r chi.Router) {
		r.Use(app.AuthMiddleware)
		r.Post("/", app.CreateServer)
		r.Get("/", app.ListServers)
		r.Get("/snippets", app.ListSnippets)
		r.Post("/snippets", app.CreateSnippet)
		r.Delete("/snippets/{snippetId}", app.DeleteSnippet)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", app.GetServer)
			r.Put("/", app.UpdateServer)
			r.Delete("/", app.DeleteServer)
			r.Post("/inject-key", app.InjectKey)
			r.Post("/manual-key", app.ManualKeySetup)
			r.Post("/test-connection", app.TestConnection)
			r.Get("/public-key", app.GetPublicKey)
			r.Get("/reinject-key", app.ReinjectKey)
			r.Get("/metrics/current", app.GetCurrentMetrics)
			r.Get("/metrics/history", app.GetMetricsHistory)
			r.Post("/metrics/collect", app.CollectMetricsNow)
			r.Get("/availability", app.GetAvailability)
			r.Post("/execute", app.ExecuteCommand)
			r.Get("/execute/classify", app.ClassifyCommandPreview)
			r.Get("/history", app.GetServerHistory)
			r.Get("/status-history", app.GetStatusHistory)
		})
	})

	r.Route("/api/admin", func(r chi.Router) {
		r.Use(app.AuthMiddleware)
		r.Get("/dashboard", app.DashboardSummary)
		r.Get("/audit", app.GetAuditLog)
		r.Post("/reset-password", app.ResetPassword)

		r.Group(func(r chi.Router) {
			r.Use(app.AdminMiddleware)
			r.Get("/audit/verify", app.VerifyAuditChain)
			r.Post("/config/export", app.ExportConfig)
			r.Post("/config/import", app.ImportConfig)
			r.Get("/app-config", app.GetAppConfig)
			r.Put("/app-config", app.UpdateAppConfig)
			r.Post("/toggle-2fa", app.Toggle2FA)
		})
	})

	r.Get("/ws/logs/{id}", websocket.HandleLogStream(cfg, database.Pool, crypto))
	r.Get("/ws/terminal/{id}", websocket.HandleTerminal(cfg, database.Pool, crypto))
	r.Get("/ws/gs-terminal/{id}", websocket.HandleGSTerminal(cfg, database.Pool, crypto))
	r.Get("/ws/gs-logs/{id}", websocket.HandleGSLogs(cfg, database.Pool, crypto))

	distPath := "frontend/dist"
	if _, err := os.Stat(distPath); err == nil {
		log.Println("Serving frontend from", distPath)
		fsys := os.DirFS(distPath)
		fileServer := http.FileServer(http.FS(fsys))

		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" { path = "index.html" }
			if _, err := fs.Stat(fsys, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}

			ext := filepath.Ext(path)
			if ext == "" || ext == ".html" {
				indexBytes, _ := fs.ReadFile(fsys, "index.html")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Write(indexBytes)
				return
			}

			http.NotFound(w, r)
		})
	} else {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			handlers.RespondPublicJSON(w, 200, map[string]interface{}{
				"message": "SynCent API", "version": "1.2.0",
				"note":    "Frontend not built. Run: cd frontend && npm install && npm run build",
			})
		})
	}

	addr := fmt.Sprintf("%s:%s", cfg.AppHost, cfg.AppPort)
	log.Printf("SynCent ready — http://%s", addr)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down...")
		healthChecker.Stop()
		database.Close()
		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal("Server error: ", err)
	}
}
