package database

import (
	"context"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestListResultsNormalizesPaginationWithoutNilItems(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
	}{
		{name: "negative page", page: -1, pageSize: 25, wantPage: 1, wantPageSize: 25},
		{name: "minimum integer page", page: minInt, pageSize: 25, wantPage: 1, wantPageSize: 25},
		{name: "negative page size", page: 1, pageSize: -1, wantPage: 1, wantPageSize: 25},
		{name: "zero page size", page: 1, pageSize: 0, wantPage: 1, wantPageSize: 25},
		{name: "page size above maximum", page: 1, pageSize: 201, wantPage: 1, wantPageSize: MaxResultPageSize},
		{name: "maximum integer page size", page: 1, pageSize: maxInt, wantPage: 1, wantPageSize: MaxResultPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := store.ListResults(ctx, ResultFilter{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatal(err)
			}
			if page.Page != tt.wantPage || page.PageSize != tt.wantPageSize {
				t.Fatalf("pagination=(%d, %d), want (%d, %d)", page.Page, page.PageSize, tt.wantPage, tt.wantPageSize)
			}
			if page.Items == nil || len(page.Items) != 0 {
				t.Fatalf("Items=%#v, want non-nil empty slice", page.Items)
			}
			if page.TotalItems != 0 || page.TotalPages != 0 {
				t.Fatalf("totals=(%d, %d), want (0, 0)", page.TotalItems, page.TotalPages)
			}
		})
	}
}

func TestListResultsMaximumIntegerPageWithExistingResultIsEmpty(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := models.Task{
		Name: "Pagination task", Enabled: true, Provider: models.ProviderCloudflare,
		CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "automatic",
		InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4", TimeoutSeconds: 30,
		PreventOverlap: true, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{},
	}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	result := models.Result{
		TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, Status: models.StatusSucceeded,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{},
	}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}

	maxInt := int(^uint(0) >> 1)
	page, err := store.ListResults(ctx, ResultFilter{TaskID: task.ID, Page: maxInt, PageSize: MaxResultPageSize})
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != maxInt || page.PageSize != MaxResultPageSize {
		t.Fatalf("pagination=(%d, %d), want (%d, %d)", page.Page, page.PageSize, maxInt, MaxResultPageSize)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("Items=%#v, want non-nil empty slice", page.Items)
	}
	if page.TotalItems != 1 || page.TotalPages != 1 {
		t.Fatalf("totals=(%d, %d), want (1, 1)", page.TotalItems, page.TotalPages)
	}
}
