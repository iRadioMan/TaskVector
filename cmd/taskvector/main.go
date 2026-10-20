package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	sqlite "modernc.org/sqlite"
)

//go:embed all:web/dist
var frontend embed.FS

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("unicode_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if len(args) != 1 || args[0] == nil {
			return nil, nil
		}
		return strings.ToLower(args[0].(string)), nil
	})
}

type App struct{ db *sql.DB }
type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"display_name"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	Theme    string `json:"theme"`
}
type Task struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	Priority     string  `json:"priority"`
	CategoryID   *int64  `json:"category_id"`
	DueAt        *string `json:"due_at"`
	CategoryName string  `json:"category_name,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func main() {
	path := os.Getenv("TV_DB_DSN")
	if path == "" {
		path = "taskvector.db"
	}
	addr := os.Getenv("TV_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		log.Fatal(err)
	}
	a := &App{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("POST /api/v1/auth/register", a.register)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("GET /api/v1/auth/me", a.me)
	mux.HandleFunc("GET /api/v1/tasks", a.tasks)
	mux.HandleFunc("POST /api/v1/tasks", a.createTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", a.updateTask)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", a.deleteTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/complete", a.completeTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/reopen", a.reopenTask)
	mux.HandleFunc("GET /api/v1/categories", a.categories)
	mux.HandleFunc("POST /api/v1/categories", a.createCategory)
	mux.HandleFunc("DELETE /api/v1/categories/{id}", a.deleteCategory)
	mux.HandleFunc("GET /api/v1/summary", a.summary)
	log.Printf("TaskVector listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, security(mux)))
}

const schema = `PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL, password TEXT NOT NULL, timezone TEXT NOT NULL DEFAULT 'UTC', locale TEXT NOT NULL DEFAULT 'ru', theme TEXT NOT NULL DEFAULT 'system', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS categories(id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(user_id,name COLLATE NOCASE));
CREATE TABLE IF NOT EXISTS tasks(id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL, title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','done')), priority TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('low','medium','high')), completed_at TEXT, due_at TEXT, remind_before_minutes INTEGER CHECK(remind_before_minutes IS NULL OR remind_before_minutes IN (5,15,30,60)), reminder_sent_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT, CHECK((status='done')=(completed_at IS NOT NULL)));
CREATE INDEX IF NOT EXISTS tasks_user_due ON tasks(user_id,deleted_at,status,due_at);
CREATE INDEX IF NOT EXISTS tasks_user_category ON tasks(user_id,category_id);
CREATE TABLE IF NOT EXISTS notifications(id INTEGER PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,kind TEXT NOT NULL DEFAULT 'reminder',created_at TEXT NOT NULL,delivered_at TEXT,read_at TEXT);`

func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		static, err := fs.Sub(frontend, "web/dist")
		if err != nil {
			http.Error(w, "frontend unavailable", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/" {
			if f, err := static.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				_ = f.Close()
				http.FileServer(http.FS(static)).ServeHTTP(w, r)
				return
			}
		}
		data, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "frontend unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func body(r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v) == nil
}
func dbTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func stamp() string { return dbTime(time.Now()) }
func localDayBounds(now time.Time, timezone string) (string, string) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return dbTime(start), dbTime(start.AddDate(0, 0, 1))
}
func localDateBounds(day, timezone string) (string, string, bool) {
	date, err := time.Parse("2006-01-02", day)
	if err != nil {
		return "", "", false
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	return dbTime(start), dbTime(start.AddDate(0, 0, 1)), true
}
func (a *App) ownsCategory(id *int64, userID int64) bool {
	if id == nil {
		return true
	}
	var found int
	return a.db.QueryRow("SELECT 1 FROM categories WHERE id=? AND user_id=?", *id, userID).Scan(&found) == nil
}
func validReminder(minutes *int, dueAt *string) bool {
	if minutes == nil {
		return true
	}
	if dueAt == nil {
		return false
	}
	switch *minutes {
	case 5, 15, 30, 60:
		return true
	default:
		return false
	}
}
func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"display_name"`
		Timezone string `json:"timezone"`
		Locale   string `json:"locale"`
		Theme    string `json:"theme"`
	}
	if !body(r, &in) || len(in.Password) < 10 || len(in.Password) > 128 || strings.TrimSpace(in.Name) == "" {
		write(w, 400, map[string]any{"error": map[string]any{"code": "validation_failed"}})
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if in.Locale == "" {
		in.Locale = "ru"
	}
	if in.Theme == "" {
		in.Theme = "system"
	}
	if in.Locale != "ru" && in.Locale != "en" {
		write(w, 400, map[string]any{"error": "invalid locale"})
		return
	}
	hash, err := passwordHash(in.Password)
	if err != nil {
		write(w, 500, map[string]string{"error": "internal"})
		return
	}
	res, e := a.db.Exec(`INSERT INTO users(email,display_name,password,timezone,locale,theme,created_at) VALUES(?,?,?,?,?,?,?)`, in.Email, strings.TrimSpace(in.Name), hash, in.Timezone, in.Locale, in.Theme, stamp())
	if e != nil {
		write(w, 409, map[string]any{"error": map[string]string{"code": "conflict"}})
		return
	}
	id, _ := res.LastInsertId()
	a.session(w, id, r)
	write(w, 201, map[string]any{"user": User{ID: id, Email: in.Email, Name: in.Name, Timezone: in.Timezone, Locale: in.Locale, Theme: in.Theme}})
}
func passwordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	enc := base64.RawStdEncoding
	return "$argon2id$v=19$m=65536,t=3,p=2$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key), nil
}
func passwordMatches(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}
func (a *App) session(w http.ResponseWriter, id int64, r *http.Request) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	exp := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = a.db.Exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", hex.EncodeToString(h[:]), id, exp)
	name := "tv_session"
	if r.TLS != nil {
		name = "__Host-tv_session"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: token, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, Path: "/", Expires: time.Now().Add(30 * 24 * time.Hour)})
}
func (a *App) user(r *http.Request) (User, error) {
	c, e := r.Cookie("tv_session")
	if e != nil {
		c, e = r.Cookie("__Host-tv_session")
	}
	if e != nil {
		return User{}, e
	}
	h := sha256.Sum256([]byte(c.Value))
	var u User
	var exp string
	e = a.db.QueryRow(`SELECT u.id,u.email,u.display_name,u.timezone,u.locale,u.theme,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, hex.EncodeToString(h[:])).Scan(&u.ID, &u.Email, &u.Name, &u.Timezone, &u.Locale, &u.Theme, &exp)
	if e != nil {
		return u, e
	}
	t, _ := time.Parse(time.RFC3339, exp)
	if time.Now().After(t) {
		return u, sql.ErrNoRows
	}
	return u, nil
}
func auth(a *App, next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, e := a.user(r)
		if e != nil {
			write(w, 401, map[string]any{"error": map[string]string{"code": "unauthenticated"}})
			return
		}
		next(w, r, u)
	}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !body(r, &in) {
		write(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	var u User
	var p string
	e := a.db.QueryRow("SELECT id,email,display_name,timezone,locale,theme,password FROM users WHERE email=?", strings.ToLower(strings.TrimSpace(in.Email))).Scan(&u.ID, &u.Email, &u.Name, &u.Timezone, &u.Locale, &u.Theme, &p)
	if e != nil || !passwordMatches(p, in.Password) {
		write(w, 401, map[string]any{"error": map[string]string{"code": "unauthenticated"}})
		return
	}
	a.session(w, u.ID, r)
	write(w, 200, map[string]any{"user": u})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie("tv_session")
	if e != nil {
		c, e = r.Cookie("__Host-tv_session")
	}
	if e == nil {
		h := sha256.Sum256([]byte(c.Value))
		_, _ = a.db.Exec("DELETE FROM sessions WHERE token_hash=?", hex.EncodeToString(h[:]))
	}
	http.SetCookie(w, &http.Cookie{Name: "tv_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "__Host-tv_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	write(w, 200, map[string]bool{"ok": true})
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) { write(w, 200, map[string]any{"user": u}) })(w, r)
}
func (a *App) tasks(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		section := r.URL.Query().Get("section")
		if section == "" {
			section = "all"
		}
		where := `t.user_id=? AND t.deleted_at IS NULL`
		args := []any{u.ID}
		_, nextDay := localDayBounds(time.Now(), u.Timezone)
		switch section {
		case "completed":
			where += ` AND t.status='done'`
		case "today":
			where += ` AND t.status='open' AND t.due_at IS NOT NULL AND t.due_at<?`
			args = append(args, nextDay)
		case "upcoming":
			where += ` AND t.status='open' AND t.due_at>=?`
			args = append(args, nextDay)
		case "all":
			where += ` AND t.status='open'`
		default:
		}
		if id := r.URL.Query().Get("category_id"); id != "" {
			where += ` AND t.category_id=?`
			args = append(args, id)
		}
		if q != "" {
			pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q) + "%"
			where += ` AND (unicode_lower(t.title) LIKE unicode_lower(?) ESCAPE '\' OR unicode_lower(t.description) LIKE unicode_lower(?) ESCAPE '\')`
			args = append(args, pattern, pattern)
		}
		if day := r.URL.Query().Get("due_date"); day != "" {
			start, end, valid := localDateBounds(day, u.Timezone)
			if !valid {
				write(w, 400, map[string]string{"error": "validation_failed"})
				return
			}
			where += ` AND t.due_at>=? AND t.due_at<?`
			args = append(args, start, end)
		}
		sort := "CASE WHEN t.status='done' THEN 1 ELSE 0 END,CASE WHEN t.due_at IS NULL THEN 1 ELSE 0 END,t.due_at,CASE t.priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,t.created_at,t.id"
		if r.URL.Query().Get("sort") == "priority" {
			sort = "CASE WHEN t.status='done' THEN 1 ELSE 0 END,CASE t.priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,CASE WHEN t.due_at IS NULL THEN 1 ELSE 0 END,t.due_at,t.created_at,t.id"
		}
		if section == "completed" {
			sort = "t.completed_at DESC,t.id DESC"
		}
		rows, e := a.db.Query(`SELECT t.id,t.title,t.description,t.status,t.priority,t.category_id,t.due_at,coalesce(c.name,''),t.created_at FROM tasks t LEFT JOIN categories c ON c.id=t.category_id WHERE `+where+` ORDER BY `+sort, args...)
		if e != nil {
			log.Printf("task list query failed: %v", e)
			write(w, 500, map[string]string{"error": "internal"})
			return
		}
		defer rows.Close()
		out := []Task{}
		for rows.Next() {
			var t Task
			var cat sql.NullInt64
			var due sql.NullString
			if rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Priority, &cat, &due, &t.CategoryName, &t.CreatedAt) == nil {
				if cat.Valid {
					t.CategoryID = &cat.Int64
				}
				if due.Valid {
					t.DueAt = &due.String
				}
				out = append(out, t)
			}
		}
		write(w, 200, map[string]any{"items": out, "total": len(out)})
	})(w, r)
}
func (a *App) createTask(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		var x struct {
			Title, Description, Priority string
			CategoryID                   *int64  `json:"category_id"`
			DueAt                        *string `json:"due_at"`
			Remind                       *int    `json:"remind_before_minutes"`
		}
		if !body(r, &x) || strings.TrimSpace(x.Title) == "" || len([]rune(x.Title)) > 200 || len(x.Description) > 5000 {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		if x.Priority == "" {
			x.Priority = "medium"
		}
		if x.Priority != "low" && x.Priority != "medium" && x.Priority != "high" {
			write(w, 400, map[string]string{"error": "invalid priority"})
			return
		}
		if !a.ownsCategory(x.CategoryID, u.ID) {
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		if x.DueAt != nil {
			parsed, err := time.Parse(time.RFC3339, *x.DueAt)
			if err != nil {
				write(w, 400, map[string]string{"error": "validation_failed"})
				return
			}
			utc := dbTime(parsed)
			x.DueAt = &utc
		}
		if !validReminder(x.Remind, x.DueAt) {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		now := stamp()
		res, e := a.db.Exec(`INSERT INTO tasks(user_id,category_id,title,description,priority,due_at,remind_before_minutes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, u.ID, x.CategoryID, strings.TrimSpace(x.Title), x.Description, x.Priority, x.DueAt, x.Remind, now, now)
		if e != nil {
			log.Printf("task create failed: %v", e)
			write(w, 500, map[string]string{"error": "internal"})
			return
		}
		id, _ := res.LastInsertId()
		write(w, 201, map[string]any{"id": id})
	})(w, r)
}
func (a *App) updateTask(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		var x struct {
			Title, Description, Priority string
			CategoryID                   *int64  `json:"category_id"`
			DueAt                        *string `json:"due_at"`
			Remind                       *int    `json:"remind_before_minutes"`
		}
		if !body(r, &x) || strings.TrimSpace(x.Title) == "" || len([]rune(x.Title)) > 200 || len(x.Description) > 5000 {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		if x.Priority != "low" && x.Priority != "medium" && x.Priority != "high" {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		if !a.ownsCategory(x.CategoryID, u.ID) {
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		if x.DueAt != nil {
			parsed, err := time.Parse(time.RFC3339, *x.DueAt)
			if err != nil {
				write(w, 400, map[string]string{"error": "validation_failed"})
				return
			}
			utc := dbTime(parsed)
			x.DueAt = &utc
		}
		if !validReminder(x.Remind, x.DueAt) {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		res, e := a.db.Exec(`UPDATE tasks SET title=?,description=?,priority=?,category_id=?,due_at=?,remind_before_minutes=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, strings.TrimSpace(x.Title), x.Description, x.Priority, x.CategoryID, x.DueAt, x.Remind, stamp(), r.PathValue("id"), u.ID)
		n, _ := res.RowsAffected()
		if e != nil || n == 0 {
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	})(w, r)
}
func (a *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		res, e := a.db.Exec(`UPDATE tasks SET deleted_at=?,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, stamp(), stamp(), r.PathValue("id"), u.ID)
		n, _ := res.RowsAffected()
		if e != nil || n == 0 {
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	})(w, r)
}
func (a *App) setDone(w http.ResponseWriter, r *http.Request, done bool) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		status := "open"
		var completed any
		if done {
			status = "done"
			completed = stamp()
		}
		res, e := a.db.Exec(`UPDATE tasks SET status=?,completed_at=?,reminder_sent_at=NULL,updated_at=? WHERE id=? AND user_id=? AND deleted_at IS NULL`, status, completed, stamp(), r.PathValue("id"), u.ID)
		n, _ := res.RowsAffected()
		if e != nil || n == 0 {
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	})(w, r)
}
func (a *App) completeTask(w http.ResponseWriter, r *http.Request) { a.setDone(w, r, true) }
func (a *App) reopenTask(w http.ResponseWriter, r *http.Request)   { a.setDone(w, r, false) }
func (a *App) categories(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		rows, e := a.db.Query(`SELECT c.id,c.name,COALESCE(SUM(CASE WHEN t.deleted_at IS NULL AND t.status='open' THEN 1 ELSE 0 END),0),count(t.id) FROM categories c LEFT JOIN tasks t ON t.category_id=c.id WHERE c.user_id=? GROUP BY c.id ORDER BY c.name COLLATE NOCASE`, u.ID)
		if e != nil {
			log.Printf("category list query failed: %v", e)
			write(w, 500, nil)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, n, detachCount int64
			var name string
			_ = rows.Scan(&id, &name, &n, &detachCount)
			out = append(out, map[string]any{"id": id, "name": name, "task_count": n, "detach_count": detachCount})
		}
		write(w, 200, map[string]any{"items": out})
	})(w, r)
}
func (a *App) createCategory(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		var x struct {
			Name string `json:"name"`
		}
		if !body(r, &x) || strings.TrimSpace(x.Name) == "" || len([]rune(x.Name)) > 50 {
			write(w, 400, map[string]string{"error": "validation_failed"})
			return
		}
		res, e := a.db.Exec(`INSERT INTO categories(user_id,name,created_at) VALUES(?,?,?)`, u.ID, strings.TrimSpace(x.Name), stamp())
		if e != nil {
			log.Printf("category create failed: %v", e)
			write(w, 409, map[string]string{"error": "conflict"})
			return
		}
		id, _ := res.LastInsertId()
		write(w, 201, map[string]any{"id": id, "name": strings.TrimSpace(x.Name)})
	})(w, r)
}
func (a *App) deleteCategory(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		tx, e := a.db.Begin()
		if e != nil {
			write(w, 500, nil)
			return
		}
		if _, err := tx.Exec(`UPDATE tasks SET category_id=NULL,updated_at=? WHERE category_id=? AND user_id=?`, stamp(), r.PathValue("id"), u.ID); err != nil {
			_ = tx.Rollback()
			log.Printf("category detach failed: %v", err)
			write(w, 500, map[string]string{"error": "internal"})
			return
		}
		res, e := tx.Exec(`DELETE FROM categories WHERE id=? AND user_id=?`, r.PathValue("id"), u.ID)
		n, _ := res.RowsAffected()
		if e != nil || n == 0 {
			_ = tx.Rollback()
			write(w, 404, map[string]string{"error": "not_found"})
			return
		}
		_ = tx.Commit()
		write(w, 200, map[string]bool{"ok": true})
	})(w, r)
}
func (a *App) summary(w http.ResponseWriter, r *http.Request) {
	auth(a, func(w http.ResponseWriter, r *http.Request, u User) {
		var all, done, today, up int
		_ = a.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=? AND deleted_at IS NULL AND status='open'`, u.ID).Scan(&all)
		_ = a.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=? AND deleted_at IS NULL AND status='done'`, u.ID).Scan(&done)
		_, nextDay := localDayBounds(time.Now(), u.Timezone)
		_ = a.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=? AND deleted_at IS NULL AND status='open' AND due_at IS NOT NULL AND due_at<?`, u.ID, nextDay).Scan(&today)
		_ = a.db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=? AND deleted_at IS NULL AND status='open' AND due_at>=?`, u.ID, nextDay).Scan(&up)
		write(w, 200, map[string]int{"all": all, "completed": done, "today": today, "upcoming": up})
	})(w, r)
}
