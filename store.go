package compliance

// RegisterPlan 登记一名员工的培训计划。同一员工重复登记返回 ErrPlanExists。
func (s *Store) RegisterPlan(p Plan) error {
	if p.EmployeeID == "" || len(p.Modules) == 0 || len(p.Modules) > 16 {
		return ErrInvalidPlan
	}
	seen := make(map[string]bool, len(p.Modules))
	for _, m := range p.Modules {
		if m.ModuleID == "" || m.RequiredMs < 0 || seen[m.ModuleID] {
			return ErrInvalidPlan
		}
		seen[m.ModuleID] = true
	}
	if _, ok := s.plans[p.EmployeeID]; ok {
		return ErrPlanExists
	}
	cp := p
	cp.Modules = append([]ModulePlan(nil), p.Modules...)
	s.plans[p.EmployeeID] = &cp
	return nil
}

// AppendEvidence 为一名员工追加一条学习证据。证据可乱序、可重复上报。
// 要求该员工已登记计划，且模块属于其计划。
func (s *Store) AppendEvidence(employeeID string, ev Evidence) error {
	if ev.EndMs <= ev.StartMs || ev.ModuleID == "" {
		return ErrInvalidEvidence
	}
	plan, ok := s.plans[employeeID]
	if !ok {
		return ErrPlanNotFound
	}
	found := false
	for _, m := range plan.Modules {
		if m.ModuleID == ev.ModuleID {
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidEvidence
	}
	s.evidence[employeeID] = append(s.evidence[employeeID], ev)
	return nil
}

// Query 查询一名员工在 nowMs 时刻的合规结果。
// 仅使用 min(nowMs, 截止时刻) 之前的学时与测验通过记录。
func (s *Store) Query(employeeID string, nowMs int64) (QueryResult, error) {
	plan, ok := s.plans[employeeID]
	if !ok {
		return QueryResult{}, ErrPlanNotFound
	}
	return computeResult(plan, s.evidence[employeeID], nowMs), nil
}
