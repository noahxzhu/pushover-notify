package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/noahxzhu/pushover-notify/internal/model"
	"github.com/noahxzhu/pushover-notify/internal/storage"
	"github.com/noahxzhu/pushover-notify/internal/worker"
)

//go:embed templates/*
var templateFS embed.FS

type Server struct {
	store    *storage.Store
	router   *http.ServeMux
	sessions map[string]time.Time
	worker   *worker.Worker // Inject Worker to trigger Refresh
}

func NewServer(store *storage.Store, w *worker.Worker) *Server {
	s := &Server{
		store:    store,
		router:   http.NewServeMux(),
		sessions: make(map[string]time.Time),
		worker:   w,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Public routes
	s.router.HandleFunc("/login", s.handleLogin)
	s.router.HandleFunc("/setup", s.handleSetup)

	// Protected routes
	s.router.HandleFunc("/", s.authMiddleware(s.handleIndex))
	s.router.HandleFunc("/add", s.authMiddleware(s.handleAdd))
	s.router.HandleFunc("/settings", s.authMiddleware(s.handleSettings))
	s.router.HandleFunc("/logout", s.handleLogout)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Middleware
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings := s.store.GetSettings()

		if settings.Password == "" {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		expiry, ok := s.sessions[cookie.Value]
		if !ok || time.Now().After(expiry) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

// Handlers

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	if settings.Password != "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		s.renderTemplate(w, "setup.html", nil)
		return
	}

	if r.Method == "POST" {
		password := r.FormValue("password")
		if password == "" {
			http.Error(w, "Password is required", 400)
			return
		}

		settings.Password = password
		if settings.RetryInterval == "" {
			settings.RetryInterval = "30m"
			settings.MaxRetries = 3
		}

		if err := s.store.UpdateSettings(settings); err != nil {
			http.Error(w, "Failed to save settings", 500)
			return
		}

		s.worker.Refresh() // Trigger worker update

		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	if settings.Password == "" {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		s.renderTemplate(w, "login.html", nil)
		return
	}

	if r.Method == "POST" {
		password := r.FormValue("password")
		if password != settings.Password {
			s.renderTemplate(w, "login.html", map[string]interface{}{"Error": "Invalid password"})
			return
		}

		sessionToken := uuid.New().String()
		s.sessions[sessionToken] = time.Now().Add(24 * time.Hour)

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    sessionToken,
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("session_token")
	if cookie != nil {
		delete(s.sessions, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		data := s.store.GetSettings()
		s.renderTemplate(w, "settings.html", data)
		return
	}

	if r.Method == "POST" {
		settings := s.store.GetSettings()
		settings.PushoverToken = r.FormValue("pushover_token")
		settings.PushoverUser = r.FormValue("pushover_user")
		settings.RetryInterval = r.FormValue("retry_interval")
		fmt.Sscanf(r.FormValue("max_retries"), "%d", &settings.MaxRetries)

		newPass := r.FormValue("new_password")
		if newPass != "" {
			settings.Password = newPass
		}

		if err := s.store.UpdateSettings(settings); err != nil {
			http.Error(w, "Failed to update settings", 500)
			return
		}

		s.worker.Refresh() // Trigger worker update

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	notifs := s.store.GetAllNotifications()
	s.renderTemplate(w, "index.html", notifs)
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	datetimeStr := r.FormValue("datetime")
	content := r.FormValue("content")

	layout := "2006-01-02T15:04"

	scheduledTime, err := time.ParseInLocation(layout, datetimeStr, time.Local)
	if err != nil {
		http.Error(w, "Invalid date/time format. Error: "+err.Error(), 400)
		return
	}

	n := &model.Notification{
		ID:            uuid.New().String(),
		Content:       content,
		ScheduledTime: scheduledTime,
		Status:        model.StatusPending,
		SendsCount:    0,
	}

	if err := s.store.AddNotification(n); err != nil {
		http.Error(w, "Failed to save: "+err.Error(), 500)
		return
	}

	s.worker.Refresh() // Trigger worker update

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderTemplate(w http.ResponseWriter, tmplName string, data interface{}) {
	tmpl, err := template.ParseFS(templateFS, "templates/"+tmplName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Execute error: %v", err), 500)
	}
}
