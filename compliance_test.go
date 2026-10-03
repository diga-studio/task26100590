package compliance

import (
	"math/rand"
	"sort"
	"testing"
	"time"
)

func int64ptr(v int64) *int64 { return &v }

// 通用校验：返回的归属区间必须有对应模块证据支撑、跨模块不重复计时、
// 每模块计入不超过要求、累计计入与缺口之和等于总要求。
func validateResult(t *testing.T, plan Plan, evs []Evidence, nowMs int64, res QueryResult) {
	t.Helper()
	effective := plan.DeadlineMs
	if nowMs < effective {
		effective = nowMs
	}

	// 每模块证据并集（截断到有效时刻）。
	unionByModule := map[string][]Interval{}
	for _, ev := range evs {
		end := ev.EndMs
		if end > effective {
			end = effective
		}
		if end <= ev.StartMs {
			continue
		}
		unionByModule[ev.ModuleID] = append(unionByModule[ev.ModuleID], Interval{ev.StartMs, end})
	}
	for m := range unionByModule {
		unionByModule[m] = mergeIntervals(unionByModule[m])
	}

	var totalRequired, totalCredited int64
	var allCredited []Interval
	for _, mr := range res.Modules {
		totalRequired += mr.RequiredMs
		if mr.CreditedMs > mr.RequiredMs {
			t.Errorf("module %s credited %d exceeds required %d", mr.ModuleID, mr.CreditedMs, mr.RequiredMs)
		}
		// 区间升序且不重叠。
		var sum int64
		for i, iv := range mr.Intervals {
			if iv.EndMs <= iv.StartMs {
				t.Fatalf("module %s has empty/invalid interval %+v", mr.ModuleID, iv)
			}
			if i > 0 && iv.StartMs < mr.Intervals[i-1].EndMs {
				t.Fatalf("module %s intervals overlap: %+v", mr.ModuleID, mr.Intervals)
			}
			if iv.EndMs > effective {
				t.Errorf("module %s interval %+v crosses effective time %d", mr.ModuleID, iv, effective)
			}
			if !coveredBy(unionByModule[mr.ModuleID], iv) {
				t.Errorf("module %s interval %+v not backed by evidence", mr.ModuleID, iv)
			}
			sum += iv.EndMs - iv.StartMs
		}
		if sum != mr.CreditedMs {
			t.Errorf("module %s intervals sum %d != credited %d", mr.ModuleID, sum, mr.CreditedMs)
		}
		totalCredited += mr.CreditedMs
		allCredited = append(allCredited, mr.Intervals...)
	}

	// 跨模块不重复计时。
	sort.Slice(allCredited, func(i, j int) bool { return allCredited[i].StartMs < allCredited[j].StartMs })
	for i := 1; i < len(allCredited); i++ {
		if allCredited[i].StartMs < allCredited[i-1].EndMs {
			t.Fatalf("credited intervals double-count time: %+v vs %+v", allCredited[i-1], allCredited[i])
		}
	}

	if totalCredited+res.DeficitMs != totalRequired {
		t.Errorf("credited %d + deficit %d != required %d", totalCredited, res.DeficitMs, totalRequired)
	}
	if res.DeficitMs < 0 {
		t.Errorf("negative deficit %d", res.DeficitMs)
	}
}

// coveredBy 检查 iv 是否完全被并集区间 union（已合并、升序）覆盖。
func coveredBy(union []Interval, iv Interval) bool {
	for _, u := range union {
		if u.StartMs > iv.StartMs {
			return false
		}
		if u.EndMs >= iv.EndMs {
			return true
		}
	}
	return false
}

func mustRegister(t *testing.T, s *Store, p Plan) {
	t.Helper()
	if err := s.RegisterPlan(p); err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
}

func mustAppend(t *testing.T, s *Store, emp string, ev Evidence) {
	t.Helper()
	if err := s.AppendEvidence(emp, ev); err != nil {
		t.Fatalf("AppendEvidence %+v: %v", ev, err)
	}
}

func mustQuery(t *testing.T, s *Store, emp string, nowMs int64) QueryResult {
	t.Helper()
	res, err := s.Query(emp, nowMs)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return res
}

const sec = int64(1000)

// 验收用例1：A、B各需60秒，A覆盖0~120秒、B覆盖0~60秒。
// 先到先得会把0~60秒分给A导致B不足；正确判定必须达标。
func TestTwoModulesNonGreedyAllocation(t *testing.T) {
	plan := Plan{
		EmployeeID: "e1",
		DeadlineMs: 1000 * sec,
		Modules: []ModulePlan{
			{ModuleID: "A", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(100 * sec)},
			{ModuleID: "B", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(100 * sec)},
		},
	}
	evs := []Evidence{
		{ModuleID: "A", StartMs: 0, EndMs: 120 * sec},
		{ModuleID: "B", StartMs: 0, EndMs: 60 * sec},
	}
	s := NewStore()
	mustRegister(t, s, plan)
	for _, ev := range evs {
		mustAppend(t, s, "e1", ev)
	}
	res := mustQuery(t, s, "e1", 500*sec)
	if res.Status != StatusCompliant {
		t.Fatalf("status = %v, want compliant; deficit=%d", res.Status, res.DeficitMs)
	}
	if res.DeficitMs != 0 {
		t.Fatalf("deficit = %d, want 0", res.DeficitMs)
	}
	validateResult(t, plan, evs, 500*sec, res)
}

// 验收用例2：A、B、C各需60秒，A和B都仅覆盖0~60秒、C覆盖60~180秒。
// 总覆盖180秒看似够，但0~60秒最多只能计入一个模块，缺口60秒。
func TestThreeModulesSharedWindowDeficit(t *testing.T) {
	plan := Plan{
		EmployeeID: "e2",
		DeadlineMs: 1000 * sec,
		Modules: []ModulePlan{
			{ModuleID: "A", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(10 * sec)},
			{ModuleID: "B", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(10 * sec)},
			{ModuleID: "C", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(10 * sec)},
		},
	}
	evs := []Evidence{
		{ModuleID: "A", StartMs: 0, EndMs: 60 * sec},
		{ModuleID: "B", StartMs: 0, EndMs: 60 * sec},
		{ModuleID: "C", StartMs: 60 * sec, EndMs: 180 * sec},
	}
	s := NewStore()
	mustRegister(t, s, plan)
	for _, ev := range evs {
		mustAppend(t, s, "e2", ev)
	}
	res := mustQuery(t, s, "e2", 500*sec)
	if res.Status == StatusCompliant {
		t.Fatalf("must not be compliant")
	}
	if res.DeficitMs != 60*sec {
		t.Fatalf("deficit = %d, want %d", res.DeficitMs, 60*sec)
	}
	validateResult(t, plan, evs, 500*sec, res)
}

// 验收用例3：重复与乱序补报不改变结果。
func TestDuplicateAndOutOfOrderEvidence(t *testing.T) {
	plan := Plan{
		EmployeeID: "e3",
		DeadlineMs: 1000 * sec,
		Modules: []ModulePlan{
			{ModuleID: "A", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(10 * sec)},
			{ModuleID: "B", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(10 * sec)},
		},
	}
	base := []Evidence{
		{ModuleID: "A", StartMs: 0, EndMs: 30 * sec},
		{ModuleID: "A", StartMs: 30 * sec, EndMs: 90 * sec},
		{ModuleID: "B", StartMs: 10 * sec, EndMs: 80 * sec},
		{ModuleID: "B", StartMs: 20 * sec, EndMs: 120 * sec},
	}
	reference := NewStore()
	mustRegister(t, reference, plan)
	for _, ev := range base {
		mustAppend(t, reference, "e3", ev)
	}
	want := mustQuery(t, reference, "e3", 500*sec)

	// 乱序 + 每条重复3次 + 完全重复的嵌套区间。
	shuffled := NewStore()
	mustRegister(t, shuffled, plan)
	rng := rand.New(rand.NewSource(42))
	order := rng.Perm(len(base))
	for round := 0; round < 3; round++ {
		for _, i := range order {
			mustAppend(t, shuffled, "e3", base[i])
		}
	}
	mustAppend(t, shuffled, "e3", Evidence{ModuleID: "A", StartMs: 5 * sec, EndMs: 25 * sec})
	got := mustQuery(t, shuffled, "e3", 500*sec)

	if got.Status != want.Status || got.DeficitMs != want.DeficitMs {
		t.Fatalf("got status=%v deficit=%d, want status=%v deficit=%d",
			got.Status, got.DeficitMs, want.Status, want.DeficitMs)
	}
	if got.DeficitMs != 0 || got.Status != StatusCompliant {
		t.Fatalf("expected compliant with 0 deficit, got status=%v deficit=%d", got.Status, got.DeficitMs)
	}
	validateResult(t, plan, base, 500*sec, got)
}

// 验收用例4：跨截止时刻的区间只计前半段；补报只能补回截止前的证据。
func TestDeadlineClampingAndLateBackfill(t *testing.T) {
	plan := Plan{
		EmployeeID: "e4",
		DeadlineMs: 60 * sec,
		Modules: []ModulePlan{
			{ModuleID: "A", RequiredMs: 60 * sec, QuizPassedAtMs: int64ptr(50 * sec)},
		},
	}
	s := NewStore()
	mustRegister(t, s, plan)
	// 证据跨过截止时刻：只有 [0,60s) 部分有效。
	mustAppend(t, s, "e4", Evidence{ModuleID: "A", StartMs: 0, EndMs: 120 * sec})
	res := mustQuery(t, s, "e4", 200*sec)
	if res.Status != StatusCompliant || res.DeficitMs != 0 {
		t.Fatalf("crossing interval should still satisfy: status=%v deficit=%d", res.Status, res.DeficitMs)
	}
	for _, mr := range res.Modules {
		for _, iv := range mr.Intervals {
			if iv.EndMs > 60*sec {
				t.Fatalf("interval %+v crosses deadline", iv)
			}
		}
	}

	// 只有截止后的学习：逾期且不可补。
	s2 := NewStore()
	plan2 := plan
	plan2.EmployeeID = "e5"
	mustRegister(t, s2, plan2)
	mustAppend(t, s2, "e5", Evidence{ModuleID: "A", StartMs: 60 * sec, EndMs: 120 * sec})
	res2 := mustQuery(t, s2, "e5", 200*sec)
	if res2.Status != StatusOverdue {
		t.Fatalf("status = %v, want overdue", res2.Status)
	}
	if res2.DeficitMs != 60*sec {
		t.Fatalf("deficit = %d, want %d", res2.DeficitMs, 60*sec)
	}
	// 补报截止前发生的学习，可以修正先前结果。
	mustAppend(t, s2, "e5", Evidence{ModuleID: "A", StartMs: 0, EndMs: 60 * sec})
	res3 := mustQuery(t, s2, "e5", 200*sec)
	if res3.Status != StatusCompliant || res3.DeficitMs != 0 {
		t.Fatalf("late backfill of pre-deadline evidence should fix: status=%v deficit=%d", res3.Status, res3.DeficitMs)
	}
	// 再补截止后的学习，不应改变结果（已全部满足，且无更早证据可挖）。
	mustAppend(t, s2, "e5", Evidence{ModuleID: "A", StartMs: 60 * sec, EndMs: 300 * sec})
	res4 := mustQuery(t, s2, "e5", 200*sec)
	if res4.Status != StatusCompliant || res4.DeficitMs != 0 {
		t.Fatalf("post-deadline evidence must not change result: status=%v deficit=%d", res4.Status, res4.DeficitMs)
	}
}

// 测验规则：未通过不达标；截止后通过不能补成按时达标；等于有效时刻算通过。
func TestQuizRules(t *testing.T) {
	mkPlan := func(emp string, quiz *int64) Plan {
		return Plan{
			EmployeeID: emp,
			DeadlineMs: 60 * sec,
			Modules:    []ModulePlan{{ModuleID: "A", RequiredMs: 30 * sec, QuizPassedAtMs: quiz}},
		}
	}
	ev := Evidence{ModuleID: "A", StartMs: 0, EndMs: 30 * sec}

	// 未通过测验。
	s1 := NewStore()
	mustRegister(t, s1, mkPlan("q1", nil))
	mustAppend(t, s1, "q1", ev)
	if r := mustQuery(t, s1, "q1", 30*sec); r.Status != StatusNonCompliant {
		t.Fatalf("no quiz: status=%v, want non-compliant", r.Status)
	}

	// 截止后通过：逾期。
	s2 := NewStore()
	mustRegister(t, s2, mkPlan("q2", int64ptr(61*sec)))
	mustAppend(t, s2, "q2", ev)
	if r := mustQuery(t, s2, "q2", 200*sec); r.Status != StatusOverdue {
		t.Fatalf("quiz after deadline: status=%v, want overdue", r.Status)
	}

	// 通过时刻等于有效时刻（查询时刻）算有效。
	s3 := NewStore()
	mustRegister(t, s3, mkPlan("q3", int64ptr(30*sec)))
	mustAppend(t, s3, "q3", ev)
	if r := mustQuery(t, s3, "q3", 30*sec); r.Status != StatusCompliant {
		t.Fatalf("quiz at query time: status=%v, want compliant", r.Status)
	}

	// 测验通过前的学习学时有效。
	s4 := NewStore()
	mustRegister(t, s4, mkPlan("q4", int64ptr(59*sec)))
	mustAppend(t, s4, "q4", ev)
	if r := mustQuery(t, s4, "q4", 59*sec); r.Status != StatusCompliant {
		t.Fatalf("learning before quiz must count: status=%v", r.Status)
	}
}

// 随机场景：归属区间完整性 + 追加顺序无关性。
func TestRandomizedInvariantsAndOrderIndependence(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20; trial++ {
		numModules := 1 + rng.Intn(6)
		modules := make([]ModulePlan, numModules)
		for i := range modules {
			modules[i] = ModulePlan{
				ModuleID:       string(rune('A' + i)),
				RequiredMs:     int64(1+rng.Intn(200)) * sec,
				QuizPassedAtMs: int64ptr(int64(rng.Intn(400)) * sec),
			}
		}
		plan := Plan{EmployeeID: "r", DeadlineMs: 300 * sec, Modules: modules}

		numEv := 1 + rng.Intn(60)
		evs := make([]Evidence, numEv)
		for i := range evs {
			start := int64(rng.Intn(400)) * sec
			evs[i] = Evidence{
				ModuleID: modules[rng.Intn(numModules)].ModuleID,
				StartMs:  start,
				EndMs:    start + int64(1+rng.Intn(120))*sec,
			}
		}

		s1 := NewStore()
		mustRegister(t, s1, plan)
		for _, ev := range evs {
			mustAppend(t, s1, "r", ev)
		}
		now := int64(rng.Intn(500)) * sec
		r1 := mustQuery(t, s1, "r", now)
		validateResult(t, plan, evs, now, r1)

		// 不同顺序 + 重复追加，状态与缺口必须一致。
		s2 := NewStore()
		mustRegister(t, s2, plan)
		perm := rng.Perm(numEv)
		for _, i := range perm {
			mustAppend(t, s2, "r", evs[i])
			mustAppend(t, s2, "r", evs[i])
		}
		r2 := mustQuery(t, s2, "r", now)
		if r1.Status != r2.Status || r1.DeficitMs != r2.DeficitMs {
			t.Fatalf("trial %d: order changed result: (%v,%d) vs (%v,%d)",
				trial, r1.Status, r1.DeficitMs, r2.Status, r2.DeficitMs)
		}
		validateResult(t, plan, evs, now, r2)
	}
}

// 满规模性能：16个模块、10000条区间、90天跨度，单次查询应在3秒内。
func TestFullScalePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	rng := rand.New(rand.NewSource(1))
	day := int64(24) * 3600 * 1000
	span := 90 * day
	modules := make([]ModulePlan, 16)
	for i := range modules {
		modules[i] = ModulePlan{
			ModuleID:       string(rune('A' + i)),
			RequiredMs:     5 * day,
			QuizPassedAtMs: int64ptr(80 * day),
		}
	}
	plan := Plan{EmployeeID: "perf", DeadlineMs: span, Modules: modules}
	s := NewStore()
	mustRegister(t, s, plan)
	for i := 0; i < 10000; i++ {
		start := rng.Int63n(span - day)
		mustAppend(t, s, "perf", Evidence{
			ModuleID: modules[rng.Intn(16)].ModuleID,
			StartMs:  start,
			EndMs:    start + rng.Int63n(day) + 1,
		})
	}
	begin := time.Now()
	res := mustQuery(t, s, "perf", span)
	elapsed := time.Since(begin)
	t.Logf("full-scale query took %v, deficit=%d", elapsed, res.DeficitMs)
	if elapsed > 3*time.Second {
		t.Fatalf("query took %v, exceeds 3s", elapsed)
	}
}

// 基本错误处理。
func TestErrors(t *testing.T) {
	s := NewStore()
	if _, err := s.Query("nobody", 0); err != ErrPlanNotFound {
		t.Fatalf("query without plan: %v", err)
	}
	if err := s.AppendEvidence("nobody", Evidence{ModuleID: "A", EndMs: 1}); err != ErrPlanNotFound {
		t.Fatalf("append without plan: %v", err)
	}
	plan := Plan{EmployeeID: "e", DeadlineMs: 10, Modules: []ModulePlan{{ModuleID: "A", RequiredMs: 5}}}
	mustRegister(t, s, plan)
	if err := s.RegisterPlan(plan); err != ErrPlanExists {
		t.Fatalf("duplicate plan: %v", err)
	}
	if err := s.AppendEvidence("e", Evidence{ModuleID: "A", StartMs: 5, EndMs: 5}); err != ErrInvalidEvidence {
		t.Fatalf("empty interval: %v", err)
	}
	if err := s.AppendEvidence("e", Evidence{ModuleID: "B", StartMs: 0, EndMs: 5}); err != ErrInvalidEvidence {
		t.Fatalf("unknown module: %v", err)
	}
}
