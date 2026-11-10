package verdict

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Rin0107/ideas/internal/config"
)

// fuzzing: どんな JSON でも panic しない。読めた採点は、検査を通らなければ選定にも評価にも使われない。
func FuzzReadJSON(f *testing.F) {
	f.Add(`[{"idea":"a","wakuwaku":5}]`)
	f.Add(`{"core_works":true,"wakuwaku":9}`)
	f.Add(`[{"idea":"a","unknown":1}]`)
	f.Fuzz(func(t *testing.T, body string) {
		p := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		var ss []SelectScore
		if ReadJSON(p, &ss) == nil {
			for _, s := range ss {
				_ = s.validate()
			}
		}
		var e EvalScore
		if ReadJSON(p, &e) == nil {
			if v, _, err := Decide(e, 0, 2, config.Evaluate{ShipMinWakuwaku: 4, ShipMinQuality: 3}); err == nil && v != "ship" && v != "iterate" && v != "drop" {
				t.Fatal(v)
			}
		}
	})
}
