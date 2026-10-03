package compliance

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// 满规模性能：16 个模块、10000 条学习区间、90 天跨度，单次查询应在 3 秒内完成。
func TestFullScaleQueryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过性能测试")
	}
	s := NewStore()
	modules := make([]ModuleSpec, 16)
	for i := range modules {
		modules[i] = ModuleSpec{
			ModuleID:       fmt.Sprintf("M%02d", i),
			RequiredMillis: 8 * time.Hour.Milliseconds(),
			HasQuizPass:    true,
			QuizPassMillis: 0,
		}
	}
	plan := Plan{
		EmployeeID:     "perf",
		DeadlineMillis: 90 * 24 * time.Hour.Milliseconds(),
		Modules:        modules,
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 10000; i++ {
		start := rng.Int63n(plan.DeadlineMillis)
		length := 1 + rng.Int63n(time.Hour.Milliseconds())
		e := Evidence{
			EmployeeID:  "perf",
			ModuleID:    modules[rng.Intn(len(modules))].ModuleID,
			StartMillis: start,
			EndMillis:   start + length,
		}
		if err := s.AppendEvidence(e); err != nil {
			t.Fatal(err)
		}
	}
	begin := time.Now()
	res, err := s.Query("perf", plan.DeadlineMillis)
	elapsed := time.Since(begin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("满规模单次查询耗时 %v，缺口 %d ms", elapsed, res.TotalGapMillis)
	if elapsed > 3*time.Second {
		t.Fatalf("查询耗时 %v 超过 3 秒上限", elapsed)
	}
}
