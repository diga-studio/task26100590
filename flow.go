package compliance

// flowEdge 描述一条有向边：from -> to，容量 cap。
type flowEdge struct {
	from, to int
	cap      int64
}

// dinic 实现 Dinic 最大流算法。
type dinic struct {
	n     int
	to    []int
	cap   []int64
	next  []int
	head  []int
	level []int
	iter  []int
}

func newDinic(n int) *dinic {
	head := make([]int, n)
	for i := range head {
		head[i] = -1
	}
	return &dinic{n: n, head: head}
}

// addEdge 添加一条边及其反向边，返回正向边在 to 切片中的下标。
func (d *dinic) addEdge(from, to int, cap int64) int {
	idx := len(d.to)
	d.to = append(d.to, to)
	d.cap = append(d.cap, cap)
	d.next = append(d.next, d.head[from])
	d.head[from] = idx
	d.to = append(d.to, from)
	d.cap = append(d.cap, 0)
	d.next = append(d.next, d.head[to])
	d.head[to] = idx + 1
	return idx
}

func (d *dinic) bfs(source, sink int) bool {
	for i := range d.level {
		d.level[i] = -1
	}
	queue := make([]int, 0, d.n)
	d.level[source] = 0
	queue = append(queue, source)
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for e := d.head[v]; e != -1; e = d.next[e] {
			if d.cap[e] > 0 && d.level[d.to[e]] < 0 {
				d.level[d.to[e]] = d.level[v] + 1
				queue = append(queue, d.to[e])
			}
		}
	}
	return d.level[sink] >= 0
}

func (d *dinic) dfs(v, sink int, f int64) int64 {
	if v == sink {
		return f
	}
	for ; d.iter[v] != -1; d.iter[v] = d.next[d.iter[v]] {
		e := d.iter[v]
		if d.cap[e] > 0 && d.level[v] < d.level[d.to[e]] {
			pushed := d.dfs(d.to[e], sink, min(f, d.cap[e]))
			if pushed > 0 {
				d.cap[e] -= pushed
				d.cap[e^1] += pushed
				return pushed
			}
		}
	}
	return 0
}

// maxFlow 在 nodeCount 个节点的网络上计算 source 到 sink 的最大流。
// 返回最大流值，以及与输入 edges 一一对应的各边流量。
func maxFlow(nodeCount int, edges []flowEdge, source, sink int) (int64, []int64) {
	d := newDinic(nodeCount)
	d.level = make([]int, nodeCount)
	d.iter = make([]int, nodeCount)
	edgeIdx := make([]int, len(edges))
	for i, e := range edges {
		edgeIdx[i] = d.addEdge(e.from, e.to, e.cap)
	}
	var flow int64
	const inf = int64(1) << 62
	for d.bfs(source, sink) {
		copy(d.iter, d.head)
		for {
			pushed := d.dfs(source, sink, inf)
			if pushed == 0 {
				break
			}
			flow += pushed
		}
	}
	flows := make([]int64, len(edges))
	for i, idx := range edgeIdx {
		flows[i] = d.cap[idx^1]
	}
	return flow, flows
}
