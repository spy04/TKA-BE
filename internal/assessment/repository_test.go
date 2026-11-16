package assessment

import (
	"strings"
	"testing"
)

func TestAssessmentQueriesUsePostgresSyntaxAndKeepGeneratedColumnReadOnly(t *testing.T) {
	for _, placeholder := range []string{"$1", "$2", "$3", "$4", "$5", "$6", "$7"} {
		if !strings.Contains(createAttemptQuery, placeholder) {
			t.Errorf("createAttemptQuery is missing placeholder %s", placeholder)
		}
	}
	if strings.Contains(createAttemptQuery, "?") ||
		strings.Contains(createAttemptQuery, "active_marker") ||
		!strings.Contains(createAttemptQuery, "INTERVAL '1 second'") ||
		!strings.Contains(createAttemptQuery, "statement_timestamp()") {
		t.Fatalf("createAttemptQuery is incompatible with PostgreSQL or writes a generated column: %s", createAttemptQuery)
	}
	if strings.Contains(createAttemptAnswersQuery, "?") ||
		strings.Contains(createAttemptAnswersQuery, "active_marker") ||
		!strings.Contains(createAttemptAnswersQuery, "FALSE") {
		t.Fatalf("createAttemptAnswersQuery is incompatible with PostgreSQL: %s", createAttemptAnswersQuery)
	}
	for _, placeholder := range []string{"$1", "$2", "$3", "$4", "$5", "$6", "$7", "$8", "$9", "$10"} {
		if !strings.Contains(finalizeAttemptQuery, placeholder) {
			t.Errorf("finalizeAttemptQuery is missing placeholder %s", placeholder)
		}
	}
	updateSet := strings.SplitN(finalizeAttemptQuery, " WHERE ", 2)[0]
	if strings.Contains(updateSet, "?") || strings.Contains(updateSet, "active_marker") ||
		!strings.Contains(finalizeAttemptQuery, "active_marker = 1") {
		t.Fatalf("finalizeAttemptQuery writes or misuses the generated active_marker: %s", finalizeAttemptQuery)
	}
}
