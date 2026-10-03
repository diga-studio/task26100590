// Package compliance 提供培训学时合规判定能力。
// 计划与学习证据均保存在进程内存中，由上层顺序调用。
package compliance

import "errors"

// Evidence 表示一条学习证据：模块在 [StartMs, EndMs) 内有有效学习活动。
// 时间统一为整数毫秒，区间包含开始、不包含结束。
type Evidence struct {
	ModuleID string
	StartMs  int64
	EndMs    int64
}

// ModulePlan 描述一个必修模块的要求。
type ModulePlan struct {
	ModuleID   string
	RequiredMs int64
	// QuizPassedAtMs 为该模块最早通过测验的时刻；nil 表示尚未通过。
	QuizPassedAtMs *int64
}

// Plan 为一名员工的培训计划。
type Plan struct {
	EmployeeID string
	DeadlineMs int64
	Modules    []ModulePlan
}

// Interval 表示一个半开区间 [StartMs, EndMs)。
type Interval struct {
	StartMs int64
	EndMs   int64
}

// ModuleResult 为查询时单个模块的计入结果。
type ModuleResult struct {
	ModuleID   string
	RequiredMs int64
	// CreditedMs 为实际计入的学时（不超过 RequiredMs）。
	CreditedMs int64
	// Intervals 为计入学时对应的证据区间（互不重叠、升序）。
	Intervals []Interval
	// QuizPassed 表示测验在有效时刻前已通过。
	QuizPassed bool
}

// Status 为合规状态。
type Status int

// QueryResult 为一次合规查询的结果。
type QueryResult struct {
	Status Status
	// DeficitMs 为最小总学时缺口：全部要求学时减去最多能合法计入的总学时。
	DeficitMs int64
	Modules   []ModuleResult
}

const (
	// StatusCompliant 全部模块学时满足且测验有效。
	StatusCompliant Status = iota
	// StatusNonCompliant 未满足，且查询时刻不晚于截止时刻。
	StatusNonCompliant
	// StatusOverdue 未满足，且查询时刻晚于截止时刻。
	StatusOverdue
)

// 错误定义。
var (
	ErrPlanExists      = errors.New("compliance: plan already registered for employee")
	ErrPlanNotFound    = errors.New("compliance: no plan registered for employee")
	ErrInvalidPlan     = errors.New("compliance: invalid plan")
	ErrInvalidEvidence = errors.New("compliance: invalid evidence")
)

// Store 保存全部计划与学习证据，仅支持顺序调用。
type Store struct {
	plans    map[string]*Plan
	evidence map[string][]Evidence
}

// NewStore 创建一个空的 Store。
func NewStore() *Store {
	return &Store{
		plans:    make(map[string]*Plan),
		evidence: make(map[string][]Evidence),
	}
}
