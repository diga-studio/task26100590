// Package compliance 提供培训学时合规判定能力。
// 计划与学习证据均保存在进程内存中，由上层顺序调用。
package compliance

import "errors"

// Status 表示查询时刻的合规状态。
type Status int

const (
	// StatusCompliant 表示全部模块学时满足且测验有效。
	StatusCompliant Status = iota
	// StatusNotCompliant 表示未达标，且查询时刻不晚于截止时刻。
	StatusNotCompliant
	// StatusOverdue 表示未达标，且查询时刻晚于截止时刻。
	StatusOverdue
)

// 业务约束：每名员工最多 16 个必修模块。
const maxModulesPerPlan = 16

var (
	// ErrPlanExists 表示该员工已登记过计划。
	ErrPlanExists = errors.New("compliance: plan already registered for employee")
	// ErrPlanNotFound 表示查询或追加证据的员工尚未登记计划。
	ErrPlanNotFound = errors.New("compliance: plan not found for employee")
	// ErrTooManyModules 表示计划的必修模块数超过上限。
	ErrTooManyModules = errors.New("compliance: too many modules in plan")
	// ErrDuplicateModule 表示计划中存在重复的模块标识。
	ErrDuplicateModule = errors.New("compliance: duplicate module id in plan")
	// ErrInvalidModule 表示模块要求学时为负。
	ErrInvalidModule = errors.New("compliance: negative required millis in plan")
)

// Interval 表示一个半开区间 [StartMillis, EndMillis)，单位为毫秒。
type Interval struct {
	StartMillis int64
	EndMillis   int64
}

// ModuleSpec 描述一个必修模块的要求。
type ModuleSpec struct {
	ModuleID       string
	RequiredMillis int64
	// HasQuizPass 为 false 时表示该模块尚无通过记录，视为未通过。
	HasQuizPass    bool
	QuizPassMillis int64
}

// Plan 针对一名员工的固定培训计划。
type Plan struct {
	EmployeeID     string
	DeadlineMillis int64
	Modules        []ModuleSpec
}

// Evidence 表示一条学习证据：某模块在 [StartMillis, EndMillis) 内有有效学习活动。
type Evidence struct {
	EmployeeID  string
	ModuleID    string
	StartMillis int64
	EndMillis   int64
}

// ModuleResult 表示单个模块的查询结果。
type ModuleResult struct {
	ModuleID       string
	RequiredMillis int64
	CountedMillis  int64
	QuizSatisfied  bool
	// Intervals 为实际计入该模块的归属区间，互不重叠。
	Intervals []Interval
}

// QueryResult 表示一次合规查询的结果。
type QueryResult struct {
	Status Status
	// TotalGapMillis 为最小总学时缺口，不包含测验缺口。
	TotalGapMillis int64
	Modules        []ModuleResult
}

// Store 保存全部计划与学习证据，仅供顺序调用。
type Store struct {
	plans map[string]*planState
}

type planState struct {
	plan     Plan
	evidence []Evidence
}

// NewStore 创建一个空的 Store。
func NewStore() *Store {
	return &Store{plans: make(map[string]*planState)}
}

// RegisterPlan 登记一名员工的培训计划。同一员工重复登记返回 ErrPlanExists。
func (s *Store) RegisterPlan(p Plan) error {
	if _, ok := s.plans[p.EmployeeID]; ok {
		return ErrPlanExists
	}
	if len(p.Modules) > maxModulesPerPlan {
		return ErrTooManyModules
	}
	seen := make(map[string]struct{}, len(p.Modules))
	for _, m := range p.Modules {
		if m.RequiredMillis < 0 {
			return ErrInvalidModule
		}
		if _, ok := seen[m.ModuleID]; ok {
			return ErrDuplicateModule
		}
		seen[m.ModuleID] = struct{}{}
	}
	s.plans[p.EmployeeID] = &planState{plan: p}
	return nil
}

// AppendEvidence 追加一条学习证据，允许乱序、重复、嵌套或部分重叠。
// 证据的上报顺序与次数不影响合规状态与最小总学时缺口。
func (s *Store) AppendEvidence(e Evidence) error {
	st, ok := s.plans[e.EmployeeID]
	if !ok {
		return ErrPlanNotFound
	}
	st.evidence = append(st.evidence, e)
	return nil
}

// Query 查询某员工在 queryMillis 时刻的合规结果。
// 仅使用 min(queryMillis, 截止时刻) 之前的学时与测验通过记录。
func (s *Store) Query(employeeID string, queryMillis int64) (QueryResult, error) {
	st, ok := s.plans[employeeID]
	if !ok {
		return QueryResult{}, ErrPlanNotFound
	}
	plan := st.plan
	effective := queryMillis
	if plan.DeadlineMillis < effective {
		effective = plan.DeadlineMillis
	}

	counted, intervals, totalGap := solve(plan, st.evidence, effective)

	result := QueryResult{TotalGapMillis: totalGap}
	allSatisfied := totalGap == 0
	for _, m := range plan.Modules {
		quizOK := m.HasQuizPass && m.QuizPassMillis <= effective
		if !quizOK {
			allSatisfied = false
		}
		result.Modules = append(result.Modules, ModuleResult{
			ModuleID:       m.ModuleID,
			RequiredMillis: m.RequiredMillis,
			CountedMillis:  counted[m.ModuleID],
			QuizSatisfied:  quizOK,
			Intervals:      intervals[m.ModuleID],
		})
	}

	switch {
	case allSatisfied:
		result.Status = StatusCompliant
	case queryMillis <= plan.DeadlineMillis:
		result.Status = StatusNotCompliant
	default:
		result.Status = StatusOverdue
	}
	return result, nil
}
