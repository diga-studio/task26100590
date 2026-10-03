package compliance

import "sort"

// mergeIntervals 将同一模块的证据区间裁剪到 effectiveMillis 之前并求并集。
// 输入允许乱序、重复、嵌套或部分重叠。
func mergeIntervals(evid []Evidence, effectiveMillis int64) []Interval {
	clipped := make([]Interval, 0, len(evid))
	for _, e := range evid {
		start := e.StartMillis
		end := e.EndMillis
		if end > effectiveMillis {
			end = effectiveMillis
		}
		if start < end {
			clipped = append(clipped, Interval{StartMillis: start, EndMillis: end})
		}
	}
	if len(clipped) == 0 {
		return nil
	}
	sort.Slice(clipped, func(i, j int) bool {
		if clipped[i].StartMillis != clipped[j].StartMillis {
			return clipped[i].StartMillis < clipped[j].StartMillis
		}
		return clipped[i].EndMillis < clipped[j].EndMillis
	})
	merged := make([]Interval, 0, len(clipped))
	cur := clipped[0]
	for _, iv := range clipped[1:] {
		if iv.StartMillis <= cur.EndMillis {
			if iv.EndMillis > cur.EndMillis {
				cur.EndMillis = iv.EndMillis
			}
			continue
		}
		merged = append(merged, cur)
		cur = iv
	}
	merged = append(merged, cur)
	return merged
}

// solve 在 effectiveMillis 之前计算最大学时归属。
// 返回各模块计入毫秒数、各模块归属区间，以及最小总学时缺口。
// 归属模型：源 -> 基本区间（容量=区间长度）-> 覆盖该区间且有证据的模块 -> 汇（容量=要求学时）。
func solve(plan Plan, evidence []Evidence, effectiveMillis int64) (map[string]int64, map[string][]Interval, int64) {
	moduleCount := len(plan.Modules)
	byModule := make(map[string][]Evidence, moduleCount)
	for _, e := range evidence {
		byModule[e.ModuleID] = append(byModule[e.ModuleID], e)
	}

	unions := make([][]Interval, moduleCount)
	boundarySet := make(map[int64]struct{})
	for i, m := range plan.Modules {
		unions[i] = mergeIntervals(byModule[m.ModuleID], effectiveMillis)
		for _, iv := range unions[i] {
			boundarySet[iv.StartMillis] = struct{}{}
			boundarySet[iv.EndMillis] = struct{}{}
		}
	}
	boundaries := make([]int64, 0, len(boundarySet))
	for b := range boundarySet {
		boundaries = append(boundaries, b)
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })

	counted := make(map[string]int64, moduleCount)
	intervals := make(map[string][]Interval, moduleCount)
	var totalRequired int64
	for _, m := range plan.Modules {
		counted[m.ModuleID] = 0
		intervals[m.ModuleID] = nil
		totalRequired += m.RequiredMillis
	}
	if len(boundaries) < 2 {
		return counted, intervals, totalRequired
	}

	segCount := len(boundaries) - 1
	source := 0
	segBase := 1
	modBase := segBase + segCount
	sink := modBase + moduleCount

	edges := make([]flowEdge, 0, segCount*(moduleCount+1)+moduleCount)
	// 源 -> 基本区间
	for s := 0; s < segCount; s++ {
		edges = append(edges, flowEdge{from: source, to: segBase + s, cap: boundaries[s+1] - boundaries[s]})
	}
	// 基本区间 -> 覆盖它的模块
	type segModEdge struct {
		seg, mod int
	}
	segModEdges := make([]segModEdge, 0, segCount*moduleCount)
	boundIndex := make(map[int64]int, len(boundaries))
	for i, b := range boundaries {
		boundIndex[b] = i
	}
	for mi := 0; mi < moduleCount; mi++ {
		for _, iv := range unions[mi] {
			lo := boundIndex[iv.StartMillis]
			hi := boundIndex[iv.EndMillis]
			for s := lo; s < hi; s++ {
				segModEdges = append(segModEdges, segModEdge{seg: s, mod: mi})
				edges = append(edges, flowEdge{from: segBase + s, to: modBase + mi, cap: boundaries[s+1] - boundaries[s]})
			}
		}
	}
	// 模块 -> 汇
	modEdgeStart := len(edges)
	for mi := 0; mi < moduleCount; mi++ {
		edges = append(edges, flowEdge{from: modBase + mi, to: sink, cap: plan.Modules[mi].RequiredMillis})
	}

	flow, flows := maxFlow(sink+1, edges, source, sink)

	// 还原归属：每个基本区间按边顺序依次切分，互不重叠。
	segModFlowBase := segCount
	segAssigned := make([]int64, segCount)
	modSegs := make([][]Interval, moduleCount)
	for k, sme := range segModEdges {
		f := flows[segModFlowBase+k]
		if f <= 0 {
			continue
		}
		start := boundaries[sme.seg] + segAssigned[sme.seg]
		segAssigned[sme.seg] += f
		modSegs[sme.mod] = append(modSegs[sme.mod], Interval{StartMillis: start, EndMillis: start + f})
	}
	for mi := 0; mi < moduleCount; mi++ {
		id := plan.Modules[mi].ModuleID
		counted[id] = flows[modEdgeStart+mi]
		intervals[id] = joinAdjacent(modSegs[mi])
	}
	return counted, intervals, totalRequired - flow
}

// joinAdjacent 合并首尾相接的相邻区间，使输出更紧凑。
func joinAdjacent(ivs []Interval) []Interval {
	if len(ivs) == 0 {
		return nil
	}
	out := make([]Interval, 0, len(ivs))
	cur := ivs[0]
	for _, iv := range ivs[1:] {
		if iv.StartMillis == cur.EndMillis {
			cur.EndMillis = iv.EndMillis
			continue
		}
		out = append(out, cur)
		cur = iv
	}
	out = append(out, cur)
	return out
}
