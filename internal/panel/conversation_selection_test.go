package panel

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/settings"
)

func TestConversationConfigurationUsesOpenIntakeSelection(t *testing.T) {
	root, home := t.TempDir(), panelHome(t)
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "对话")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	serviceStopped := make(chan struct{})
	go func() { defer close(serviceStopped); service.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-serviceStopped; service.Release() })
	window, err := service.Server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(root, "other-intake.json")
	data, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	other, err := service.Server.NewWindow(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	if err := service.Server.AttachWindowCloser(window.ID, func() error { closed++; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "对话", nil); err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil || effective.ConfigurationName != "对话" || !effective.Values.PanelSelection {
		t.Fatalf("conversation settings: %+v %v", effective, err)
	}
	select {
	case <-window.Done:
		t.Fatal("saving configuration completed intake selection")
	default:
	}
	if err := SelectOpenIntake(ctx, root, candidate, "不存在"); err == nil {
		t.Fatal("invalid conversation choice accepted")
	}
	select {
	case <-window.Done:
		t.Fatal("invalid configuration resolved waiter")
	default:
	}
	if err := SelectOpenIntake(ctx, root, candidate, "对话"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-window.Done:
	default:
		t.Fatal("conversation selection did not resolve original waiter")
	}
	result := window.Result()
	if result.Status != "selected" || result.Configuration != "对话" || closed != 1 {
		t.Fatalf("original waiter: %+v closed=%d", result, closed)
	}
	open, saved, err := service.WindowStatus(ctx, window.ID)
	if err != nil || open || saved == nil || saved.Configuration != "对话" {
		t.Fatalf("chosen panel remained open: %v %+v %v", open, saved, err)
	}
	select {
	case <-other.Done:
		t.Fatal("conversation choice resolved another intake")
	default:
	}
}

func TestConversationChoiceWithoutPanelDoesNotCreateService(t *testing.T) {
	root := t.TempDir()
	if err := SelectOpenIntake(context.Background(), root, "intake.json", "对话"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("conversation opened panel service: %v", err)
	}
}

func TestConversationStaleIntakeDoesNotResolveOtherWindow(t *testing.T) {
	root, home := t.TempDir(), panelHome(t)
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "对话")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	serviceStopped := make(chan struct{})
	go func() { defer close(serviceStopped); service.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-serviceStopped; service.Release() })
	window, err := service.Server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(candidate); err != nil {
		t.Fatal(err)
	}
	if err := SelectOpenIntake(ctx, root, candidate, "对话"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-window.Done:
	default:
		t.Fatal("stale original waiter not resolved")
	}
	if result := window.Result(); result.Status != "superseded" {
		t.Fatalf("stale intake selected: %+v", result)
	}
	select {
	case <-other.Done:
		t.Fatal("stale conversation choice closed another window")
	default:
	}
}

func TestPanelWaitCancellationLeavesWindowAndServiceShutdownReturnsResult(t *testing.T) {
	root, home := t.TempDir(), panelHome(t)
	candidate := intakeFixture(t, root)
	server, err := NewServer(Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan struct{}, 2)
	handler := server.handler
	server.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") == "true" {
			requests <- struct{}{}
		}
		handler.ServeHTTP(w, r)
	})
	ctx, stop := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); server.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-stopped })
	instance := Instance{Addr: server.Addr(), Token: server.token}
	waiterCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, _, err := WaitWindowOn(waiterCtx, instance, window.ID); done <- err }()
	<-requests
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled wait returned success")
	}
	select {
	case <-window.Done:
		t.Fatal("cancelling wait closed user window")
	default:
	}
	resultDone := make(chan struct{})
	var result *Result
	var waitErr error
	go func() {
		_, result, waitErr = WaitWindowOn(context.Background(), instance, window.ID)
		close(resultDone)
	}()
	<-requests
	stop()
	select {
	case <-resultDone:
	case <-time.After(10 * time.Second):
		t.Fatal("service shutdown did not return panel result")
	}
	if waitErr != nil || result == nil || result.Status != "cancelled" {
		t.Fatalf("shutdown wait: %+v %v", result, waitErr)
	}
	<-stopped
}
