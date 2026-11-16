package assessment

import "testing"

func TestCalculateScoreUsesWeightedPoints(t *testing.T) {
	selectedCorrect := "B"
	selectedWrong := "A"
	questions := []attemptQuestion{
		{ID: "q1", Points: "1.00", SelectedOptionKey: &selectedCorrect, CorrectOptionKey: "B"},
		{ID: "q2", Points: "2.00", SelectedOptionKey: &selectedWrong, CorrectOptionKey: "C", Flagged: true},
		{ID: "q3", Points: "3.00", CorrectOptionKey: "A"},
		{ID: "q4", Points: "4.00", SelectedOptionKey: &selectedCorrect, CorrectOptionKey: "B"},
	}

	summary, earnedByQuestion, err := calculateScore(questions, "10.00")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Score != "50.00" || summary.EarnedPoints != "5.00" || summary.TotalPoints != "10.00" {
		t.Fatalf("unexpected weighted score summary: %+v", summary)
	}
	if summary.AnsweredCount != 3 || summary.CorrectCount != 2 || summary.WrongCount != 1 || summary.UnansweredCount != 1 {
		t.Fatalf("unexpected answer counts: %+v", summary)
	}
	if earnedByQuestion["q1"] != "1.00" || earnedByQuestion["q2"] != "0.00" ||
		earnedByQuestion["q3"] != "0.00" || earnedByQuestion["q4"] != "4.00" {
		t.Fatalf("unexpected per-question points: %#v", earnedByQuestion)
	}
}

func TestCalculateScoreRejectsInconsistentTotal(t *testing.T) {
	questions := []attemptQuestion{
		{ID: "q1", Points: "2.00", CorrectOptionKey: "A"},
	}
	if _, _, err := calculateScore(questions, "3.00"); err == nil {
		t.Fatal("expected inconsistent total to be rejected")
	}
}
