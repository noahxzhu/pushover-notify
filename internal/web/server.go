package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/noahxzhu/pushover-notify/internal/model"
	"github.com/noahxzhu/pushover-notify/internal/storage"
	"github.com/noahxzhu/pushover-notify/internal/worker"
)

//go:embed templates/* templates/layouts/* templates/partials/*
var templateFS embed.FS

type Server struct {
	store      *storage.Store
	router     *http.ServeMux
	sessions   map[string]time.Time
	worker     *worker.Worker // Inject Worker to trigger Refresh
	sseClients map[chan string]bool
	sseMux     sync.Mutex
}

func NewServer(store *storage.Store, w *worker.Worker) *Server {
	s := &Server{
		store:      store,
		router:     http.NewServeMux(),
		sessions:   make(map[string]time.Time),
		worker:     w,
		sseClients: make(map[chan string]bool),
	}
	s.routes()

	// Register callback for worker updates
	w.SetOnUpdate(s.broadcastRefresh)

	return s
}

// parseRepeatInterval extracts value and unit from interval string like "30m", "2h", "1d"
func parseRepeatInterval(interval string) (value int, unit string) {
	re := regexp.MustCompile(`^(\d+)([mhd])$`)
	matches := re.FindStringSubmatch(interval)
	if len(matches) == 3 {
		fmt.Sscanf(matches[1], "%d", &value)
		unit = matches[2]
		return
	}
	// Default fallback
	return 30, "m"
}

// combineRepeatInterval combines value and unit into interval string
func combineRepeatInterval(value string, unit string) string {
	if value == "" {
		value = "30"
	}
	if unit == "" {
		unit = "m"
	}
	return value + unit
}

func (s *Server) routes() {
	// Public routes
	s.router.HandleFunc("/login", s.handleLogin)
	s.router.HandleFunc("/setup", s.handleSetup)

	// Protected routes
	s.router.HandleFunc("/", s.authMiddleware(s.handleIndex))
	s.router.HandleFunc("/settings", s.authMiddleware(s.handleSettings))
	s.router.HandleFunc("/logout", s.handleLogout)

	// HTMX API routes
	s.router.HandleFunc("/api/notifications", s.authMiddleware(s.handleAPINotifications))
	s.router.HandleFunc("/api/notifications/", s.authMiddleware(s.handleAPINotificationByID))
	s.router.HandleFunc("/api/notifications-list", s.authMiddleware(s.handleAPINotificationsList))
	s.router.HandleFunc("/api/events", s.authMiddleware(s.handleSSE))
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
		if settings.RepeatInterval == "" {
			settings.RepeatInterval = "30m"
			settings.RepeatTimes = 3
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
		settings := s.store.GetSettings()
		value, unit := parseRepeatInterval(settings.RepeatInterval)
		data := struct {
			model.Settings
			RepeatIntervalValue int
			RepeatIntervalUnit  string
		}{
			Settings:           settings,
			RepeatIntervalValue: value,
			RepeatIntervalUnit:  unit,
		}
		s.renderTemplate(w, "settings.html", data)
		return
	}

	if r.Method == "POST" {
		settings := s.store.GetSettings()
		settings.PushoverToken = r.FormValue("pushover_token")
		settings.PushoverUser = r.FormValue("pushover_user")
		settings.RepeatInterval = combineRepeatInterval(r.FormValue("repeat_interval_value"), r.FormValue("repeat_interval_unit"))
		fmt.Sscanf(r.FormValue("repeat_times"), "%d", &settings.RepeatTimes)

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
	settings := s.store.GetSettings()
	intervalValue, intervalUnit := parseRepeatInterval(settings.RepeatInterval)

	data := struct {
		Notifications      []*model.Notification
		Defaults           model.Settings
		RepeatIntervalValue int
		RepeatIntervalUnit  string
	}{
		Notifications:      notifs,
		Defaults:           settings,
		RepeatIntervalValue: intervalValue,
		RepeatIntervalUnit:  intervalUnit,
	}
	s.renderTemplate(w, "index.html", data)
}

// HTMX API Handlers

func (s *Server) handleAPINotificationsList(w http.ResponseWriter, r *http.Request) {
	notifs := s.store.GetAllNotifications()
	s.renderPartial(w, "notifications_list", notifs)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := make(chan string, 10)

	s.sseMux.Lock()
	s.sseClients[clientChan] = true
	s.sseMux.Unlock()

	defer func() {
		s.sseMux.Lock()
		delete(s.sseClients, clientChan)
		s.sseMux.Unlock()
		close(clientChan)
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: ok\n\n")
	flusher.Flush()

	for {
		select {
		case msg := <-clientChan:
			fmt.Fprintf(w, "%s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) broadcastRefresh() {
	s.sseMux.Lock()
	defer s.sseMux.Unlock()

	for clientChan := range s.sseClients {
		select {
		case clientChan <- "event: refresh\ndata: notifications":
		default:
			// Client buffer full, skip
		}
	}
}

func (s *Server) handleAPINotifications(w http.ResponseWriter, r *http.Request) {
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
	// Truncate to minute (ensure seconds = 0)
	scheduledTime = scheduledTime.Truncate(time.Minute)

	n := &model.Notification{
		ID:            uuid.New().String(),
		Content:       content,
		ScheduledTime: scheduledTime,
		Status:        model.StatusPending,
		SendsCount:    0,
	}

	// Parse optional overrides
	if repeatTimesStr := r.FormValue("repeat_times"); repeatTimesStr != "" {
		fmt.Sscanf(repeatTimesStr, "%d", &n.RepeatTimes)
	}
	if n.RepeatTimes == 0 {
		n.RepeatTimes = 3 // Fallback if 0 or parse fail
	}

	intervalValue := r.FormValue("repeat_interval_value")
	intervalUnit := r.FormValue("repeat_interval_unit")
	n.RepeatInterval = combineRepeatInterval(intervalValue, intervalUnit)

	if err := s.store.AddNotification(n); err != nil {
		http.Error(w, "Failed to save: "+err.Error(), 500)
		return
	}

	s.worker.Refresh() // Trigger worker update
	s.broadcastRefresh()

	// Return the full notifications list
	notifs := s.store.GetAllNotifications()
	s.renderPartial(w, "notifications_list", notifs)
}

func (s *Server) handleAPINotificationByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/notifications/{id} or /api/notifications/{id}/edit
	path := strings.TrimPrefix(r.URL.Path, "/api/notifications/")
	parts := strings.Split(path, "/")
	id := parts[0]

	if id == "" {
		http.Error(w, "Missing ID", 400)
		return
	}

	// Check if this is an edit or delete-confirm request
	if len(parts) > 1 && parts[1] == "edit" {
		s.handleAPIGetEditForm(w, r, id)
		return
	}
	if len(parts) > 1 && parts[1] == "delete-confirm" {
		s.handleAPIGetDeleteConfirm(w, r, id)
		return
	}

	switch r.Method {
	case "PUT":
		s.handleAPIUpdateNotification(w, r, id)
	case "DELETE":
		s.handleAPIDeleteNotification(w, r, id)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

func (s *Server) handleAPIGetEditForm(w http.ResponseWriter, r *http.Request, id string) {
	n, err := s.store.GetNotification(id)
	if err != nil {
		http.Error(w, "Not found", 404)
		return
	}

	value, unit := parseRepeatInterval(n.RepeatInterval)
	data := struct {
		*model.Notification
		RepeatIntervalValue int
		RepeatIntervalUnit  string
	}{
		Notification:       n,
		RepeatIntervalValue: value,
		RepeatIntervalUnit:  unit,
	}
	s.renderPartial(w, "edit_modal", data)
}

func (s *Server) handleAPIGetDeleteConfirm(w http.ResponseWriter, r *http.Request, id string) {
	n, err := s.store.GetNotification(id)
	if err != nil {
		http.Error(w, "Not found", 404)
		return
	}
	s.renderPartial(w, "delete_modal", n)
}

func (s *Server) handleAPIUpdateNotification(w http.ResponseWriter, r *http.Request, id string) {
	n, err := s.store.GetNotification(id)
	if err != nil {
		http.Error(w, "Not found", 404)
		return
	}

	// Update fields
	datetimeStr := r.FormValue("datetime")
	content := r.FormValue("content")
	repeatTimesStr := r.FormValue("repeat_times")
	intervalValue := r.FormValue("repeat_interval_value")
	intervalUnit := r.FormValue("repeat_interval_unit")

	layout := "2006-01-02T15:04"
	if scheduledTime, err := time.ParseInLocation(layout, datetimeStr, time.Local); err == nil {
		n.ScheduledTime = scheduledTime.Truncate(time.Minute)
	}

	n.Content = content

	var repeatTimes int
	if _, err := fmt.Sscanf(repeatTimesStr, "%d", &repeatTimes); err == nil && repeatTimes > 0 {
		n.RepeatTimes = repeatTimes
	}

	n.RepeatInterval = combineRepeatInterval(intervalValue, intervalUnit)

	if err := s.store.UpdateNotification(n); err != nil {
		http.Error(w, "Failed to update", 500)
		return
	}

	s.worker.Refresh()
	s.broadcastRefresh()

	// Return updated list
	notifs := s.store.GetAllNotifications()
	s.renderPartial(w, "notifications_list", notifs)
}

func (s *Server) handleAPIDeleteNotification(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.store.DeleteNotification(id); err != nil {
		http.Error(w, "Failed to delete: "+err.Error(), 500)
		return
	}

	s.worker.Refresh()
	s.broadcastRefresh()

	// Return updated list
	notifs := s.store.GetAllNotifications()
	s.renderPartial(w, "notifications_list", notifs)
}

func (s *Server) renderTemplate(w http.ResponseWriter, tmplName string, data interface{}) {
	tmpl, err := template.ParseFS(templateFS, "templates/"+tmplName, "templates/layouts/*.html", "templates/partials/*.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Execute error: %v", err), 500)
	}
}

func (s *Server) renderPartial(w http.ResponseWriter, partialName string, data interface{}) {
	tmpl, err := template.ParseFS(templateFS, "templates/partials/*.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
		return
	}
	if err := tmpl.ExecuteTemplate(w, partialName, data); err != nil {
		http.Error(w, fmt.Sprintf("Execute error: %v", err), 500)
	}
}
