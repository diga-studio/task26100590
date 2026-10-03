package compliance

import "sort"

// computeResult 对一名员工的计划与证据在 nowMs 时刻求合规结果。
// 核心模型：把时间轴切成基本段，每段内覆盖它的模块集合恒定；
// 在 源→模块(容量=要求学时)→段(容量=段长)→汇 的流网络上求最大流，
// 最大可计入学时即最大流，缺口 = 总要求 - 最大流。
func computeResult(plan *Plan, evs []Evidence, nowMs int64) QueryResult {
	effective := plan.DeadlineMs
	if nowMs < effective {
		effective = nowMs
	}

	moduleIndex := make(map[string]int, len(plan.Modules))
	for i, m := range plan.Modules {
		moduleIndex[m.ModuleID] = i
	}

	// 每个模块的证据先各自求并集（去重、去重叠、截断到有效时刻之前）。
	unions := make([][]Interval, len(plan.Modules))
	for _, ev := range evs {
		idx, ok := moduleIndex[ev.ModuleID]
		if !ok {
			continue
		}
		start, end := ev.StartMs, ev.EndMs
		if end > effective {
			end = effective
		}
		if end <= start {
			continue
		}
		unions[idx] = append(unions[idx], Interval{StartMs: start, EndMs: end})
	}
	for i := range unions {
		unions[i] = mergeIntervals(unions[i])
	}

	// 扫描线：收集所有断点，计算每个基本段被哪些模块覆盖（位掩码）。
	breakpoints := make([]int64, 0, 2*len(evs))
	for _, ivs := range unions {
		for _, iv := range ivs {
			breakpoints = append(breakpoints, iv.StartMs, iv.EndMs)
		}
	}
	sort.Slice(breakpoints, func(i, j int) bool { return breakpoints[i] < breakpoints[j] })
	breakpoints = uniqueInt64(breakpoints)

	numSegments := len(breakpoints) - 1
	if numSegments < 0 {
		numSegments = 0
	}
	addMask := make([]uint16, len(breakpoints))
	removeMask := make([]uint16, len(breakpoints))
	bpIndex := make(map[int64]int, len(breakpoints))
	for i, bp := range breakpoints {
		bpIndex[bp] = i
	}
	for m, ivs := range unions {
		bit := uint16(1) << uint(m)
		for _, iv := range ivs {
			addMask[bpIndex[iv.StartMs]] |= bit
			removeMask[bpIndex[iv.EndMs]] |= bit
		}
	}

	type segment struct {
		start, end int64
		mask       uint16
	}
	segments := make([]segment, 0, numSegments)
	var active uint16
	for i := 0; i < numSegments; i++ {
		active = (active | addMask[i]) &^ removeMask[i]
		if active != 0 {
			segments = append(segments, segment{start: breakpoints[i], end: breakpoints[i+1], mask: active})
		}
	}

	// 建流网络：0=源，1..M=模块，M+1..M+S=段，M+S+1=汇。
	m := len(plan.Modules)
	n := m + len(segments) + 2
	source, sink := 0, n-1
	d := newDinic(n)
	var totalRequired int64
	for i, mp := range plan.Modules {
		d.addEdge(source, 1+i, mp.RequiredMs)
		totalRequired += mp.RequiredMs
	}
	// segEdges[si] 保存该段上的 (边编号, 模块下标)，用于回流后提取归属区间。
	type edgeRef struct {
		edgeID int
		module int
	}
	segEdges := make([][]edgeRef, len(segments))
	for si, seg := range segments {
		segNode := 1 + m + si
		length := seg.end - seg.start
		for mm := 0; mm < m; mm++ {
			if seg.mask&(uint16(1)<<uint(mm)) != 0 {
				id := d.addEdge(1+mm, segNode, length)
				segEdges[si] = append(segEdges[si], edgeRef{edgeID: id, module: mm})
			}
		}
		d.addEdge(segNode, sink, length)
	}

	maxCredited := d.maxFlow(source, sink)

	// 提取每个模块的归属区间：段内按模块顺序依次切分（各模块流量之和
	// 不超过段长，由段到汇的容量保证），再合并相邻段。
	results := make([]ModuleResult, m)
	moduleIvs := make([][]Interval, m)
	for si, seg := range segments {
		var offset int64
		for _, ref := range segEdges[si] {
			f := d.flowOn(ref.edgeID)
			if f > 0 {
				moduleIvs[ref.module] = append(moduleIvs[ref.module],
					Interval{StartMs: seg.start + offset, EndMs: seg.start + offset + f})
				offset += f
			}
		}
	}
	for mm := 0; mm < m; mm++ {
		ivs := mergeIntervals(moduleIvs[mm])
		var credited int64
		for _, iv := range ivs {
			credited += iv.EndMs - iv.StartMs
		}
		mp := plan.Modules[mm]
		quizPassed := mp.QuizPassedAtMs != nil && *mp.QuizPassedAtMs <= effective
		results[mm] = ModuleResult{
			ModuleID:   mp.ModuleID,
			RequiredMs: mp.RequiredMs,
			CreditedMs: credited,
			Intervals:  ivs,
			QuizPassed: quizPassed,
		}
	}

	deficit := totalRequired - maxCredited
	allQuizPassed := true
	for _, r := range results {
		if !r.QuizPassed {
			allQuizPassed = false
			break
		}
	}

	res := QueryResult{DeficitMs: deficit, Modules: results}
	switch {
	case deficit == 0 && allQuizPassed:
		res.Status = StatusCompliant
	case nowMs > plan.DeadlineMs:
		res.Status = StatusOverdue
	default:
		res.Status = StatusNonCompliant
	}
	return res
}

// mergeIntervals 对区间排序并合并重叠或相邻的区间。
func mergeIntervals(ivs []Interval) []Interval {
	if len(ivs) == 0 {
		return nil
	}
	sort.Slice(ivs, func(i, j int) bool {
		if ivs[i].StartMs != ivs[j].StartMs {
			return ivs[i].StartMs < ivs[j].StartMs
		}
		return ivs[i].EndMs < ivs[j].EndMs
	})
	merged := make([]Interval, 0, len(ivs))
	cur := ivs[0]
	for _, iv := range ivs[1:] {
		if iv.StartMs <= cur.EndMs {
			if iv.EndMs > cur.EndMs {
				cur.EndMs = iv.EndMs
			}
		} else {
			merged = append(merged, cur)
			cur = iv
		}
	}
	return append(merged, cur)
}

func uniqueInt64(xs []int64) []int64 {
	if len(xs) == 0 {
		return xs
	}
	out := xs[:1]
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
