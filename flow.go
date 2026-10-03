package compliance

// dinic 实现 Dinic 最大流算法，容量与流量均为 int64。
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

// addEdge 添加一条容量为 c 的有向边，返回正向边编号（偶数）。
func (d *dinic) addEdge(from, to int, c int64) int {
	id := len(d.to)
	d.to = append(d.to, to)
	d.cap = append(d.cap, c)
	d.next = append(d.next, d.head[from])
	d.head[from] = id
	d.to = append(d.to, from)
	d.cap = append(d.cap, 0)
	d.next = append(d.next, d.head[to])
	d.head[to] = id + 1
	return id
}

// flowOn 返回 addEdge 返回编号对应边上的当前流量。
func (d *dinic) flowOn(id int) int64 {
	return d.cap[id^1]
}

func (d *dinic) bfs(s, t int) bool {
	for i := range d.level {
		d.level[i] = -1
	}
	if cap(d.level) < d.n {
		d.level = make([]int, d.n)
		for i := range d.level {
			d.level[i] = -1
		}
	}
	queue := make([]int, 0, d.n)
	d.level[s] = 0
	queue = append(queue, s)
	for head := 0; head < len(queue); head++ {
		v := queue[head]
		for e := d.head[v]; e != -1; e = d.next[e] {
			if d.cap[e] > 0 && d.level[d.to[e]] < 0 {
				d.level[d.to[e]] = d.level[v] + 1
				queue = append(queue, d.to[e])
			}
		}
	}
	return d.level[t] >= 0
}

func (d *dinic) dfs(v, t int, f int64) int64 {
	if v == t {
		return f
	}
	for ; d.iter[v] != -1; d.iter[v] = d.next[d.iter[v]] {
		e := d.iter[v]
		u := d.to[e]
		if d.cap[e] > 0 && d.level[u] == d.level[v]+1 {
			pushed := d.dfs(u, t, min64(f, d.cap[e]))
			if pushed > 0 {
				d.cap[e] -= pushed
				d.cap[e^1] += pushed
				return pushed
			}
		}
	}
	return 0
}

// maxFlow 计算 s 到 t 的最大流。
func (d *dinic) maxFlow(s, t int) int64 {
	d.level = make([]int, d.n)
	var flow int64
	for d.bfs(s, t) {
		if cap(d.iter) < d.n {
			d.iter = make([]int, d.n)
		}
		copy(d.iter, d.head)
		for {
			pushed := d.dfs(s, t, int64(^uint64(0)>>1))
			if pushed == 0 {
				break
			}
			flow += pushed
		}
	}
	return flow
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
