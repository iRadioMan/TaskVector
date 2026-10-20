package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestArgon2PasswordHash(t *testing.T) {
	hash, err := passwordHash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !passwordMatches(hash, "correct horse battery staple") {
		t.Fatal("password did not match its Argon2id hash")
	}
	if passwordMatches(hash, "wrong password") {
		t.Fatal("incorrect password matched")
	}
}

func TestCategoryOwnership(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`INSERT INTO users(id,email,display_name,password,created_at) VALUES(1,'one@example.test','One','hash','now'),(2,'two@example.test','Two','hash','now')`)
	_, _ = db.Exec(`INSERT INTO categories(id,user_id,name,created_at) VALUES(1,1,'One category','now')`)
	a := &App{db: db}
	id := int64(1)
	if !a.ownsCategory(&id, 1) {
		t.Fatal("owner should be able to use their category")
	}
	if a.ownsCategory(&id, 2) {
		t.Fatal("another user must not be able to use this category")
	}
	if !a.ownsCategory(nil, 2) {
		t.Fatal("nil category should be allowed")
	}
}

func TestUnicodeSearchAndWildcards(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var found int
	query := `SELECT count(*) FROM (SELECT 'Ёжик 100% готов' AS title) WHERE unicode_lower(title) LIKE unicode_lower(?) ESCAPE '\'`
	if err := db.QueryRow(query, "%"+"ЁЖИК 100\\%"+"%").Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatalf("case-insensitive Cyrillic search with escaped percent should find result; got %d", found)
	}
	if err := db.QueryRow(query, "%"+"ЁЖИК 100%"+"%").Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatalf("ordinary percent search should find result; got %d", found)
	}
}

func TestTaskAndCategoryAPI(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id,email,display_name,password,created_at) VALUES(1,'one@example.test','One','hash',?)`, stamp()); err != nil {
		t.Fatal(err)
	}
	token := "test-session-token"
	h := sha256.Sum256([]byte(token))
	if _, err := db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hex.EncodeToString(h[:]), 1, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks", a.tasks)
	mux.HandleFunc("POST /api/v1/tasks", a.createTask)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", a.deleteTask)
	mux.HandleFunc("GET /api/v1/categories", a.categories)
	mux.HandleFunc("POST /api/v1/categories", a.createCategory)
	mux.HandleFunc("DELETE /api/v1/categories/{id}", a.deleteCategory)
	request := func(method, path, payload string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(payload))
		r.AddCookie(&http.Cookie{Name: "tv_session", Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	category := request("POST", "/api/v1/categories", `{"name":"Работа"}`)
	if category.Code != http.StatusCreated {
		t.Fatalf("create category returned %d: %s", category.Code, category.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(category.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	dueAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	task := request("POST", "/api/v1/tasks", fmt.Sprintf(`{"title":"Проверить SQLite","description":"Тестовая задача","priority":"medium","category_id":%d,"due_at":%q}`, int64(created["id"].(float64)), dueAt))
	if task.Code != http.StatusCreated {
		t.Fatalf("create task returned %d: %s", task.Code, task.Body.String())
	}
	var createdTask map[string]any
	if err := json.Unmarshal(task.Body.Bytes(), &createdTask); err != nil {
		t.Fatal(err)
	}
	secondTask := request("POST", "/api/v1/tasks", fmt.Sprintf(`{"title":"Архивная задача","category_id":%d}`, int64(created["id"].(float64))))
	if secondTask.Code != http.StatusCreated {
		t.Fatalf("create second task returned %d: %s", secondTask.Code, secondTask.Body.String())
	}
	var createdSecond map[string]any
	if err := json.Unmarshal(secondTask.Body.Bytes(), &createdSecond); err != nil {
		t.Fatal(err)
	}
	softDeleted := request("DELETE", fmt.Sprintf("/api/v1/tasks/%d", int64(createdSecond["id"].(float64))), "")
	if softDeleted.Code != http.StatusOK {
		t.Fatalf("soft-delete task returned %d: %s", softDeleted.Code, softDeleted.Body.String())
	}
	list := request("GET", "/api/v1/tasks?section=all", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list tasks returned %d: %s", list.Code, list.Body.String())
	}
	var result struct {
		Items []Task `json:"items"`
		Total int    `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].Title != "Проверить SQLite" {
		t.Fatalf("unexpected task list: %+v", result)
	}
	today := request("GET", "/api/v1/tasks?section=today", "")
	if today.Code != http.StatusOK {
		t.Fatalf("today tasks returned %d: %s", today.Code, today.Body.String())
	}
	search := request("GET", "/api/v1/tasks?section=all&q=sqlite", "")
	if search.Code != http.StatusOK {
		t.Fatalf("searched tasks returned %d: %s", search.Code, search.Body.String())
	}
	categoryList := request("GET", "/api/v1/categories", "")
	if categoryList.Code != http.StatusOK {
		t.Fatalf("list categories returned %d: %s", categoryList.Code, categoryList.Body.String())
	}
	var listed struct {
		Items []struct {
			TaskCount   int `json:"task_count"`
			DetachCount int `json:"detach_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(categoryList.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].TaskCount != 1 || listed.Items[0].DetachCount != 2 {
		t.Fatalf("category count did not include created task: %+v", listed)
	}
	deleted := request("DELETE", fmt.Sprintf("/api/v1/categories/%d", int64(created["id"].(float64))), "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete category returned %d: %s", deleted.Code, deleted.Body.String())
	}
	var remaining, categorized int
	if err := db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=1 AND category_id IS NULL`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM tasks WHERE user_id=1 AND category_id IS NOT NULL`).Scan(&categorized); err != nil {
		t.Fatal(err)
	}
	if remaining != 2 || categorized != 0 {
		t.Fatalf("category deletion should keep both tasks and detach both; remaining=%d categorized=%d", remaining, categorized)
	}
}
