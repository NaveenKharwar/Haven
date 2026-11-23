package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationActionsTargetOnlyRequestedItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	n := NewNotifier(path)
	n.Add("info", "First", "", "", "")
	n.Add("warn", "Second", "", "", "server")
	n.Add("bad", "Third", "", "job:third", "apps")
	s := &Server{notes: n}
	call := func(handle http.HandlerFunc, body string, status int) {
		t.Helper()
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if rec.Code != status {
			t.Fatalf("body %s: got %d, want %d: %s", body, rec.Code, status, rec.Body)
		}
	}
	call(s.readNotifications, `{"ids":[2]}`, http.StatusOK)
	items, unread := n.List()
	if unread != 2 || !items[1].Read || items[0].Read || items[2].Read {
		t.Fatalf("only the second item should be read: %+v", items)
	}
	call(s.readNotifications, `{"ids":[2],"read":false}`, http.StatusOK)
	call(s.clearNotifications, `{"ids":[3]}`, http.StatusOK)
	call(s.clearNotifications, `{"ids":[]}`, http.StatusOK)
	call(s.clearNotifications, `{`, http.StatusBadRequest)
	n = NewNotifier(path)
	n.Add("bad", "Third again", "", "job:third", "apps")
	items, unread = n.List()
	if len(items) != 2 || unread != 2 || items[0].ID != 2 || items[1].ID != 1 {
		t.Fatalf("read state or targeted dismissal did not persist: %+v, unread=%d", items, unread)
	}
}
