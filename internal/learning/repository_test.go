package learning

import (
	"strings"
	"testing"
)

func TestSaveProgressQueryUsesPostgresUpsert(t *testing.T) {
	for _, placeholder := range []string{"$1", "$2", "$3", "$4", "$5"} {
		if !strings.Contains(saveProgressQuery, placeholder) {
			t.Errorf("saveProgressQuery is missing placeholder %s", placeholder)
		}
	}
	if strings.Contains(saveProgressQuery, "?") ||
		!strings.Contains(saveProgressQuery, "ON CONFLICT (student_id, lesson_id) DO UPDATE") ||
		!strings.Contains(saveProgressQuery, "EXCLUDED.last_position_seconds") ||
		strings.Contains(saveProgressQuery, "ON DUPLICATE KEY") {
		t.Fatalf("saveProgressQuery is not a PostgreSQL upsert: %s", saveProgressQuery)
	}
}
