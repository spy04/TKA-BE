package learning

import "testing"

func TestPercentCompletedUsesCompletedLessons(t *testing.T) {
	tests := []struct {
		name      string
		completed int64
		total     int64
		want      int
	}{
		{name: "empty", completed: 0, total: 0, want: 0},
		{name: "none completed", completed: 0, total: 4, want: 0},
		{name: "rounded percentage", completed: 2, total: 3, want: 67},
		{name: "all completed", completed: 4, total: 4, want: 100},
		{name: "bounded at one hundred", completed: 5, total: 4, want: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := percentCompleted(test.completed, test.total); got != test.want {
				t.Fatalf("percentCompleted(%d, %d) = %d, want %d", test.completed, test.total, got, test.want)
			}
		})
	}
}

func TestNewUUIDUsesCanonicalShape(t *testing.T) {
	value, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID() error = %v", err)
	}
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		t.Fatalf("newUUID() = %q, want canonical UUID shape", value)
	}
}
