package verdict

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/config"
)

func base(idea string) SelectScore {
	return SelectScore{Idea: idea, Wakuwaku: 3, Light: 3, Free: 3, Own: 3, Findy: 3, Stack: "web", Reason: "r", DataPlan: "なし"}
}

func TestSelectScoreValidate(t *testing.T) {
	for name, mut := range map[string]func(*SelectScore){
		"idea が空":      func(s *SelectScore) { s.Idea = "" },
		"stack":        func(s *SelectScore) { s.Stack = "ios" },
		"reason が空":    func(s *SelectScore) { s.Reason = " " },
		"data_plan が空": func(s *SelectScore) { s.DataPlan = "" },
	} {
		s := base("a")
		mut(&s)
		if err := s.validate(); err == nil || !strings.Contains(err.Error(), strings.Fields(name)[0]) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestChooseIdeaTieBreaksByID(t *testing.T) {
	got, _, err := ChooseIdea([]SelectScore{base("b"), base("a")}, map[string]bool{"a": true, "b": true}, config.Select{MinFree: 3, MinLight: 3})
	if err != nil || got.Idea != "a" {
		t.Fatal(got.Idea, err)
	}
}

func TestReadJSONStdinAndMissing(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
	go func() { w.WriteString(`{"core_works":true}`); w.Close() }()
	var e EvalScore
	if err := ReadJSON("-", &e); err != nil || !e.CoreWorks {
		t.Fatal(e, err)
	}
	if ReadJSON(filepath.Join(t.TempDir(), "none.json"), &e) == nil {
		t.Fatal("ないファイルを読めた")
	}
}

func TestChooseIdeaPrefersHigherTotal(t *testing.T) {
	hi := base("z")
	hi.Own = 4
	got, _, err := ChooseIdea([]SelectScore{base("a"), hi}, map[string]bool{"a": true, "z": true}, config.Select{MinFree: 3, MinLight: 3})
	if err != nil || got.Idea != "z" {
		t.Fatal(got.Idea, err)
	}
}
