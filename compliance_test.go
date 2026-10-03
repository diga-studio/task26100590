package compliance

import (
	"math/rand"
	"testing"
)

const (
	second = int64(1000)
	minute = 60 * second
)

func spec(id string, required int64) ModuleSpec {
	return ModuleSpec{ModuleID: id, RequiredMillis: required, HasQuizPass: true, QuizPassMillis: 0}
}

// checkAttribution 校验归属区间：有对应模块证据支撑、跨模块不重复计时、
// 各模块计入不超过要求、累计值与报告缺口一致。
func checkAttribution(t *testing.T, plan Plan, evid []Evidence, res QueryResult, effective int64) {
	t.Helper()
	type msEvent struct {
		pos   int64
		delta int
	}
	events := []msEvent{}
	var countedTotal int64
	for _, mr := range res.Modules {
		var modSum, prevEnd int64
		for _, iv := range mr.Intervals {
			if iv.StartMillis >= iv.EndMillis {
				t.Fatalf("模块 %s 含空区间 %+v", mr.ModuleID, iv)
			}
			if iv.EndMillis > effective {
				t.Fatalf("模块 %s 区间 %+v 越过有效时刻 %d", mr.ModuleID, iv, effective)
			}
			if iv.StartMillis < prevEnd {
				t.Fatalf("模块 %s 区间 %+v 与前一区间重叠或乱序", mr.ModuleID, iv)
			}
			prevEnd = iv.EndMillis
			// 区间内每一毫秒都必须有该模块的证据支撑
			if !coveredByEvidence(iv, mr.ModuleID, evid) {
				t.Fatalf("模块 %s 区间 %+v 无证据支撑", mr.ModuleID, iv)
			}
			modSum += iv.EndMillis - iv.StartMillis
			events = append(events, msEvent{iv.StartMillis, 1}, msEvent{iv.EndMillis, -1})
		}
		if modSum != mr.CountedMillis {
			t.Fatalf("模块 %s 区间累计 %d 与 CountedMillis %d 不一致", mr.ModuleID, modSum, mr.CountedMillis)
		}
		if modSum > mr.RequiredMillis {
			t.Fatalf("模块 %s 计入 %d 超过要求 %d", mr.ModuleID, modSum, mr.RequiredMillis)
		}
		countedTotal += modSum
	}
	// 扫描线验证同一毫秒最多计入一个模块
	depth := 0
	// 简单排序（事件数有限）；同一位置先处理结束事件再处理开始事件
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].pos < events[i].pos ||
				(events[j].pos == events[i].pos && events[j].delta < events[i].delta) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
	for _, ev := range events {
		depth += ev.delta
		if depth > 1 {
			t.Fatalf("时刻 %d 被计入多个模块", ev.pos)
		}
	}
	// 缺口一致性
	var requiredTotal int64
	for _, m := range plan.Modules {
		requiredTotal += m.RequiredMillis
	}
	if want := requiredTotal - countedTotal; res.TotalGapMillis != want {
		t.Fatalf("报告缺口 %d 与归属推算缺口 %d 不一致", res.TotalGapMillis, want)
	}
}

// coveredByEvidence 检查区间 [iv.Start, iv.End) 是否被该模块证据完全覆盖。
func coveredByEvidence(iv Interval, moduleID string, evid []Evidence) bool {
	points := []int64{iv.StartMillis, iv.EndMillis}
	for _, e := range evid {
		if e.ModuleID != moduleID {
			continue
		}
		if e.StartMillis > iv.StartMillis && e.StartMillis < iv.EndMillis {
			points = append(points, e.StartMillis)
		}
		if e.EndMillis > iv.StartMillis && e.EndMillis < iv.EndMillis {
			points = append(points, e.EndMillis)
		}
	}
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			if points[j] < points[i] {
				points[i], points[j] = points[j], points[i]
			}
		}
	}
	for i := 0; i+1 < len(points); i++ {
		mid := points[i] // 段 [points[i], points[i+1]) 内任取起点判断
		ok := false
		for _, e := range evid {
			if e.ModuleID == moduleID && e.StartMillis <= mid && mid < e.EndMillis {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// 验收场景 1：A、B 各需 60 秒，A 覆盖 0-120 秒、B 覆盖 0-60 秒，必须达标。
// 先到先得会把 0-60 全分给先到的模块导致另一模块不足，正确归属必须全局最优。
func TestAcceptanceTwoModulesSharedWindow(t *testing.T) {
	s := NewStore()
	plan := Plan{
		EmployeeID:     "emp1",
		DeadlineMillis: 10 * minute,
		Modules:        []ModuleSpec{spec("A", minute), spec("B", minute)},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	// 故意先上报 B 的证据，验证不按到达顺序抢占
	mustAppend(t, s, Evidence{"emp1", "B", 0, minute})
	mustAppend(t, s, Evidence{"emp1", "A", 0, 2*minute})

	res, err := s.Query("emp1", 10*minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusCompliant {
		t.Fatalf("期望达标，实际状态 %v，缺口 %d", res.Status, res.TotalGapMillis)
	}
	if res.TotalGapMillis != 0 {
		t.Fatalf("期望缺口 0，实际 %d", res.TotalGapMillis)
	}
	checkAttribution(t, plan, planEvidence("emp1", s), res, 10*minute)
}

// 验收场景 2：A、B、C 各需 60 秒，A、B 仅覆盖 0-60 秒，C 覆盖 60-180 秒。
// 总覆盖 180 秒看似足够，但 0-60 秒只能分给一个模块，必须报告缺口 60 秒。
func TestAcceptanceGapNotMaskedByTotalCoverage(t *testing.T) {
	s := NewStore()
	plan := Plan{
		EmployeeID:     "emp2",
		DeadlineMillis: 10 * minute,
		Modules:        []ModuleSpec{spec("A", minute), spec("B", minute), spec("C", minute)},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, Evidence{"emp2", "A", 0, minute})
	mustAppend(t, s, Evidence{"emp2", "B", 0, minute})
	mustAppend(t, s, Evidence{"emp2", "C", minute, 3*minute})

	res, err := s.Query("emp2", 10*minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == StatusCompliant {
		t.Fatal("不应达标：0-60 秒无法同时满足 A 和 B")
	}
	if res.TotalGapMillis != minute {
		t.Fatalf("期望缺口 60 秒，实际 %d", res.TotalGapMillis)
	}
	checkAttribution(t, plan, planEvidence("emp2", s), res, 10*minute)
}

// 验收场景 3：重复补报与跨截止时刻区间。
// 截止前缺失的学时可通过补报修正；截止后的学习不能补成按时达标。
func TestLateReportAndDeadlineCrossing(t *testing.T) {
	s := NewStore()
	deadline := 10 * minute
	plan := Plan{
		EmployeeID:     "emp3",
		DeadlineMillis: deadline,
		Modules: []ModuleSpec{
			{ModuleID: "A", RequiredMillis: minute, HasQuizPass: true, QuizPassMillis: deadline},
			{ModuleID: "B", RequiredMillis: minute, HasQuizPass: true, QuizPassMillis: 0},
		},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	// A 的证据跨越截止时刻：仅截止前部分可计入
	mustAppend(t, s, Evidence{"emp3", "A", 9*minute, 12*minute})
	// B 的证据完全在截止之后
	mustAppend(t, s, Evidence{"emp3", "B", 11*minute, 12*minute})

	res, err := s.Query("emp3", 20*minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOverdue {
		t.Fatalf("期望逾期未达标，实际 %v", res.Status)
	}
	if res.TotalGapMillis != minute {
		t.Fatalf("期望缺口 60 秒（B 截止后学习不计），实际 %d", res.TotalGapMillis)
	}
	for _, mr := range res.Modules {
		if mr.ModuleID == "A" && mr.CountedMillis != minute {
			t.Fatalf("A 截止前应计满 60 秒，实际 %d", mr.CountedMillis)
		}
		if mr.ModuleID == "B" && mr.CountedMillis != 0 {
			t.Fatalf("B 截止后学习不应计入，实际 %d", mr.CountedMillis)
		}
	}
	checkAttribution(t, plan, planEvidence("emp3", s), res, deadline)

	// 补报 B 在截止前的证据（重复上报多次、乱序、嵌套），缺口应被修正为 0
	for i := 0; i < 3; i++ {
		mustAppend(t, s, Evidence{"emp3", "B", 8*minute, 9*minute})
	}
	mustAppend(t, s, Evidence{"emp3", "B", 8*minute+30*second, 9*minute}) // 嵌套重复
	res2, err := s.Query("emp3", 20*minute)
	if err != nil {
		t.Fatal(err)
	}
	if res2.TotalGapMillis != 0 {
		t.Fatalf("补报后期望缺口 0，实际 %d", res2.TotalGapMillis)
	}
	if res2.Status != StatusCompliant {
		t.Fatalf("补报后期望达标，实际 %v", res2.Status)
	}
	checkAttribution(t, plan, planEvidence("emp3", s), res2, deadline)
}

// 测验通过时刻等于有效时刻算通过；晚于截止的通过不计。
func TestQuizTiming(t *testing.T) {
	s := NewStore()
	deadline := 10 * minute
	plan := Plan{
		EmployeeID:     "emp4",
		DeadlineMillis: deadline,
		Modules: []ModuleSpec{
			{ModuleID: "A", RequiredMillis: minute, HasQuizPass: true, QuizPassMillis: deadline},
			{ModuleID: "B", RequiredMillis: minute, HasQuizPass: true, QuizPassMillis: deadline + 1},
			{ModuleID: "C", RequiredMillis: minute, HasQuizPass: false},
		},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"A", "B", "C"} {
		mustAppend(t, s, Evidence{"emp4", id, 0, minute})
	}
	res, err := s.Query("emp4", 20*minute)
	if err != nil {
		t.Fatal(err)
	}
	quiz := map[string]bool{}
	for _, mr := range res.Modules {
		quiz[mr.ModuleID] = mr.QuizSatisfied
	}
	if !quiz["A"] {
		t.Fatal("通过时刻等于截止时刻应算有效")
	}
	if quiz["B"] {
		t.Fatal("截止之后的通过不应算有效")
	}
	if quiz["C"] {
		t.Fatal("无通过记录应视为未通过")
	}
	if res.Status != StatusOverdue {
		t.Fatalf("期望逾期未达标，实际 %v", res.Status)
	}
}

// 查询时刻早于截止时刻时，按查询时刻截断。
func TestQueryBeforeDeadline(t *testing.T) {
	s := NewStore()
	plan := Plan{
		EmployeeID:     "emp5",
		DeadlineMillis: 10 * minute,
		Modules:        []ModuleSpec{spec("A", 2*minute)},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, Evidence{"emp5", "A", 0, 2*minute})
	res, err := s.Query("emp5", minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalGapMillis != minute {
		t.Fatalf("查询时刻为 60 秒时缺口应为 60 秒，实际 %d", res.TotalGapMillis)
	}
	if res.Status != StatusNotCompliant {
		t.Fatalf("期望未达标（未逾期），实际 %v", res.Status)
	}
	checkAttribution(t, plan, planEvidence("emp5", s), res, minute)
}

// 同一证据集合任意顺序、任意重复次数追加，状态与缺口必须一致。
func TestOrderAndDuplicationInvariance(t *testing.T) {
	deadline := 10 * minute
	plan := Plan{
		EmployeeID:     "emp6",
		DeadlineMillis: deadline,
		Modules:        []ModuleSpec{spec("A", minute), spec("B", minute), spec("C", 30*second)},
	}
	evid := []Evidence{
		{"emp6", "A", 0, 90 * second},
		{"emp6", "B", 30 * second, 90 * second},
		{"emp6", "C", 60 * second, 2 * minute},
		{"emp6", "A", 2 * minute, 3 * minute},
		{"emp6", "B", 0, 20 * second},
	}
	var wantStatus Status
	var wantGap int64
	for trial := 0; trial < 20; trial++ {
		s := NewStore()
		if err := s.RegisterPlan(plan); err != nil {
			t.Fatal(err)
		}
		perm := rand.New(rand.NewSource(int64(trial))).Perm(len(evid))
		for _, i := range perm {
			mustAppend(t, s, evid[i])
			mustAppend(t, s, evid[i]) // 重复追加
		}
		res, err := s.Query("emp6", deadline)
		if err != nil {
			t.Fatal(err)
		}
		if trial == 0 {
			wantStatus, wantGap = res.Status, res.TotalGapMillis
		} else if res.Status != wantStatus || res.TotalGapMillis != wantGap {
			t.Fatalf("第 %d 次试验结果不一致：状态 %v/%v 缺口 %d/%d",
				trial, res.Status, wantStatus, res.TotalGapMillis, wantGap)
		}
		checkAttribution(t, plan, evid, res, deadline)
	}
}

// 重叠与嵌套证据不重复计时。
func TestOverlappingEvidenceNotDoubleCounted(t *testing.T) {
	s := NewStore()
	plan := Plan{
		EmployeeID:     "emp7",
		DeadlineMillis: 10 * minute,
		Modules:        []ModuleSpec{spec("A", 2*minute)},
	}
	if err := s.RegisterPlan(plan); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, Evidence{"emp7", "A", 0, minute})
	mustAppend(t, s, Evidence{"emp7", "A", 30 * second, 90 * second})
	mustAppend(t, s, Evidence{"emp7", "A", 45 * second, 60 * second}) // 嵌套
	res, err := s.Query("emp7", 10*minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalGapMillis != 30*second {
		t.Fatalf("并集为 90 秒，缺口应为 30 秒，实际 %d", res.TotalGapMillis)
	}
	checkAttribution(t, plan, planEvidence("emp7", s), res, 10*minute)
}

func mustAppend(t *testing.T, s *Store, e Evidence) {
	t.Helper()
	if err := s.AppendEvidence(e); err != nil {
		t.Fatal(err)
	}
}

func planEvidence(employeeID string, s *Store) []Evidence {
	return s.plans[employeeID].evidence
}
