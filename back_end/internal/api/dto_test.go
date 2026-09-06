package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backup-report/internal/report"
)

func TestJobDTOWithTimes(t *testing.T) {
	start := time.Date(2026, 9, 4, 1, 0, 1, 0, time.UTC)
	end := time.Date(2026, 9, 4, 2, 23, 20, 0, time.UTC)

	got := jobDTOs([]report.Job{{
		Environment: "KT", Label: "MINIO_BACKUPS", Name: "MinIO",
		OK: true, Start: start, End: end, Node: "kt-minio01",
		Nodes: []string{"kt-minio01"},
	}})

	if len(got) != 1 {
		t.Fatalf("получено %d задач, ожидалась 1", len(got))
	}
	j := got[0]
	if j.Start == nil || !j.Start.Equal(start) {
		t.Errorf("Start = %v, ожидалось %v", j.Start, start)
	}
	if j.DurationSeconds == nil || *j.DurationSeconds != 4999 {
		t.Errorf("DurationSeconds = %v, ожидалось 4999", j.DurationSeconds)
	}
}

func TestJobDTOWithoutTimesUsesNull(t *testing.T) {
	// Статус ERROR: команда не запускалась, времён нет вовсе.
	got := jobDTOs([]report.Job{{
		Environment: "KT", Label: "MINIO_VK_CLOUD_BACKUP", Name: "MinIO VK Cloud",
		OK: false, Nodes: []string{"kt-backup01"},
	}})

	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	for _, want := range []string{`"start":null`, `"end":null`, `"duration_seconds":null`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("в JSON нет %s; получено: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "0001-01-01") {
		t.Errorf("нулевое время утекло в JSON: %s", raw)
	}
}

func TestNodesAlwaysArray(t *testing.T) {
	// У задачи без нод Nodes равен nil. В JSON это обязано быть [],
	// иначе .map на фронте падает.
	got := jobDTOs([]report.Job{{Environment: "KT", Name: "MinIO", Nodes: nil}})

	raw, _ := json.Marshal(got[0])
	if !strings.Contains(string(raw), `"nodes":[]`) {
		t.Errorf("nodes сериализовались не в []; получено: %s", raw)
	}
}

func TestEmptyJobListIsArray(t *testing.T) {
	raw, _ := json.Marshal(jobDTOs(nil))
	if string(raw) != "[]" {
		t.Errorf("пустой список задач дал %s, ожидалось []", raw)
	}
}
